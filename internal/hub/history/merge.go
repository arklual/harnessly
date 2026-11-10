package history

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// HRN-883: a machine's own projects and favorites merged into its person's sidebar by the machine itself, as the page
// merges them (web/app/next/features/sidebar/profile-layout.ts mergeMachine): a project joins the profile's of the same
// name or becomes one of its own, the favorites' groups join by name with their chats added where missing, the loose
// ones after the loose; a machine's favorites are merged once, its projects whenever one is new.

// ProfileDoc is the person's sidebar document (the page's ProfileSidebar).
type ProfileDoc struct {
	Projects  []ProfileProject `json:"projects"`
	Favorites ProfileFavorites `json:"favorites"`
	Merged    []string         `json:"merged"`
}

// ProfileProject is a project of the profile and the machine projects it stands for.
type ProfileProject struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	CreatedAt int64           `json:"createdAt"`
	Members   []ProfileMember `json:"members"`
}

// ProfileMember is one machine's project in a project of the profile.
type ProfileMember struct {
	MachineID string `json:"machineId"`
	ProjectID string `json:"projectId"`
}

// ProfileFavorites is where the favorites stand: the groups, then the loose ones.
type ProfileFavorites struct {
	Groups []ProfileGroup `json:"groups"`
	Loose  []string       `json:"loose"`
}

// ProfileGroup is a named group of favorites.
type ProfileGroup struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Chats []string `json:"chats"`
}

// MachineProject is a project as a machine has it.
type MachineProject struct {
	ID        string
	Name      string
	CreatedAt int64
}

// favGroupsMax is the most groups the favorites hold (wire.FavGroupsMax).
const favGroupsMax = 50

func sameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func randomID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// MergeMachine merges one machine's projects and favorites into doc; changed is false when nothing is new.
func MergeMachine(doc ProfileDoc, machine string, projects []MachineProject, favorites *ProfileFavorites) (ProfileDoc, bool) {
	changed := false
	known := map[string]bool{}
	for _, p := range doc.Projects {
		for _, m := range p.Members {
			known[m.MachineID+"/"+m.ProjectID] = true
		}
	}
	ps := append([]MachineProject{}, projects...)
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].CreatedAt < ps[j].CreatedAt })
	for _, p := range ps {
		// A project the document could not keep (no name, an id out of shape) stays the machine's own.
		if !sidebarID.MatchString(p.ID) || !sidebarName(&p.Name) || known[machine+"/"+p.ID] {
			continue
		}
		known[machine+"/"+p.ID] = true
		changed = true
		member := ProfileMember{MachineID: machine, ProjectID: p.ID}
		joined := false
		for i := range doc.Projects {
			if sameName(doc.Projects[i].Name, p.Name) && len(doc.Projects[i].Members) < 50 {
				doc.Projects[i].Members = append(append([]ProfileMember{}, doc.Projects[i].Members...), member)
				joined = true
				break
			}
		}
		if !joined {
			doc.Projects = append(doc.Projects, ProfileProject{ID: randomID("pp-"), Name: p.Name, CreatedAt: p.CreatedAt,
				Members: []ProfileMember{member}})
		}
	}
	for _, m := range doc.Merged {
		if m == machine {
			return doc, changed
		}
	}
	placed := map[string]bool{}
	for _, g := range doc.Favorites.Groups {
		for _, c := range g.Chats {
			placed[c] = true
		}
	}
	for _, c := range doc.Favorites.Loose {
		placed[c] = true
	}
	fresh := func(chats []string) []string {
		out := []string{}
		for _, c := range chats {
			if sidebarID.MatchString(c) && !placed[c] {
				placed[c] = true
				out = append(out, c)
			}
		}
		return out
	}
	if favorites != nil {
		for _, g := range favorites.Groups {
			if !sidebarName(&g.Name) {
				continue
			}
			at := -1
			for i, h := range doc.Favorites.Groups {
				if sameName(h.Name, g.Name) {
					at = i
					break
				}
			}
			switch {
			case at >= 0:
				doc.Favorites.Groups[at].Chats = append(append([]string{}, doc.Favorites.Groups[at].Chats...), fresh(g.Chats)...)
			case len(doc.Favorites.Groups) < favGroupsMax:
				id := g.ID
				for _, h := range doc.Favorites.Groups {
					if h.ID == id {
						id = ""
						break
					}
				}
				if !sidebarID.MatchString(id) {
					id = randomID("fg-")
				}
				doc.Favorites.Groups = append(doc.Favorites.Groups, ProfileGroup{ID: id, Name: g.Name, Chats: fresh(g.Chats)})
			}
		}
		doc.Favorites.Loose = append(doc.Favorites.Loose, fresh(favorites.Loose)...)
	}
	doc.Merged = append(doc.Merged, machine)
	return doc, true
}

// ErrSidebarBusy: the person's sidebar kept changing under the merge.
var ErrSidebarBusy = errors.New("history: the sidebar kept changing")

// MergeMachineInto merges a machine's projects and favorites into its person's sidebar, written on the revision it
// was read at and read again when another write came first. merged: the document says the machine's favorites are in.
func MergeMachineInto(ctx context.Context, db *store.DB, userID, machine string, projects []MachineProject,
	favorites *ProfileFavorites, nowMs int64) (merged bool, err error) {
	for range 4 {
		rev, raw, err := Sidebar(ctx, db, userID)
		if err != nil {
			return false, err
		}
		doc := ProfileDoc{Projects: []ProfileProject{}, Favorites: ProfileFavorites{Groups: []ProfileGroup{}, Loose: []string{}},
			Merged: []string{}}
		if raw != nil {
			if err := json.Unmarshal(raw, &doc); err != nil {
				return false, err
			}
		}
		next, changed := MergeMachine(doc, machine, projects, favorites)
		if !changed {
			return true, nil
		}
		out, err := json.Marshal(next)
		if err != nil {
			return false, err
		}
		if _, ok := ParseSidebar(out); !ok || len(out) > SidebarMaxBytes {
			return false, errors.New("history: the merged sidebar is not one the hub keeps")
		}
		if _, err := WriteSidebar(ctx, db, userID, rev, out, nowMs); errors.Is(err, ErrSidebarChanged) {
			continue
		} else if err != nil {
			return false, err
		}
		return true, nil
	}
	return false, ErrSidebarBusy
}

// MachineMerged reports whether the person's sidebar has the machine's favorites in.
func MachineMerged(ctx context.Context, db *store.DB, userID, machine string) (bool, error) {
	_, raw, err := Sidebar(ctx, db, userID)
	if err != nil || raw == nil {
		return false, err
	}
	var doc struct {
		Merged []string `json:"merged"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, nil
	}
	for _, m := range doc.Merged {
		if m == machine {
			return true, nil
		}
	}
	return false, nil
}
