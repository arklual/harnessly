package asks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// SettingsFile is the person's settings of asks, beside the asks.
const SettingsFile = "asks-settings.json"

// Contact is a person the agents may tag, by the name the agents use.
type Contact struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Username   string `json:"username,omitempty"`
	TelegramID int64  `json:"telegramId,omitempty"`
	// Me: the owner of this machine. An ask to them also reaches their browser.
	Me bool `json:"me,omitempty"`
	// CanApprove: may approve an approval that is not addressed to them.
	CanApprove bool `json:"canApprove,omitempty"`
	// Note is the person's hint to the agents: what to ask this person about.
	Note string `json:"note,omitempty"`
}

// Person is the contact as an addressee.
func (c Contact) Person() Person {
	return Person{Name: c.Name, Username: c.Username, TelegramID: c.TelegramID}
}

// Quiet are the hours when an ask is delivered without a sound.
type Quiet struct {
	Enabled bool   `json:"enabled"`
	From    string `json:"from"` // "22:00", the runtime's local time
	To      string `json:"to"`   // "09:00"
}

// Settings are the person's rules for asks on this machine.
type Settings struct {
	// Enabled: agents may ask people. On by default; nothing is sent while no bot has a chat.
	Enabled bool `json:"enabled"`
	// DefaultBot is the bot (its id) an ask goes through when the agent names none; "" is the first bot with a chat.
	DefaultBot string `json:"defaultBot"`
	// Bots are the bots asks may go through; empty is every bot with a chat.
	Bots []string `json:"bots"`
	// OnlyContacts: an agent may tag only the contacts below, not any @username.
	OnlyContacts bool      `json:"onlyContacts"`
	Contacts     []Contact `json:"contacts"`
	// DefaultTimeoutMin is how long an agent waits when it says nothing; MaxTimeoutMin the longest it may.
	DefaultTimeoutMin int `json:"defaultTimeoutMin"`
	MaxTimeoutMin     int `json:"maxTimeoutMin"`
	// PerHour is how many asks one conversation may send in an hour.
	PerHour int   `json:"perHour"`
	Quiet   Quiet `json:"quiet"`
	// ShowSource: the message names the conversation that asks.
	ShowSource bool `json:"showSource"`
	// BrowserNotify: an ask to the owner (a contact marked Me) also comes as a browser notification.
	BrowserNotify bool `json:"browserNotify"`
}

// Defaults are the settings of a machine where the person changed nothing.
func Defaults() Settings {
	return Settings{Enabled: true, Bots: []string{}, Contacts: []Contact{}, DefaultTimeoutMin: 60, MaxTimeoutMin: 24 * 60,
		PerHour: 10, Quiet: Quiet{From: "22:00", To: "09:00"}, ShowSource: true, BrowserNotify: true}
}

// Limits of the settings.
const (
	MaxContacts   = 100
	maxNote       = 500
	maxTimeoutMin = 7 * 24 * 60
)

// SettingsStore reads and writes the settings file.
type SettingsStore struct {
	Path string
	mu   sync.Mutex
}

var (
	settingsMu     sync.Mutex
	settingsStores = map[string]*SettingsStore{}
)

// OpenSettings is the process's one SettingsStore of a data folder.
func OpenSettings(dataDir string) *SettingsStore {
	path := filepath.Join(dataDir, "v2", SettingsFile)
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if s := settingsStores[path]; s != nil {
		return s
	}
	s := &SettingsStore{Path: path}
	settingsStores[path] = s
	return s
}

// Get is the settings, the defaults under what the person changed.
func (s *SettingsStore) Get() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Defaults()
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Defaults(), err
	}
	if out.Bots == nil {
		out.Bots = []string{}
	}
	if out.Contacts == nil {
		out.Contacts = []Contact{}
	}
	return out, nil
}

// Put checks and writes the settings; the stored ones come back.
func (s *SettingsStore) Put(in Settings) (Settings, error) {
	clean, err := Check(in)
	if err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return clean, writeFile(s.Path, clean)
}

