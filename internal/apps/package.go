package apps

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

// A package on disk: the manifest plus the files it names. Every path the manifest names stays inside the package (no
// "..", no absolute path, no link that leads out), so an installed app cannot reach the rest of the machine through its
// own files.

const (
	// MaxPackageBytes is the most a package may weigh, every file counted.
	MaxPackageBytes = 50 << 20
	// MaxInstructionsBytes is the most the always-in-context instructions may weigh; past InstructionsWarnBytes the
	// check warns that every turn pays for them.
	MaxInstructionsBytes  = 16 << 10
	InstructionsWarnBytes = 4 << 10
	// maxPackageFiles bounds the walk.
	maxPackageFiles = 5000
)

// Package is a package read from a folder.
type Package struct {
	Dir      string
	Manifest *Manifest
	// MCP is the package's .mcp.json servers by name, as written.
	MCP map[string]MCPServer
	// PluginParts are the parts of a Claude Code plugin the package carries beyond what an app is made of: hooks that
	// run commands on the agent's events, agents, output styles and the like (pluginParts). A declarative or sandboxed
	// package may carry none; a trusted one shows them in its permissions.
	PluginParts []string
}

// MCPServer is one server of .mcp.json: a remote one (url) or, for a trusted app, a local process (command).
type MCPServer struct {
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Connection makes the server the person's own connection (Settings → Connections, internal/mcpconn) from the
	// catalog entry of that id: the address and the sign-in are the connection's, so a server with it names no url,
	// headers or command. Nobody's token is in the package.
	Connection string `json:"connection,omitempty"`
	// Auth says how the host signs the person in to the server (docs/build-apps/10-auth.md); nil: as the headers say.
	Auth *ServerAuth `json:"auth,omitempty"`
	// Credentialed: the headers carry a credential (a secret the install keeps, a connection's token, the platform's
	// token). A 401 to such a call is an expired sign-in, not a missing one. Set by the host, never read from a package.
	Credentialed bool `json:"-"`
	// ClientCert is the app's certificate for mutual TLS to this server (network.clientCert); set by the host, never read
	// from a package, and used for this server's calls only.
	ClientCert *tls.Certificate `json:"-"`
}

// The kinds of ServerAuth.
const (
	// AuthPlatform: the host puts the platform's JWT for the person (apptoken) in Authorization.
	AuthPlatform = "platform"
	// AuthOIDC: the host puts the organization's identity provider's token for the server, got by token exchange.
	AuthOIDC = "oidc"
	// AuthOAuth: the person signs in to the server itself, the way a connection does (MCP authorization: discovery,
	// dynamic registration, PKCE); the host keeps the token in the safe and puts it in Authorization.
	AuthOAuth = "oauth"
	// AuthSecret: the headers carry ${secret:…} of the install, as without auth.
	AuthSecret = "secret"
)

// ServerAuth is how the host signs the person in to an app's remote server. A platform token's aud is always the
// server's origin (https://host[:port]): an app cannot ask a token for another backend and send it to its own address.
// Audience names the identity provider's audience for oidc, which the hub's admin must have allowed for that origin
// (HUB_APP_TOKEN_EXCHANGE_AUDIENCES); the origin when empty. Scope is asked of the provider for oidc and oauth. The
// view never sees a token of any kind: the host adds it to the calls it makes for the view and the agent.
type ServerAuth struct {
	Type     string `json:"type"`
	Audience string `json:"audience,omitempty"`
	Scope    string `json:"scope,omitempty"`
}

// AudienceFor is the audience of the server's token: the origin of its url, or for oidc the declared one.
func (s MCPServer) AudienceFor() string {
	if s.Auth != nil && s.Auth.Type == AuthOIDC && s.Auth.Audience != "" {
		return s.Auth.Audience
	}
	return s.Origin()
}

