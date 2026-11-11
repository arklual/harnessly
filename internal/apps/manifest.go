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
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
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

// The slots of the interface an extension fills: a view in a sandboxed frame at a place the page declares, or,
// for branding, a few declared values. With the runtime's HARNESSLY_UI_SLOTS off the page shows none of them.
const (
	TargetSettings     = "settings"      // a page in the settings
	TargetOnboarding   = "onboarding"    // a step of the first run
	TargetObjectAction = "object-action" // a button at an object (a machine, a chat) that opens the view
	TargetHubSettings  = "hub-settings"  // a page in the settings of a Hub
	TargetBranding     = "branding"      // the product's name and accent; no view, see Branding
)

// SlotTargets are the targets above, in the order the docs name them.
var SlotTargets = []string{TargetSettings, TargetOnboarding, TargetObjectAction, TargetHubSettings, TargetBranding}

// IsSlot says whether a target is one of the slots.
func IsSlot(target string) bool { return contains(SlotTargets, target) }

// Objects an object-action view can sit at.
var ObjectKinds = []string{"machine", "chat"}

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
	Branding    *Branding       `json:"branding,omitempty"`
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
	// Budget is what the app may spend a day through `ai` and `agents.run` (spend.go).
	Budget *Budget `json:"budget,omitempty"`
	// Workflow is the statuses of the app's documents and the moves between them (workflow.go).
	Workflow *Workflow `json:"workflow,omitempty"`
	// Delivery, Points and Aliases are the host's own apps' (builtin.go): an installed app is a package and has none.
	Delivery string      `json:"delivery,omitempty"`
	Points   []PointDecl `json:"points,omitempty"`
	Aliases  []AliasDecl `json:"aliases,omitempty"`
	// Kind says which of the extension kinds the package is: "ui", "runtime" or "tool", see ext.go. Empty is
	// worked out from what the package contains.
	Kind string `json:"kind,omitempty"`
	// Data is the version of the app's own data in hl.storage and the steps that bring older data to it.
	Data *DataSpec `json:"data,omitempty"`
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
	// ClientCert is the app's own certificate for mutual TLS to its own backend: the name of one of its secrets,
	// which holds the certificate and its key as PEM. The key is in the machine's safe, never in the manifest.
	ClientCert *ClientCert `json:"clientCert,omitempty"`
}

// ClientCert names the secret with an app's client certificate and, when the app has several backends, which of the hosts
// of its own MCP servers (.mcp.json) it is for; none listed: every one. It is never sent to another host.
type ClientCert struct {
	Secret string   `json:"secret"`
	Hosts  []string `json:"hosts,omitempty"`
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
	Validators   []Validator   `json:"validators,omitempty"`
	Flow         string        `json:"flow,omitempty"`
	Exclusive    bool          `json:"exclusive,omitempty"`
}

// Validator is a check of the agent's turn (validate.go): a script of the package that runs on PostToolUse or Stop.
type Validator struct {
	ID         string `json:"id"`
	On         string `json:"on"`
	Match      string `json:"match,omitempty"`
	Run        string `json:"run"`
	MaxRetries int    `json:"maxRetries,omitempty"`
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
	// When are rules that hold only while the document the chat works on is in one of the statuses (workflow.go); they
	// join the rules above, and the most specific rule decides as ever.
	When []ToolWhen `json:"when,omitempty"`
}

// ToolWhen is a set of rules for some statuses of the app's workflow.
type ToolWhen struct {
	Status  []string `json:"status"`
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
	// Label, Icon, Order and Objects are for the slots: the name on the settings entry, the step or the button (Title when
	// empty), a name from the icon set, the place among its neighbours, and the kinds of object an object-action view is
	// shown at (machine, chat; every kind when empty).
	Label   string   `json:"label,omitempty"`
	Icon    string   `json:"icon,omitempty"`
	Order   int      `json:"order,omitempty"`
	Objects []string `json:"objects,omitempty"`
	// Pinned: the view is the app's tab in the chat's right pane beside a document of its type made of a Sinopia site
	// the first tab, never closed, shown only in the app's own documents.
	Pinned bool `json:"pinned,omitempty"`
}