// Learn fills in a contact's Telegram id once the person wrote under their @username, so they can be tagged even
// after they change it.
func (s *SettingsStore) Learn(p Person) {
	if p.Username == "" || p.TelegramID == 0 {
		return
	}
	set, err := s.Get()
	if err != nil {
		return
	}
	changed := false
	for i := range set.Contacts {
		c := &set.Contacts[i]
		if c.TelegramID == 0 && strings.EqualFold(c.Username, p.Username) {
			c.TelegramID, changed = p.TelegramID, true
		}
	}
	if changed {
		s.mu.Lock()
		_ = writeFile(s.Path, set)
		s.mu.Unlock()
	}
}

var (
	usernameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)
	clockRe    = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

// SettingsError names the field the page shows the refusal at.
type SettingsError struct{ Field string }

func (e *SettingsError) Error() string { return "asks: bad " + e.Field }

// CleanUsername is a pasted @username or t.me link as a bare username; "" when it is not one.
func CleanUsername(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://"), "t.me/")
	s = strings.TrimPrefix(s, "@")
	if !usernameRe.MatchString(s) {
		return ""
	}
	return s
}

// Check is the settings as they are stored, or what is wrong with them.
func Check(in Settings) (Settings, error) {
	out := in
	if out.DefaultTimeoutMin <= 0 || out.DefaultTimeoutMin > maxTimeoutMin {
		return Settings{}, &SettingsError{"defaultTimeoutMin"}
	}
	if out.MaxTimeoutMin < out.DefaultTimeoutMin || out.MaxTimeoutMin > maxTimeoutMin {
		return Settings{}, &SettingsError{"maxTimeoutMin"}
	}
	if out.PerHour <= 0 || out.PerHour > 120 {
		return Settings{}, &SettingsError{"perHour"}
	}
	if out.Quiet.Enabled && (!clockRe.MatchString(out.Quiet.From) || !clockRe.MatchString(out.Quiet.To)) {
		return Settings{}, &SettingsError{"quiet"}
	}
	if out.Bots == nil {
		out.Bots = []string{}
	}
	if len(out.Contacts) > MaxContacts {
		return Settings{}, &SettingsError{"contacts"}
	}
	contacts := make([]Contact, 0, len(out.Contacts))
	names := map[string]bool{}
	for i, c := range out.Contacts {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" || utf8.RuneCountInString(c.Name) > 64 || names[strings.ToLower(c.Name)] {
			return Settings{}, &SettingsError{fmt.Sprintf("contacts.%d.name", i)}
		}
		names[strings.ToLower(c.Name)] = true
		if c.Username != "" {
			if c.Username = CleanUsername(c.Username); c.Username == "" {
				return Settings{}, &SettingsError{fmt.Sprintf("contacts.%d.username", i)}
			}
		}
		if c.Username == "" && c.TelegramID == 0 {
			return Settings{}, &SettingsError{fmt.Sprintf("contacts.%d.username", i)}
		}
		c.Note = strings.TrimSpace(c.Note)
		if utf8.RuneCountInString(c.Note) > maxNote {
			return Settings{}, &SettingsError{fmt.Sprintf("contacts.%d.note", i)}
		}
		if c.ID == "" {
			c.ID = "pc_" + randomHex(5)
		}
		contacts = append(contacts, c)
	}
	out.Contacts = contacts
	return out, nil
}

func minutes(clock string) int {
	var h, m int
	_, _ = fmt.Sscanf(clock, "%d:%d", &h, &m)
	return h*60 + m
}

// InQuiet says whether t falls into the quiet hours (which may run over midnight).
func (q Quiet) InQuiet(t time.Time) bool {
	if !q.Enabled || !clockRe.MatchString(q.From) || !clockRe.MatchString(q.To) {
		return false
	}
	now, from, to := t.Hour()*60+t.Minute(), minutes(q.From), minutes(q.To)
	if from == to {
		return false
	}
	if from < to {
		return now >= from && now < to
	}
	return now >= from || now < to
}

// FindContact is a contact by its name (any case), its @username or its id.
func (s Settings) FindContact(ref string) (Contact, bool) {
	ref = strings.TrimSpace(ref)
	user := CleanUsername(ref)
	for _, c := range s.Contacts {
		if c.ID == ref || strings.EqualFold(c.Name, ref) || (user != "" && strings.EqualFold(c.Username, user)) {
			return c, true
		}
	}
	return Contact{}, false
}
