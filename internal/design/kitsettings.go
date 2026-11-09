package design

// A kit's settings: its maker may make it for viewing only, for everyone. While it is, nobody edits it, the
// maker included: the draft is not started, published or discarded (403), the projects do not move onto it, and the
// draft's project is for viewing only, so no write of the library reaches it. The maker turns it off again.

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// errKitReadOnly is what an edit of a kit for viewing only answers.
func errKitReadOnly() error {
	return kitErr(403, "The kit is for viewing only", "Кит только для просмотра")
}

// KitSettingsState is a kit's settings as a person sees them.
type KitSettingsState struct {
	ReadOnly bool `json:"readOnly"`
	// Owner: the person made the kit, so they may turn ReadOnly on and off.
	Owner bool `json:"owner"`
}

// KitOwner is the email of the kit's maker: the one recorded, or the library's owner.
func (l *Library) KitOwner(kit string) string {
	if s := designkit.ReadSettings(l.KitsDir(), kit); s.Owner != "" {
		return s.Owner
	}
	return l.ownerEmail()
}

// IsKitOwner is whether the person made the kit ("" is the machine's own person, who installed it).
func (l *Library) IsKitOwner(kit, person string) bool {
	return person == "" || strings.EqualFold(person, l.KitOwner(kit))
}

// KitReadOnly is whether the kit is for viewing only.
func (l *Library) KitReadOnly(kit string) bool {
	return designkit.ValidID(kit) && designkit.ReadSettings(l.KitsDir(), kit).ReadOnly
}

// KitSettingsFor are the kit's settings for a person.
func (l *Library) KitSettingsFor(kit, person string) KitSettingsState {
	return KitSettingsState{ReadOnly: l.KitReadOnly(kit), Owner: l.IsKitOwner(kit, person)}
}

// SetKitReadOnly turns the kit's viewing only on or off; only its maker may.
func (l *Library) SetKitReadOnly(kit, person string, on bool) error {
	if !designkit.ValidID(kit) {
		return kitErr(400, "Unknown kit", "Неизвестный кит")
	}
	if _, ok := designkit.Latest(l.KitsDir(), kit); !ok {
		return kitErr(404, "The kit is not installed", "Кит не установлен")
	}
	if !l.IsKitOwner(kit, person) {
		return kitErr(403, "Only the kit's maker changes this", "Это меняет только создатель кита")
	}
	m := l.lockOf("kitdraft\x00" + kit)
	m.Lock()
	defer m.Unlock()
	s := designkit.ReadSettings(l.KitsDir(), kit)
	if s.Owner == "" {
		s.Owner = l.KitOwner(kit)
	}
	s.ReadOnly = on
	if err := designkit.WriteSettings(l.KitsDir(), kit, s); err != nil {
		return err
	}
	readOnlyDrafts.forget()
	return nil
}

// readOnlyDrafts are the draft projects of the kits for viewing only, read again at most once a second: a role is
// asked on every request, the kits folder is not read for each.
var readOnlyDrafts = &draftCache{}

type draftCache struct {
	mu   sync.Mutex
	dir  string
	at   time.Time
	have map[string]bool
}

func (c *draftCache) forget() {
	c.mu.Lock()
	c.have = nil
	c.mu.Unlock()
}

func (c *draftCache) of(kitsDir string) map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.have != nil && c.dir == kitsDir && time.Since(c.at) < time.Second {
		return c.have
	}
	have := map[string]bool{}
	for _, k := range designkit.List(kitsDir) {
		if !designkit.ReadSettings(kitsDir, k.ID).ReadOnly {
			continue
		}
		if d, ok := designkit.ReadDraft(kitsDir, k.ID); ok {
			have[d.Project] = true
		}
	}
	c.dir, c.at, c.have = kitsDir, time.Now(), have
	return have
}

// capReadOnlyKit: the draft of a kit for viewing only is looked at, whatever else the person may do in it.
func (l *Library) capReadOnlyKit(id string, r Role) Role {
	if r.AtLeast(RoleEdit) && l.KitsDir() != "" && readOnlyDrafts.of(l.KitsDir())[id] {
		return RoleView
	}
	return r
}

// kitCache is how long a kit file may be kept: a version's for good, the draft's (a project's folder, outside the
// kits folder) only while it is the same.
func (l *Library) kitCache(file string) string {
	kits, err := filepath.EvalSymlinks(l.KitsDir())
	if err == nil && strings.HasPrefix(file, kits+string(filepath.Separator)) {
		return kitImmutable
	}
	return "no-cache"
}

// draftGate: the kit's draft is read on the kit screen by whoever may open the draft's project.
func (a *API) draftGate(r *http.Request, kit, version string) bool {
	if version != designkit.DraftVersion {
		return true
	}
	d, ok := a.L.liveDraft(kit)
	return ok && a.L.RoleFor(d.Project, a.PersonOf(r)).AtLeast(RoleView)
}

// refuseKitDraft answers 404 for the draft pseudo-version of a kit on the library server (/kit/<id>/draft/...,
// /kitv/<id>/draft/...) when there is no draft or the draft's project is not open to this server's person: the same
// files are its project's, and refuseBoard closes that project's board.
func (l *Library) refuseKitDraft(w http.ResponseWriter, kit, version string, head bool) bool {
	if version != designkit.DraftVersion {
		return false
	}
	d, ok := l.liveDraft(kit)
	if !ok {
		sendHTML(w, http.StatusNotFound, l.notFoundPage(), head)
		return true
	}
	return l.refuseBoard(w, d.Project, head)
}