// Branding is the product's name and accent as an app sets them, for the `branding` target. Declared values, no code: the
// page takes the name for its title and the sign-in and the accent for its colour.
type Branding struct {
	Name       string `json:"name"`
	Accent     string `json:"accent,omitempty"`     // #rrggbb
	SupportURL string `json:"supportUrl,omitempty"` // https://
	Tagline    string `json:"tagline,omitempty"`
}

// Chat is what the app adds to the chat.
type Chat struct {
	Commands []string `json:"commands,omitempty"`
	Cards    []string `json:"cards,omitempty"`
	// Starters are lines an empty chat offers under its field; a click puts the text into the field, it sends nothing.
	Starters []Starter `json:"starters,omitempty"`
	// Buttons stand at the chat's field: each runs one of the app's commands or opens one of its views in the pane.
	Buttons []ChatButton `json:"buttons,omitempty"`
	// Links are the addresses the app shows a card for: a message with such a link gets the card under it.
	Links []LinkCard `json:"links,omitempty"`
	// Mentions is what «@» in the chat's field finds in the app: a tool of its own MCP server, asked with what is typed.
	Mentions *ChatMentions `json:"mentions,omitempty"`
}

// ChatMentions is the app's part of «@»: Tool (<server>.<tool> of its .mcp.json) is called with {"query": "<typed>"}
// and answers {"items": [{"label", "detail", "insert"}]}; the palette lists them under Title.
type ChatMentions struct {
	Title string `json:"title"`
	Tool  string `json:"tool"`
}

const maxMentionTitle = 40

var mentionToolRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}\.[A-Za-z0-9_.-]{1,64}$`)

// Starter is a line an empty chat offers.
type Starter struct {
	Text string `json:"text"`
}

// ChatButton is a button of the app at the chat's field: Command (one of chat.commands) or View (a view with the pane
// target), one of the two.
type ChatButton struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Icon    string `json:"icon,omitempty"`
	Command string `json:"command,omitempty"`
	View    string `json:"view,omitempty"`
}

// LinkCard is a kind of address the app shows a card for. Match is https://<host>/<path> where the host may start with
// "*." and the path may end with "*"; Card is one of chat.cards, given the link as its data ({url}).
type LinkCard struct {
	Match string `json:"match"`
	Card  string `json:"card"`
}

// Starters, buttons and link kinds an app may give: more would crowd the field and the empty chat.
const (
	maxStarters  = 4
	maxButtons   = 3
	maxLinkCards = 8
	maxStarter   = 140
)

// linkMatchRe is the form of a link kind: https, a host (a leading "*." for its subdomains), a path that may end with *.
var linkMatchRe = regexp.MustCompile(`^https://(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(/[^\s*?#]*\*?)?$`)

