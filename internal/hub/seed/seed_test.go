package seed_test

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/seed"
)

// Two organizations of the shape of a real installation's seed: three people in the first, two of them in the second.
const twoOrgs = `
orgs:
  - slug: alpha
    name: Альфа
    admins: [anna@example.com]
    tenants:
      - slug: alpha-main
        name: Альфа
        owners: [anna@example.com]
        members: [Boris@Example.com, gleb@example.com]
        teams:
          - slug: general
            name: Общая
            members: [anna@example.com, boris@example.com, gleb@example.com]
  - slug: beta
    name: Бета
    timezone: Europe/Moscow
    currency: RUB
    admins: [anna@example.com, boris@example.com]
    tenants:
      - slug: beta-main
        name: Бета
        owners: [anna@example.com]
        members: [boris@example.com]
        teams:
          - slug: general
            name: Общая
            leads: [boris@example.com]
            members: [anna@example.com]
`

func parse(t *testing.T, s string) seed.File {
	t.Helper()
	f, err := seed.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSeedTwiceChangesNothing(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com")             // the instance administrator, not in the seed
	existing := s.SignIn("anna@example.com") // a person who signed in before the seed
	f := parse(t, twoOrgs)
	ctx := t.Context()

	dry, err := seed.Apply(ctx, s.DB, f, true, time.Now())
	if err != nil || len(dry) == 0 {
		t.Fatalf("dry run: %v %v", dry, err)
	}
	if orgs, _ := s.DB.S().AllOrgs(ctx); len(orgs) != 0 {
		t.Fatalf("a dry run wrote %d organizations", len(orgs))
	}
	first, err := seed.Apply(ctx, s.DB, f, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(first, "\n") != strings.Join(dry, "\n") {
		t.Errorf("the dry run said\n%s\nthe run did\n%s", strings.Join(dry, "\n"), strings.Join(first, "\n"))
	}
	for _, c := range first {
		if strings.Contains(c, "user anna@example.com") {
			t.Errorf("a person who signed in was made again: %s", c)
		}
	}
	again, err := seed.Apply(ctx, s.DB, f, false, time.Now())
	if err != nil || len(again) != 0 {
		t.Errorf("second run: %v %v", again, err)
	}
	if dry, err := seed.Apply(ctx, s.DB, f, true, time.Now()); err != nil || len(dry) != 0 {
		t.Errorf("dry run after: %v %v", dry, err)
	}

	// Signing in by the address joins the seeded person; each sees the organizations they are in.
	orgsOf := func(p *hubtest.Person) []string {
		t.Helper()
		var list struct{ Orgs []struct{ Slug, Role string } }
		if r := p.Do("GET", "/api/orgs", nil); r.Code != 200 {
			t.Fatalf("list: %s", r)
		} else {
			r.JSON(&list)
		}
		var out []string
		for _, o := range list.Orgs {
			out = append(out, o.Slug+":"+o.Role)
		}
		sort.Strings(out)
		return out
	}
	boris := s.SignIn("boris@example.com")
	gleb := s.SignIn("gleb@example.com")
	if got := strings.Join(orgsOf(existing), ","); got != "alpha:admin,beta:admin" {
		t.Errorf("anna sees %s", got)
	}
	if got := strings.Join(orgsOf(boris), ","); got != "alpha:member,beta:admin" {
		t.Errorf("boris sees %s", got)
	}
	if got := strings.Join(orgsOf(gleb), ","); got != "alpha:member" {
		t.Errorf("gleb sees %s", got)
	}
	// Exactly one tenant and one team in each, the team the primary of its people.
	var v struct {
		Tenants []struct{ Slug string }
		Teams   []struct {
			Slug    string
			LeadIDs []string
		}
		Me struct{ Teams []struct{ IsPrimary bool } }
	}
	boris.Do("GET", "/api/orgs/beta", nil).JSON(&v)
	if len(v.Tenants) != 1 || len(v.Teams) != 1 || len(v.Teams[0].LeadIDs) != 1 || v.Teams[0].LeadIDs[0] != boris.ID {
		t.Errorf("beta for boris = %+v", v)
	}
	if len(v.Me.Teams) != 1 || !v.Me.Teams[0].IsPrimary {
		t.Errorf("boris's team in beta = %+v", v.Me.Teams)
	}
}

func TestParseRefusesWhatItCannotMake(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key":  "orgs:\n  - slug: alpha\n    name: A\n    owner: x\n",
		"bad slug":     "orgs:\n  - slug: Al\n    name: A\n",
		"bad email":    "orgs:\n  - slug: alpha\n    name: A\n    admins: [not-an-address]\n",
		"no owner":     "orgs:\n  - slug: alpha\n    name: A\n    tenants:\n      - slug: alpha-t\n        name: T\n",
		"not a member": "orgs:\n  - slug: alpha\n    name: A\n    tenants:\n      - slug: alpha-t\n        name: T\n        owners: [a@example.com]\n        teams:\n          - slug: team\n            name: Team\n            members: [b@example.com]\n",
		"bad zone":     "orgs:\n  - slug: alpha\n    name: A\n    timezone: Mars/Base\n",
	} {
		if _, err := seed.Parse(strings.NewReader(body)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}
