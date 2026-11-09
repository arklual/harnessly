package orgs_test

import (
	"net/http"
	"strings"
	"testing"
)

// A plain member of a tenant (in a team, no lead or admin role) opens the new hub's screens: the shell, the machines of
// the tenant, the team. The owner's complaint (09.11): a person added to a team saw no machine until made a lead.
func TestMemberSeesTenantMachines(t *testing.T) {
	for _, mode := range []string{"off", "on"} {
		t.Run(mode, func(t *testing.T) {
			ac := newAccess(t, mode)
			for name, p := range map[string]string{"vera (team member)": "vera", "dan (tenant member)": "dan"} {
				who := ac.vera
				if p == "dan" {
					who = ac.dan
				}
				for _, path := range []string{
					"",
					"/machines?scope=tenant:" + ac.tenant,
					"/machines?scope=team:" + ac.team.ID,
				} {
					r := who.Do("GET", ac.url(path), nil)
					t.Logf("%s GET %s -> %d %s", name, path, r.Code, trunc(string(r.Body)))
					if strings.HasPrefix(path, "/machines?scope=tenant") && (r.Code != http.StatusOK || !strings.Contains(string(r.Body), ac.machine)) {
						t.Errorf("%s does not see the tenant's machine: %s", name, r)
					}
				}
				if r := who.Do("GET", "/api/machines/"+ac.machine+"/open", nil); r.Code != http.StatusOK {
					t.Errorf("%s cannot open: %s", name, r)
				}
			}
		})
	}
}

func trunc(s string) string {
	if len(s) > 300 {
		return s[:300]
	}
	return s
}