// Backend is the app's own server.
type Backend struct {
	URL string `json:"url,omitempty"`
	// Tools are a builtin app's (builtin.go): its backend runs in the runtime's process. Instructions is what the host's
	// tools server says of them to the agent.
	Tools        []BackendTool `json:"tools,omitempty"`
	Instructions string        `json:"instructions,omitempty"`
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

// permissions, unsupportedPermissions and paramPermissions (a right written right:<value>) are made from the surface
// (surface.go).

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
	case IsBuiltin(m.ID):
		add("id", "%q is an app of the host's own", m.ID)
	}
	if m.Delivery != "" {
		add("delivery", "an installed app is a package: only the host's own apps are delivered otherwise")
	}
	if len(m.Points) > 0 {
		add("points", "only the host's own apps declare points for now: contribute to theirs (`sinopia`) instead")
	}
	if len(m.Aliases) > 0 {
		add("aliases", "only the host's own apps declare aliases")
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
		add("when", "not supported: a condition is set on tool rules by the document's status, agent.tools.when")
	}
	if len(m.Storage) > 0 {
		notYet("storage")
	}
	if m.Backend != nil {
		notYet("backend")
	}
	m.checkWorkflow(add)
	switch m.Trust {
	case TrustDeclarative, TrustSandboxed, TrustTrusted:
	case "":
		add("trust", "is required: declarative, sandboxed or trusted")
	default:
		add("trust", "%q is not declarative, sandboxed or trusted", m.Trust)
	}

	targets := map[string]bool{}
	if len(m.Targets) == 0 {
		add("targets", "is required: where the app works (chat, pane, app, settings, onboarding, object-action, hub-settings, branding, sinopia.<kind>)")
	}
	for i, t := range m.Targets {
		path := fmt.Sprintf("targets[%d]", i)
		if !validTarget(t) {
			add(path, "%q is not a target: chat, pane, app, settings, onboarding, object-action, hub-settings, branding, sinopia.<kind> or sinopia.*", t)
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
		if name == "secrets.write" && !secretRe.MatchString(value) {
			add(path, "secrets.write:%s: a secret name is letters, digits, '.', '_' and '-'", value)
		}
		if name == "events" && !slices.Contains(EventKinds, value) {
			add(path, "events:%s: the kinds are %s", value, strings.Join(EventKinds, ", "))
		}
		if name == "terminal.run" || name == "process" {
			if why := CheckCommandMask(value); why != "" {
				add(path, "the mask %q %s", value, why)
			}
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
				if field == "frames" && installAt(h, true) {
					// The address of a service of the installation, put in when the app is installed.
					continue
				}
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
		if cc := n.ClientCert; cc != nil {
			if !m.hasSecret(cc.Secret) {
				add("network.clientCert.secret", "%q is not one of the app's secrets: declare it in secrets (the certificate and its key as PEM) and ask for it in permissions", cc.Secret)
			}
			for i, h := range cc.Hosts {
				if !hostRe.MatchString(h) {
					add(fmt.Sprintf("network.clientCert.hosts[%d]", i), "%q is not a host name", h)
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
		if len(ag.Validators) > 0 && m.Trust != TrustTrusted {
			add("agent.validators", "validators run local scripts and need trust trusted; a %s app may not have them", m.Trust)
		}
		seenV := map[string]bool{}
		for i, v := range ag.Validators {
			path := fmt.Sprintf("agent.validators[%d]", i)
			if !kindRe.MatchString(v.ID) {
				add(path+".id", "%q: a validator id is lower case latin letters, digits and '-'", v.ID)
			} else if seenV[v.ID] {
				add(path+".id", "%q is used twice", v.ID)
			}
			seenV[v.ID] = true
			if v.On != ValidatorPostToolUse && v.On != ValidatorStop {
				add(path+".on", "%q is not %s or %s", v.On, ValidatorPostToolUse, ValidatorStop)
			}
			if v.Match != "" {
				if v.On != ValidatorPostToolUse {
					add(path+".match", "only a %s validator has a match: a Stop validator checks the whole turn", ValidatorPostToolUse)
				} else if why := checkToolPattern(v.Match); why != "" {
					add(path+".match", "%q: %s", v.Match, why)
				}
			}
			if strings.TrimSpace(v.Run) == "" {
				add(path+".run", "is required: the script of the package that checks")
			} else if filepath.IsAbs(v.Run) || strings.HasPrefix(v.Run, "/") || v.Run == ".." || strings.HasPrefix(filepath.ToSlash(filepath.Clean(v.Run)), "../") {
				add(path+".run", "%q must lead to a file inside the package", v.Run)
			}
			if v.MaxRetries < 0 || v.MaxRetries > ValidatorMaxRetries {
				add(path+".maxRetries", "%d is out of 1..%d (default %d)", v.MaxRetries, ValidatorMaxRetries, ValidatorDefaultRetries)
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
		} else if strings.HasPrefix(v.Entry, "https://") || installAt(v.Entry, false) {
			if m.Network == nil || !hostListed(m.Network.Frames, v.Entry) {
				add(path+".entry", "the host of %q must be in network.frames", v.Entry)
			}
		}
		if m.Trust == TrustDeclarative && (strings.HasPrefix(v.Entry, "https://") || installAt(v.Entry, false)) {
			add(path+".entry", "a declarative app serves its views from the package")
		}
		if v.Height != "" {
			notYet(path + ".height")
		}
		if len(v.Targets) == 0 {
			add(path+".targets", "is required: where the view shows")
		}
		for j, t := range v.Targets {
			if w := wildcardOf(t); !targets[t] && (w == "" || !targets[w]) {
				add(fmt.Sprintf("%s.targets[%d]", path, j), "%q is not in the app's targets", t)
			}
		}
		for j, t := range v.Targets {
			if t == TargetBranding {
				add(fmt.Sprintf("%s.targets[%d]", path, j), "branding has no view: declare the `branding` block")
			}
		}
		for j, o := range v.Objects {
			if !contains(ObjectKinds, o) {
				add(fmt.Sprintf("%s.objects[%d]", path, j), "%q is not an object kind: %s", o, strings.Join(ObjectKinds, ", "))
			}
		}
		if len(v.Objects) > 0 && !contains(v.Targets, TargetObjectAction) {
			add(path+".objects", "is for views of the object-action target")
		}
		if utf8.RuneCountInString(v.Label) > 40 {
			add(path+".label", "is up to 40 characters")
		}
	}

	m.checkBranding(targets, add)
	m.checkBudget(add)

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
		m.checkChatHost(c, add)
		if (len(c.Commands) > 0 || len(c.Cards) > 0 || len(c.Starters) > 0 || len(c.Buttons) > 0 || len(c.Links) > 0 || c.Mentions != nil) && !targets[TargetChat] {
			add("chat", "chat contributions need the chat target")
		}
	}
	if len(m.Sinopia) > 0 && !hasSinopia(targets) {
		add("sinopia", "sinopia contributions need a sinopia target")
	}
	m.checkSinopia(targets, add, warn)
	m.checkExtension(targets, add, warn)

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
	case target == TargetBranding:
		return m.Branding != nil
	case IsSinopiaTarget(target):
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
	w := wildcardOf(target)
	return w != "" && contains(m.Targets, w)
}

// wildcardOf is the wildcard alias that names the target (sinopia.* for sinopia.slides), "" for none. It covers the
// host's own document types, not those an app adds, which an app has to name to be in.
func wildcardOf(target string) string {
	_, scope, ok := aliasOwner(target)
	if !ok || scope == "*" || strings.Contains(scope, ".") {
		return ""
	}
	return target[:len(target)-len(scope)] + "*"
}

// TargetCovers says whether a listed target (of an app or a view) covers the target asked for: the same one, or a
// wildcard alias of a builtin app's screen over it (sinopia.* over sinopia.slides).
func TargetCovers(listed, target string) bool {
	if listed == target {
		return true
	}
	owner, scope, ok := aliasOwner(listed)
	if !ok || scope != "*" {
		return false
	}
	to, _, ok := aliasOwner(target)
	return ok && to == owner
}

// validTarget: a place of the host's own, or an alias of a builtin app's screen (builtin.go) with a document type
// after it (sinopia.slides, sinopia.acme.board) or its wildcard.
func validTarget(t string) bool {
	switch t {
	case TargetChat, TargetPane, TargetApp:
		return true
	case TargetSettings, TargetOnboarding, TargetObjectAction, TargetHubSettings, TargetBranding:
		return true
	}
	_, scope, ok := aliasOwner(t)
	return ok && (scope == "*" || kindRe.MatchString(scope) || appKindRe.MatchString(scope))
}

func hasViewTarget(targets map[string]bool) bool {
	return targets[TargetPane] || targets[TargetApp] || targets[TargetSettings] || targets[TargetOnboarding] ||
		targets[TargetObjectAction] || targets[TargetHubSettings]
}

// hasSinopia says whether a target names Sinopia's screen through one of its aliases.
func hasSinopia(targets map[string]bool) bool {
	for t := range targets {
		if IsSinopiaTarget(t) {
			return true
		}
	}
	return false
}

// IsSinopiaTarget says whether a target is an alias of Sinopia's screen.
func IsSinopiaTarget(t string) bool {
	owner, _, ok := aliasOwner(t)
	return ok && owner == SinopiaApp
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

var accentRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// checkBranding keeps the declared values plain: a short name, a colour, an https address.
func (m *Manifest) checkBranding(targets map[string]bool, add func(path, format string, a ...any)) {
	b := m.Branding
	if b == nil {
		return
	}
	if !targets[TargetBranding] {
		add("branding", "needs the branding target in targets")
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(b.Name)); n == 0 || n > 40 {
		add("branding.name", "is required, up to 40 characters")
	}
	if b.Accent != "" && !accentRe.MatchString(b.Accent) {
		add("branding.accent", "%q is not a colour #rrggbb", b.Accent)
	}
	if b.SupportURL != "" && !strings.HasPrefix(b.SupportURL, "https://") {
		add("branding.supportUrl", "must be an https:// address")
	}
	if utf8.RuneCountInString(b.Tagline) > 120 {
		add("branding.tagline", "is up to 120 characters")
	}
}

// checkChatHost checks what the app puts into the host's own chat: the starters, the buttons at the field and the
// kinds of link it shows a card for.
func (m *Manifest) checkChatHost(c *Chat, add func(field, format string, args ...any)) {
	if len(c.Starters) > maxStarters {
		add("chat.starters", "at most %d starters", maxStarters)
	}
	for i, st := range c.Starters {
		if text := strings.TrimSpace(st.Text); text == "" || utf8.RuneCountInString(text) > maxStarter {
			add(fmt.Sprintf("chat.starters[%d].text", i), "a starter is a line of 1 to %d characters", maxStarter)
		}
	}
	if len(c.Buttons) > maxButtons {
		add("chat.buttons", "at most %d buttons", maxButtons)
	}
	ids := map[string]bool{}
	for i, b := range c.Buttons {
		field := fmt.Sprintf("chat.buttons[%d]", i)
		if !viewIDRe.MatchString(b.ID) || ids[b.ID] {
			add(field+".id", "%q: a button needs an id of its own (a-z, 0-9, -)", b.ID)
		}
		ids[b.ID] = true
		if strings.TrimSpace(b.Title) == "" {
			add(field+".title", "a button needs a title: it is what people read on hover")
		}
		switch {
		case (b.Command == "") == (b.View == ""):
			add(field, "a button runs a command or opens a view: one of the two")
		case b.Command != "" && !slices.Contains(c.Commands, b.Command):
			add(field+".command", "%q is not one of chat.commands", b.Command)
		case b.View != "":
			if v := m.view(b.View); v == nil || !contains(v.Targets, TargetPane) {
				add(field+".view", "%q is not a view with the pane target", b.View)
			}
		}
	}
	if mn := c.Mentions; mn != nil {
		if title := strings.TrimSpace(mn.Title); title == "" || utf8.RuneCountInString(title) > maxMentionTitle {
			add("chat.mentions.title", "a title of 1 to %d characters: the palette lists the app's finds under it", maxMentionTitle)
		}
		if !mentionToolRe.MatchString(mn.Tool) {
			add("chat.mentions.tool", "%q: a tool of the app's MCP server, <server>.<tool>", mn.Tool)
		}
	}
	if len(c.Links) > maxLinkCards {
		add("chat.links", "at most %d kinds of link", maxLinkCards)
	}
	for i, l := range c.Links {
		field := fmt.Sprintf("chat.links[%d]", i)
		if strings.HasPrefix(l.Match, "https://*.") && !wideEnough(l.Match) {
			add(field+".match", "%q: a wildcard needs a host of at least three parts (*.bank.example), or it takes the links of a whole zone", l.Match)
		}
		if !linkMatchRe.MatchString(l.Match) {
			add(field+".match", "%q: a link kind is https://<host>/<path>, the host may start with *. and the path end with *", l.Match)
		}
		if !slices.Contains(c.Cards, l.Card) {
			add(field+".card", "%q is not one of chat.cards", l.Card)
		}
	}
}

// wideEnough says whether the host under a link kind's wildcard has two labels below its zone at least: *.bank.example
// and *.co.uk.example are, *.co.uk is not (it would take every link of a country's zone).
func wideEnough(match string) bool {
	host := strings.TrimPrefix(match, "https://*.")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	return strings.Count(host, ".") >= 1 && !(strings.Count(host, ".") == 1 && len(strings.Split(host, ".")[0]) <= 3)
}
