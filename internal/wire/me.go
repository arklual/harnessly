package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"unicode/utf8"
)

// PersonSettings are a person's settings (spec §5): the cluster keeps them in person_settings, a runtime on its own in
// <data>/v2/me.json. A patch replaces whole top-level keys.
type PersonSettings struct {
	UIVersion string `json:"uiVersion"` // legacy | next
	Locale    string `json:"locale"`    // ru | en
	Density   string `json:"density"`   // compact | full
	TextSize  string `json:"textSize"`  // s | m | l
	// NoticeColor is the colour of what pops up (notices, toasts): contrast — the other of the theme (dark on light, light
	// on dark); theme — the theme's own.
	NoticeColor    string         `json:"noticeColor"`
	EnterSends     bool           `json:"enterSends"`
	DefaultMachine *string        `json:"defaultMachine"`
	Notify         NotifySettings `json:"notify"`
	QuickPrompts   []string       `json:"quickPrompts"`
	// Keys are the shortcuts the person changed: an action's id to its shortcuts («Mod+Shift+KeyO»); an empty list
	// turns the action off.
	Keys  map[string][]string `json:"keys"`
	Voice VoiceSettings       `json:"voice"`
	// DayStartHour is the Moscow hour the person's day starts at (0–23): «today» of the spend is counted from it.
	DayStartHour int `json:"dayStartHour"`
}

// The colours of what pops up (PersonSettings.NoticeColor).
const (
	NoticeColorContrast = "contrast"
	NoticeColorTheme    = "theme"
)

// VoiceEngines are the recognition models a person may pick for dictation: the machine's own, the ChatGPT plan's, OpenAI's,
// Yandex's.
const (
	VoiceEngineLocal   = "local"
	VoiceEngineChatGPT = "chatgpt"
	VoiceEngineOpenAI  = "openai"
	VoiceEngineYandex  = "yandex"
)

// VoiceEngines is every recognition model, in the order Settings lists them.
var VoiceEngines = []string{VoiceEngineLocal, VoiceEngineChatGPT, VoiceEngineOpenAI, VoiceEngineYandex}

// VoiceSettings are the dictation's: which model recognizes and the person's own words.
type VoiceSettings struct {
	Engine string      `json:"engine"`
	Words  []VoiceWord `json:"words"`
}

// VoiceWord is a word the recognizer gets wrong: how it is written and, optionally, how it is heard.
type VoiceWord struct {
	Word  string   `json:"word"`
	Heard []string `json:"heard"`
}

// Limits of the dictionary.
const (
	MaxVoiceWords    = 200
	MaxVoiceWordLen  = 64
	MaxVoiceHeard    = 10
	maxVoiceHeardLen = 64
)

func validVoice(v VoiceSettings) bool {
	ok := slices.Contains(VoiceEngines, v.Engine) && v.Words != nil && len(v.Words) <= MaxVoiceWords
	for _, w := range v.Words {
		ok = ok && w.Word != "" && utf8.RuneCountInString(w.Word) <= MaxVoiceWordLen && w.Heard != nil && len(w.Heard) <= MaxVoiceHeard
		for _, h := range w.Heard {
			ok = ok && h != "" && utf8.RuneCountInString(h) <= maxVoiceHeardLen
		}
	}
	return ok
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
	return PersonSettings{UIVersion: "next", Locale: "ru", Density: "compact", TextSize: "m", NoticeColor: NoticeColorContrast, EnterSends: true,
		Notify:       NotifySettings{Types: []string{"turn-finished", "question", "permission"}},
		QuickPrompts: []string{}, Keys: map[string][]string{}, Voice: VoiceSettings{Engine: VoiceEngineLocal, Words: []VoiceWord{}},
		DayStartHour: DefaultDayStartHour}
}

