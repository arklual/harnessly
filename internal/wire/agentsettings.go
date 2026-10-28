package wire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// The Settings screen's pages for the agent CLIs: what Claude Code keeps in ~/.claude/settings.json and Codex in
// ~/.codex/config.toml on this machine, read and changed key by key. The page never sees a file: it sees the closed list
// of options a CLI takes (`options`, each set or null), the permission rules and environment of Claude Code, the MCP
// servers the machine knows and how the CLI is signed in. Secrets never leave the machine: an environment value that
// looks like one comes masked to its tail, a sign-in comes as a method, a plan and a name, never a token or a key.

// AgentOptionKey names one option of an agent CLI. `model` and `effort` are both CLIs'; the rest belong to one.
type AgentOptionKey string

const (
	OptModel          AgentOptionKey = "model"
	OptEffort         AgentOptionKey = "effort"
	OptBaseURL        AgentOptionKey = "baseUrl"
	OptFallbackModel  AgentOptionKey = "fallbackModel"
	OptThinking       AgentOptionKey = "thinking"
	OptOutputStyle    AgentOptionKey = "outputStyle"
	OptPermissionMode AgentOptionKey = "permissionMode"
	OptSandbox        AgentOptionKey = "sandbox"
	OptProjectMcp     AgentOptionKey = "projectMcp"
	OptCompactWindow  AgentOptionKey = "compactWindow"
	OptMemory         AgentOptionKey = "memory"
	OptKeepDays       AgentOptionKey = "keepDays"
	OptHooks          AgentOptionKey = "hooks"
	OptCoauthor       AgentOptionKey = "coauthor"
	OptUpdates        AgentOptionKey = "updates"
	OptSummary        AgentOptionKey = "summary"
	OptFast           AgentOptionKey = "fast"
	OptSandboxMode    AgentOptionKey = "sandboxMode"
	OptNetwork        AgentOptionKey = "network"
	OptApproval       AgentOptionKey = "approval"
	OptSearch         AgentOptionKey = "search"
	OptHistory        AgentOptionKey = "history"
	// OptUltracode is Claude Code's `ultracode` key: new sessions start in ultracode (2.1.310+).
	OptUltracode AgentOptionKey = "ultracode"
)

// AgentOptionKeys is every AgentOptionKey, in the schema's order.
var AgentOptionKeys = []AgentOptionKey{OptModel, OptEffort, OptBaseURL, OptFallbackModel, OptThinking, OptOutputStyle, OptPermissionMode, OptSandbox, OptProjectMcp, OptCompactWindow, OptMemory, OptKeepDays, OptHooks, OptCoauthor, OptUpdates, OptSummary, OptFast, OptSandboxMode, OptNetwork, OptApproval, OptSearch, OptHistory, OptUltracode}

// AgentOptionKind is the type of an option's value.
type AgentOptionKind string

const (
	OptKindBool AgentOptionKind = "bool"
	OptKindInt  AgentOptionKind = "int"
	OptKindEnum AgentOptionKind = "enum"
	OptKindText AgentOptionKind = "text"
	OptKindURL  AgentOptionKind = "url"
)

// AgentOptionSpec is what an option takes: a switch, a whole number in a range, one of a list, a short name or an address.
type AgentOptionSpec struct {
	Key  AgentOptionKey
	Kind AgentOptionKind
	Enum []string
	// ReadExtra are values the file may hold that the page cannot write: it shows them, and lets the person pick
	// another.
	ReadExtra []string
	Min, Max  int64
	// Pattern checks a text.
	Pattern *regexp.Regexp
}

