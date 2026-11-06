package apps

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// SecretSource is the machine's safe as far as the apps need it: the values of an app's secrets, by their own names.
type SecretSource interface {
	HasApp(app, name string) bool
	RevealApp(app string, names []string) (map[string]string, error)
}

// ProxyPrefix is where the runtime's MCP proxy for the apps' remote servers answers: <prefix><app>/<server>.
const ProxyPrefix = "/api/cli/apps/mcp/"

// What a launch does with the secrets of an app's MCP servers (an app lists them in secrets, a server names one with
// ${secret:<name>}):
//
//   - A remote server is never given a value. Its entry in the launch's .mcp.json points at the runtime's proxy
//     (ProxyPrefix, proxy.go) and carries the launch's own token, so no file holds the secret. The proxy checks that the
//     token's launch has the app on, puts the headers of the package with the secrets in, and calls the real address under
//     the same network rules as hl.tools.call.
//   - A local server of a trusted app takes the value in its env: that file has to hold it. It lives in a folder of this
//     launch under <data>/app-launch, which the wall leaves readable for this chat only (Seal.Open): another chat's
//     folder is not in the wall at all. The runtime removes the folder once no process of the launch is left
//     (sweepOverlays).
//
// The registry's package is never changed, so a new value is read at the next launch (or at the next call, for a remote
// server): nothing is reinstalled. A server whose secret has no value works as far as the app waits for it: the proxy
// answers 409, a local server is left out.

// tokenName is the launch's token file in its folder.
const tokenName = "token"

// sessionDir is the folder of one launch under <data>/app-launch: its token, its grant, its MCP configs and the plugin
// folders of its apps. The wall opens it read-only for the chat of that launch and for no other.
func (l *Launcher) sessionDir(session string) string { return filepath.Join(l.overlayRoot(), session) }

// OverlayRoot is the folder of the launches' own plugin folders, outside the registry (and so outside the read-only
// part of the wall).
func (l *Launcher) overlayRoot() string {
	return filepath.Join(filepath.Dir(l.Registry.Root), "app-launch")
}

