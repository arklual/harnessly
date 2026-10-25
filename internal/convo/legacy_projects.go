package convo

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// LegacyProject is a project of Legacy's organizer: a name over a free group of conversations.
type LegacyProject struct{ ID, Name string }

// ImportProjects takes over Legacy's projects: each becomes a v2 project under its id and name, at the folder most
// of its conversations work in, and its conversations go into it. Legacy's projects are groups: several of them in one
// folder stay several. assign is conversation id → Legacy project id.
// A conversation is put into its project once: what the person does in v2 afterwards stays.
func (r *Runtime) ImportProjects(projects []LegacyProject, assign map[string]string) int {
	placed := map[string]bool{}
	if s := r.d.Store; s != nil {
		ids, err := s.loadLegacyPlaced()
		if err != nil {
			r.d.Log.Warn("legacy projects unreadable", "err", err)
			return 0
		}
		for _, id := range ids {
			placed[id] = true
		}
	}
	byProject := map[string][]string{}
	cwds := map[string]map[string]int{}
	for id, pid := range assign {
		if placed[id] {
			continue
		}
		var cwd string
		live := false
		if r.call(context.Background(), id, func(c *conv, _ time.Time) { cwd, live = c.rec.Cwd, c.deletedAt == 0 }) != nil || !live {
			continue
		}
		byProject[pid] = append(byProject[pid], id)
		if cwds[pid] == nil {
			cwds[pid] = map[string]int{}
		}
		if cwd != "" {
			cwds[pid][cwd]++
		}
	}
	n := 0
	for _, lp := range projects {
		ids := byProject[lp.ID]
		path := mostOf(cwds[lp.ID])
		if len(ids) == 0 || path == "" {
			continue
		}
		p, ok := r.project(lp.ID)
		if !ok {
			if st, err := os.Stat(path); err != nil || !st.IsDir() {
				continue
			}
			p, _ = r.addProject(wire.Project{ID: lp.ID, Path: path, Name: lp.Name})
		}
		for _, id := range ids {
			_ = r.call(context.Background(), id, func(c *conv, now time.Time) {
				if c.rec.ProjectID == nil {
					pid := p.ID
					c.rec.ProjectID = &pid
					c.recResult(now)
				}
			})
			placed[id] = true
			n++
		}
	}
	if s := r.d.Store; s != nil && n > 0 {
		ids := make([]string, 0, len(placed))
		for id := range placed {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		if err := s.saveLegacyPlaced(ids); err != nil {
			r.d.Log.Warn("legacy projects not saved", "err", err)
		}
	}
	return n
}

// mostOf is the folder named most often; of equals, the first in order.
func mostOf(counts map[string]int) string {
	best := ""
	for path, n := range counts {
		if best == "" || n > counts[best] || n == counts[best] && path < best {
			best = path
		}
	}
	return best
}

// LegacyOrganizer reads Legacy's organizer file: its projects, and conversation id → project for the conversations
// of agent tabs (`agent:<machine>:<tab>` or `agent:<tab>`; the tab is the conversation). A missing file is none.
func LegacyOrganizer(file string) ([]LegacyProject, map[string]string, error) {
	raw, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var o struct {
		Projects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
		Assignments map[string]string `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, nil, err
	}
	projects := make([]LegacyProject, 0, len(o.Projects))
	for _, p := range o.Projects {
		if p.ID != "" && p.Name != "" {
			projects = append(projects, LegacyProject{ID: p.ID, Name: p.Name})
		}
	}
	assign := map[string]string{}
	for ref, pid := range o.Assignments {
		if !strings.HasPrefix(ref, "agent:") {
			continue
		}
		if tab := ref[strings.LastIndex(ref, ":")+1:]; wire.ValidConvID(tab) {
			assign[tab] = pid
		}
	}
	return projects, assign, nil
}