var (
	modelName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]-]{0,99}$`)
	styleName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)
	efforts       = []string{"low", "medium", "high", "xhigh", "max"}
	codexEfforts  = []string{"low", "medium", "high", "xhigh"}
	claudeOptions = []AgentOptionSpec{
		{Key: OptModel, Kind: OptKindText, Pattern: modelName},
		{Key: OptEffort, Kind: OptKindEnum, Enum: efforts},
		{Key: OptBaseURL, Kind: OptKindURL},
		{Key: OptFallbackModel, Kind: OptKindText, Pattern: modelName},
		{Key: OptThinking, Kind: OptKindBool},
		{Key: OptOutputStyle, Kind: OptKindText, Pattern: styleName},
		{Key: OptPermissionMode, Kind: OptKindEnum, Enum: []string{"default", "acceptEdits", "plan"}, ReadExtra: []string{"bypassPermissions", "auto", "dontAsk"}},
		{Key: OptSandbox, Kind: OptKindBool},
		{Key: OptProjectMcp, Kind: OptKindBool},
		{Key: OptCompactWindow, Kind: OptKindInt, Min: 100000, Max: 1000000},
		{Key: OptMemory, Kind: OptKindBool},
		{Key: OptKeepDays, Kind: OptKindInt, Min: 1, Max: 3650},
		{Key: OptHooks, Kind: OptKindBool},
		{Key: OptCoauthor, Kind: OptKindBool},
		{Key: OptUpdates, Kind: OptKindEnum, Enum: []string{"stable", "latest"}},
		{Key: OptUltracode, Kind: OptKindBool},
	}
	codexOptions = []AgentOptionSpec{
		{Key: OptModel, Kind: OptKindText, Pattern: modelName},
		{Key: OptEffort, Kind: OptKindEnum, Enum: codexEfforts, ReadExtra: []string{"minimal"}},
		{Key: OptBaseURL, Kind: OptKindURL},
		{Key: OptSummary, Kind: OptKindEnum, Enum: []string{"auto", "concise", "detailed", "none"}},
		{Key: OptFast, Kind: OptKindBool},
		{Key: OptSandboxMode, Kind: OptKindEnum, Enum: []string{"read-only", "workspace-write", "danger-full-access"}},
		{Key: OptNetwork, Kind: OptKindBool},
		{Key: OptApproval, Kind: OptKindEnum, Enum: []string{"untrusted", "on-failure", "on-request", "never"}},
		{Key: OptSearch, Kind: OptKindEnum, Enum: []string{"disabled", "cached", "live"}},
		{Key: OptHistory, Kind: OptKindBool},
	}
)

// AgentOptionSpecs are the options a provider's CLI takes, in the order the page lists them.
func AgentOptionSpecs(p Provider) []AgentOptionSpec {
	switch p {
	case ProviderClaude:
		return claudeOptions
	case ProviderCodex:
		return codexOptions
	}
	return nil
}

// AgentOptionSpecOf is the spec of a provider's option; false when the CLI has no such option.
func AgentOptionSpecOf(p Provider, key AgentOptionKey) (AgentOptionSpec, bool) {
	for _, s := range AgentOptionSpecs(p) {
		if s.Key == key {
			return s, true
		}
	}
	return AgentOptionSpec{}, false
}

// Check says whether v is a value of the option a page may write: nil (the option unset) always is.
func (s AgentOptionSpec) Check(v any) error { return s.check(v, nil) }

// CheckRead says whether v is a value of the option the file may hold, the ones a page cannot write included.
func (s AgentOptionSpec) CheckRead(v any) error { return s.check(v, s.ReadExtra) }

func (s AgentOptionSpec) check(v any, extra []string) error {
	if v == nil {
		return nil
	}
	switch s.Kind {
	case OptKindBool:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: not a switch", s.Key)
		}
	case OptKindInt:
		n, ok := v.(float64)
		if !ok || n != float64(int64(n)) || int64(n) < s.Min || int64(n) > s.Max {
			return fmt.Errorf("%s: not a whole number from %d to %d", s.Key, s.Min, s.Max)
		}
	case OptKindEnum:
		str, ok := v.(string)
		if !ok || !(slices.Contains(s.Enum, str) || slices.Contains(extra, str)) {
			return fmt.Errorf("%s: unknown %v", s.Key, v)
		}
	case OptKindText:
		str, ok := v.(string)
		if !ok || !s.Pattern.MatchString(str) {
			return fmt.Errorf("%s: not a name", s.Key)
		}
	case OptKindURL:
		str, ok := v.(string)
		if !ok || len(str) > 500 {
			return fmt.Errorf("%s: not an address", s.Key)
		}
		u, err := url.Parse(str)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || strings.ContainsAny(str, " \t\r\n") {
			return fmt.Errorf("%s: not an address", s.Key)
		}
	}
	return nil
}

// AgentOption is one option and its value: a string, a switch, a whole number, or null while the file does not set it.
type AgentOption struct {
	Key   AgentOptionKey `json:"key"`
	Value any            `json:"value"`
}

// AgentRules are Claude Code's permission rules (`permissions.allow`, `ask`, `deny`): a rule is a tool and a pattern,
// as `Bash(npm run *)` or `mcp__gitlab`.
type AgentRules struct {
	Allow []string `json:"allow"`
	Ask   []string `json:"ask"`
	Deny  []string `json:"deny"`
}

// Limits of the agent settings a page may write.
const (
	MaxAgentRules        = 200
	MaxAgentRuleLen      = 300
	MaxAgentEnvValueLen  = 2000
	MaxAgentEnvVars      = 100
	MaxAgentSettingsBody = 128 << 10
)

var (
	envName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	mcpName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	// SecretName is an environment variable whose value is shown by its tail only.
	SecretName = regexp.MustCompile(`KEY|TOKEN|SECRET|PASS`)
)

func rule(name, r string) error {
	if r == "" || utf8.RuneCountInString(r) > MaxAgentRuleLen || strings.ContainsFunc(r, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		return fmt.Errorf("%s: not a rule", name)
	}
	return nil
}

func (r AgentRules) Validate() error {
	if r.Allow == nil || r.Ask == nil || r.Deny == nil {
		return errors.New("rules: a list is missing")
	}
	var errs []error
	for name, list := range map[string][]string{"allow": r.Allow, "ask": r.Ask, "deny": r.Deny} {
		if len(list) > MaxAgentRules {
			errs = append(errs, fmt.Errorf("%s: more than %d rules", name, MaxAgentRules))
		}
		seen := map[string]bool{}
		for _, v := range list {
			if err := rule(name, v); err != nil {
				errs = append(errs, err)
			}
			if seen[v] {
				errs = append(errs, fmt.Errorf("%s: %q twice", name, v))
			}
			seen[v] = true
		}
	}
	return errors.Join(errs...)
}

// AgentEnvVar is a variable of the CLI's `env`. A value that looks like a secret comes as its last four characters
// after bullets (`secret`), never whole.
type AgentEnvVar struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

func (e AgentEnvVar) Validate() error {
	var name error
	if !envName.MatchString(e.Key) {
		name = fmt.Errorf("env: %q is not a variable name", e.Key)
	}
	return name
}

// AgentMcpKind is how the CLI reaches an MCP server: it starts a command (`stdio`) or calls an address (`http`).
type AgentMcpKind string

const (
	McpStdio AgentMcpKind = "stdio"
	McpHTTP  AgentMcpKind = "http"
)

// AgentMcpKinds is every AgentMcpKind, in the schema's order.
var AgentMcpKinds = []AgentMcpKind{McpStdio, McpHTTP}

// AgentMcp is an MCP server the machine has set up for the CLI. `target` is what it runs (the program's name) or where
// it is (the host), never its arguments or a path with a key in it; `enabled` is false when the CLI is told not to use it.
type AgentMcp struct {
	Name    string       `json:"name"`
	Kind    AgentMcpKind `json:"kind"`
	Target  string       `json:"target"`
	Enabled bool         `json:"enabled"`
}

func (m AgentMcp) Validate() error {
	var name error
	if !mcpName.MatchString(m.Name) {
		name = fmt.Errorf("mcp: %q is not a server name", m.Name)
	}
	return errors.Join(name, in("kind", m.Kind, AgentMcpKinds))
}

// AgentAccountMethod is how a CLI is signed in: with the person's subscription or with an API key.
type AgentAccountMethod string

const (
	AccountSubscription AgentAccountMethod = "subscription"
	AccountKey          AgentAccountMethod = "key"
)

// AgentAccountMethods is every AgentAccountMethod, in the schema's order.
var AgentAccountMethods = []AgentAccountMethod{AccountSubscription, AccountKey}

// AgentAccount is how the CLI is signed in on this machine. `plan` and `who` (an e-mail) are what the CLI itself keeps
// beside its sign-in, null where it keeps none; a key is never sent.
type AgentAccount struct {
	SignedIn bool                `json:"signedIn"`
	Method   *AgentAccountMethod `json:"method"`
	Plan     *string             `json:"plan"`
	Who      *string             `json:"who"`
}

func (a AgentAccount) Validate() error {
	var method error
	switch {
	case a.SignedIn && a.Method == nil:
		method = errors.New("method: none for a signed in CLI")
	case !a.SignedIn && (a.Method != nil || a.Plan != nil || a.Who != nil):
		method = errors.New("method: a CLI that is not signed in has none")
	case a.Method != nil:
		method = in("method", *a.Method, AgentAccountMethods)
	}
	return errors.Join(method, needPtr("plan", a.Plan), needPtr("who", a.Who))
}

// AgentSettings is the answer of GET /api/v2/agent-settings/<provider>: every option the CLI takes (null while unset),
// and for Claude Code its rules and environment (Codex has neither: `rules` is null and `env` empty). `path` is the file with the home shortened to `~`;
// `rev` is the file's content hash, the value a write has to quote.
type AgentSettings struct {
	Provider Provider      `json:"provider"`
	Path     string        `json:"path"`
	Rev      string        `json:"rev"`
	Account  AgentAccount  `json:"account"`
	Options  []AgentOption `json:"options"`
	Rules    *AgentRules   `json:"rules"`
	Env      []AgentEnvVar `json:"env"`
	Mcp      []AgentMcp    `json:"mcp"`
}

func (a AgentSettings) Validate() error {
	specs := AgentOptionSpecs(a.Provider)
	var errs []error
	if len(a.Options) != len(specs) {
		errs = append(errs, fmt.Errorf("options: %d of the %d the CLI takes", len(a.Options), len(specs)))
	}
	for i, o := range a.Options {
		if i >= len(specs) || o.Key != specs[i].Key {
			errs = append(errs, fmt.Errorf("options[%d]: %q out of order", i, o.Key))
			continue
		}
		errs = append(errs, specs[i].CheckRead(o.Value))
	}
	var shape error
	switch {
	case a.Provider == ProviderClaude && a.Rules == nil:
		shape = errors.New("rules: Claude Code has rules")
	case a.Provider == ProviderCodex && (a.Rules != nil || len(a.Env) > 0):
		shape = errors.New("rules: Codex has neither rules nor an environment")
	}
	if a.Rules != nil {
		errs = append(errs, a.Rules.Validate())
	}
	return errors.Join(append(errs, in("provider", a.Provider, Providers), need("path", a.Path), need("rev", a.Rev), a.Account.Validate(), shape, each("env", a.Env), each("mcp", a.Mcp))...)
}

// AgentEnvSet sets a variable of the CLI's environment.
type AgentEnvSet struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// AgentMcpToggle turns an MCP server on or off for the CLI.
type AgentMcpToggle struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// AgentSettingsPatch names what a write changes: options (a null value unsets the option), the whole rules, variables
// of the environment to set or to remove, one MCP server turned on or off. What it does not name stays as it is.
type AgentSettingsPatch struct {
	Options  []AgentOption   `json:"options,omitempty"`
	Rules    *AgentRules     `json:"rules,omitempty"`
	EnvSet   []AgentEnvSet   `json:"envSet,omitempty"`
	EnvUnset []string        `json:"envUnset,omitempty"`
	Mcp      *AgentMcpToggle `json:"mcp,omitempty"`
}

// AgentSettingsEdit is the body of PATCH /api/v2/agent-settings/<provider>: the patch and the revision it was made on.
type AgentSettingsEdit struct {
	Rev   string             `json:"rev"`
	Patch AgentSettingsPatch `json:"patch"`
}

// ValidateFor checks a patch against what a provider's CLI takes.
func (p AgentSettingsPatch) ValidateFor(provider Provider) error {
	if len(p.Options) == 0 && p.Rules == nil && len(p.EnvSet) == 0 && len(p.EnvUnset) == 0 && p.Mcp == nil {
		return errors.New("patch: nothing to change")
	}
	var errs []error
	seen := map[AgentOptionKey]bool{}
	for _, o := range p.Options {
		spec, ok := AgentOptionSpecOf(provider, o.Key)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("options: %q is not an option of this CLI", o.Key))
		case seen[o.Key]:
			errs = append(errs, fmt.Errorf("options: %q twice", o.Key))
		default:
			errs = append(errs, spec.Check(o.Value))
		}
		seen[o.Key] = true
	}
	if provider != ProviderClaude && (p.Rules != nil || len(p.EnvSet) > 0 || len(p.EnvUnset) > 0) {
		errs = append(errs, errors.New("patch: only Claude Code has rules and an environment"))
	}
	if p.Rules != nil {
		errs = append(errs, p.Rules.Validate())
	}
	if len(p.EnvSet)+len(p.EnvUnset) > MaxAgentEnvVars {
		errs = append(errs, errors.New("env: too many changes"))
	}
	for _, e := range p.EnvSet {
		if e.Key == "ANTHROPIC_BASE_URL" {
			errs = append(errs, errors.New("env: the address of the requests is the option baseUrl"))
		}
		if !envName.MatchString(e.Key) || e.Value == "" || len(e.Value) > MaxAgentEnvValueLen || strings.ContainsAny(e.Value, "\r\n\x00") {
			errs = append(errs, fmt.Errorf("env: %q is not a variable", e.Key))
		}
	}
	for _, k := range p.EnvUnset {
		if !envName.MatchString(k) {
			errs = append(errs, fmt.Errorf("env: %q is not a variable name", k))
		}
	}
	if p.Mcp != nil && !mcpName.MatchString(p.Mcp.Name) {
		errs = append(errs, fmt.Errorf("mcp: %q is not a server name", p.Mcp.Name))
	}
	return errors.Join(errs...)
}

// The errors a source answers a write with.
var (
	// ErrAgentSettingsConflict: the file changed since the revision the write quotes.
	ErrAgentSettingsConflict = errors.New("wire: the agent settings are at another revision")
	// ErrAgentSettingsRefused: the file cannot take the change (the CLI is not installed, or it does not read).
	ErrAgentSettingsRefused = errors.New("wire: the agent settings cannot take the change")
)

// AgentSettingsSource reads and writes the agent CLIs' settings files of this machine.
type AgentSettingsSource interface {
	AgentSettings(ctx context.Context, p Provider) (AgentSettings, error)
	// PatchAgentSettings applies a patch made on rev: ErrAgentSettingsConflict when the file is at another revision,
	// ErrAgentSettingsRefused when it cannot take the change.
	PatchAgentSettings(ctx context.Context, p Provider, rev string, patch AgentSettingsPatch) (AgentSettings, error)
}

// agentSettings serves GET and PATCH /api/v2/agent-settings/<provider>. A write changes a file of the person's, so it is
// refused to other origins (item 41).
func (s *Server) agentSettings(w http.ResponseWriter, r *http.Request, name string) {
	p := Provider(name)
	if s.AgentSettings == nil || !slices.Contains(Providers, p) {
		refuse(w, RefuseNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		got, err := s.AgentSettings.AgentSettings(r.Context(), p)
		if err != nil {
			s.agentSettingsFailed(w, r, err)
			return
		}
		s.writeChecked(w, r, got)
	case http.MethodPatch:
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxAgentSettingsBody))
		var edit AgentSettingsEdit
		if err == nil {
			err = decodeStrict(raw, &edit)
		}
		if err == nil {
			err = errors.Join(need("rev", edit.Rev), edit.Patch.ValidateFor(p))
		}
		if err != nil {
			refuse(w, RefuseBadRequest)
			return
		}
		got, err := s.AgentSettings.PatchAgentSettings(r.Context(), p, edit.Rev, edit.Patch)
		switch {
		case errors.Is(err, ErrAgentSettingsConflict):
			refuse(w, RefuseConflict)
		case errors.Is(err, ErrAgentSettingsRefused):
			refuse(w, RefuseBadRequest)
		case err != nil:
			s.agentSettingsFailed(w, r, err)
		default:
			s.writeChecked(w, r, got)
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) agentSettingsFailed(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil || errors.Is(err, context.DeadlineExceeded) {
		refuse(w, RefuseTimeout)
		return
	}
	jsonlog.Log("warn", "v2: the agent settings failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
	refuse(w, RefuseInternal)
}