// Origin is the origin (scheme://host[:port]) of the server's url; "" for a server without one.
func (s MCPServer) Origin() string {
	u, err := url.Parse(s.URL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// authProblems is what is wrong with a server's auth.
func authProblems(s MCPServer) []string {
	a := s.Auth
	if a == nil {
		return nil
	}
	var out []string
	switch a.Type {
	case AuthPlatform, AuthOIDC, AuthOAuth, AuthSecret:
	default:
		return []string{fmt.Sprintf("auth.type %q is none of platform, oidc, oauth, secret", a.Type)}
	}
	if s.Connection != "" {
		out = append(out, "a server with a connection signs in by the connection: no auth")
	}
	if s.Command != "" || s.URL == "" {
		out = append(out, "auth is for a remote server (url)")
	}
	if a.Type == AuthPlatform && a.Audience != "" && a.Audience != s.Origin() {
		out = append(out, "a platform token's audience is the server's own origin ("+s.Origin()+"): drop auth.audience")
	}
	if a.Type != AuthSecret {
		for k := range s.Headers {
			if strings.EqualFold(k, "Authorization") {
				out = append(out, "auth "+a.Type+" puts its own Authorization header: drop the one in headers")
			}
		}
	}
	return out
}

// connectionProblems is what is wrong with a server that names a connection: it is a remote one (http or sse) whose
// address and sign-in come from the person's connection, so it brings no address, credentials or process of its own.
func connectionProblems(s MCPServer) []string {
	var out []string
	if !mcpconn.KnownCatalogID(s.Connection) {
		out = append(out, fmt.Sprintf("connection %q is not in the catalog of connections", s.Connection))
	}
	if s.Type != "" && s.Type != "http" && s.Type != "sse" {
		out = append(out, "a server with a connection is remote: its type is http or sse")
	}
	if s.Command != "" || len(s.Args) > 0 || len(s.Env) > 0 {
		out = append(out, "a server with a connection is a remote one: it has no command, args or env")
	}
	if s.URL != "" || len(s.Headers) > 0 {
		out = append(out, "a server with a connection takes its address and sign-in from the connection: no url or headers")
	}
	return out
}

// Connections are the catalog ids of the connections the package's servers name, sorted, each once.
func (p *Package) Connections() []string {
	var out []string
	for _, s := range p.MCP {
		if s.Connection != "" && !slices.Contains(out, s.Connection) {
			out = append(out, s.Connection)
		}
	}
	slices.Sort(out)
	return out
}

// loads counts the full reads of a package (a test watches that a repeated call does not make another).
var loads atomic.Int64

type cachedPackage struct {
	stamp string
	pkg   *Package
	ps    Problems
}

var packageCache sync.Map // abs dir -> cachedPackage

// packageStamp is what a package read depends on without walking its files: the manifest, .mcp.json, the plugin's
// manifest and the package's top folder (a part of a plugin appearing in it). Size and modification time of each.
func packageStamp(dir string) string {
	var b strings.Builder
	for _, rel := range []string{".", ManifestPath, ".mcp.json", ".claude-plugin/plugin.json"} {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			b.WriteString("-;")
			continue
		}
		fmt.Fprintf(&b, "%d:%d:%d;", info.Size(), info.ModTime().UnixNano(), changedAt(info))
	}
	return b.String()
}

// CachedPackage is LoadPackage for a caller that asks again and again (every tool call, every Sinopia call, every row of
// a list): the package is read once and kept until the stamp of its manifest files changes. The Package it returns is
// shared: read it, do not change it.
func CachedPackage(dir string) (*Package, Problems) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return LoadPackage(dir)
	}
	stamp := packageStamp(abs)
	if v, ok := packageCache.Load(abs); ok {
		if c := v.(cachedPackage); c.stamp == stamp {
			return c.pkg, c.ps
		}
	}
	pkg, ps := LoadPackage(abs)
	packageCache.Store(abs, cachedPackage{stamp: stamp, pkg: pkg, ps: ps})
	return pkg, ps
}

// LoadPackage reads the package in dir and checks it whole. The package comes back whenever the manifest could be read,
// with the problems found; nil means there is no manifest to read.
func LoadPackage(dir string) (*Package, Problems) {
	loads.Add(1)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, Problems{{Message: err.Error()}}
	}
	raw, err := os.ReadFile(filepath.Join(abs, ManifestPath))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Problems{{Path: ManifestPath, Message: "no manifest: an app package has harnessly/app.json (harnessly app new makes one)"}}
	}
	if err != nil {
		return nil, Problems{{Path: ManifestPath, Message: err.Error()}}
	}
	m, ps := ParseManifest(raw)
	if m == nil {
		for i := range ps {
			ps[i].Path = ManifestPath
		}
		return nil, ps
	}
	p := &Package{Dir: abs, Manifest: m}
	ps = append(ps, p.checkFiles()...)
	return p, ps
}

