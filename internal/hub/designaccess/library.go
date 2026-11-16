package designaccess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The person's library from the hub's store, for the page while their machine is off or slow:
// GET <platform api>/design/store-library under any machine of the space the person may open (it only names the
// space). The projects are the ones the person has a role in, as design access has it, in the shape the machine's
// v2 library gives them (wire.DesignArtifact); the shelves are the person's own layout file in `.folders/`. What only
// the machine knows — the library server's port, the chats that drew a project, the agents and claims on it, its
// checks, the pins — is not there, and `missing` names it.

// LibraryItem is a project of a space's library as the store has it at its newest revision (Rev).
type LibraryItem struct {
	ID        string
	Title     string
	UpdatedAt int64 // ms
	Kind      string
	Boards    []LibraryBoard
	Rev       int64
}

// LibraryBoard is a board of a listed project.
type LibraryBoard struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

// Coverer is the part of a Viewer that gives the cover pictures kept in the store (designview.View).
type Coverer interface {
	// CoverVersions are the projects of the space that have a cover, each with the cover's sha.
	CoverVersions(ctx context.Context, spaceID string) (map[string]string, error)
	// Cover is a project's cover, at most limit bytes; nil when there is none.
	Cover(ctx context.Context, spaceID, project string, limit int64) ([]byte, error)
}

const storeCoverPath = "/design/store-cover"

// maxCover caps a cover the hub serves.
const maxCover = 1 << 20

// coverURL is where the library points a project's cover: a route of the hub under the same access as the library.
func coverURL(project, sha string) string {
	return "/api/platform" + storeCoverPath + "?project=" + project + "&v=" + sha[:12]
}

// Librarian is the part of a Viewer that lists a space's library (designview.View).
type Librarian interface {
	// Library lists the projects named of the space's library that the store has files of, newest first.
	Library(ctx context.Context, spaceID string, ids []string) ([]LibraryItem, error)
	// ReadFile is a file of the library at its newest revision, at most limit bytes; nil when there is none.
	ReadFile(ctx context.Context, spaceID, rel string, limit int64) ([]byte, error)
}

type storeOwner struct {
	Name string `json:"name"`
	Me   bool   `json:"me"`
}

type storeArtifact struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	UpdatedAt   int64          `json:"updatedAt"`
	Kind        string         `json:"kind"`
	Pinned      bool           `json:"pinned"`
	Cover       *string        `json:"cover"`
	ProjectPath string         `json:"projectPath"`
	Boards      []LibraryBoard `json:"boards"`
	Convs       []string       `json:"convs"`
	Owner       *storeOwner    `json:"owner,omitempty"`
	Role        string         `json:"role,omitempty"`
	// WorkMachine is the machine whose folder holds the working copy (hub-design-owner).
	WorkMachine string `json:"workMachine,omitempty"`
}

