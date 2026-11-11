package cliversion

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckAgainstTheTable(t *testing.T) {
	table := []Range{{Provider: Claude, From: "2.1.296", To: "2.1.316", Screens: []string{"2.1.296", "2.1.316"}}}
	for version, want := range map[string]Verdict{
		"2.1.296": Checked, "2.1.300": Checked, "2.1.316": Checked,
		"2.1.295": Unchecked, "2.1.317": Unchecked, "2.2.0": Unchecked, "3.0.1": Unchecked, "nightly": Unchecked,
	} {
		if got := checkIn(table, Claude, version); got != want {
			t.Errorf("claude %s: %s, want %s", version, got, want)
		}
	}
	if got := checkIn(table, Codex, "0.50.0"); got != NoContract {
		t.Errorf("a provider without rows: %s, want %s", got, NoContract)
	}
}

func TestTheTableIsWellFormed(t *testing.T) {
	for _, r := range Table() {
		from, okFrom := parse(r.From)
		to, okTo := parse(r.To)
		if !okFrom || !okTo || less(to, from) {
			t.Fatalf("row %+v: bad ends", r)
		}
		ends := map[string]bool{}
		for _, s := range r.Screens {
			v, ok := parse(s)
			if !ok || less(v, from) || less(to, v) {
				t.Errorf("row %s..%s names screens of %s, outside it", r.From, r.To, s)
			}
			ends[s] = true
		}
		if !ends[r.From] || !ends[r.To] {
			t.Errorf("row %s..%s: both ends must have reference screens", r.From, r.To)
		}
	}
}

func TestOfProbesOncePerBinary(t *testing.T) {
	Reset()
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude-1")
	if err := os.WriteFile(bin, []byte("one"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "claude")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	probes := 0
	probe := func(v string) func() (string, error) {
		return func() (string, error) { probes++; return v, nil }
	}
	for i := 0; i < 3; i++ {
		if v, err := Of(link, probe("1.0.0")); err != nil || v != "1.0.0" {
			t.Fatalf("version %q, %v", v, err)
		}
	}
	if probes != 1 {
		t.Fatalf("the CLI was started %d times for one binary, want 1", probes)
	}

	// The link now names another file: an update. It is probed again, once.
	next := filepath.Join(dir, "claude-2")
	if err := os.WriteFile(next, []byte("two!"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(link)
	if err := os.Symlink(next, link); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if v, _ := Of(link, probe("2.0.0")); v != "2.0.0" {
			t.Fatalf("after the update: %q", v)
		}
	}
	if probes != 2 {
		t.Fatalf("probes after an update: %d, want 2", probes)
	}

	// The same file rewritten in place (another time of change) is probed again too.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(next, later, later); err != nil {
		t.Fatal(err)
	}
	if _, _ = Of(link, probe("2.0.1")); probes != 3 {
		t.Fatalf("probes after a rewrite: %d, want 3", probes)
	}
}

func TestOfDoesNotKeepAFailedProbe(t *testing.T) {
	Reset()
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Of(bin, func() (string, error) { return "", errors.New("busy") }); err == nil {
		t.Fatal("a failed probe must be an error")
	}
	if v, err := Of(bin, func() (string, error) { return "1.2.3", nil }); err != nil || v != "1.2.3" {
		t.Fatalf("the probe after a failed one: %q, %v", v, err)
	}
}

func TestNoteTellsOncePerVersion(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var told []string
	OnUnchecked(func(provider, version string) { told = append(told, provider+" "+version) })
	if Cautious(Claude) {
		t.Fatal("cautious before any version was read")
	}
	for i := 0; i < 5; i++ { // five chats start on the same CLI
		if v := Note(Claude, "9.9.9"); v != Unchecked {
			t.Fatalf("verdict %s", v)
		}
	}
	if !Cautious(Claude) {
		t.Fatal("an unchecked version must turn the cautious mode on")
	}
	Note(Claude, "9.9.10")
	Note(Claude, "") // a probe that did not answer changes nothing
	if !Cautious(Claude) {
		t.Fatal("an empty version dropped what was known")
	}
	if len(told) != 2 || told[0] != "claude 9.9.9" || told[1] != "claude 9.9.10" {
		t.Fatalf("told %v, want one line per version", told)
	}
	row := Table()[0]
	if v := Note(Claude, row.To); v != Checked || Cautious(Claude) {
		t.Fatalf("a checked version: %s, cautious %v", v, Cautious(Claude))
	}
	if v := Note(Codex, "0.1.0"); v != NoContract || Cautious(Codex) {
		t.Fatalf("a provider without rows: %s, cautious %v", v, Cautious(Codex))
	}
	if got := Seen(); len(got) != 2 || got[0].Provider != Claude || got[0].Version != row.To || got[0].Verdict != Checked {
		t.Fatalf("seen %+v", got)
	}
}
