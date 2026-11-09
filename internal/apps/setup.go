package apps

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// What a person sets up when they install an app, and again in «О приложении»: the settings its manifest describes with a
// JSON Schema (the host draws the form), and the secrets it asks for (the person puts them into the machine's safe, off
// the chat). Settings are kept in the install area of the app's storage under SettingsKey; a view reads them with
// hl.settings.get() and the agent with the host tool settings_get. A secret's value reaches neither: the machine fills it
// into the headers and environment of the app's MCP servers when a chat starts (launch_secrets.go), and nowhere else.

// SettingsKey is the key of the install area that holds the settings' values. A view cannot write it through
// hl.storage: the form is the way (the host checks the values against the schema).
const SettingsKey = "_settings"

// SettingField is one setting as the form draws it.
type SettingField struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Enum        []any    `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Min         *float64 `json:"minimum,omitempty"`
	Max         *float64 `json:"maximum,omitempty"`
}

type settingsSchema struct {
	Properties map[string]struct {
		Type        string   `json:"type"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Enum        []any    `json:"enum"`
		Default     any      `json:"default"`
		Minimum     *float64 `json:"minimum"`
		Maximum     *float64 `json:"maximum"`
		MinLength   *int     `json:"minLength"`
		MaxLength   *int     `json:"maxLength"`
	} `json:"properties"`
	Required []string `json:"required"`
}

func (m *Manifest) settingsSchema() (settingsSchema, bool) {
	var sc settingsSchema
	if len(m.Settings) == 0 || json.Unmarshal(m.Settings, &sc) != nil {
		return sc, false
	}
	return sc, true
}

