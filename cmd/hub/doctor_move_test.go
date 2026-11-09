package main

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// `hub doctor` lists every machine's move and, for a code that failed, its cause and fix from the page of the
// move this version carries.
func TestDoctorSaysTheMoveAndTheFixOfItsCodes(t *testing.T) {
	c := describeMoves([]hubproto.MigrationStatus{
		{Machine: "m1", Migration: "0001-data-to-hub", Total: 420, Verified: 412, Failed: 8,
			Failures: []hubproto.MigrationObject{{Kind: "uploads", Object: "up_1", Code: "MIG-READ"}}},
		{Machine: "m2", Migration: "0001-data-to-hub", Total: 3, Verified: 3, FinishedAt: "2026-11-09T12:00:00Z"},
	})
	if c.verdict != vWarn || c.problem == nil || c.problem.Code != "HUB-M001" {
		t.Fatalf("check = %+v, want a warning HUB-M001", c.check)
	}
	text := strings.Join(c.lines, "\n")
	for _, want := range []string{"m1: 412 of 420, 8 failed (MIG-READ ×1)", "m2: 3 of 3, done", "MIG-READ (docs/migrations/0001-data-to-hub.md):", "harnessly migrate retry"} {
		if !strings.Contains(text, want) {
			t.Errorf("doctor does not say %q:\n%s", want, text)
		}
	}
	if c := describeMoves(nil); c.verdict != vOK {
		t.Fatalf("no moves: %+v", c.check)
	}
}

// Every error code the page of the move lists has a cause and a fix under it.
func TestThePageOfTheMoveExplainsEveryCode(t *testing.T) {
	for _, code := range []string{"MIG-READ", "MIG-GONE", "MIG-HASH", "MIG-TOO-BIG", "MIG-PUT", "MIG-VERIFY", "MIG-SESSION"} {
		lines := codeSection("0001-data-to-hub", code)
		if len(lines) < 2 || !strings.Contains(strings.Join(lines, " "), "Fix:") {
			t.Errorf("%s: %q", code, lines)
		}
	}
}
