package agent

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJWTRoundTrip(t *testing.T) {
	secret := []byte(strings.Repeat("s", 40))
	token := SignHS256(secret, "runtime-1", "ingest", time.Minute)
	if err := VerifyHS256(secret, token, "ingest", time.Now()); err != nil {
		t.Fatalf("own token rejected: %v", err)
	}
	if VerifyHS256(secret, token, "agent", time.Now()) == nil {
		t.Fatal("a token for the other direction was accepted")
	}
	if VerifyHS256([]byte(strings.Repeat("x", 40)), token, "ingest", time.Now()) == nil {
		t.Fatal("a token signed with another secret was accepted")
	}
	if VerifyHS256(secret, token, "ingest", time.Now().Add(2*time.Minute)) == nil {
		t.Fatal("an expired token was accepted")
	}
	if VerifyHS256(secret, SignHS256(secret, "machine-agent", "", time.Minute), "agent", time.Now()) == nil {
		t.Fatal("a token without an audience was accepted")
	}
	parts := strings.Split(token, ".")
	none := b64.EncodeToString([]byte(`{"alg":"none"}`)) + "." + parts[1] + "."
	if VerifyHS256(secret, none, "ingest", time.Now()) == nil {
		t.Fatal("alg none was accepted")
	}
}

// A token signed by the platform's JavaScript side (jose, HS256), checked here: the format is the same.
func TestJWTAudienceArray(t *testing.T) {
	secret := []byte(strings.Repeat("k", 32))
	payload := b64.EncodeToString([]byte(`{"aud":["other","agent"],"iat":1,"exp":99999999999}`))
	header := b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	token := header + "." + payload + "." + sign(secret, header+"."+payload)
	if err := VerifyHS256(secret, token, "agent", time.Now()); err != nil {
		t.Fatalf("audience array rejected: %v", err)
	}
}

func sign(secret []byte, s string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(s))
	return b64.EncodeToString(mac.Sum(nil))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListTranscripts(t *testing.T) {
	home := t.TempDir()
	r := TranscriptRoots(home)
	writeFile(t, filepath.Join(r.Claude, "p1", "s1.jsonl"), "{}\n")
	writeFile(t, filepath.Join(r.Claude, "p1", "s1", "subagents", "a.jsonl"), "{}\n")
	writeFile(t, filepath.Join(r.Claude, "p1", "notes.txt"), "x")
	writeFile(t, filepath.Join(r.Claude, "p1", "s1", "tool-results", "deep.jsonl"), "{}\n")
	writeFile(t, filepath.Join(r.Codex, "2026", "10", "17", "c.jsonl"), "{}\n")
	writeFile(t, filepath.Join(r.Codex, "misc", "10", "17", "skip.jsonl"), "{}\n")
	files, err := ListTranscripts(r)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[strings.TrimPrefix(f.Path, home)] = f.Provider
		if f.Inode == 0 || f.MtimeMs == 0 || f.Size != 3 {
			t.Errorf("%s: incomplete stat %+v", f.Path, f)
		}
	}
	want := map[string]string{
		"/.claude/projects/p1/s1.jsonl":             "claude",
		"/.claude/projects/p1/s1/subagents/a.jsonl": "claude",
		"/.codex/sessions/2026/10/17/c.jsonl":       "codex",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
	empty, err := ListTranscripts(TranscriptRoots(t.TempDir()))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("a home without CLIs should list [] (got %v, %v)", empty, err)
	}
}

func TestIsTranscriptPath(t *testing.T) {
	r := TranscriptRoots("/home/u")
	for path, ok := range map[string]bool{
		"/home/u/.claude/projects/p/s.jsonl":       true,
		"/home/u/.codex/sessions/2026/1/1/x.jsonl": true,
		"/home/u/.claude/projects/../x.jsonl":      false,
		"/home/u/.claude/projects/p/s.json":        false,
		"/etc/passwd":                              false,
		"relative/s.jsonl":                         false,
		"/home/u/.claude/projects.jsonl":           false,
	} {
		if r.IsTranscriptPath(path) != ok {
			t.Errorf("%s: want %v", path, ok)
		}
	}
}

