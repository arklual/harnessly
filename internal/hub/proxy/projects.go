package proxy

import (
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/switches"
)

// asleepLabel is what the sidebar says of the projects of a machine that is not on.
const asleepLabel = "машина спит"

var errProjectsOff = httpx.New(http.StatusNotFound, "projects_off", "Проекты из хаба не включены")

// ProjectRow is a project of a machine the person may open, as the sidebar «Проекты» shows it.
type ProjectRow struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Directories []string `json:"directories"`
	MachineID   string   `json:"machineId"`
	MachineName string   `json:"machineName"`
	Mine        bool     `json:"mine"`
	// Asleep: the machine is not on; its projects are the registry's last word. Label says so in words.
	Asleep  bool   `json:"asleep"`
	Label   string `json:"label,omitempty"`
	OrgName string `json:"orgName,omitempty"`
}

// meProjects is GET /api/me/projects: the registry's projects of every machine the person may open now (fleet: the same
// rule as /m/<id>/…, the owner's own-work rule and a closed machine's list included), a machine that sleeps among them,
// marked. Behind the rollout switch hub-projects; off: 404 projects_off and the sidebar asks the runtime.
func (s *Service) meProjects(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	if s.switches == nil || !s.switches.On(r.Context(), switches.HubProjects, strings.ToLower(p.User.Email), "") {
		return errProjectsOff
	}
	list, err := s.fleet(r.Context(), p.User.ID, false)
	if err != nil {
		return err
	}
	ids := make([]string, len(list))
	for i, f := range list {
		ids[i] = f.ID
	}
	byMachine, err := s.db.S().MachineProjectsOf(r.Context(), ids)
	if err != nil {
		return err
	}
	out := []ProjectRow{}
	for _, f := range list {
		for _, mp := range byMachine[f.ID] {
			row := ProjectRow{ID: mp.ID, Name: mp.Name, Directories: mp.Directories, MachineID: f.ID, MachineName: f.Name, Mine: f.Mine,
				Asleep: !f.Online, OrgName: f.OrgName}
			if row.Directories == nil {
				row.Directories = []string{}
			}
			if row.Asleep {
				row.Label = asleepLabel
			}
			out = append(out, row)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"projects": out})
	return nil
}
