package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

// The person's MCP servers (Settings → Connections, internal/mcpconn) as the agents get them: each enabled server is
// one more MCP server named mcp-<slug> whose address is the runtime's proxy (/api/cli/mcp-proxy/<id>) on loopback,
// with the runtime's own token in its header. The service's credentials never reach an agent: the proxy adds them.

// mcpProxyPath is the proxy's route; a server's address is this plus its id.
const mcpProxyPath = "/api/cli/mcp-proxy/"

// mcpAgentServerName is the name an agent knows a server by: Claude's tools come as mcp__mcp-<slug>__<tool>.
func mcpAgentServerName(srv mcpconn.Server) string { return "mcp-" + srv.Slug }

// mcpStaleAfter is how long the config files of earlier launches are kept: a launch reads its file when the CLI
// starts, a few seconds after the line is typed.
const mcpStaleAfter = time.Hour

// mcpClaudeConfig is claude.LaunchConfig.ExtraMCP: the --mcp-config file of the enabled servers, written by content in
// dir (0600, it carries the runtime's token), and the rules that allow their tools. The token, its header and the
// runtime's loopback address come from the runtime's own config (runtimeMCP, preview-mcp.json), read at every launch;
// "" when there is no enabled server or that config is unreadable.
func mcpClaudeConfig(enabled func() []mcpconn.Server, runtimeMCP, dir string) func() (string, []string) {
	return func() (string, []string) {
		list := enabled()
		if len(list) == 0 {
			return "", nil
		}
		base, headers, ok := runtimeMCPEndpoint(runtimeMCP)
		if !ok {
			return "", nil
		}
		servers := map[string]any{}
		allow := make([]string, 0, len(list))
		for _, srv := range list {
			if srv.Slug == "" || srv.ID == "" {
				continue
			}
			name := mcpAgentServerName(srv)
			servers[name] = map[string]any{"type": "http", "url": base + mcpProxyPath + url.PathEscape(srv.ID), "headers": headers}
			allow = append(allow, "mcp__"+name)
		}
		if len(servers) == 0 {
			return "", nil
		}
		body, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
		if err != nil {
			return "", nil
		}
		path, err := writeMCPByContent(dir, body)
		if err != nil {
			return "", nil
		}
		return path, allow
	}
}

// runtimeMCPEndpoint reads the runtime's own MCP config: the scheme and host of its first server with an address, and
// that server's headers (the token).
func runtimeMCPEndpoint(path string) (base string, headers map[string]any, ok bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, false
	}
	var cfg struct {
		Servers map[string]struct {
			URL     string         `json:"url"`
			Headers map[string]any `json:"headers"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return "", nil, false
	}
	for _, srv := range cfg.Servers {
		u, err := url.Parse(srv.URL)
		if err != nil || u.Host == "" || len(srv.Headers) == 0 {
			continue
		}
		return u.Scheme + "://" + u.Host, srv.Headers, true
	}
	return "", nil, false
}

// writeMCPByContent writes body to <dir>/servers-<hash>.json unless it is there, removes the files of earlier launches,
// and gives the path.
func writeMCPByContent(dir string, body []byte) (string, error) {
	sum := sha256.Sum256(body)
	path := filepath.Join(dir, "servers-"+hex.EncodeToString(sum[:8])+".json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if old, _ := filepath.Glob(filepath.Join(dir, "servers-*.json")); len(old) > 0 {
		for _, f := range old {
			if info, err := os.Stat(f); f != path && err == nil && time.Since(info.ModTime()) > mcpStaleAfter {
				_ = os.Remove(f)
			}
		}
	}
	if have, err := os.ReadFile(path); err == nil && string(have) == string(body) {
		return path, nil
	}
	tmp, err := os.CreateTemp(dir, ".servers-*.tmp")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return path, nil
}

// mcpCodexServers is codex.LaunchConfig.ExtraMCP: the enabled servers at the proxy on the runtime's port, with the
// token header and variable of the runtime's own server.
func mcpCodexServers(enabled func() []mcpconn.Server, own codex.MCP, port int) func() []codex.MCP {
	return func() []codex.MCP {
		if port <= 0 || own.TokenHeader == "" || own.TokenEnv == "" {
			return nil
		}
		var out []codex.MCP
		for _, srv := range enabled() {
			if srv.Slug == "" || srv.ID == "" {
				continue
			}
			out = append(out, codex.MCP{Server: mcpAgentServerName(srv), URL: codex.MCPURL(port, mcpProxyPath+url.PathEscape(srv.ID)),
				TokenHeader: own.TokenHeader, TokenEnv: own.TokenEnv})
		}
		return out
	}
}

// mcpEnabledOf reads the enabled servers from the data folder at every call (the page changes them while the runtime
// runs); nil without a data folder.
func mcpEnabledOf(dataDir string) func() []mcpconn.Server {
	if strings.TrimSpace(dataDir) == "" {
		return func() []mcpconn.Server { return nil }
	}
	svc := mcpconn.New(dataDir, nil, nil)
	return svc.Enabled
}

// appConnections is apps.ConnectionSource over the person's connections of this machine: a launch reads the file at
// every call, so a sign-in made while the runtime runs counts at the next launch.
type appConnections struct{ ms *mcpconn.Service }

func (a appConnections) Transport(id string) (string, bool) {
	srv, ok := a.ms.ByCatalog(id)
	return string(srv.Transport), ok
}
