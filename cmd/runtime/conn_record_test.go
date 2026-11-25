package main

import (
	"context"
	"errors"
	"testing"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/secrets/secretstest"
)

// refusingKV is a hub whose switch connections-shared is off: it refuses the record.
type refusingKV struct{ *secretstest.MemKV }

func (refusingKV) Read(context.Context, string) (map[string]string, error) {
	return nil, &secrets.Refusal{Reason: secrets.ReasonDenied}
}

func (refusingKV) Write(context.Context, string, map[string]string) error {
	return &secrets.Refusal{Reason: secrets.ReasonDenied}
}

func TestConnRecordReadsTheHubsRecord(t *testing.T) {
	kv := secretstest.New()
	rec := connRecord{secrets.NewRemote(kv, "me", t.TempDir())}
	if v, err := rec.ReadIndex(); err != nil || v != "" {
		t.Fatalf("no record yet: %q %v", v, err)
	}
	if err := rec.WriteIndex(`{"version":1}`); err != nil {
		t.Fatal(err)
	}
	if kv.Data["me/sys/connections"]["value"] != `{"version":1}` {
		t.Fatalf("kept at %v", kv.Data)
	}
	if v, err := rec.ReadIndex(); err != nil || v != `{"version":1}` {
		t.Fatalf("read back: %q %v", v, err)
	}
	off := connRecord{secrets.NewRemote(refusingKV{secretstest.New()}, "me", t.TempDir())}
	if _, err := off.ReadIndex(); !errors.Is(err, connections.ErrDenied) {
		t.Fatalf("a refusing hub reads as %v", err)
	}
	if err := off.WriteIndex("{}"); !errors.Is(err, connections.ErrDenied) {
		t.Fatalf("a refusing hub writes as %v", err)
	}
	kv.Down = true
	if _, err := rec.ReadIndex(); err == nil || errors.Is(err, connections.ErrDenied) {
		t.Fatalf("a hub away reads as %v", err)
	}
}
