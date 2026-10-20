package status

import (
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

func trustScreen(folder string) string {
	return " Accessing workspace:\n " + folder + "\n Quick safety check: Is this a project you created or one you trust?\n" +
		" Claude Code'll be able to read, edit, and execute files here.\n ❯ No, exit\n   Yes, I trust this folder\n" +
		" Enter to confirm · Esc to cancel\n"
}

func TestAcceptTrustAnswersYesForTheTabsFolderOnce(t *testing.T) {
	e := newEnv(t, claudeTab(Inactive), &tmux.PaneInfo{Command: "claude", Path: "/srv/app", PID: 10})
	if !e.m.acceptTrust("t1", session, trustScreen("/srv/app")) {
		t.Fatal("the tab's own folder was not trusted")
	}
	if want := []string{session + " Down", session + " Enter"}; !reflect.DeepEqual(e.tm.keys, want) {
		t.Fatalf("keys %v, want %v", e.tm.keys, want)
	}
	// The dialog still on screen a moment later is left to close, not answered twice.
	if e.m.acceptTrust("t1", session, trustScreen("/srv/app")) {
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