// checkFiles is the part of the check that reads the package's files.
func (p *Package) checkFiles() Problems {
	var ps Problems
	add := func(path, format string, a ...any) {
		ps = append(ps, Problem{Path: path, Message: fmt.Sprintf(format, a...)})
	}
	warn := func(path, format string, a ...any) {
		ps = append(ps, Problem{Path: path, Message: fmt.Sprintf(format, a...), Warning: true})
	}
	m := p.Manifest

	var total int64
	files := 0
	_ = filepath.WalkDir(p.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(p.Dir, path)
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		files++
		if files > maxPackageFiles {
			add("", "more than %d files", maxPackageFiles)
			return filepath.SkipAll
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if _, err := p.inside(rel); err != nil {
				add(filepath.ToSlash(rel), "the link leads out of the package")
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	if total > MaxPackageBytes {
		add("", "the package weighs %d MB, at most %d MB", total>>20, MaxPackageBytes>>20)
	}

	if ag := m.Agent; ag != nil {
		if ag.Instructions != "" {
			if info, err := p.stat("agent.instructions", ag.Instructions, &ps); err == nil {
				switch {
				case info.Size() > MaxInstructionsBytes:
					add("agent.instructions", "%s weighs %d KB, at most %d KB: move the rest into a skill", ag.Instructions, info.Size()>>10, MaxInstructionsBytes>>10)
				case info.Size() > InstructionsWarnBytes:
					warn("agent.instructions", "%s weighs %d KB: every turn of the chat carries it, keep it under %d KB", ag.Instructions, info.Size()>>10, InstructionsWarnBytes>>10)
				}
			}
		}
		for i, s := range ag.Skills {
			_, _ = p.stat(fmt.Sprintf("agent.skills[%d]", i), "skills/"+s+"/SKILL.md", &ps)
		}
		for i, v := range ag.Validators {
			if strings.TrimSpace(v.Run) != "" {
				_, _ = p.stat(fmt.Sprintf("agent.validators[%d].run", i), v.Run, &ps)
			}
		}
		if ag.Flow != "" {
			p.checkJSON("agent.flow", ag.Flow, &ps)
		}
	}

	p.PluginParts = p.pluginParts()
	if m.Agent != nil && len(m.Agent.Validators) > 0 {
		// Local scripts on the agent's events: listed with the rights a person agrees to at install and update. (A
		// package that is not trusted is refused by Check.)
		ids := make([]string, 0, len(m.Agent.Validators))
		for _, v := range m.Agent.Validators {
			ids = append(ids, v.ID)
		}
		p.PluginParts = append(p.PluginParts, "agent.validators: "+strings.Join(ids, ", "))
	}
	for _, part := range p.PluginParts {
		if strings.HasPrefix(part, "agent.validators:") {
			if m.Trust == TrustTrusted {
				warn("agent.validators", "this trusted app runs local scripts (%s) on the agent's events; they are listed with its rights", strings.TrimPrefix(part, "agent.validators: "))
			}
			continue
		}
		if m.Trust == TrustTrusted {
			warn(part, "this trusted app carries %s, a part of a Claude Code plugin that runs on the agent's events or outside the app's rules; it is listed with its rights", part)
		} else {
			add(part, "a %s app may not carry %s: it is a part of a Claude Code plugin that runs outside the app's rules (only skills/, commands/, .mcp.json with remote servers, harnessly/, ui/ and the app's own data); a trusted app may", m.Trust, part)
		}
	}

	mcpRaw, err := p.read(".mcp.json")
	switch {
	case err == nil:
		var doc struct {
			MCPServers map[string]MCPServer `json:"mcpServers"`
		}
		if err := json.Unmarshal(mcpRaw, &doc); err != nil {
			add(".mcp.json", "%v", err)
			break
		}
		p.MCP = doc.MCPServers
		for name, s := range doc.MCPServers {
			path := ".mcp.json:" + name
			switch {
			case name == hostServer:
				add(path, "%s is the host's own server: name yours otherwise", hostServer)
			case s.Command != "" && m.Trust != TrustTrusted:
				add(path, "a local server (command) needs trust trusted; a %s app uses remote servers (url)", m.Trust)
			case s.Connection != "":
				for _, why := range connectionProblems(s) {
					add(path, "%s", why)
				}
			case s.Command == "" && !strings.HasPrefix(s.URL, "https://"):
				add(path, "a remote server has an https:// url")
			}
			for _, why := range authProblems(s) {
				add(path, "%s", why)
			}
		}
	case !errors.Is(err, fs.ErrNotExist):
		add(".mcp.json", "%v", err)
	}
	for _, why := range m.secretUseProblems(p.MCP) {
		add(".mcp.json", "%s", why)
	}
	if ag := m.Agent; ag != nil {
		for i, name := range ag.MCP {
			if _, ok := p.MCP[name]; !ok {
				add(fmt.Sprintf("agent.mcp[%d]", i), "there is no server %q in .mcp.json", name)
			}
		}
	}

	for i, v := range m.Views {
		if v.Entry == "" || strings.HasPrefix(v.Entry, "https://") {
			continue
		}
		field := fmt.Sprintf("views[%d].entry", i)
		if _, err := p.stat(field, v.Entry, &ps); err == nil {
			p.checkInlineScripts(field, v.Entry, &ps)
		}
	}
	if c := m.Chat; c != nil {
		for i, cmd := range c.Commands {
			_, _ = p.stat(fmt.Sprintf("chat.commands[%d]", i), "commands/"+strings.TrimPrefix(cmd, "/")+".md", &ps)
		}
		if c.Mentions != nil && c.Mentions.Tool != "" {
			if _, _, err := p.CanCallTool(c.Mentions.Tool); err != nil {
				ps = append(ps, Problem{Path: "chat.mentions.tool", Message: err.Error()})
			}
		}
	}

	if sin, err := m.SinopiaOf(); err == nil {
		if sin.Bindings != nil && sin.Bindings.Schema != "" {
			p.checkJSON("sinopia.bindings.schema", sin.Bindings.Schema, &ps)
		}
		for i, k := range sin.Kinds {
			path := fmt.Sprintf("sinopia.kinds[%d]", i)
			if k.Schema != "" {
				p.checkJSON(path+".schema", k.Schema, &ps)
			}
			if k.Icon != "" {
				_, _ = p.stat(path+".icon", k.Icon, &ps)
			}
			for from, file := range k.Migrations {
				if file == "" {
					continue
				}
				if _, err := p.stat(path+".migrations."+from, file, &ps); err != nil {
					continue
				}
				raw, err := p.read(file)
				if err != nil {
					continue
				}
				if _, err := ParsePatch(raw); err != nil {
					add(path+".migrations."+from, "%s: %v", file, err)
				}
			}
		}
		for i, t := range sin.Templates {
			_, _ = p.stat(fmt.Sprintf("sinopia.templates[%d]", i), strings.TrimSuffix(t, "/")+"/"+TemplateBoard, &ps)
		}
	}

	if raw, err := p.read(".claude-plugin/plugin.json"); err == nil {
		var pj struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &pj) != nil || pj.Name == "" {
			add(".claude-plugin/plugin.json", "is not a plugin manifest with a name")
		} else if pj.Name != PluginName(m.ID) {
			warn(".claude-plugin/plugin.json", "name %q differs from %q: the install writes its own", pj.Name, PluginName(m.ID))
		}
	}
	return ps
}

// PluginName is the Claude Code plugin name of an app: its skills are <plugin>:<skill> to the agent.
func PluginName(id string) string { return strings.ReplaceAll(id, ".", "-") }

// inside resolves a path of the manifest to a file of the package, refusing one that leaves it.
func (p *Package) inside(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%q is not a path inside the package", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q leads out of the package", rel)
	}
	full := filepath.Join(p.Dir, clean)
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return full, err
	}
	root, err := filepath.EvalSymlinks(p.Dir)
	if err != nil {
		return full, err
	}
	if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%q leads out of the package", rel)
	}
	return real, nil
}

