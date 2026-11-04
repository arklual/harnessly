package agentbrowser

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// ErrProfileUnavailable deliberately makes missing and unauthorized names indistinguishable.
var ErrProfileUnavailable = errors.New("Browser profile is unavailable to this chat")

// ProjectAccess names a project's default and additional permitted browser profiles.
type ProjectAccess struct {
	Default string   `json:"default"`
	Granted []string `json:"granted"`
}

type conversationAccess struct {
	Project  string   `json:"project"`
	Selected string   `json:"selected,omitempty"`
	Granted  []string `json:"granted,omitempty"`
}

type profileData struct {
	Profiles      map[string]Profile            `json:"profiles"`
	Projects      map[string]ProjectAccess      `json:"projects"`
	Conversations map[string]conversationAccess `json:"conversations"`
	NoSandbox     bool                          `json:"noSandbox"`
}

// ProfileStore keeps machine-local permissions separate from portable conversation records.
// Mutations publish only after an owner-only atomic write succeeds.
type ProfileStore struct {
	sizes profileSizes
	mu    sync.Mutex
	path  string
	data  profileData
	err   error
	cdp   string
}

// OpenProfiles reads existing metadata without starting Chrome or changing the legacy common directory.
// A corrupt or inaccessible registry fails closed until the runtime is restarted after repair.
func OpenProfiles(dataDir, cdp string) *ProfileStore {
	s := &ProfileStore{path: filepath.Join(dataDir, "agent-browser", "profiles.json"), cdp: cdp,
		data: profileData{Profiles: map[string]Profile{}, Projects: map[string]ProjectAccess{},
			Conversations: map[string]conversationAccess{}}}
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s
	}
	if err != nil {
		s.err = err
		return s
	}
	if err = json.Unmarshal(raw, &s.data); err != nil {
		s.err = err
		return s
	}
	if s.data.Profiles == nil || s.data.Projects == nil || s.data.Conversations == nil {
		s.err = errors.New("invalid browser profile registry")
		return s
	}
	for id, p := range s.data.Profiles {
		if !profileID.MatchString(id) || p.ID != id || (p.Kind != "managed" && p.Kind != "attached") {
			s.err = errors.New("invalid browser profile registry")
			break
		}
	}
	return s
}

// Err reports an unavailable registry without revealing any profile names.
func (s *ProfileStore) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return errors.New("Browser profile storage is unavailable")
	}
	return nil
}

func copyProfileData(d profileData) profileData {
	raw, _ := json.Marshal(d)
	var out profileData
	_ = json.Unmarshal(raw, &out)
	return out
}

func (s *ProfileStore) change(fn func(*profileData) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return errors.New("Browser profile storage is unavailable")
	}
	next := copyProfileData(s.data)
	if err := fn(&next); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err = writeProfiles(s.path, raw); err != nil {
		return errors.New("Cannot save browser profiles")
	}
	s.data = next
	return nil
}

func writeProfiles(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := host.PrivateDir(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".profiles-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func randomProfileID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "bp_" + hex.EncodeToString(b)
}

var profileColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func checkProfileFields(name, color string) error {
	if name == "" || utf8.RuneCountInString(name) > 120 || strings.ContainsAny(name, "\r\n\x00") {
		return errors.New("Profile name must contain 1 to 120 characters on one line")
	}
	if !profileColor.MatchString(color) {
		return errors.New("Profile color must be a six-digit hex color")
	}
	return nil
}

func addProfile(d *profileData, name, color string) (Profile, error) {
	name = strings.TrimSpace(name)
	if color == "" {
		color = "#6b7280"
	}
	if err := checkProfileFields(name, color); err != nil {
		return Profile{}, err
	}
	for _, p := range d.Profiles {
		if p.Name == name {
			return Profile{}, errors.New("A browser profile already has this name")
		}
	}
	p := Profile{ID: randomProfileID(), Name: name, Color: color, Kind: "managed", CreatedAt: time.Now().UnixMilli()}
	d.Profiles[p.ID] = p
	return p, nil
}

// Create adds a managed profile; its directory is created lazily by the browser host.
func (s *ProfileStore) Create(name, color string) (Profile, error) {
	var p Profile
	err := s.change(func(d *profileData) (err error) { p, err = addProfile(d, name, color); return err })
	return p, err
}

// Lookup returns a detached copy for manager configuration and the person's settings page.
func (s *ProfileStore) Lookup(id string) (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return Profile{}, false
	}
	p, ok := s.data.Profiles[id]
	p.UsedBy = slices.Clone(p.UsedBy)
	return p, ok
}

// ProfileSummary contains only information a conversation may learn about an allowed profile.
type ProfileSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Kind  string `json:"kind"`
}

func summary(p Profile) ProfileSummary {
	name := p.Name
	if p.ID == DefaultProfile && name == "Common" {
		name = locale.Pick("Common", "Общий")
	}
	return ProfileSummary{p.ID, name, p.Color, p.Kind}
}

