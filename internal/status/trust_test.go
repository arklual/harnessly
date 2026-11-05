package status

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

func trustScreen(folder string) string {
	return " Accessing workspace:\n " + folder + "\n Quick safety check: Is this a project you created or one you trust?\n" +
		" Claude Code'll be able to read, edit, and execute files here.\n ❯ No, exit\n   Yes, I trust this folder\n" +
		" Enter to confirm · Esc to cancel\n"
}

func TestAcceptTrustPressesEnterOnlyOnTheYesOption(t *testing.T) {
	e := newEnv(t, claudeTab(Inactive), &tmux.PaneInfo{Command: "claude", Path: "/srv/app", PID: 10})
	waited := func() {
		e.m.mu.Lock()
		delete(e.m.trustNext, "t1")
		e.m.mu.Unlock()
	}
	wantKeys := func(want ...string) {
		t.Helper()
		if !reflect.DeepEqual(e.tm.keys, want) {
			t.Fatalf("keys %v, want %v", e.tm.keys, want)
		}
	}
	if !e.m.acceptTrust("t1", session, trustScreen("/srv/app")) {
		t.Fatal("the tab's own folder was not trusted")
	}
	wantKeys(session + " Down")
	// The arrow gets time to show on the screen.
	if e.m.acceptTrust("t1", session, trustScreen("/srv/app")) {
		t.Fatal("stepped again right away")
	}
	// An agent that just painted can drop the arrow: Enter here would answer "No, exit".
	waited()
	if !e.m.acceptTrust("t1", session, trustScreen("/srv/app")) {
		t.Fatal("a lost arrow was not sent again")
	}
	wantKeys(session+" Down", session+" Down")
	waited()
	yes := strings.Replace(strings.Replace(trustScreen("/srv/app"), "❯ No", "  No", 1), "  Yes", "❯ Yes", 1)
	if !e.m.acceptTrust("t1", session, yes) {
		t.Fatal("the yes option was not confirmed")
	}
	wantKeys(session+" Down", session+" Down", session+" Enter")
	// The dialog still on screen a moment later is left to close, not answered twice.
	if e.m.acceptTrust("t1", session, yes) {
		t.Fatal("answered again right away")
	}
}

func TestAcceptTrustLeavesOtherFoldersAndOptOut(t *testing.T) {
	e := newEnv(t, claudeTab(Inactive), &tmux.PaneInfo{Command: "claude", Path: "/srv/app", PID: 10})
	if e.m.acceptTrust("t1", session, trustScreen("/srv/other")) {
		t.Fatal("trusted a folder the tab does not run in")
	}
	if e.m.acceptTrust("t1", session, "vibe@host:/srv/app$ ") {
		t.Fatal("answered a screen with no dialog")
	}
	e.m.d.AskFolderTrust = true
	if e.m.acceptTrust("t1", session, trustScreen("/srv/app")) {
		t.Fatal("answered although the person asked to be asked")
	}
	if len(e.tm.keys) != 0 {
		t.Fatalf("keys sent: %v", e.tm.keys)
	}
}
