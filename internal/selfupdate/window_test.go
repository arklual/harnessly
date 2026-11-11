package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowHoldsTheNextAutomaticUpdate(t *testing.T) {
	files := map[string][]byte{
		"agent-linux-amd64":   []byte("old harnessly-agent"),
		"runtime-linux-amd64": []byte("runtime 2"),
	}
	srv := hub(t, files, false)
	u, dir, applied := setup(t, srv)
	now := time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)
	u.Now = func() time.Time { return now }
	u.MinGap = 24 * time.Hour
	u.StateFile = filepath.Join(t.TempDir(), "state", "selfupdate.json")
	if !u.heldUntil().IsZero() {
		t.Fatal("a machine that never updated has nothing to wait for")
	}
	if err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*applied) != 1 {
		t.Fatalf("applied %v", *applied)
	}
	// The hub releases again an hour later. The restarted runtime is another Updater that reads the same file.
	files["runtime-linux-amd64"] = []byte("runtime 3")
	now = now.Add(time.Hour)
	next := &Updater{HubURL: u.HubURL, GOOS: u.GOOS, GOARCH: u.GOARCH, Files: u.Files, Auto: true, Now: u.Now,
		MinGap: u.MinGap, StateFile: u.StateFile, Applied: u.Applied}
	want := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	if got := next.heldUntil(); !got.Equal(want) {
		t.Fatalf("held until %v, want %v", got, want)
	}
	if st := next.Status(); st.NextAuto != "2026-03-02T03:00:00Z" {
		t.Fatalf("the status must say when: %+v", st)
	}
	// What Run does inside the window: it checks, so the page knows, and replaces nothing.
	*applied = nil
	if err := next.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "harnessly-runtime")); string(b) != "runtime 2" || len(*applied) != 0 {
		t.Fatalf("an automatic update inside the window: %q %v", b, *applied)
	}
	if st := next.Status(); !st.Checked || st.UpToDate {
		t.Fatalf("the check must still see the update: %+v", st)
	}
	// The person's «Update now» is not held back, and starts the window again.
	if err := next.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "harnessly-runtime")); string(b) != "runtime 3" {
		t.Fatalf("a manual update must go through: %q", b)
	}
	if got := next.heldUntil(); !got.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("held until %v after the manual update", got)
	}
	// Past the window Run updates by itself again.
	files["runtime-linux-amd64"] = []byte("runtime 4")
	now = now.Add(25 * time.Hour)
	if err := next.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "harnessly-runtime")); string(b) != "runtime 4" {
		t.Fatalf("past the window: %q", b)
	}
}

func TestWindowOffAndUnreadableState(t *testing.T) {
	state := filepath.Join(t.TempDir(), "selfupdate.json")
	now := time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)
	u := &Updater{StateFile: state, MinGap: time.Hour, Now: func() time.Time { return now }}
	u.markApplied()
	if u.heldUntil().IsZero() {
		t.Fatal("held right after an update")
	}
	u.MinGap = 0
	if !u.heldUntil().IsZero() {
		t.Fatal("no window, nothing held")
	}
	u.MinGap = time.Hour
	// The clock went back: the update is "in the future" and must not hold anything.
	now = now.Add(-48 * time.Hour)
	if !u.heldUntil().IsZero() {
		t.Fatal("an update dated in the future must not hold the machine")
	}
	if err := os.WriteFile(state, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !u.heldUntil().IsZero() {
		t.Fatal("an unreadable state must not hold the machine")
	}
}