// SettingFields are the manifest's settings in the order of their names; nil for an app with none.
func (m *Manifest) SettingFields() []SettingField {
	sc, ok := m.settingsSchema()
	if !ok {
		return nil
	}
	var out []SettingField
	for name, p := range sc.Properties {
		title := p.Title
		if title == "" {
			title = name
		}
		out = append(out, SettingField{Name: name, Type: p.Type, Title: title, Description: p.Description, Enum: p.Enum,
			Default: p.Default, Required: contains(sc.Required, name), Min: p.Minimum, Max: p.Maximum})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CheckSettings judges values against the manifest's schema: a key it does not describe, a value of another type, one
// outside an enum, a range or a length is a problem, as is a required key left out. The problems are by key.
func (m *Manifest) CheckSettings(values map[string]any) map[string]string {
	sc, ok := m.settingsSchema()
	if !ok {
		if len(values) > 0 {
			return map[string]string{"": "the app has no settings"}
		}
		return nil
	}
	bad := map[string]string{}
	for k, v := range values {
		p, known := sc.Properties[k]
		if !known {
			bad[k] = "not a setting of the app"
			continue
		}
		if why := checkSettingValue(p.Type, p.Enum, p.Minimum, p.Maximum, p.MinLength, p.MaxLength, v); why != "" {
			bad[k] = why
		}
	}
	for _, name := range sc.Required {
		if _, has := values[name]; !has {
			if p := sc.Properties[name]; p.Default == nil {
				bad[name] = "is required"
			}
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return bad
}

func checkSettingValue(typ string, enum []any, min, max *float64, minLen, maxLen *int, v any) string {
	switch typ {
	case "string":
		s, ok := v.(string)
		if !ok {
			return "must be a text"
		}
		if minLen != nil && len([]rune(s)) < *minLen {
			return fmt.Sprintf("must be at least %d characters", *minLen)
		}
		if maxLen != nil && len([]rune(s)) > *maxLen {
			return fmt.Sprintf("must be at most %d characters", *maxLen)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return "must be true or false"
		}
	case "number", "integer":
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return "must be a number"
		}
		if typ == "integer" && n != math.Trunc(n) {
			return "must be a whole number"
		}
		if min != nil && n < *min {
			return fmt.Sprintf("must be at least %v", *min)
		}
		if max != nil && n > *max {
			return fmt.Sprintf("must be at most %v", *max)
		}
	default:
		return "this type of setting is not supported (string, number, integer, boolean)"
	}
	if len(enum) > 0 {
		for _, e := range enum {
			if e == v {
				return ""
			}
		}
		return "is not one of the allowed values"
	}
	return ""
}

// Settings are the app's settings values: what the person saved, over the schema's defaults. Keys the schema no longer
// describes are dropped, so an update that removes a setting does not leave it for the app to read.
func (s *Storage) Settings(m *Manifest, app string) (map[string]any, error) {
	out := map[string]any{}
	sc, ok := m.settingsSchema()
	if !ok {
		return out, nil
	}
	for name, p := range sc.Properties {
		if p.Default != nil {
			out[name] = p.Default
		}
	}
	raw, has, err := s.Get(app, ScopeInstall, "", SettingsKey)
	if err != nil {
		return nil, err
	}
	if has {
		var saved map[string]any
		if json.Unmarshal(raw, &saved) == nil {
			for k, v := range saved {
				if _, known := sc.Properties[k]; known {
					out[k] = v
				}
			}
		}
	}
	return out, nil
}

// SaveSettings keeps values (all of the app's settings, as the form sends them) once they pass CheckSettings. The
// returned problems are by key; the error is the storage's.
func (s *Storage) SaveSettings(m *Manifest, app string, values map[string]any) (map[string]string, error) {
	merged := map[string]any{}
	if sc, ok := m.settingsSchema(); ok {
		for name, p := range sc.Properties {
			if p.Default != nil {
				merged[name] = p.Default
			}
		}
	}
	for k, v := range values {
		merged[k] = v
	}
	if bad := m.CheckSettings(merged); bad != nil {
		return bad, nil
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return nil, s.Set(app, ScopeInstall, "", SettingsKey, raw)
}

// SecretPlaceholder finds ${secret:<name>} in a text of an MCP server's config.
var secretPlaceholder = regexp.MustCompile(`\$\{secret:([A-Za-z0-9][A-Za-z0-9._-]{0,63})\}`)

// SecretsIn are the names of the secrets a text names with ${secret:<name>}.
func SecretsIn(s string) []string {
	var out []string
	for _, m := range secretPlaceholder.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// FillSecrets puts the secrets' values into a text; a name with no value is left as written and reported.
func FillSecrets(s string, values map[string]string) (string, []string) {
	var missing []string
	out := secretPlaceholder.ReplaceAllStringFunc(s, func(ph string) string {
		name := secretPlaceholder.FindStringSubmatch(ph)[1]
		v, ok := values[name]
		if !ok {
			missing = append(missing, name)
			return ph
		}
		return v
	})
	return out, missing
}

// ServerSecrets are the names of the secrets a server of .mcp.json uses: in its headers and, for a local one, in its
// environment.
func (s MCPServer) Secrets() []string {
	var out []string
	add := func(v string) {
		for _, n := range SecretsIn(v) {
			if !contains(out, n) {
				out = append(out, n)
			}
		}
	}
	for _, v := range s.Headers {
		add(v)
	}
	for _, v := range s.Env {
		add(v)
	}
	sort.Strings(out)
	return out
}

// Filled is the server with its secrets in place; missing are the names that had no value. The result is for the
// machine's config files only (0600), never for a view, a log or the agent's context.
func (s MCPServer) Filled(values map[string]string) (MCPServer, []string) {
	var missing []string
	fill := func(in map[string]string) map[string]string {
		if in == nil {
			return nil
		}
		out := make(map[string]string, len(in))
		for k, v := range in {
			f, miss := FillSecrets(v, values)
			out[k] = f

			missing = append(missing, miss...)
		}
		return out
	}
	for _, v := range s.Headers {
		if strings.Contains(v, "${secret:") {
			s.Credentialed = true
		}
	}
	s.Headers = fill(s.Headers)
	s.Env = fill(s.Env)
	return s, missing
}

// SecretState is one secret the app asks for, as «О приложении» shows it: whether it has a value, never the value.
type SecretState struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Optional    bool   `json:"optional"`
	Set         bool   `json:"set"`
}

// SecretStates are the manifest's secrets with whether each has a value; has says it by the secret's own name.
func (m *Manifest) SecretStates(has func(name string) bool) []SecretState {
	out := make([]SecretState, 0, len(m.Secrets))
	for _, s := range m.Secrets {
		out = append(out, SecretState{Name: s.Name, Description: s.Description, Optional: s.Optional, Set: has(s.Name)})
	}
	return out
}

// WaitingFor are the required secrets that have no value: while there are any, the app is «ждёт секрет». Its chat does
// not fail: the servers that use the missing secrets are left out of the launch, and the rest of the app works.
func (m *Manifest) WaitingFor(has func(name string) bool) []string {
	var out []string
	for _, s := range m.Secrets {
		if !s.Optional && !has(s.Name) {
			out = append(out, s.Name)
		}
	}
	return out
}

// checkSecretUse reports what the .mcp.json of a package gets wrong about ${secret:...}: a name the manifest does not
// list, or an environment of a server that is not a local one of a trusted app (only trusted apps run local servers).
func (m *Manifest) secretUseProblems(mcp map[string]MCPServer) []string {
	var out []string
	names := make([]string, 0, len(mcp))
	for n := range mcp {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		srv := mcp[name]
		for _, n := range srv.Secrets() {
			if !m.hasSecret(n) {
				out = append(out, fmt.Sprintf("%s uses ${secret:%s}, which is not in secrets", name, n))
			}
		}
		if strings.Contains(srv.URL, "${secret:") {
			out = append(out, fmt.Sprintf("%s: ${secret:...} goes in headers, not in the url (an address shows in logs)", name))
		}
		for _, v := range srv.Env {
			if strings.Contains(v, "${secret:") && (srv.Command == "" || m.Trust != TrustTrusted) {
				out = append(out, fmt.Sprintf("%s: ${secret:...} in env needs a local server (command) of a trusted app; a remote one takes it in headers", name))
			}
		}
		if srv.Command != "" {
			for _, a := range srv.Args {
				if strings.Contains(a, "${secret:") {
					out = append(out, fmt.Sprintf("%s: ${secret:...} is for headers and env, not for args (a command line shows in the process list)", name))
				}
			}
		}
	}
	return out
}