func TestReadLines(t *testing.T) {
	file := filepath.Join(t.TempDir(), "s.jsonl")
	writeFile(t, file, "aaa\nbbbb\ncc")
	decode := func(c ReadChunk) string { b, _ := base64.StdEncoding.DecodeString(c.Data); return string(b) }

	c, err := ReadLines(file, 0, 1<<20)
	if err != nil || decode(c) != "aaa\nbbbb\n" || c.Next != 9 || c.Size != 11 || c.LineTooLong {
		t.Fatalf("whole read: %+v %v", c, err)
	}
	if c, _ = ReadLines(file, 9, 1<<20); c.Data != "" || c.Next != 9 || c.LineTooLong {
		t.Fatalf("unfinished line must wait: %+v", c)
	}
	if c, _ = ReadLines(file, 4, 3); c.Data != "" || !c.LineTooLong || c.Next != 4 {
		t.Fatalf("a line longer than the window: %+v", c)
	}
	if c, _ = ReadLines(file, 4, 5); decode(c) != "bbbb\n" || c.Next != 9 {
		t.Fatalf("exact window: %+v", c)
	}
	if c, _ = ReadLines(file, 50, 10); c.Next != 50 || c.Data != "" {
		t.Fatalf("past the end: %+v", c)
	}
	if c, _ = ReadLines(file, 9, 10); c.LineTooLong {
		t.Fatalf("an unfinished line shorter than the window is not too long: %+v", c)
	}
	// As the protocol has always answered: an unfinished last line that exactly fills the window counts as too long.
	if c, _ = ReadLines(file, 9, 2); !c.LineTooLong {
		t.Fatalf("protocol v1 marks a full window without a newline: %+v", c)
	}
}

func TestFrames(t *testing.T) {
	id := strings.Repeat("0f", 16)
	frame := EncodeFrame(FrameData, id, []byte("hello"))
	if len(frame) != 1+16+5 || frame[0] != FrameData {
		t.Fatalf("frame layout: %x", frame)
	}
	kind, gotID, payload, ok := DecodeFrame(frame)
	if !ok || kind != FrameData || gotID != id || string(payload) != "hello" {
		t.Fatalf("decode: %v %s %q %v", kind, gotID, payload, ok)
	}
	if _, _, _, ok := DecodeFrame([]byte{3, 1, 2}); ok {
		t.Fatal("short frame decoded")
	}
	if _, _, _, ok := DecodeFrame(append([]byte{9}, make([]byte, 16)...)); ok {
		t.Fatal("unknown kind decoded")
	}
	closeFrame := EncodeFrame(FrameClose, id, nil)
	if _, _, p, ok := DecodeFrame(closeFrame); !ok || len(p) != 0 {
		t.Fatal("close frame")
	}
}

func TestStatePut(t *testing.T) {
	dir := t.TempDir()
	notified := make(chan string, 16)
	s := newStateFiles(dir, nil, func(any) {}, func(name string) { notified <- name })
	layout := "workspaces/w1/layout.json"
	put := func(content string, version float64, dirty string, renumber bool) putResult {
		t.Helper()
		v := version
		res, err := s.Put(layout, content, &v, dirty, renumber)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if _, err := s.Put("../evil.json", "{}", nil, "", false); err == nil {
		t.Fatal("a file the cluster does not own was written")
	}
	if _, err := s.Put("keybindings.json", "{", nil, "", false); err == nil {
		t.Fatal("broken JSON was written")
	}
	if res, err := s.Put("keybindings.json", `{"k":1}`, nil, "", false); err != nil || !res.Written {
		t.Fatalf("owned file: %+v %v", res, err)
	}
	if res := put(`{"stateVersion":5}`, 5, "", false); !res.Written {
		t.Fatal("first copy of a layout not written")
	}
	if res := put(`{"stateVersion":5}`, 5, "", false); res.Written || res.Reason != "older" {
		t.Fatalf("same version written again: %+v", res)
	}
	if res := put(`{"stateVersion":2}`, 2, "", true); !res.Written {
		t.Fatal("renumber skipped")
	}
	dirty := `{"stateVersion":7,"stateDirty":true}`
	writeFile(t, filepath.Join(dir, layout), dirty)
	if res := put(`{"stateVersion":8}`, 8, "nope", false); res.Written || res.Reason != "dirty" {
		t.Fatalf("a dirty copy was replaced: %+v", res)
	}
	sum := sha256.Sum256([]byte(dirty))
	if res := put(`{"stateVersion":8}`, 8, hex.EncodeToString(sum[:]), false); !res.Written {
		t.Fatal("the answer to that very copy was refused")
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, layout)); !bytes.Equal(raw, []byte(`{"stateVersion":8}`)) {
		t.Fatalf("content: %s", raw)
	}
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("the runtime was not told about a written layout")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "workspaces", "w1", "*.put")); len(matches) != 0 {
		t.Fatalf("temporary files left: %v", matches)
	}
}

