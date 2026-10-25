package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"
)

// PersonSettings are a person's settings (spec §5): the cluster keeps them in person_settings, a runtime on its own in
// <data>/v2/me.json. A patch replaces whole top-level keys.
type PersonSettings struct {
	UIVersion      string         `json:"uiVersion"` // legacy | next
	Locale         string         `json:"locale"`    // ru | en
	Density        string         `json:"density"`   // compact | full
	TextSize       string         `json:"textSize"`  // s | m | l
	EnterSends     bool           `json:"enterSends"`
	DefaultMachine *string        `json:"defaultMachine"`
	Notify         NotifySettings `json:"notify"`
	QuickPrompts   []string       `json:"quickPrompts"`
}

// NotifySettings: which events notify, with a sound, and the do-not-disturb switch.
type NotifySettings struct {
	Types []string `json:"types"`
	Sound bool     `json:"sound"`
	DND   bool     `json:"dnd"`
}

// Me is GET /api/v2/me: the person and their settings at a revision. A runtime on its own has one person, with no
// e-mail.
type Me struct {
	Email    *string        `json:"email"`
	Name     *string        `json:"name"`
	Settings PersonSettings `json:"settings"`
	Rev      int64          `json:"rev"`
}

// DefaultSettings are the settings of a person who changed nothing.
func DefaultSettings() PersonSettings {
	return PersonSettings{UIVersion: "next", Locale: "ru", Density: "compact", TextSize: "m", EnterSends: true,
		Notify:       NotifySettings{Types: []string{"turn-finished", "question", "permission"}},
		QuickPrompts: []string{}}
}

// Limits of the settings a page may write.
const (
	MaxQuickPrompts   = 50
	MaxQuickPromptLen = 2000
	MaxNotifyTypes    = 32
	MaxSettingsBody   = 128 << 10
)

// MeStore keeps the person of a runtime on its own.
type MeStore interface {
	Me() (Me, error)
	// Patch applies a patch to the settings at rev: ErrMeConflict when rev is not the current revision,
	// ErrMeBadPatch when the patch is not settings.
	Patch(rev int64, patch json.RawMessage) (Me, error)
}

var (
	ErrMeConflict = errors.New("wire: the settings are at another revision")
	ErrMeBadPatch = errors.New("wire: not a settings patch")
)

// FileMe is a MeStore in a file; Name is shown as the person's name.
type FileMe struct {
	Path string
	Name string

	mu sync.Mutex
}

func (f *FileMe) load() Me {
	me := Me{Settings: DefaultSettings(), Rev: 1}
	if raw, err := os.ReadFile(f.Path); err == nil {
		var saved Me
		// The file is the page's own record; one that does not read is as if the person changed nothing.
		if json.Unmarshal(raw, &saved) == nil && saved.Rev > 0 {
			me = saved
		}
	}
	// Finished turns fold; «full» was the old default, written with every other setting, and no
	// page offers the switch yet: a saved «full» is that default. Goes when the settings get the switch.
	if me.Settings.Density == "full" {
		me.Settings.Density = "compact"
	}
	me.Email = nil
	if f.Name != "" {
		n := f.Name
		me.Name = &n
	}
	return me
}

func (f *FileMe) Me() (Me, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.load(), nil
}

func (f *FileMe) Patch(rev int64, patch json.RawMessage) (Me, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	me := f.load()
	if rev != me.Rev {
		return me, ErrMeConflict
	}
	next, err := applySettingsPatch(me.Settings, patch)
	if err != nil {
		return me, err
	}
	me.Settings, me.Rev = next, me.Rev+1
	raw, err := json.Marshal(me)
	if err != nil {
		return me, err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return me, err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return me, err
	}
	return me, os.Rename(tmp, f.Path)
}

// applySettingsPatch replaces the keys the patch names; a key that is not a setting, or a value out of its type or its
// limits, refuses the whole patch.
func applySettingsPatch(cur PersonSettings, patch json.RawMessage) (PersonSettings, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(patch, &keys); err != nil || keys == nil {
		return cur, ErrMeBadPatch
	}
	one := func(raw json.RawMessage, v any) bool {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		return dec.Decode(v) == nil
	}
	oneOf := func(raw json.RawMessage, to *string, allowed ...string) bool {
		var s string
		if !one(raw, &s) {
			return false
		}
		for _, a := range allowed {
			if s == a {
				*to = s
				return true
			}
		}
		return false
	}
	next := cur
	for k, raw := range keys {
		ok := false
		switch k {
		case "uiVersion":
			ok = oneOf(raw, &next.UIVersion, "legacy", "next")
		case "locale":
			ok = oneOf(raw, &next.Locale, "ru", "en")
		case "density":
			ok = oneOf(raw, &next.Density, "compact", "full")
		case "textSize":
			ok = oneOf(raw, &next.TextSize, "s", "m", "l")
		case "enterSends":
			ok = one(raw, &next.EnterSends)
		case "defaultMachine":
			var m *string
			ok = one(raw, &m) && (m == nil || ValidMachineID(*m))
			next.DefaultMachine = m
		case "notify":
			var n NotifySettings
			ok = one(raw, &n) && len(n.Types) <= MaxNotifyTypes && n.Types != nil
			next.Notify = n
		case "quickPrompts":
			var q []string
			ok = one(raw, &q) && q != nil && len(q) <= MaxQuickPrompts
			for _, p := range q {
				ok = ok && p != "" && utf8.RuneCountInString(p) <= MaxQuickPromptLen
			}
			next.QuickPrompts = q
		}
		if !ok {
			return cur, ErrMeBadPatch
		}
	}
	return next, nil
}

// ValidMachineID: a machine id is a short token, not a path or a sentence.
func ValidMachineID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// me serves GET /api/v2/me.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	if s.Me == nil {
		refuse(w, RefuseNotFound)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	me, err := s.Me.Me()
	if err != nil {
		refuse(w, RefuseInternal)
		return
	}
	writeJSON(w, http.StatusOK, me)
}

// meSettings serves PATCH /api/v2/me/settings {rev, patch}: {r: accepted, me} or refused{conflict | bad-request}.
func (s *Server) meSettings(w http.ResponseWriter, r *http.Request) {
	if s.Me == nil {
		refuse(w, RefuseNotFound)
		return
	}
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, MaxSettingsBody+1))
	var body struct {
		Rev   *int64          `json:"rev"`
		Patch json.RawMessage `json:"patch"`
	}
	if err != nil || len(raw) > MaxSettingsBody || json.Unmarshal(raw, &body) != nil || body.Rev == nil || body.Patch == nil {
		refuse(w, RefuseBadRequest)
		return
	}
	me, err := s.Me.Patch(*body.Rev, body.Patch)
	switch {
	case errors.Is(err, ErrMeConflict):
		refuse(w, RefuseConflict)
	case errors.Is(err, ErrMeBadPatch):
		refuse(w, RefuseBadRequest)
	case err != nil:
		refuse(w, RefuseInternal)
	default:
		writeJSON(w, http.StatusOK, struct {
			R  ResultKind `json:"r"`
			Me Me         `json:"me"`
		}{ResultAccepted, me})
	}
}
