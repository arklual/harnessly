package sidewidget

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// The one core of `harnessly widget create|update|list|remove` and of the MCP tools of the same names: a widget's
// package is written under <data>/side-widgets/<name>/ (its spec beside it, so an update changes only what it is told)
// and installed as a dev install of that folder, which the page lists at once.

var (
	// ErrExists: create of a name that is a widget already (update it).
	ErrExists = errors.New("sidewidget: a widget of that name exists: update it")
	// ErrNotFound: update or remove of a name that is no widget.
	ErrNotFound = errors.New("sidewidget: no widget of that name")
)

// Installer is what the service needs of the registry.
type Installer interface {
	Install(dir string, opt apps.InstallOptions) (apps.Installed, apps.Problems, error)
	Uninstall(id string, purge bool, safe *secrets.Store) error
	List() ([]apps.Installed, error)
}

// Service makes, changes, lists and removes the widgets an agent makes.
type Service struct {
	Reg  Installer
	Root string // <data>/side-widgets
	Host string // the runtime's version, for the install's check
	Now  func() time.Time
}

// Info is a widget as list shows it.
type Info struct {
	Name    string   `json:"name"`
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Smart   string   `json:"smart,omitempty"`
	Every   int      `json:"every,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Size    string   `json:"size,omitempty"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// ErrBadName is a widget's name that is not one: it never reaches a path.
var ErrBadName = errors.New("sidewidget: a widget's name is lower case latin letters, digits and '-', up to 33")

// dir is a widget's folder, and only ever inside Root: the name is checked first, and the folder (and Root) are
// resolved through their links, so neither `../x`, an absolute path nor a link out of Root lets a write or a removal
// out of it.
func (s *Service) dir(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", ErrBadName
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	dir := filepath.Join(root, name)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if rel, err := filepath.Rel(root, dir); err != nil || rel != name {
		return "", ErrBadName
	}
	return dir, nil
}

func (s *Service) readSpec(name string) (Spec, bool) {
	dir, err := s.dir(name)
	if err != nil {
		return Spec{}, false
	}
	raw, err := os.ReadFile(filepath.Join(dir, "spec.json"))
	if err != nil {
		return Spec{}, false
	}
	var sp Spec
	return sp, json.Unmarshal(raw, &sp) == nil
}

// Patch is an update: only the fields given change.
type Patch struct {
	Title   *string   `json:"title,omitempty"`
	Icon    *string   `json:"icon,omitempty"`
	HTML    *string   `json:"html,omitempty"`
	CSS     *string   `json:"css,omitempty"`
	JS      *string   `json:"js,omitempty"`
	Size    *string   `json:"size,omitempty"`
	Refresh *int      `json:"refresh,omitempty"`
	Sources *[]string `json:"sources,omitempty"`
	Smart   *string   `json:"smart,omitempty"`
	Every   *int      `json:"every,omitempty"`
}

// Apply is the spec with the patch's fields.
func (p Patch) Apply(sp Spec) Spec {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&sp.Title, p.Title)
	set(&sp.Icon, p.Icon)
	set(&sp.HTML, p.HTML)
	set(&sp.CSS, p.CSS)
	set(&sp.JS, p.JS)
	set(&sp.Size, p.Size)
	set(&sp.Smart, p.Smart)
	if p.Refresh != nil {
		sp.Refresh = *p.Refresh
	}
	if p.Every != nil {
		sp.Every = *p.Every
	}
	if p.Sources != nil {
		sp.Sources = *p.Sources
	}
	return sp
}

// Create makes a new widget and installs it.
func (s *Service) Create(sp Spec) (apps.Installed, error) {
	dir, err := s.dir(sp.Name)
	if err != nil {
		return apps.Installed{}, err
	}
	if _, ok := s.readSpec(sp.Name); ok {
		return apps.Installed{}, ErrExists
	}
	got, err := s.save(sp)
	if err != nil {
		// A widget refused leaves nothing behind: the agent makes it again, corrected, by the same name.
		_ = os.RemoveAll(dir)
	}
	return got, err
}

// Update changes a widget's fields and installs it again.
func (s *Service) Update(name string, p Patch) (apps.Installed, error) {
	if _, err := s.dir(name); err != nil {
		return apps.Installed{}, err
	}
	sp, ok := s.readSpec(name)
	if !ok {
		return apps.Installed{}, ErrNotFound
	}
	return s.save(p.Apply(sp))
}

func (s *Service) save(sp Spec) (apps.Installed, error) {
	m, files, err := Package(sp, s.now())
	if err != nil {
		return apps.Installed{}, err
	}
	dir, err := s.dir(sp.Name)
	if err != nil {
		return apps.Installed{}, err
	}
	manifest, err := ManifestJSON(m)
	if err != nil {
		return apps.Installed{}, err
	}
	// The folder is written anew: a file the last version had and this one has not is gone. The new one is made beside
	// it under a name of its own (never a path some link may already hold).
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return apps.Installed{}, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), "."+sp.Name+".new-")
	if err != nil {
		return apps.Installed{}, err
	}
	defer os.RemoveAll(tmp)
	specRaw, _ := json.MarshalIndent(sp, "", "  ")
	if _, probs := apps.ParseManifest(manifest); len(probs.Errors()) > 0 {
		return apps.Installed{}, probs.Errors()
	}
	files[apps.ManifestPath] = string(manifest)
	files["spec.json"] = string(specRaw)
	for path, body := range files {
		full := filepath.Join(tmp, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return apps.Installed{}, err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return apps.Installed{}, err
		}
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return apps.Installed{}, err
	}
	got, problems, err := s.Reg.Install(dir, apps.InstallOptions{Dev: true, HostVersion: s.Host, Consent: apps.ConsentAsk})
	if err != nil {
		if errs := problems.Errors(); len(errs) > 0 {
			return apps.Installed{}, errs
		}
		return apps.Installed{}, err
	}
	return got, nil
}

// List is every widget an agent made, by name.
func (s *Service) List() ([]Info, error) {
	installed, err := s.Reg.List()
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, a := range installed {
		name := NameOf(a.ID)
		if name == "" {
			continue
		}
		sp, _ := s.readSpec(name)
		out = append(out, Info{Name: name, ID: a.ID, Title: sp.Title, Smart: sp.Smart, Every: sp.Every, Sources: sp.Sources, Size: sp.Size})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Remove uninstalls a widget and forgets its folder.
func (s *Service) Remove(name string) error {
	name = strings.TrimSpace(name)
	dir, err := s.dir(name)
	if err != nil {
		return err
	}
	if err := s.Reg.Uninstall(ID(name), true, nil); err != nil {
		if errors.Is(err, apps.ErrNotInstalled) {
			return ErrNotFound
		}
		return err
	}
	return os.RemoveAll(dir)
}