func allowed(d *profileData, conv, project, id string) bool {
	if _, ok := d.Profiles[id]; !ok {
		return false
	}
	c := d.Conversations[conv]
	if c.Project == project && slices.Contains(c.Granted, id) {
		return true
	}
	if project == "" {
		return id == DefaultProfile
	}
	a := d.Projects[project]
	return a.Default == id || slices.Contains(a.Granted, id)
}

// Resolve chooses a permitted chat selection, then the project's lazily created default.
// Project identity comes from the runtime record, never an agent argument.
func (s *ProfileStore) Resolve(conv, project, projectName, title string) (Profile, error) {
	// Streaming panels resolve frequently. Persist usage at most once a minute per
	// chat, while permission changes are still observed on every call.
	s.mu.Lock()
	if s.err == nil {
		c, exists := s.data.Conversations[conv]
		id := c.Selected
		if !allowed(&s.data, conv, project, id) {
			id = DefaultProfile
			if project != "" {
				id = s.data.Projects[project].Default
			}
		}
		if p, ok := s.data.Profiles[id]; ok && exists && c.Project == project {
			for _, use := range p.UsedBy {
				if use.Conv == conv && use.Title == title && time.Now().UnixMilli()-use.At < time.Minute.Milliseconds() {
					p.UsedBy = slices.Clone(p.UsedBy)
					s.mu.Unlock()
					return p, nil
				}
			}
		}
	}
	s.mu.Unlock()
	var selected Profile
	err := s.change(func(d *profileData) error {
		c, exists := d.Conversations[conv]
		if !exists || c.Project != project {
			c = conversationAccess{Project: project}
		}
		d.Conversations[conv] = c
		id := c.Selected
		if !allowed(d, conv, project, id) {
			id = DefaultProfile
			if project != "" {
				id = d.Projects[project].Default
			}
		}
		if _, ok := d.Profiles[id]; !ok {
			if project == "" {
				kind := "managed"
				if s.cdp != "" {
					kind = "attached"
				}
				d.Profiles[DefaultProfile] = Profile{ID: DefaultProfile, Name: "Common", Color: "#6b7280", Kind: kind,
					CDP: s.cdp, CreatedAt: time.Now().UnixMilli()}
				id = DefaultProfile
			} else {
				base := secrets.CleanWhy(projectName)
				if runes := []rune(base); len(runes) > 100 {
					base = string(runes[:100])
				}
				if base == "" {
					base = "Project"
				}
				// Imported projects can share a display name; IDs never derive from that name.
				name := base
				for n := 2; ; n++ {
					duplicate := false
					for _, p := range d.Profiles {
						if p.Name == name {
							duplicate = true
							break
						}
					}
					if !duplicate {
						break
					}
					name = fmt.Sprintf("%s (%d)", base, n)
				}
				p, err := addProfile(d, name, "")
				if err != nil {
					return err
				}
				id = p.ID
				a := d.Projects[project]
				a.Default = id
				d.Projects[project] = a
			}
		}
		selected = d.Profiles[id]
		now := time.Now().UnixMilli()
		selected.LastUsedAt = now
		selected.UsedBy = slices.DeleteFunc(selected.UsedBy, func(u secrets.Use) bool { return u.Conv == conv })
		selected.UsedBy = append([]secrets.Use{{Conv: conv, Title: title, At: now}}, selected.UsedBy...)
		if len(selected.UsedBy) > 32 {
			selected.UsedBy = selected.UsedBy[:32]
		}
		d.Profiles[id] = selected
		return nil
	})
	return selected, err
}

