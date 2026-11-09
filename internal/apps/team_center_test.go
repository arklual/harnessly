package apps

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
)

func mustCurrent(t *testing.T, c *filesync.Client) *filestore.Entry {
	t.Helper()
	e, err := c.Current(context.Background(), TeamStateFile)
	if err != nil || e == nil {
		t.Fatalf("the team in the store: %v %v", e, err)
	}
	return e
}

// an app an owner installs for the space reaches a member's machine through the hub's store alone (no
// platform snapshot), with the member's own role, and a team older than the store's is not written over it.
func TestTeamAppsTravelThroughTheStore(t *testing.T) {
	center := centerFor(t)
	ctx := context.Background()
	owner := newRegistry(t)
	if err := owner.SetTeam("sp1", RoleOwner, teamNow); err != nil {
		t.Fatal(err)
	}
	publishedToTeam(t, owner, "acme.demo", "1.0.0")
	if _, err := owner.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{HostVersion: TemplateVersion, By: "ann@team.test", Now: teamNow.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	co := center("owner", "p-1")
	if err := owner.PublishTeam(ctx, co); err != nil {
		t.Fatal(err)
	}

	member := newRegistry(t)
	cm := center("member", "p-2")
	if _, err := member.ApplyStoredTeam(ctx, cm, filestore.Entry{Path: TeamStateFile, Sha: mustCurrent(t, cm).Sha}, TemplateVersion, teamNow); err == nil {
		t.Fatal("a machine in no space took the store's team")
	}
	if err := member.SetTeam("sp1", RoleMember, teamNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	e, err := cm.Current(ctx, TeamStateFile)
	if err != nil || e == nil {
		t.Fatalf("the team in the store: %v %v", e, err)
	}
	problems, err := member.ApplyStoredTeam(ctx, cm, *e, TemplateVersion, teamNow.Add(2*time.Minute))
	if err != nil || len(problems) > 0 {
		t.Fatalf("apply: %v %v", problems, err)
	}
	list, err := member.List()
	if err != nil || len(list) != 1 || list[0].ID != "acme.demo" {
		t.Fatalf("the member's apps: %+v %v", list, err)
	}
	team, _ := member.Team()
	if team.Space != "sp1" || team.Role != RoleMember {
		t.Fatalf("the member's team: %+v", team)
	}

	// A team from the future is not taken: it would hold every later snapshot off.
	other := newRegistry(t)
	if err := other.SetTeam("sp2", RoleMember, teamNow); err != nil {
		t.Fatal(err)
	}
	if _, err := other.ApplyStoredTeam(ctx, cm, *e, TemplateVersion, teamNow); err == nil {
		t.Fatal("a machine of another space took the store's team")
	}
	if _, err := member.ApplyStoredTeam(ctx, cm, *e, TemplateVersion, teamNow.Add(-time.Hour)); err == nil {
		t.Fatal("a team from the future was taken")
	}

	// A machine whose team is older does not write it over the store's.
	stale := newRegistry(t)
	if err := stale.SetTeam("sp1", RoleOwner, teamNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := stale.PublishTeam(ctx, center("stale", "p-3")); err != nil {
		t.Fatal(err)
	}
	if again, _ := cm.Current(ctx, TeamStateFile); again.Rev != e.Rev {
		t.Fatalf("an older team replaced the store's: rev %d → %d", e.Rev, again.Rev)
	}
}
