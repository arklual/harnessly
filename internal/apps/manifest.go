// Package apps is harnessly's apps: a package an author writes once (a Claude Code plugin plus the manifest
// harnessly/app.json) that a person installs on a machine and turns on in a chat, a pane tab, a full screen of its own
// or a Sinopia document. A plugin is an app without views.
//
// This file reads and checks the manifest. The checks are the ones `harnessly app validate` reports and an install
// refuses on: every problem carries the path of the field (targets[2], agent.tools.deny[0]) so an author, or an agent
// editing the package, can find it.
package apps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// ManifestPath is where the manifest lives inside a package.
const ManifestPath = "harnessly/app.json"

// Trust levels: what a package may carry. A declarative app is text only (instructions, skills, commands, remote MCP
// servers, views as static files); a sandboxed one also runs its views' scripts in the sandboxed frame; a trusted one
// may start local processes (stdio MCP servers, validator commands) and is installed by hand only.
const (
	TrustDeclarative = "declarative"
	TrustSandboxed   = "sandboxed"
	TrustTrusted     = "trusted"
)

// Targets an app works in. sinopia.<kind> names one Sinopia document type, sinopia.* all of them.
const (
	TargetChat = "chat"
	TargetPane = "pane"
	TargetApp  = "app"
)

// Activations: how an app turns on.
const (
	OnManual   = "manual"
	OnView     = "view"
	OnDocument = "document"
	OnAlways   = "always"
)

// Manifest is harnessly/app.json.
type Manifest struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Icon        string          `json:"icon,omitempty"`
	Version     string          `json:"version"`
	Harnessly   string          `json:"harnessly"`
	Trust       string          `json:"trust"`
	Targets     []string        `json:"targets"`
	When        string          `json:"when,omitempty"`
	Activation  *Activation     `json:"activation,omitempty"`
	Permissions []string        `json:"permissions"`
	Network     *Network        `json:"network,omitempty"`
	Secrets     []Secret        `json:"secrets,omitempty"`
	Settings    json.RawMessage `json:"settings,omitempty"`
	Agent       *Agent          `json:"agent,omitempty"`
	Views       []View          `json:"views,omitempty"`
	Chat        *Chat           `json:"chat,omitempty"`
	Sinopia     json.RawMessage `json:"sinopia,omitempty"`
	Storage     json.RawMessage `json:"storage,omitempty"`
	Backend     *Backend        `json:"backend,omitempty"`
}

// Activation is how the app turns on and what it opens when it does.
type Activation struct {
	On       []string `json:"on"`
	OpenView string   `json:"openView,omitempty"`
	Template string   `json:"template,omitempty"`
}