type storeFolder struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"parent"`
}

type storeLayout struct {
	Folders []storeFolder     `json:"folders"`
	Placed  map[string]string `json:"placed"`
	Systems map[string]any    `json:"systems"`
}

type storeLibrary struct {
	Artifacts []storeArtifact `json:"artifacts"`
	Layout    storeLayout     `json:"layout"`
	// Missing is what the store cannot tell, the machine's alone.
	Missing []string `json:"missing"`
}

// libraryMissing: the library server (its port and the boards' pages), the chats, the agents and claims, the checks, the pins.
var libraryMissing = []string{"server", "chats", "agents", "checks", "pins"}

const storeLibraryPath = "/design/store-library"

var folderIDRe = regexp.MustCompile(`^f-[0-9a-f]{8}$`)

// layoutKey names a person's layout file in .folders/ as the runtime does (internal/design/folders.go).
func layoutKey(email string) string {
	sum := sha256.Sum256([]byte("person:" + normEmail(email)))
	return hex.EncodeToString(sum[:16])
}

func (s *Service) libraryRoute(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) error {
	out, err := s.storeLibraryOf(r.Context(), u, m)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
	return nil
}

// storeLibraryOf is the person's library and shelves as the hub's store has them (the store-library answer).
func (s *Service) storeLibraryOf(ctx context.Context, u store.User, m store.Machine) (storeLibrary, error) {
	lister, ok := s.currentView().(Librarian)
	if !ok || m.SpaceID == "" {
		return storeLibrary{}, fail("store_off", "Хранилище дизайнов не подключено")
	}
	t, err := s.read(ctx, m)
	if err != nil {
		return storeLibrary{}, err
	}
	spaceID := m.SpaceID
	email := normEmail(u.Email)
	rows, err := t.projects()
	if err != nil {
		return storeLibrary{}, err
	}
	// Only the projects open to the person are read from the store: a project closed to them is not there
	// for them, and reading the whole space's library for one person's list is what made a cold hub too slow to answer.
	recorded := make(map[string]project, len(rows))
	roles := make(map[string]string, len(rows))
	also := make([]string, 0, len(rows))
	for _, p := range rows {
		f, err := t.fold(p, email)
		if err != nil {
			return storeLibrary{}, err
		}
		if f.Role == "" {
			continue
		}
		recorded[p.ID] = p
		roles[p.ID] = f.Role
		also = append(also, p.ID)
	}
	items, err := lister.Library(ctx, spaceID, also)
	if err != nil {
		return storeLibrary{}, err
	}
	out := storeLibrary{Artifacts: []storeArtifact{}, Missing: libraryMissing,
		Layout: storeLayout{Folders: []storeFolder{}, Placed: map[string]string{}, Systems: map[string]any{}}}
	seen := map[string]bool{}
	hubOwns := s.OwnerOn(ctx, m)
	var covers map[string]string
	if c, ok := s.currentView().(Coverer); ok {
		covers, _ = c.CoverVersions(ctx, spaceID)
	}
	for _, it := range items {
		// Only a project the hub has a row for, as the store's reads through the hub (team.go): an unrecorded one is
		// still the machines', and its access is theirs to tell.
		p, ok := recorded[it.ID]
		if !ok {
			continue
		}
		title := it.Title
		if title == "" {
			title = it.ID
		}
		a := storeArtifact{ID: it.ID, Title: title, UpdatedAt: it.UpdatedAt, Kind: it.Kind, Boards: it.Boards, Convs: []string{}, Role: roles[it.ID]}
		if a.Boards == nil {
			a.Boards = []LibraryBoard{}
		}
		if p.Owner != "" {
			name := p.OwnerName
			if name == "" {
				name, _, _ = strings.Cut(p.Owner, "@")
			}
			a.Owner = &storeOwner{Name: name, Me: p.Owner == email}
		}
		if hubOwns {
			if mr, err := t.metaOf(it.ID); err == nil {
				a.WorkMachine = mr.WorkMachine
				if mr.Title != "" {
					a.Title = mr.Title
				}
				if mr.Preview != "" {
					p := mr.Preview
					a.Cover = &p
				}
			}
		}
		if sha := covers[it.ID]; sha != "" && a.Cover == nil {
			u := coverURL(it.ID, sha)
			a.Cover = &u
		}
		seen[it.ID] = true
		out.Artifacts = append(out.Artifacts, a)
	}
	// The hub's own rows: a recorded project the person has a role in is in their library whether or not the store has
	// its files and whether or not its machine is on.
	if hubOwns {
		for _, id := range also {
			if seen[id] {
				continue
			}
			p, ok := recorded[id]
			if !ok {
				continue
			}
			mr, err := t.metaOf(id)
			if err != nil {
				continue
			}
			a := storeArtifact{ID: id, Title: mr.Title, UpdatedAt: mr.UpdatedAt, Kind: libraryKind(mr.Kind), Boards: []LibraryBoard{}, Convs: []string{}, Role: roles[id], WorkMachine: mr.WorkMachine}
			if a.Title == "" {
				a.Title = id
			}
			if mr.Preview != "" {
				pv := mr.Preview
				a.Cover = &pv
			}
			if p.Owner != "" {
				name := p.OwnerName
				if name == "" {
					name, _, _ = strings.Cut(p.Owner, "@")
				}
				a.Owner = &storeOwner{Name: name, Me: p.Owner == email}
			}
			seen[id] = true
			out.Artifacts = append(out.Artifacts, a)
		}
	}
	sort.SliceStable(out.Artifacts, func(i, j int) bool { return out.Artifacts[i].UpdatedAt > out.Artifacts[j].UpdatedAt })
	// The person's shelves: their own layout file, with the projects they cannot see left out.
	if raw, err := lister.ReadFile(ctx, spaceID, ".folders/"+layoutKey(email)+".json", 1<<20); err == nil && raw != nil {
		var f struct {
			Folders []storeFolder     `json:"folders"`
			Placed  map[string]string `json:"placed"`
		}
		if json.Unmarshal(raw, &f) == nil {
			known := map[string]bool{}
			for _, fo := range f.Folders {
				if folderIDRe.MatchString(fo.ID) && fo.Name != "" {
					known[fo.ID] = true
					out.Layout.Folders = append(out.Layout.Folders, fo)
				}
			}
			for p, fo := range f.Placed {
				if known[fo] && seen[p] {
					out.Layout.Placed[p] = fo
				}
			}
		}
	}
	return out, nil
}

// libraryKind is a kind of the library's own vocabulary (wire.DesignKind): a kit or a document type the page does not list
// among projects, and a kind the hub has no word for, are shown as a site.
func libraryKind(k string) string {
	switch k {
	case "slides", "deck":
		return "slides"
	case "ds", "video":
		return k
	}
	return "site"
}

// coverRoute is GET <platform>/design/store-cover?project=<id>: the cover of a project the person may open.
func (s *Service) coverRoute(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) error {
	c, ok := s.view.(Coverer)
	id := r.URL.Query().Get("project")
	if !ok || m.SpaceID == "" || !projectIDRe.MatchString(id) {
		return fail("not_found", "Обложки нет")
	}
	t, err := s.read(r.Context(), m)
	if err != nil {
		return err
	}
	rows, err := t.projects()
	if err != nil {
		return err
	}
	for _, p := range rows {
		if p.ID != id {
			continue
		}
		f, err := t.fold(p, normEmail(u.Email))
		if err != nil {
			return err
		}
		if f.Role == "" {
			break
		}
		b, err := c.Cover(r.Context(), m.SpaceID, id, maxCover)
		if err != nil {
			return err
		}
		if len(b) == 0 {
			break
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(b)
		return nil
	}
	return fail("not_found", "Обложки нет")
}
