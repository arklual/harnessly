package wire

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

// SettingOwner is who a setting belongs to, and so where it is kept.
type SettingOwner string

const (
	// OwnerPerson: the setting is the person's. A hub keeps it by its user (person_settings) and answers for it
	// itself; a runtime on its own, with its one person, keeps it in <data>/v2/me.json.
	OwnerPerson SettingOwner = "person"
	// OwnerMachine: the setting is the machine's, the same for everyone who works on it. The machine keeps it; a hub
	// only passes it on, and may keep a copy to show while the machine is away.
	OwnerMachine SettingOwner = "machine"
)

// The documents of settings a page reads and writes under /api/v2.
const (
	SettingsDocMe      = "me"               // GET me, PATCH me/settings
	SettingsDocRuntime = "runtime-settings" // GET, PUT
	SettingsDocAgent   = "agent-settings"   // GET, PUT agent-settings/<provider>
)

// Setting is one top-level key of a settings document and its owner.
type Setting struct {
	Doc   string
	Key   string
	Owner SettingOwner
	// Runtime: the runtime itself works by this personal setting, so a hub hands the asking person's value to it with
	// the request (PersonHeader).
	Runtime bool
}

// Settings is every setting and its owner: the one table the hub and the runtime go by. A key of a settings document
// that is not here fails the tests.
var Settings = []Setting{
	{Doc: SettingsDocMe, Key: "uiVersion", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "locale", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "density", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "textSize", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "noticeColor", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "enterSends", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "defaultMachine", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "notify", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "quickPrompts", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "keys", Owner: OwnerPerson},
	{Doc: SettingsDocMe, Key: "voice", Owner: OwnerPerson, Runtime: true},
	{Doc: SettingsDocMe, Key: "dayStartHour", Owner: OwnerPerson},

	{Doc: SettingsDocRuntime, Key: "idleStop", Owner: OwnerMachine},
	{Doc: SettingsDocRuntime, Key: "limitContinue", Owner: OwnerMachine},
	{Doc: SettingsDocRuntime, Key: "restartContinue", Owner: OwnerMachine},
	{Doc: SettingsDocRuntime, Key: "delegation", Owner: OwnerMachine},

	{Doc: SettingsDocAgent, Key: "provider", Owner: OwnerMachine},
	{Doc: SettingsDocAgent, Key: "path", Owner: OwnerMachine},
	{Doc: SettingsDocAgent, Key: "rev", Owner: OwnerMachine},
	{Doc: SettingsDocAgent, Key: "account", Owner: OwnerMachine},
	{Doc: SettingsDocAgent, Key: "options", Owner: OwnerMachine},
	{Doc: SettingsDocAgent, Key: "rules", Owner: OwnerMachine},
	{Doc: SettingsDocAgent, Key: "env", Owner: OwnerMachine},
	{Doc: SettingsDocAgent, Key: "mcp", Owner: OwnerMachine},
}

// SettingsDocOf tells which settings document a path under /api/v2 is ("me", "me/settings", "runtime-settings",
// "agent-settings/claude"), and who owns it; ok is false for a path that is no settings document. A document has one
// owner: every key of it is that owner's.
func SettingsDocOf(v2path string) (doc string, owner SettingOwner, ok bool) {
	p := strings.Trim(v2path, "/")
	switch {
	case p == "me" || p == "me/settings":
		doc = SettingsDocMe
	case p == SettingsDocRuntime:
		doc = SettingsDocRuntime
	case strings.HasPrefix(p, SettingsDocAgent+"/"):
		doc = SettingsDocAgent
	default:
		return "", "", false
	}
	for _, s := range Settings {
		if s.Doc == doc {
			return doc, s.Owner, true
		}
	}
	return "", "", false
}

// PersonHeader carries, from a hub to a runtime, the settings of the person a request is made for that the runtime
// works by (RuntimePerson, as base64 of its JSON). A runtime on its own gets none and reads its file.
const PersonHeader = "X-Harnessly-Person-Settings"

// MaxPersonHeader bounds the header's value; words that do not fit are left out, last first.
const MaxPersonHeader = 48 << 10

// RuntimePerson is the part of a person's settings the runtime works by: the keys of Settings marked Runtime.
type RuntimePerson struct {
	Voice VoiceSettings `json:"voice"`
}

// RuntimePersonOf is the part of the settings a runtime is handed.
func RuntimePersonOf(st PersonSettings) RuntimePerson { return RuntimePerson{Voice: st.Voice} }

// EncodePersonHeader is the header's value for the settings, within MaxPersonHeader.
func EncodePersonHeader(p RuntimePerson) string {
	for {
		raw, err := json.Marshal(p)
		if err != nil {
			return ""
		}
		if v := base64.RawURLEncoding.EncodeToString(raw); len(v) <= MaxPersonHeader {
			return v
		}
		if len(p.Voice.Words) == 0 {
			return ""
		}
		p.Voice.Words = p.Voice.Words[:len(p.Voice.Words)/2]
	}
}

// personOf is the settings the request's person works with: what the hub handed with the request, else the runtime's
// own record. ok is false when there are none to read.
func (s *Server) personOf(r *http.Request) (RuntimePerson, bool) {
	if v := r.Header.Get(PersonHeader); v != "" && len(v) <= MaxPersonHeader {
		var p RuntimePerson
		if raw, err := base64.RawURLEncoding.DecodeString(v); err == nil && json.Unmarshal(raw, &p) == nil && validVoice(p.Voice) {
			return p, true
		}
	}
	if s.Me == nil {
		return RuntimePerson{}, false
	}
	me, err := s.Me.Me()
	if err != nil {
		return RuntimePerson{}, false
	}
	return RuntimePersonOf(me.Settings), true
}

// RuntimeSettingsFor is the runtime's settings as a page with caps reads them.
func RuntimeSettingsFor(s RuntimeSettings, caps Caps) RuntimeSettings { return settingsFor(s, caps) }
