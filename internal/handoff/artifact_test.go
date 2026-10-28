package handoff

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	artifactSource = "tab-source_123"
	artifactTarget = "tab-target_456"
)

func writeTranscript(t *testing.T, root string, data []byte) string {
	t.Helper()
	path := filepath.Join(root, "source.jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestArtifactStore(t *testing.T, root string) *ArtifactStore {
	t.Helper()
	store, err := NewArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func sourceMessage(t *testing.T, role, text string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": role, "message": map[string]any{"role": role, "content": text}})
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func TestArtifactSnapshotKeepsEveryCompleteRecordAndDropsOnlyTrailingPartial(t *testing.T) {
	root := t.TempDir()
	middle := string(bytes.Repeat([]byte("m"), 32*1024))
	sourceData := append(sourceMessage(t, "user", "early"), sourceMessage(t, "assistant", middle)...)
	sourceData = append(sourceData, sourceMessage(t, "assistant", "final")...)
	source := writeTranscript(t, root, append(sourceData, []byte(`{"type":"assistant","message":{"role":"assistant","content":"still-writing"}`)...))
	store := newTestArtifactStore(t, root)

	artifact, err := store.Snapshot(artifactSource, source, artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ID == "" || artifact.Bytes <= 0 {
		t.Fatalf("artifact = %+v", artifact)
	}
	path, size, err := store.Claim(artifactSource, artifact.ID, artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(got)) || artifact.Bytes != size || !bytes.Contains(got, []byte("early")) ||
		!bytes.Contains(got, []byte(middle)) || !bytes.Contains(got, []byte("final")) || bytes.Contains(got, []byte("still-writing")) {
		t.Fatalf("claimed %d bytes; content length %d", size, len(got))
	}
}

func TestArtifactSnapshotStreamsLargeSourceAndUsesPrivateAtomicFiles(t *testing.T) {
	root := t.TempDir()
	record := sourceMessage(t, "assistant", string(bytes.Repeat([]byte("x"), 64*1024)))
	want := bytes.Repeat(record, 40)
	store := newTestArtifactStore(t, root)
	artifact, err := store.Snapshot(artifactSource, writeTranscript(t, root, want), artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	path, _, err := store.Claim(artifactSource, artifact.ID, artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "v2", "handoffs"), filepath.Dir(path), path} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		wantMode := os.FileMode(0o700)
		if !info.IsDir() {
			wantMode = 0o600
		}
		if got := info.Mode().Perm(); got != wantMode {
			t.Errorf("%s mode = %o, want %o", path, got, wantMode)
		}
	}
	err = filepath.Walk(filepath.Join(root, "v2", "handoffs"), func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.Contains(info.Name(), ".tmp") {
			t.Errorf("temporary artifact left behind: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestArtifactSnapshotRequiresACompleteRecord(t *testing.T) {
	root := t.TempDir()
	store := newTestArtifactStore(t, root)
	_, err := store.Snapshot(artifactSource, writeTranscript(t, root, []byte(`{"partial":true}`)), artifactTarget)
	if !errors.Is(err, ErrNoCompleteHistory) {
		t.Fatalf("Snapshot error = %v, want ErrNoCompleteHistory", err)
	}
}

func TestArtifactClaimIsBoundAndSingleUse(t *testing.T) {
	root := t.TempDir()
	store := newTestArtifactStore(t, root)
	artifact, err := store.Snapshot(artifactSource, writeTranscript(t, root, sourceMessage(t, "user", "one")), artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	bad := []struct{ source, id, target string }{
		{"tab-other_123", artifact.ID, artifactTarget},
		{artifactSource, artifact.ID, "tab-other_456"},
		{artifactSource, "../" + artifact.ID, artifactTarget},
		{artifactSource, "/tmp/claimed", artifactTarget},
	}
	for _, c := range bad {
		if _, _, err := store.Claim(c.source, c.id, c.target); err == nil {
			t.Errorf("Claim(%q, %q, %q) succeeded", c.source, c.id, c.target)
		}
	}
	if _, _, err := store.Claim(artifactSource, artifact.ID, artifactTarget); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Claim(artifactSource, artifact.ID, artifactTarget); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("second claim error = %v, want ErrArtifactNotFound", err)
	}
}

func TestArtifactStoreRemovesStaleDraftsButKeepsClaimed(t *testing.T) {
	root := t.TempDir()
	store := newTestArtifactStore(t, root)
	first, err := store.Snapshot(artifactSource, writeTranscript(t, root, sourceMessage(t, "user", "one")), artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	claimed, _, err := store.Claim(artifactSource, first.ID, artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Snapshot(artifactSource, writeTranscript(t, root, sourceMessage(t, "user", "two")), artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	draft := store.draftPath(artifactSource, second.ID, artifactTarget)
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(draft, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := NewArtifactStore(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(draft); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale draft still exists: %v", err)
	}
	if _, err := os.Stat(claimed); err != nil {
		t.Fatalf("claimed artifact was removed: %v", err)
	}
}

func TestArtifactStoreRestartRemovesStaleClaimedArtifacts(t *testing.T) {
	root := t.TempDir()
	store := newTestArtifactStore(t, root)
	artifact, err := store.Snapshot(artifactSource, writeTranscript(t, root, sourceMessage(t, "user", "one")), artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	claimed, _, err := store.Claim(artifactSource, artifact.ID, artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(claimed, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := NewArtifactStore(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(claimed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale claimed artifact still exists: %v", err)
	}
}

func TestArtifactReleaseIsBoundToTheTargetConversation(t *testing.T) {
	root := t.TempDir()
	store := newTestArtifactStore(t, root)
	artifact, err := store.Snapshot(artifactSource, writeTranscript(t, root, sourceMessage(t, "user", "one")), artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	claimed, _, err := store.Claim(artifactSource, artifact.ID, artifactTarget)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Release("tab-other_456", claimed); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("release for another target = %v, want ErrArtifactNotFound", err)
	}
	if _, err := os.Stat(claimed); err != nil {
		t.Fatalf("another target removed the artifact: %v", err)
	}
	if err := store.Release(artifactTarget, claimed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(claimed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("released artifact still exists: %v", err)
	}
	if err := store.Release(artifactTarget, claimed); err != nil {
		t.Fatalf("release is not idempotent: %v", err)
	}
}

func TestArtifactSnapshotCanonicalizesClaudeAndCodexWithoutSourceIdentity(t *testing.T) {
	tests := map[string]string{
		"claude": strings.Join([]string{
			`{"type":"user","sessionId":"source-session","cwd":"/source/vm","provider":"claude","message":{"role":"user","content":"Fix the parser"}}`,
			`{"type":"assistant","sessionId":"source-session","message":{"role":"assistant","model":"claude-secret","content":[{"type":"text","text":"I will inspect it."},{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"go test ./...","model":"sonnet"}}]}}`,
			`{"type":"user","sessionId":"source-session","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok","is_error":false}]}}`,
		}, "\n") + "\n",
		"codex": strings.Join([]string{
			`{"type":"session_meta","payload":{"id":"source-session","cwd":"/source/vm","cli_version":"secret"}}`,
			`{"type":"turn_context","payload":{"cwd":"/source/vm","model":"gpt-secret","model_provider":"source-provider"}}`,
			`{"type":"event_msg","payload":{"type":"user_message","message":"Fix the parser"}}`,
			`{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":\"go test ./...\",\"provider\":\"source-provider\"}","call_id":"call-1"}}`,
			`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"ok"}}`,
			`{"type":"event_msg","payload":{"type":"agent_message","message":"Tests pass."}}`,
		}, "\n") + "\n",
	}
	for name, transcript := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store := newTestArtifactStore(t, root)
			artifact, err := store.Snapshot(artifactSource, writeTranscript(t, root, []byte(transcript)), artifactTarget)
			if err != nil {
				t.Fatal(err)
			}
			path, _, err := store.Claim(artifactSource, artifact.ID, artifactTarget)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got := string(raw)
			for _, secret := range []string{"source-session", "/source/vm", "claude-secret", "gpt-secret", "sonnet", "source-provider",
				`"sessionId"`, `"session_meta"`, `"turn_context"`, `"response_item"`, `"event_msg"`, `"model"`, `"provider"`, `"cwd"`} {
				if strings.Contains(got, secret) {
					t.Fatalf("canonical history disclosed %q:\n%s", secret, got)
				}
			}
			for _, semantic := range []string{"Fix the parser", "go test ./...", "ok"} {
				if !strings.Contains(got, semantic) {
					t.Fatalf("canonical history lost %q:\n%s", semantic, got)
				}
			}
			if name == "claude" && (!strings.Contains(got, "I will inspect it.") || !strings.Contains(got, `"kind":"tool"`)) {
				t.Fatalf("Claude semantics were not canonicalized:\n%s", got)
			}
			if name == "codex" && (!strings.Contains(got, "Tests pass.") || !strings.Contains(got, `"kind":"tool_result"`)) {
				t.Fatalf("Codex semantics were not canonicalized:\n%s", got)
			}
		})
	}
}
