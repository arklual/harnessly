package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/handoff"
	"github.com/ly-solutions/harnessly/internal/wire"
)

type handoffTestPublisher struct{ seq int64 }

func (p *handoffTestPublisher) Publish(wire.Channel, wire.Event) int64 {
	p.seq++
	return p.seq
}

func TestHistoryDraftSnapshotsWithoutAnAgentSessionOrNoteWriter(t *testing.T) {
	root := t.TempDir()
	transcript := filepath.Join(root, "active.jsonl")
	const source = "{\"type\":\"user\",\"sessionId\":\"private-session\",\"message\":{\"role\":\"user\",\"content\":\"one\"}}\n" +
		"{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"model\":\"private-model\",\"content\":[{\"type\":\"text\",\"text\":\"two\"}]}}\n"
	if err := os.WriteFile(transcript, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	artifacts, err := handoff.NewArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rt := convo.NewInline(convo.Deps{MachineID: "m1", Handoffs: artifacts, Publisher: &handoffTestPublisher{}})
	rt.Import([]convo.ImportTab{{ID: "tab-source_123", Provider: wire.ProviderClaude, Cwd: "/w",
		TranscriptPath: transcript}}, nil)
	writes := 0
	h := &v2Handoff{rt: rt, write: func(context.Context, handoff.NoteRun) (string, error) {
		writes++
		return "note", nil
	}}
	draft, err := h.Draft(context.Background(), "tab-source_123", wire.HandoffDraftRequest{Mode: wire.DraftHistory})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Mode != wire.DraftHistory || draft.Artifact == nil || draft.Artifact.Bytes == 0 || draft.Text != "" {
		t.Fatalf("draft: %+v", draft)
	}
	if writes != 0 {
		t.Fatalf("history called the note writer %d times", writes)
	}
}