// proxyOrigin and tokenHeaders come from the runtime's own MCP config: the address agents reach the runtime at, and the
// names of the headers its token goes in.
func (l *Launcher) proxyOrigin() (origin string, headers []string) {
	raw, err := os.ReadFile(l.MCPConfigPath)
	if err != nil {
		return "", nil
	}
	var cfg struct {
		Servers map[string]struct {
			URL     string         `json:"url"`
			Headers map[string]any `json:"headers"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return "", nil
	}
	for _, srv := range cfg.Servers {
		u, err := url.Parse(srv.URL)
		if err != nil || u.Host == "" {
			continue
		}
		for k := range srv.Headers {
			headers = append(headers, k)
		}
		return u.Scheme + "://" + u.Host, headers
	}
	return "", nil
}

// pluginDir is the folder Claude loads an app's package from in a launch: the package itself, unless a server of its
// .mcp.json names a secret. Then it is a folder of this launch's own: every entry of the package is a link to the real
// one except .mcp.json, written anew. mine are the folders the chat's wall must leave readable.
func (l *Launcher) pluginDir(a Installed, session, token string) (dir string, err error) {
	raw, rerr := os.ReadFile(filepath.Join(a.Dir, ".mcp.json"))
	if rerr != nil || !strings.Contains(string(raw), "${secret:") && !strings.Contains(string(raw), `"connection"`) {
		return a.Dir, nil
	}
	var doc map[string]any
	pkg, _ := CachedPackage(a.Dir)
	if pkg == nil || json.Unmarshal(raw, &doc) != nil {
		return a.Dir, nil
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	origin, tokenHeaders := l.proxyOrigin()
	for name, v := range servers {
		srv, _ := v.(map[string]any)
		decl, ok := pkg.MCP[name]
		if ok && decl.Connection != "" {
			l.connectionEntry(servers, name, srv, decl, a.ID, session, token, origin, tokenHeaders)
			continue
		}
		if !ok || len(decl.Secrets()) == 0 {
			continue
		}
		names := decl.Secrets()
		listed := true
		for _, n := range names {
			listed = listed && a.Manifest.hasSecret(n)
		}
		switch {
		case !listed || token == "" || session == "":
			delete(servers, name)
		case decl.Command == "":
			// Remote: the entry names the proxy and the launch's token; the real address and the headers stay in the package.
			if origin == "" || len(tokenHeaders) == 0 {
				jsonlog.Log("warn", "apps: a server that uses a secret is left out: the runtime's address is unknown", jsonlog.F{K: "app", V: a.ID})
				delete(servers, name)
				continue
			}
			headers := map[string]any{}
			for _, h := range tokenHeaders {
				headers[h] = token
			}
			srv["url"] = origin + ProxyPrefix + a.ID + "/" + name
			srv["headers"] = headers
		default:
			values, err := l.Secrets.RevealApp(a.ID, names)
			if err != nil {
				jsonlog.Log("info", "apps: a local server is left out of the launch: its secret has no value",
					jsonlog.F{K: "app", V: a.ID}, jsonlog.F{K: "server", V: name})
				delete(servers, name)
				continue
			}
			env, _ := srv["env"].(map[string]any)
			for k, val := range env {
				if s, ok := val.(string); ok {
					env[k], _ = FillSecrets(s, values)
				}
			}
		}
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	dir = filepath.Join(l.sessionDir(session), a.ID)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(a.Dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Name() == ".mcp.json" {
			continue
		}
		if err := os.Symlink(filepath.Join(a.Dir, e.Name()), filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	if err := writeFileAtomic(filepath.Join(dir, ".mcp.json"), append(body, '\n')); err != nil {
		return "", err
	}
	return dir, nil
}

func writeGrant(sessionDir string, apps []string, conversation string) error {
	if apps == nil {
		apps = []string{}
	}
	raw, err := json.Marshal(map[string]any{"apps": apps, "conversation": conversation})
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(sessionDir, "grant.json"), raw)
}

// GrantedApps are the apps a launch's token may use the MCP proxy for; none when the launch left no record.
func (r *Registry) GrantedApps(session string) []string {
	apps, _ := r.Grant(session)
	return apps
}

// Grant is a launch's record: the apps its token may use and the chat it was made for ("" when the runtime did not know
// the chat). The agent cannot write it: the launch's folder is a read-only bind in the wall (Seal.Open).
func (r *Registry) Grant(session string) (apps []string, conversation string) {
	if session == "" || strings.ContainsAny(session, `/\.`) {
		return nil, ""
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(r.Root), "app-launch", session, "grant.json"))
	if err != nil {
		return nil, ""
	}
	var g struct {
		Apps         []string `json:"apps"`
		Conversation string   `json:"conversation"`
	}
	if json.Unmarshal(raw, &g) != nil {
		return nil, ""
	}
	return g.Apps, g.Conversation
}

// overlayGrace is how long a new launch's folder is left alone: its process may not have started yet.
const overlayGrace = 5 * time.Minute

// sweepLaunches removes the folders of launches whose agent is gone (no process has the launch's token file in its
// environment), which takes their tokens and the env values of local servers with them. Where /proc cannot be read
// nothing is removed.
func (l *Launcher) sweepLaunches() {
	entries, err := os.ReadDir(l.overlayRoot())
	if err != nil {
		return
	}
	live, ok := liveTokenFiles()
	if !ok {
		return
	}
	for _, e := range entries {
		dir := filepath.Join(l.overlayRoot(), e.Name())
		if info, err := e.Info(); err != nil || time.Since(info.ModTime()) < overlayGrace {
			continue
		}
		if !live[filepath.Clean(filepath.Join(dir, tokenName))] {
			_ = os.RemoveAll(dir)
		}
	}
}

// dropLegacyLaunchFiles removes what older launches left in the registry's launch folder, where every chat's agent could
// read it: tokens, grants and the MCP configs that carry them. Nothing goes while an agent still runs on one of them.
func (l *Launcher) dropLegacyLaunchFiles(dir string) {
	live, ok := liveTokenFiles()
	if !ok {
		return
	}
	old, _ := filepath.Glob(filepath.Join(dir, "agent-token-*"))
	for _, f := range old {
		if live[filepath.Clean(f)] {
			return
		}
	}
	for _, pattern := range []string{"agent-token-*", "grant-*.json", "mcp-*.json", "hmcp-*.json", "rmcp-*.json", "xmcp-*.json"} {
		found, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, f := range found {
			_ = os.Remove(f)
		}
	}
}

// liveTokenFiles are the token files named in the environment of the running processes.
func liveTokenFiles() (map[string]bool, bool) {
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	out := map[string]bool{}
	readable := 0
	for _, p := range procs {
		raw, err := os.ReadFile("/proc/" + p.Name() + "/environ")
		if err != nil {
			continue
		}
		readable++
		for _, kv := range strings.Split(string(raw), "\x00") {
			if v, ok := strings.CutPrefix(kv, "HARNESSLY_CLI_TOKEN_FILE="); ok {
				out[filepath.Clean(v)] = true
			}
		}
	}
	// Not even our own environment was readable: this is no /proc to judge by.
	return out, readable > 0
}