// Limits of the settings a page may write.
const (
	MaxQuickPrompts   = 50
	MaxQuickPromptLen = 2000
	MaxNotifyTypes    = 32
	MaxKeyActions     = 200
	MaxKeysPerAction  = 8
	MaxSettingsBody   = 128 << 10
	// DefaultDayStartHour: the day starts at 03:00 in Moscow, 00:00 UTC.
	DefaultDayStartHour = 3
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
		// A setting newer than the record keeps its default.
		saved := Me{Settings: DefaultSettings()}
		// The file is the page's own record; one that does not read is as if the person changed nothing.
		if json.Unmarshal(raw, &saved) == nil && saved.Rev > 0 {
			me = saved
		}
	}
	NormalizeSettings(&me.Settings)
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

// NormalizeSettings brings a saved record up to the settings of today: what an older record lacks gets its default, and
// a retired value is read as its successor.
func NormalizeSettings(st *PersonSettings) {
	// A record from before the dictation settings has none of them.
	if st.Voice.Engine == "" {
		st.Voice = DefaultSettings().Voice
	}
	// Finished turns fold; «full» was the old default, written with every other setting, and no
	// page offers the switch yet: a saved «full» is that default. Goes when the settings get the switch.
	if st.Density == "full" {
		st.Density = "compact"
	}
	// A record from before the notices' colour: the default.
	if st.NoticeColor == "" {
		st.NoticeColor = NoticeColorContrast
	}
	// A record from before the shortcuts has none of them.
	if st.Keys == nil {
		st.Keys = map[string][]string{}
	}
}

// ApplySettingsPatch applies a patch ({"<setting>": value, …}) to the settings: ErrMeBadPatch when a key is not a
// setting, or a value is out of its type or its limits (the whole patch is refused).
func ApplySettingsPatch(cur PersonSettings, patch json.RawMessage) (PersonSettings, error) {
	return applySettingsPatch(cur, patch)
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
		case "noticeColor":
			ok = oneOf(raw, &next.NoticeColor, NoticeColorContrast, NoticeColorTheme)
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
		case "keys":
			var m map[string][]string
			ok = one(raw, &m) && m != nil && len(m) <= MaxKeyActions
			for id, chords := range m {
				ok = ok && validKeyAction(id) && chords != nil && len(chords) <= MaxKeysPerAction
				for _, c := range chords {
					ok = ok && validChord(c)
				}
			}
			next.Keys = m
		case "voice":
			var v VoiceSettings
			ok = one(raw, &v) && validVoice(v)
			next.Voice = v
		case "dayStartHour":
			var h int
			ok = one(raw, &h) && h >= 0 && h <= 23
			next.DayStartHour = h
		}
		if !ok {
			return cur, ErrMeBadPatch
		}
	}
	return next, nil
}

// validKeyAction: an action's id is «scope.name», as the page's list has them.
func validKeyAction(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.') {
			return false
		}
	}
	return true
}

// validChord: a shortcut is its modifiers (Mod, Ctrl, Alt, Shift) and a key's place, joined by «+».
func validChord(c string) bool {
	if c == "" || len(c) > 64 {
		return false
	}
	for _, r := range c {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+') {
			return false
		}
	}
	return true
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
	ServeMe(w, r, s.Me)
}

// meSettings serves PATCH /api/v2/me/settings {rev, patch}: {r: accepted, me} or refused{conflict | bad-request}.
func (s *Server) meSettings(w http.ResponseWriter, r *http.Request) {
	if s.Me == nil {
		refuse(w, RefuseNotFound)
		return
	}
	ServeMeSettings(w, r, s.Me)
}

// ServeMe answers GET /api/v2/me from a store; the runtime serves its own person with it, and the hub serves a hub
// user with a store of that user's settings.
func ServeMe(w http.ResponseWriter, r *http.Request, st MeStore) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	me, err := st.Me()
	if err != nil {
		refuse(w, RefuseInternal)
		return
	}
	writeJSON(w, http.StatusOK, me)
}

// ServeMeSettings answers PATCH /api/v2/me/settings {rev, patch} from a store: {r: accepted, me}, or refused with
// conflict (rev is not the current one) or bad-request.
func ServeMeSettings(w http.ResponseWriter, r *http.Request, st MeStore) {
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
	me, err := st.Patch(*body.Rev, body.Patch)
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
