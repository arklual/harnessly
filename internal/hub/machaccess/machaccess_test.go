package machaccess_test

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/machaccess"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func user(id string) store.MachineAccessEntry {
	return store.MachineAccessEntry{MachineID: "m", Kind: store.AccessUser, RefID: id}
}
func team(id string) store.MachineAccessEntry {
	return store.MachineAccessEntry{MachineID: "m", Kind: store.AccessTeam, RefID: id}
}

func TestModeOf(t *testing.T) {
	for _, c := range []struct {
		name    string
		entries []store.MachineAccessEntry
		want    machaccess.Mode
	}{
		{"empty list is the tenant", nil, machaccess.ModeTenant},
		{"only the creator", []store.MachineAccessEntry{user("alice")}, machaccess.ModeOwner},
		{"one other person", []store.MachineAccessEntry{user("bob")}, machaccess.ModePick},
		{"two people", []store.MachineAccessEntry{user("alice"), user("bob")}, machaccess.ModePick},
		{"teams only", []store.MachineAccessEntry{team("t1"), team("t2")}, machaccess.ModeTeam},
		{"people and a team", []store.MachineAccessEntry{team("t1"), user("bob")}, machaccess.ModePick},
		{"the creator and a team", []store.MachineAccessEntry{user("alice"), team("t1")}, machaccess.ModePick},
	} {
		if got := machaccess.ModeOf("alice", c.entries); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// The matrix: who is asking against how the machine is shared.
func TestDecide(t *testing.T) {
	m := store.Machine{ID: "m", CreatedBy: "alice"}
	plain := func(id string, teams ...string) machaccess.Subject {
		return machaccess.Subject{UserID: id, Teams: teams}
	}
	for _, c := range []struct {
		name    string
		entries []store.MachineAccessEntry
		who     machaccess.Subject
		want    bool
	}{
		{"tenant: anyone", nil, plain("bob"), true},
		{"owner mode: the creator", []store.MachineAccessEntry{user("alice")}, plain("alice"), true},
		{"owner mode: someone else", []store.MachineAccessEntry{user("alice")}, plain("bob"), false},
		{"owner mode: a manager", []store.MachineAccessEntry{user("alice")}, machaccess.Subject{UserID: "carol", Manager: true}, true},
		{"owner mode: an instance admin", []store.MachineAccessEntry{user("alice")}, machaccess.Subject{UserID: "root", Admin: true}, true},
		{"team mode: a member", []store.MachineAccessEntry{team("t1")}, plain("bob", "t0", "t1"), true},
		{"team mode: another team", []store.MachineAccessEntry{team("t1")}, plain("bob", "t2"), false},
		{"team mode: no team", []store.MachineAccessEntry{team("t1")}, plain("bob"), false},
		{"team mode: the creator outside the team", []store.MachineAccessEntry{team("t1")}, plain("alice"), true},
		{"pick: a listed person", []store.MachineAccessEntry{user("bob")}, plain("bob"), true},
		{"pick: not listed", []store.MachineAccessEntry{user("bob")}, plain("dan"), false},
		{"pick: a team entry does not match a user with that id", []store.MachineAccessEntry{team("bob")}, plain("bob"), false},
		{"pick: a user entry does not match a team with that id", []store.MachineAccessEntry{user("t1")}, plain("dan", "t1"), false},
		{"pick with a team: via the team", []store.MachineAccessEntry{user("bob"), team("t1")}, plain("dan", "t1"), true},
		{"an unknown person on an empty list", nil, plain(""), true},
	} {
		if got := machaccess.Decide(m, c.entries, c.who); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// A machine without a creator is nobody's: an empty user id is not the creator.
	if machaccess.Decide(store.Machine{ID: "m"}, []store.MachineAccessEntry{user("bob")}, plain("")) {
		t.Error("an empty person passed as the creator of a machine without one")
	}
}