// Network lists the hosts a view may reach; "*" needs a reason.
type Network struct {
	Connect   []string `json:"connect,omitempty"`
	Resources []string `json:"resources,omitempty"`
	Frames    []string `json:"frames,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

// Secret is a secret the app asks for at install; the person types its value, the app never reads it.
type Secret struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Optional    bool   `json:"optional,omitempty"`
}

// Agent is the agent profile: what the agent of a chat gets while the app is on in it.
type Agent struct {
	Instructions string        `json:"instructions,omitempty"`
	Skills       []string      `json:"skills,omitempty"`
	MCP          []string      `json:"mcp,omitempty"`
	Context      []ContextItem `json:"context,omitempty"`
	Tools        *ToolPolicy   `json:"tools,omitempty"`
	Validators   string        `json:"validators,omitempty"`
	Flow         string        `json:"flow,omitempty"`
	Exclusive    bool          `json:"exclusive,omitempty"`
}

// ContextItem is a source the agent reads on demand or gets on activation.
type ContextItem struct {
	ID   string `json:"id"`
	Tool string `json:"tool"`
	Mode string `json:"mode"`
}

// ToolPolicy is checked deny → confirm → allow.
type ToolPolicy struct {
	Allow   []string `json:"allow,omitempty"`
	Deny    []string `json:"deny,omitempty"`
	Confirm []string `json:"confirm,omitempty"`
}

// View is a page of the app: a tab, a full screen, a card in the chat or a Sinopia panel.
type View struct {
	ID      string   `json:"id"`
	Title   string   `json:"title,omitempty"`
	Entry   string   `json:"entry"`
	Targets []string `json:"targets"`
	Height  string   `json:"height,omitempty"`
}

// Chat is what the app adds to the chat.
type Chat struct {
	Commands []string `json:"commands,omitempty"`
	Cards    []string `json:"cards,omitempty"`
}

// Backend is the app's own server.
type Backend struct {
	URL string `json:"url"`
}

// Problem is one thing wrong with a package: the field it is about and what is wrong, in words.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	// Warning problems do not stop an install; --strict counts them as errors.
	Warning bool `json:"warning,omitempty"`
}

func (p Problem) String() string {
	kind := "error"
	if p.Warning {
		kind = "warning"
	}
	if p.Path == "" {
		return kind + ": " + p.Message
	}
	return kind + ": " + p.Path + ": " + p.Message
}

// Problems is the list a check returns; empty means the package is fine.
type Problems []Problem

// Errors are the problems that are not warnings.
func (ps Problems) Errors() Problems {
	var out Problems
	for _, p := range ps {
		if !p.Warning {
			out = append(out, p)
		}
	}
	return out
}

func (ps Problems) Error() string {
	lines := make([]string, len(ps))
	for i, p := range ps {
		lines[i] = p.String()
	}
	return strings.Join(lines, "\n")
}

var (
	idRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}\.[a-z0-9][a-z0-9-]{0,39}$`)
	semverRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?$`)
	viewIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	secretRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	kindRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	// appKindRe is a document type an app adds: <publisher>.<name>, as its id, with up to two more segments.
	appKindRe  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}(\.[a-z][a-z0-9-]{0,39}){1,3}$`)
	commandRe  = regexp.MustCompile(`^/[a-z][a-z0-9-]{0,39}$`)
	hostRe     = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(:\d{1,5})?$`)
	toolNameRe = regexp.MustCompile(`^[A-Za-z0-9_:.()*@/ -]{1,120}$`)
)

// permissions are the rights an app may ask for; a parameterised one is written right:<value>.
var permissions = map[string]bool{
	"chat.prompt": true, "chat.read": true, "chat.card": true,
	"pane.open": true, "storage": true, "storage.space.write": true, "ai": true,
	"sinopia.read": true, "sinopia.write": true, "sinopia.comment": true, "sinopia.export": true, "sinopia.kind": true,
	"users.read": true, "notify": true,
}

// unsupportedPermissions are rights the host accepts in no call yet.
var unsupportedPermissions = map[string]bool{"ai": true, "users.read": true, "sinopia.export": true}

var paramPermissions = map[string]bool{"files.read": true, "files.write": true, "secrets": true}

// ParseManifest reads the manifest. A syntax error, or a field of the wrong type, comes back as one problem with the
// line it stands on; otherwise the manifest is checked and every problem found is returned at once.
func ParseManifest(raw []byte) (*Manifest, Problems) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		msg := decodeMessage(raw, err)
		if strings.HasPrefix(msg, "unknown field") {
			// A field this host does not know: before saying so, read the range the app asks for. An app written for a
			// newer harnessly gets the version it needs, not a complaint about its fields.
			if rng, text := peekRange(raw); rng != nil {
				if v := rng.lowest(); v != "" {
					return nil, Problems{{Path: "harnessly", Message: fmt.Sprintf("this app asks for harnessly %s and has a field this version does not know (%s): it needs harnessly %s", text, msg, v)}}
				}
			}
		}
		return nil, Problems{{Message: msg}}
	}
	if dec.More() {
		return nil, Problems{{Message: "the file holds more than one JSON value"}}
	}
	return &m, m.Check()
}

func decodeMessage(raw []byte, err error) string {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return fmt.Sprintf("line %d: %s", lineOf(raw, syn.Offset), syn.Error())
	}
	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		return fmt.Sprintf("line %d: %s must be %s, not %s", lineOf(raw, typ.Offset), typ.Field, typ.Type, typ.Value)
	}
	// Unknown fields: encoding/json words it `json: unknown field "x"`.
	return strings.TrimPrefix(err.Error(), "json: ")
}

func lineOf(raw []byte, offset int64) int {
	if offset > int64(len(raw)) {
		offset = int64(len(raw))
	}
	return bytes.Count(raw[:offset], []byte("\n")) + 1
}

// Check is every rule of the manifest on its own, without the package's files (Package.Check adds those).
func (m *Manifest) Check() Problems {
	var ps Problems
	add := func(path, format string, a ...any) {
		ps = append(ps, Problem{Path: path, Message: fmt.Sprintf(format, a...)})
	}
	warn := func(path, format string, a ...any) {
		ps = append(ps, Problem{Path: path, Message: fmt.Sprintf(format, a...), Warning: true})
	}

	switch {
	case m.ID == "":
		add("id", "is required: <publisher>.<name>, e.g. acme.cashback")
	case !idRe.MatchString(m.ID):
		add("id", "%q is not <publisher>.<name> in lower case latin letters, digits and '-'", m.ID)
	case m.ID == WidgetsApp:
		// The agents' own widgets are served and bridged under this id; a package with it would lend them its rights.
		add("id", "%q is reserved for the agents' own widgets", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		add("name", "is required: the name people see")
	}
	if strings.TrimSpace(m.Description) == "" {
		add("description", "is required: one line on what the app does")
	}
	if !semverRe.MatchString(m.Version) {
		add("version", "%q is not a version like 1.2.0", m.Version)
	}
	if m.Harnessly == "" {
		add("harnessly", "is required: the host versions it works with, e.g. >=0.17")
	} else if _, err := ParseRange(m.Harnessly); err != nil {
		add("harnessly", "%v", err)
	}
	// Fields the host reads and does nothing with are refused, so an author does not write what never happens. Each is
	// taken off this list when the host starts to run it.
	notYet := func(path string) {
		add(path, "not supported yet: the host does not run it, remove it from the manifest")
	}
	if m.Icon != "" {
		notYet("icon")
	}
	if m.When != "" {
		notYet("when")
	}
	if len(m.Storage) > 0 {
		notYet("storage")
	}
	if m.Backend != nil {
		notYet("backend")
	}
	switch m.Trust {
	case TrustDeclarative, TrustSandboxed, TrustTrusted:
	case "":
		add("trust", "is required: declarative, sandboxed or trusted")
	default:
		add("trust", "%q is not declarative, sandboxed or trusted", m.Trust)
	}

	targets := map[string]bool{}
	if len(m.Targets) == 0 {
		add("targets", "is required: where the app works (chat, pane, app, sinopia.<kind>)")
	}
	for i, t := range m.Targets {
		path := fmt.Sprintf("targets[%d]", i)
		if !validTarget(t) {
			add(path, "%q is not a target: chat, pane, app, sinopia.<kind> or sinopia.*", t)
			continue
		}
		if targets[t] {
			add(path, "%q is listed twice", t)
		}
		targets[t] = true
	}

	if a := m.Activation; a != nil {
		if a.Template != "" {
			notYet("activation.template")
		}
		for i, on := range a.On {
			path := fmt.Sprintf("activation.on[%d]", i)
			switch {
			case on == OnView:
				notYet(path)
			case on == OnManual || on == OnDocument || on == OnAlways:
			case strings.HasPrefix(on, "command:"):
				switch cmd := strings.TrimPrefix(on, "command:"); {
				case !commandRe.MatchString(cmd):
					add(path, "%q: a command is command:/name", on)
				case slices.Contains(agentCommands, cmd):
					add(path, "%q is the agent's own command: pick another name", on)
				}
			default:
				add(path, "%q is not manual, view, document, always or command:/name", on)
			}
			if on == OnView && !hasViewTarget(targets) {
				add(path, "view needs a pane or app target")
			}
			if on == OnDocument && !hasSinopia(targets) {
				add(path, "document needs a sinopia target")
			}
		}
		if a.OpenView != "" && m.view(a.OpenView) == nil {
			add("activation.openView", "there is no view %q", a.OpenView)
		}
	}

	seen := map[string]bool{}
	for i, p := range m.Permissions {
		path := fmt.Sprintf("permissions[%d]", i)
		if seen[p] {
			add(path, "%q is listed twice", p)
		}
		seen[p] = true
		if unsupportedPermissions[p] {
			notYet(path)
			continue
		}
		if permissions[p] {
			continue
		}
		name, value, ok := strings.Cut(p, ":")
		if !ok || !paramPermissions[name] || value == "" {
			add(path, "%q is not a permission", p)
			continue
		}
		if name == "secrets" && !m.hasSecret(value) {
			add(path, "the secret %q is not in secrets", value)
		}
		if name == "files.read" || name == "files.write" {
			if why := CheckMask(value); why != "" {
				add(path, "the mask %q %s: it is a path pattern of the workspace like docs/** or src/*.go", value, why)
			} else if name == "files.write" && (value == "**" || value == "*") {
				warn(path, "files.write:%s lets the app write anywhere in the workspace: narrow it to the folders it needs", value)
			}
		}
	}
	for i, s := range m.Secrets {
		path := fmt.Sprintf("secrets[%d]", i)
		if !secretRe.MatchString(s.Name) {
			add(path+".name", "%q: a secret name is letters, digits, '.', '_' and '-'", s.Name)
		}
		if strings.TrimSpace(s.Description) == "" {
			add(path+".description", "is required: the person types the value and needs to know what it is")
		}
		if !seen["secrets:"+s.Name] {
			add(path, "the app asks for %q but has no secrets:%s permission", s.Name, s.Name)
		}
	}

	if n := m.Network; n != nil {
		for field, hosts := range map[string][]string{"connect": n.Connect, "resources": n.Resources, "frames": n.Frames} {
			for i, h := range hosts {
				path := fmt.Sprintf("network.%s[%d]", field, i)
				if h == "*" {
					if strings.TrimSpace(n.Reason) == "" {
						add(path, "* needs network.reason")
					}
					continue
				}
				if !hostRe.MatchString(strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "wss://")) {
					add(path, "%q is not a host name", h)
				}
			}
		}
		if m.Trust == TrustDeclarative && (len(n.Connect) > 0 || len(n.Resources) > 0) {
			add("network", "a declarative app runs no scripts: connect and resources need trust sandboxed")
		}
	}

	if len(m.Settings) > 0 {
		var schema map[string]any
		if err := json.Unmarshal(m.Settings, &schema); err != nil || schema["type"] != "object" {
			add("settings", "must be a JSON Schema of an object")
		} else {
			for _, f := range m.SettingFields() {
				switch f.Type {
				case "string", "number", "integer", "boolean":
				default:
					add("settings.properties."+f.Name, "the type %q is not drawn by the form: use string, number, integer or boolean", f.Type)
				}
			}
		}
	}

	if ag := m.Agent; ag != nil {
		for i, s := range ag.Skills {
			if !kindRe.MatchString(s) {
				add(fmt.Sprintf("agent.skills[%d]", i), "%q: a skill is the name of a folder under skills/", s)
			}
		}
		for i, c := range ag.Context {
			path := fmt.Sprintf("agent.context[%d]", i)
			if c.ID == "" || c.Tool == "" {
				add(path, "needs id and tool")
			}
			if c.Mode != "on-demand" && c.Mode != "on-activation" {
				add(path+".mode", "%q is not on-demand or on-activation", c.Mode)
			}
		}
		if t := ag.Tools; t != nil {
			for field, list := range map[string][]string{"allow": t.Allow, "deny": t.Deny, "confirm": t.Confirm} {
				for i, name := range list {
					path := fmt.Sprintf("agent.tools.%s[%d]", field, i)
					if err := checkToolPattern(name); err != "" {
						add(path, "%q: %s", name, err)
					}
				}
			}
		}
		if ag.Flow != "" {
			notYet("agent.flow")
		}
		if ag.Exclusive && !targets[TargetChat] {
			add("agent.exclusive", "only a chat app can be exclusive")
		}
	}
	if m.Agent != nil && !targets[TargetChat] && !hasSinopia(targets) && !targets[TargetPane] {
		warn("agent", "no chat, pane or sinopia target: the agent profile is never used")
	}

	views := map[string]bool{}
	for i, v := range m.Views {
		path := fmt.Sprintf("views[%d]", i)
		if !viewIDRe.MatchString(v.ID) {
			add(path+".id", "%q: a view id is lower case latin letters, digits and '-'", v.ID)
		}
		if views[v.ID] {
			add(path+".id", "%q is used twice", v.ID)
		}
		views[v.ID] = true
		if v.Entry == "" {
			add(path+".entry", "is required: ui/<file>.html or https://… of the app's server")
		} else if strings.HasPrefix(v.Entry, "https://") {
			if m.Network == nil || !hostListed(m.Network.Frames, v.Entry) {
				add(path+".entry", "the host of %q must be in network.frames", v.Entry)
			}
		}
		if m.Trust == TrustDeclarative && strings.HasPrefix(v.Entry, "https://") {
			add(path+".entry", "a declarative app serves its views from the package")
		}
		if v.Height != "" {
			notYet(path + ".height")
		}
		if len(v.Targets) == 0 {
			add(path+".targets", "is required: where the view shows")
		}
		for j, t := range v.Targets {
			if !targets[t] && !(wildcardCovers(t) && targets["sinopia.*"]) {
				add(fmt.Sprintf("%s.targets[%d]", path, j), "%q is not in the app's targets", t)
			}
		}
	}

	// A target with nothing in it is a mistake: the app would show up where it does nothing.
	for _, t := range sortedKeys(targets) {
		if !m.contributes(t) {
			add("targets", "%q has nothing of the app in it: add a view, an agent profile or a contribution for it", t)
		}
	}

	if c := m.Chat; c != nil {
		for i, cmd := range c.Commands {
			if !commandRe.MatchString(cmd) {
				add(fmt.Sprintf("chat.commands[%d]", i), "%q: a command is /name", cmd)
			}
		}
		for i, card := range c.Cards {
			if v := m.view(card); v == nil || !contains(v.Targets, TargetChat) {
				add(fmt.Sprintf("chat.cards[%d]", i), "%q is not a view with the chat target", card)
			}
		}
		if (len(c.Commands) > 0 || len(c.Cards) > 0) && !targets[TargetChat] {
			add("chat", "chat contributions need the chat target")
		}
	}
	if len(m.Sinopia) > 0 && !hasSinopia(targets) {
		add("sinopia", "sinopia contributions need a sinopia target")
	}
	m.checkSinopia(targets, add, warn)

	// Rights the app asks for and has no use of are a warning: people see the list at install.
	if seen["pane.open"] && !targets[TargetPane] && !targets[TargetApp] {
		warn("permissions", "pane.open without a pane or app target")
	}
	if hasPrefixed(m.Permissions, "sinopia.") && !hasSinopia(targets) {
		warn("permissions", "sinopia rights without a sinopia target")
	}
	return ps
}

// contributes says whether the app puts anything in a target.
func (m *Manifest) contributes(target string) bool {
	for _, v := range m.Views {
		if contains(v.Targets, target) {
			return true
		}
	}
	switch {
	case target == TargetChat:
		return m.Agent != nil || m.Chat != nil
	case strings.HasPrefix(target, "sinopia."):
		return len(m.Sinopia) > 0 || m.Agent != nil
	}
	return false
}

func (m *Manifest) view(id string) *View {
	for i := range m.Views {
		if m.Views[i].ID == id {
			return &m.Views[i]
		}
	}
	return nil
}

func (m *Manifest) hasSecret(name string) bool {
	for _, s := range m.Secrets {
		if s.Name == name {
			return true
		}
	}
	return false
}

// HasTarget says whether the app works in a target; sinopia.* covers every Sinopia kind.
func (m *Manifest) HasTarget(target string) bool {
	if contains(m.Targets, target) {
		return true
	}
	return wildcardCovers(target) && contains(m.Targets, "sinopia.*")
}

// wildcardCovers says whether sinopia.* names the target: the host's own document types, not those an app adds, which an
// app has to name to be in.
func wildcardCovers(target string) bool {
	kind, ok := strings.CutPrefix(target, "sinopia.")
	return ok && !strings.Contains(kind, ".")
}

func validTarget(t string) bool {
	switch t {
	case TargetChat, TargetPane, TargetApp, "sinopia.*":
		return true
	}
	kind, ok := strings.CutPrefix(t, "sinopia.")
	return ok && (kindRe.MatchString(kind) || appKindRe.MatchString(kind))
}

func hasViewTarget(targets map[string]bool) bool { return targets[TargetPane] || targets[TargetApp] }

func hasSinopia(targets map[string]bool) bool {
	for t := range targets {
		if strings.HasPrefix(t, "sinopia.") {
			return true
		}
	}
	return false
}

// checkToolPattern is "" for a good tool name or mask: a built-in tool (Bash, Read, Bash(git push*)), a server's tool
// (bank-cashback.create, bank-cashback.*), the host's app.* tools or mcp:* for any MCP tool. A * stands only at the end
// of a segment.
func checkToolPattern(p string) string {
	if !toolNameRe.MatchString(p) {
		return "not a tool name"
	}
	if p == "mcp:*" || p == "*" {
		return ""
	}
	if open := strings.IndexByte(p, '('); open >= 0 {
		if !strings.HasSuffix(p, ")") || open == 0 {
			return "a built-in tool with a rule is Name(rule), e.g. Bash(git push*)"
		}
		return ""
	}
	for _, seg := range strings.Split(p, ".") {
		if seg == "" {
			return "an empty segment"
		}
		if i := strings.IndexByte(seg, '*'); i >= 0 && i != len(seg)-1 {
			return "* stands only at the end of a segment"
		}
	}
	return ""
}

// FrameURL is the address of a view the app's own server hosts, when it may be framed: https, no credentials in it, and
// its host listed in network.frames. The page frames nothing else.
func (m *Manifest) FrameURL(entry string) (string, bool) {
	u, err := url.Parse(entry)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || m.Network == nil || !hostListed(m.Network.Frames, entry) {
		return "", false
	}
	return u.String(), true
}

func hostListed(hosts []string, url string) bool {
	host := strings.TrimPrefix(url, "https://")
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	for _, h := range hosts {
		h = strings.TrimPrefix(h, "https://")
		if h == "*" || h == host || (strings.HasPrefix(h, "*.") && strings.HasSuffix(host, h[1:])) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hasPrefixed(list []string, prefix string) bool {
	for _, x := range list {
		if strings.HasPrefix(x, prefix) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ActivatesOn says whether the manifest's activation lists the value (OnAlways, OnDocument, "command:/x").
func (m *Manifest) ActivatesOn(on string) bool {
	return m.Activation != nil && slices.Contains(m.Activation.On, on)
}

// agentCommands are the slash commands of the agents themselves (Claude Code, Codex): an app may not take one, or a
// person's /clear would turn an app on instead of clearing the chat.
var agentCommands = []string{"/add-dir", "/agents", "/approvals", "/bug", "/clear", "/compact", "/config", "/context", "/cost",
	"/diff", "/doctor", "/exit", "/export", "/help", "/hooks", "/ide", "/init", "/login", "/logout", "/mcp", "/memory", "/mention",
	"/model", "/new", "/permissions", "/plugin", "/plugins", "/pr-comments", "/quit", "/release-notes", "/resume", "/review",
	"/rewind", "/security-review", "/skills", "/status", "/statusline", "/terminal-setup", "/todos", "/upgrade", "/usage", "/vim"}

// Command is the slash command that turns the app on in a chat (`activation.on: ["command:/x"]` is "/x"); "" when none.
func (m *Manifest) Command() string {
	if m.Activation == nil {
		return ""
	}
	for _, on := range m.Activation.On {
		if cmd, ok := strings.CutPrefix(on, "command:"); ok {
			return cmd
		}
	}
	return ""
}

// peekRange reads the harnessly field of a manifest before the strict parse, whatever else the manifest holds; nil when
// there is none or it is not a range.
func peekRange(raw []byte) (Range, string) {
	var head struct {
		Harnessly string `json:"harnessly"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Harnessly == "" {
		return nil, ""
	}
	rng, err := ParseRange(head.Harnessly)
	if err != nil {
		return nil, ""
	}
	return rng, head.Harnessly
}

// HostAllows says whether the host version is in the range the manifest in raw asks for; true when the manifest names
// none (the strict parse says what is wrong with it).
func HostAllows(raw []byte, host string) (ok bool, wanted string) {
	rng, text := peekRange(raw)
	if rng == nil {
		return true, ""
	}
	return rng.Allows(host), text
}