func (p *Package) read(rel string) ([]byte, error) {
	full, err := p.inside(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func (p *Package) stat(field, rel string, ps *Problems) (fs.FileInfo, error) {
	full, err := p.inside(rel)
	if err == nil {
		var info fs.FileInfo
		info, err = os.Stat(full)
		if err == nil && info.IsDir() {
			err = fmt.Errorf("%s is a folder", rel)
		}
		if err == nil {
			return info, nil
		}
	}
	msg := err.Error()
	if errors.Is(err, fs.ErrNotExist) {
		msg = fmt.Sprintf("there is no file %s in the package", rel)
	}
	*ps = append(*ps, Problem{Path: field, Message: msg})
	return nil, err
}

func (p *Package) checkJSON(field, rel string, ps *Problems) {
	if _, err := p.stat(field, rel, ps); err != nil {
		return
	}
	raw, _ := p.read(rel)
	if !json.Valid(raw) {
		*ps = append(*ps, Problem{Path: field, Message: rel + " is not valid JSON"})
	}
}

// File reads a file of the package by a manifest path.
func (p *Package) File(rel string) ([]byte, error) { return p.read(rel) }

// pluginPartDirs are the folders of a Claude Code plugin that an app does not use and that Claude loads from the root of
// a plugin by default; pluginPartFiles are the files. Names are compared in lower case: on a case-insensitive disk
// Hooks/ is hooks/.
var pluginPartDirs = []string{"hooks", "agents", "output-styles", "monitors", "themes", "bin"}
var pluginPartFiles = []string{"settings.json", ".lsp.json"}

// pluginManifestKeys are the keys of .claude-plugin/plugin.json that point the plugin at such parts (or declare MCP
// servers inline, past the check of .mcp.json).
var pluginManifestKeys = []string{"hooks", "agents", "outputStyles", "lspServers", "monitors", "themes", "mcpServers", "userConfig", "channels"}

// pluginParts lists the parts of a Claude Code plugin the package carries: a folder with a trailing slash, a file by its
// name, a key of .claude-plugin/plugin.json as ".claude-plugin/plugin.json: <key>".
func (p *Package) pluginParts() []string {
	var out []string
	if entries, err := os.ReadDir(p.Dir); err == nil {
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			isDir := e.IsDir()
			if e.Type()&fs.ModeSymlink != 0 {
				if info, err := os.Stat(filepath.Join(p.Dir, e.Name())); err == nil {
					isDir = info.IsDir()
				}
			}
			switch {
			case isDir && slices.Contains(pluginPartDirs, name):
				out = append(out, e.Name()+"/")
			case !isDir && slices.Contains(pluginPartFiles, name):
				out = append(out, e.Name())
			}
		}
	}
	if raw, err := p.read(".claude-plugin/plugin.json"); err == nil {
		var pj map[string]json.RawMessage
		if json.Unmarshal(raw, &pj) == nil {
			for _, k := range pluginManifestKeys {
				if _, ok := pj[k]; ok {
					out = append(out, ".claude-plugin/plugin.json: "+k)
				}
			}
		}
	}
	return out
}

// scriptTag is an opening <script> tag with its attributes.
var scriptTag = regexp.MustCompile(`(?is)<script\b([^>]*)>`)

// scriptSrc is the src attribute of a script tag: its code is a file.
var scriptSrc = regexp.MustCompile(`(?i)\bsrc\s*=`)

// dataScript is a script tag that holds data, not code (JSON, an import map is code to the CSP too).
var dataScript = regexp.MustCompile(`(?i)\btype\s*=\s*["']?application/(ld\+)?json`)

// checkInlineScripts warns of a script written into the page: a view's CSP runs script files only (script-src 'self'),
// so the code would never run and the view would stand still without an error of its own.
func (p *Package) checkInlineScripts(field, rel string, ps *Problems) {
	if !strings.HasSuffix(strings.ToLower(rel), ".html") {
		return
	}
	full, err := p.inside(rel)
	if err != nil {
		return
	}
	page, err := os.ReadFile(full)
	if err != nil {
		return
	}
	for _, m := range scriptTag.FindAllSubmatch(page, -1) {
		attrs := m[1]
		if scriptSrc.Match(attrs) || dataScript.Match(attrs) {
			continue
		}
		*ps = append(*ps, Problem{Path: field, Warning: true,
			Message: rel + " has a <script> written into the page: a view runs script files only (put the code in a .js file and load it with <script src>)"})
		return
	}
}