func TestStateListAndSweep(t *testing.T) {
	dir := t.TempDir()
	events := make(chan evState, 16)
	s := newStateFiles(dir, []string{"extra.json"}, func(ev any) { events <- ev.(evState) }, func(string) {})
	writeFile(t, filepath.Join(dir, "workspaces.json"), "[]")
	writeFile(t, filepath.Join(dir, "extra.json"), "{}")
	writeFile(t, filepath.Join(dir, "other.json"), "{}")
	writeFile(t, filepath.Join(dir, "workspaces", "w1", "layout.json"), "{}")
	writeFile(t, filepath.Join(dir, "workspaces", "bad.name", "layout.json"), "{}")
	names := map[string]bool{}
	for _, f := range s.List() {
		names[f.Name] = true
	}
	if len(names) != 3 || !names["workspaces.json"] || !names["extra.json"] || !names["workspaces/w1/layout.json"] {
		t.Fatalf("list: %v", names)
	}
	s.Sweep()
	drain := func() {
		for {
			select {
			case <-events:
			case <-time.After(150 * time.Millisecond):
				return
			}
		}
	}
	drain()
	writeFile(t, filepath.Join(dir, "workspaces", "w1", "layout.json"), `{"v":2}`)
	select {
	case ev := <-events:
		if ev.Name != "workspaces/w1/layout.json" || ev.Content == nil || *ev.Content != `{"v":2}` {
			t.Fatalf("event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event for a changed layout")
	}
	drain()
	if err := os.RemoveAll(filepath.Join(dir, "workspaces", "w1")); err != nil {
		t.Fatal(err)
	}
	s.Sweep()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Name == "workspaces/w1/layout.json" && ev.Content == nil {
				return
			}
		case <-deadline:
			t.Fatal("no removal event for a deleted workspace")
		}
	}
}

func TestAppendedSince(t *testing.T) {
	home := t.TempDir()
	r := TranscriptRoots(home)
	file := filepath.Join(r.Claude, "p", "s.jsonl")
	writeFile(t, file, "one\n")
	w := newTranscriptWatch(r, func(any) {})
	info, _ := os.Stat(file)
	inode, _ := fileID(info)
	if a := w.appendedSince(file, inode, info.Size()); a != nil {
		t.Fatal("the first sighting carries no lines")
	}
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("two\nthr")
	f.Close()
	info, _ = os.Stat(file)
	a := w.appendedSince(file, inode, info.Size())
	if a == nil || a.From != 4 || a.Next != 8 {
		t.Fatalf("appended: %+v", a)
	}
	if b, _ := base64.StdEncoding.DecodeString(a.Data); string(b) != "two\n" {
		t.Fatalf("data %q", b)
	}
}

func TestWriteModeCarriesGoSlices(t *testing.T) {
	dir := t.TempDir()
	s := newStateFiles(dir, nil, func(any) {}, func(string) {})
	res, err := s.WriteMode(true, true, []string{"health", "terminal"})
	if err != nil || !res.Owned || !res.Headless || res.GoSlices != nil {
		t.Fatalf("answer %+v %v: the answer stays owned and headless only", res, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "state-mode.json"))
	if !strings.Contains(string(raw), `"goSlices":["health","terminal"]`) || !strings.HasPrefix(string(raw), `{"owned":true,"headless":true,`) {
		t.Fatalf("file %s", raw)
	}
	if _, err := s.WriteMode(true, false, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "state-mode.json"))
	if strings.Contains(string(raw), "goSlices") {
		t.Fatalf("no slices left the field in: %s", raw)
	}
}
