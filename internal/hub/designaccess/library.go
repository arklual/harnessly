package designaccess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
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
	lister, ok := s.view.(Librarian)
	if !ok || m.SpaceID == "" {
		return fail("not_found", "Хранилище дизайнов не подключено")
	}
	ctx := r.Context()
	t, err := s.read(ctx, m)
	if err != nil {
		return err
	}
	spaceID := m.SpaceID
	email := normEmail(u.Email)
	rows, err := t.projects()
	if err != nil {
		return err
	}
	recorded := make(map[string]project, len(rows))
	also := make([]string, 0, len(rows))
	for _, p := range rows {
		recorded[p.ID] = p
		also = append(also, p.ID)
	}
	items, err := lister.Library(ctx, spaceID, also)
	if err != nil {
		return err
	}
	out := storeLibrary{Artifacts: []storeArtifact{}, Missing: libraryMissing,
		Layout: storeLayout{Folders: []storeFolder{}, Placed: map[string]string{}, Systems: map[string]any{}}}
	seen := map[string]bool{}
	for _, it := range items {
		// Only a project the hub has a row for, as the store's reads through the hub (team.go): an unrecorded one is
		// still the machines', and its access is theirs to tell.
		p, ok := recorded[it.ID]
		if !ok {
			continue
		}
		f, err := t.fold(p, email)
		if err != nil {
			return err
		}
		// A project closed to the person is not there for them.
		if f.Role == "" {
			continue
		}
		title := it.Title
		if title == "" {
			title = it.ID
		}
		a := storeArtifact{ID: it.ID, Title: title, UpdatedAt: it.UpdatedAt, Kind: it.Kind, Boards: it.Boards, Convs: []string{}, Role: f.Role}
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
		seen[it.ID] = true
		out.Artifacts = append(out.Artifacts, a)
	}
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
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
	return nil
}