// Allowed returns only profiles available to the conversation's current project and direct grants.
func (s *ProfileStore) Allowed(conv, project string) []ProfileSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []ProfileSummary{}
	if s.err != nil {
		return out
	}
	for id, p := range s.data.Profiles {
		if allowed(&s.data, conv, project, id) {
			out = append(out, summary(p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Select accepts only a permitted opaque ID. Names are resolved against Allowed, not the global registry.
func (s *ProfileStore) Select(conv, project, id string) error {
	return s.change(func(d *profileData) error {
		if !allowed(d, conv, project, id) {
			return ErrProfileUnavailable
		}
		c := d.Conversations[conv]
		if c.Project != project {
			c = conversationAccess{Project: project}
		}
		c.Selected = id
		d.Conversations[conv] = c
		return nil
	})
}

// Update changes person-visible metadata without changing the profile's disk identity.
func (s *ProfileStore) Update(id, name, color string) error {
	return s.change(func(d *profileData) error {
		p, ok := d.Profiles[id]
		if !ok {
			return ErrProfileUnavailable
		}
		name = strings.TrimSpace(name)
		if err := checkProfileFields(name, color); err != nil {
			return err
		}
		for other, q := range d.Profiles {
			if other != id && q.Name == name {
				return errors.New("A browser profile already has this name")
			}
		}
		p.Name, p.Color = name, color
		d.Profiles[id] = p
		return nil
	})
}

// SetProject changes the default and extra grants. An empty default means a new own profile on next use.
func (s *ProfileStore) SetProject(project string, a ProjectAccess) error {
	return s.change(func(d *profileData) error {
		if err := validateProjectAccess(d, project, a); err != nil {
			return err
		}
		a.Granted = slices.Compact(slices.Sorted(slices.Values(a.Granted)))
		d.Projects[project] = a
		for conv, c := range d.Conversations {
			if c.Project == project && !allowed(d, conv, project, c.Selected) {
				c.Selected = ""
				d.Conversations[conv] = c
			}
		}
		return nil
	})
}

// Grant is called only after the person answers an access request.
func (s *ProfileStore) Grant(conv, project, id, scope string) error {
	return s.change(func(d *profileData) error {
		if _, ok := d.Profiles[id]; !ok {
			return ErrProfileUnavailable
		}
		c := d.Conversations[conv]
		if c.Project != project {
			c = conversationAccess{Project: project}
		}
		switch scope {
		case "chat":
			if !slices.Contains(c.Granted, id) {
				c.Granted = append(c.Granted, id)
			}
		case "project":
			if project == "" {
				return errors.New("This chat has no project")
			}
			a := d.Projects[project]
			if !slices.Contains(a.Granted, id) {
				a.Granted = append(a.Granted, id)
			}
			d.Projects[project] = a
		default:
			return errors.New("Invalid access scope")
		}
		c.Selected = id
		d.Conversations[conv] = c
		return nil
	})
}

// ForgetConversation drops local choices and direct grants on deletion or project moves.
func (s *ProfileStore) ForgetConversation(conv string) error {
	return s.change(func(d *profileData) error { delete(d.Conversations, conv); return nil })
}

// ProjectOf returns the last observed project, so event delivery can detect moves without actor calls.
func (s *ProfileStore) ProjectOf(conv string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.data.Conversations[conv]
	return c.Project, ok
}

// RemoveProject drops bindings while leaving profile data available for manual removal.
func (s *ProfileStore) RemoveProject(project string) error {
	return s.change(func(d *profileData) error {
		delete(d.Projects, project)
		for conv, c := range d.Conversations {
			if c.Project == project {
				delete(d.Conversations, conv)
			}
		}
		return nil
	})
}

// Remove drops every reference after the manager has stopped and cleared the profile.
func (s *ProfileStore) Remove(id string) error {
	return s.change(func(d *profileData) error {
		delete(d.Profiles, id)
		for project, a := range d.Projects {
			if a.Default == id {
				a.Default = ""
			}
			a.Granted = slices.DeleteFunc(a.Granted, func(v string) bool { return v == id })
			d.Projects[project] = a
		}
		for conv, c := range d.Conversations {
			if c.Selected == id {
				c.Selected = ""
			}
			c.Granted = slices.DeleteFunc(c.Granted, func(v string) bool { return v == id })
			d.Conversations[conv] = c
		}
		return nil
	})
}

// Snapshot returns detached metadata for the authenticated settings page, never for agent tools.
func (s *ProfileStore) Snapshot() ([]Profile, map[string]ProjectAccess) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := copyProfileData(s.data)
	profiles := make([]Profile, 0, len(d.Profiles))
	for _, p := range d.Profiles {
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, d.Projects
}

// NoSandbox reports the explicit page preference used by managed Chrome startup.
func (s *ProfileStore) NoSandbox() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.data.NoSandbox }

// SetNoSandbox persists the person's choice; there is deliberately no agent tool for it.
func (s *ProfileStore) SetNoSandbox(value bool) error {
	return s.change(func(d *profileData) error { d.NoSandbox = value; return nil })
}

func validateProjectAccess(d *profileData, project string, a ProjectAccess) error {
	if project == "" {
		return errors.New("Project is required")
	}
	for _, id := range append(slices.Clone(a.Granted), a.Default) {
		if id != "" {
			if _, ok := d.Profiles[id]; !ok {
				return ErrProfileUnavailable
			}
		}
	}
	return nil
}

func selectedProfile(d *profileData, conv, project string) string {
	if id := d.Conversations[conv].Selected; allowed(d, conv, project, id) {
		return id
	}
	if project == "" {
		return DefaultProfile
	}
	return d.Projects[project].Default
}

func (s *ProfileStore) projectChanges(project string, access ProjectAccess) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateProjectAccess(&s.data, project, access); err != nil {
		return nil, err
	}
	next := copyProfileData(s.data)
	next.Projects[project] = access
	convs := []string{}
	for conv, c := range s.data.Conversations {
		if c.Project == project && selectedProfile(&s.data, conv, project) != selectedProfile(&next, conv, project) {
			convs = append(convs, conv)
		}
	}
	return convs, nil
}
