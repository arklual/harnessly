package codex

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// MCP is the runtime's own MCP server as Codex is told about it. The token travels in an environment variable the
// launcher sets, never in argv.
type MCP struct {
	// Server is the server's name in Codex's config; URL its address.
	Server string
	URL    string
	// TokenHeader is the header the server checks, TokenEnv the variable Codex reads its value from.
	TokenHeader string
	TokenEnv    string
}

// MCPArgs are the two `-c mcp_servers.<name>.…` flags.
func MCPArgs(m MCP) []string {
	return []string{
		"-c", "mcp_servers." + m.Server + ".url=" + string(nodejson.Marshal(m.URL)),
		"-c", "mcp_servers." + m.Server + ".env_http_headers={" + string(nodejson.Marshal(m.TokenHeader)) + "=" + string(nodejson.Marshal(m.TokenEnv)) + "}",
	}
}

// TOMLBasicString encodes text as a one-line TOML basic string (a JSON string is one).
func TOMLBasicString(s string) string { return string(nodejson.Marshal(s)) }

// LauncherFlags are `--model '<m>' --effort '<e>'` for the launcher script, which forwards them; checked first.
func LauncherFlags(l *agents.Launch) ([]string, error) {
	if err := agents.CheckLaunch(l); err != nil || l == nil {
		return nil, err
	}
	var flags []string
	if l.Model != nil && *l.Model != "" {
		flags = append(flags, "--model", agents.ShellQuote(*l.Model))
	}
	if l.Effort != nil && *l.Effort != "" {
		flags = append(flags, "--effort", agents.ShellQuote(*l.Effort))
	}
	return flags, nil
}

// RuntimeLaunchArgs are codex's own `-m <model> -c model_reasoning_effort="<e>"` (argv, no shell).
func RuntimeLaunchArgs(l *agents.Launch) ([]string, error) {
	if err := agents.CheckLaunch(l); err != nil || l == nil {
		return nil, err
	}
	var args []string
	if l.Model != nil && *l.Model != "" {
		args = append(args, "-m", *l.Model)
	}
	if l.Effort != nil && *l.Effort != "" {
		args = append(args, "-c", `model_reasoning_effort="`+*l.Effort+`"`)
	}
	return args, nil
}

// LaunchConfig is what a Codex launch is built from; the deployment fills it.
type LaunchConfig struct {
	// LauncherScript is where the launcher script lives (written by EnsureLauncher), LauncherContent its text.
	LauncherScript  string
	LauncherContent []byte
	Hooks           *HookConfig
	// MCP is the runtime's MCP server; its URL needs the port the runtime listens on.
	MCP MCP
	// WorkspacePrompt is a workspace's developer-instructions file; nil when workspaces have none.
	WorkspacePrompt func(workspaceID string) string
	// SkipPermissions adds --yolo when it says so.
	SkipPermissions func() bool
}

func invalidID(id string) error { return errors.New("Invalid codex session ID format: " + id) }

// RuntimeArgs are the arguments the launcher script receives from the runtime and passes to codex: resume, hooks, the
// MCP server, hook trust and update check off (their dialogs are invisible to the web composer), the workspace's
// instructions, permissions, model and effort. ConfigParseError reports an unreadable config.toml.
func (c LaunchConfig) RuntimeArgs(workspaceID, resumeID string, launch *agents.Launch) (args []string, configParseError bool, err error) {
	if resumeID != "" && !agents.IsUUID(resumeID) {
		return nil, false, invalidID(resumeID)
	}
	var hookArgs []string
	if c.Hooks != nil {
		if hookArgs, configParseError, err = c.Hooks.Flags(); err != nil {
			return nil, false, err
		}
	}
	var devArgs []string
	if workspaceID != "" && c.WorkspacePrompt != nil {
		if raw, err := os.ReadFile(c.WorkspacePrompt(workspaceID)); err == nil {
			devArgs = []string{"-c", "developer_instructions=" + TOMLBasicString(strings.ToValidUTF8(string(raw), "�"))}
		}
	}
	if c.MCP.URL == "" {
		return nil, false, errors.New("Runtime port is unknown")
	}
	if resumeID != "" {
		args = append(args, "resume", resumeID)
	}
	args = append(args, hookArgs...)
	args = append(args, MCPArgs(c.MCP)...)
	args = append(args, "--dangerously-bypass-hook-trust", "-c", "check_for_update_on_startup=false")
	args = append(args, devArgs...)
	if c.SkipPermissions != nil && c.SkipPermissions() {
		args = append(args, "--yolo")
	}
	launchArgs, err := RuntimeLaunchArgs(launch)
	if err != nil {
		return nil, false, err
	}
	return append(args, launchArgs...), configParseError, nil
}

// LauncherLine is what is typed into the pane: `node '<launcher>' [--workspace-id …] [--resume-session-id …]
// [--model …] [--effort …]`. The launcher script is written first when it differs.
func (c LaunchConfig) LauncherLine(workspaceID, resumeID string, launch *agents.Launch) (string, error) {
	if resumeID != "" && !agents.IsUUID(resumeID) {
		return "", invalidID(resumeID)
	}
	if err := EnsureLauncher(c.LauncherScript, c.LauncherContent); err != nil {
		return "", err
	}
	parts := []string{"node", agents.ShellQuote(c.LauncherScript)}
	if workspaceID != "" {
		parts = append(parts, "--workspace-id", agents.ShellQuote(workspaceID))
	}
	if resumeID != "" {
		parts = append(parts, "--resume-session-id", agents.ShellQuote(resumeID))
	}
	flags, err := LauncherFlags(launch)
	if err != nil {
		return "", err
	}
	return strings.Join(append(parts, flags...), " "), nil
}

// EnsureLauncher writes the launcher script (0700) when it is missing or differs, and narrows its mode when equal.
func EnsureLauncher(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(content) {
		_ = os.Chmod(path, 0o700)
		return nil
	}
	return os.WriteFile(path, content, 0o700)
}

// LauncherScriptConfig is what the launcher script needs to find the runtime.
type LauncherScriptConfig struct {
	// DataDirs are home-relative data folders, the first that exists is used (the last when none does).
	DataDirs []string
	// PortFile and TokenFile are file names in the data folder.
	PortFile, TokenFile string
	// LaunchArgsPath is the runtime route answering {args}.
	LaunchArgsPath string
	// TokenHeaders carry the token to the route; TokenEnv is the variable codex gets it in.
	TokenHeaders []string
	TokenEnv     string
}

var launcherTemplate = template.Must(template.New("launcher").Funcs(template.FuncMap{
	"js": func(s string) string { return string(nodejson.Marshal(s)) },
}).Parse(`#!/usr/bin/env node
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');

const dataDirs = [{{range $i, $d := .DataDirs}}{{if $i}}, {{end}}{{js $d}}{{end}}].map((dir) => path.join(os.homedir(), dir));
const baseDir = dataDirs.find((dir) => fs.existsSync(dir)) || dataDirs[dataDirs.length - 1];

const readTrim = (filePath) => {
  try {
    return fs.readFileSync(filePath, 'utf8').trim();
  } catch {
    return '';
  }
};

const parseArgs = (argv) => {
  const result = { workspaceId: null, resumeSessionId: null, model: null, effort: null, prompt: null };
  for (let i = 0; i < argv.length; i += 1) {
    const item = argv[i];
    const next = argv[i + 1];
    if (item === '--workspace-id' && next) {
      result.workspaceId = next;
      i += 1;
    } else if (item.startsWith('--workspace-id=')) {
      result.workspaceId = item.slice('--workspace-id='.length);
    } else if (item === '--resume-session-id' && next) {
      result.resumeSessionId = next;
      i += 1;
    } else if (item.startsWith('--resume-session-id=')) {
      result.resumeSessionId = item.slice('--resume-session-id='.length);
    } else if (item === '--model' && next) {
      result.model = next;
      i += 1;
    } else if (item === '--effort' && next) {
      result.effort = next;
      i += 1;
    } else if (item === '--prompt' && next) {
      result.prompt = next;
      i += 1;
    }
  }
  return result;
};

const fetchArgs = async (payload) => {
  if (typeof fetch !== 'function') {
    throw new Error('Node.js 20+ is required to launch Codex');
  }
  const port = readTrim(path.join(baseDir, {{js .PortFile}}));
  if (!port) throw new Error('the runtime port file is missing');
  const token = readTrim(path.join(baseDir, {{js .TokenFile}}));
  const headers = { 'content-type': 'application/json' };
  if (token) {
{{- range .TokenHeaders}}
    headers[{{js .}}] = token;
{{- end}}
  }
  const res = await fetch(` + "`http://127.0.0.1:${port}" + `{{.LaunchArgsPath}}` + "`" + `, {
    method: 'POST',
    headers,
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    const message = await res.text().catch(() => '');
    throw new Error(` + "`failed to get Codex launch args: HTTP ${res.status} ${message}`" + `.trim());
  }
  const data = await res.json();
  if (!Array.isArray(data.args) || data.args.some((arg) => typeof arg !== 'string')) {
    throw new Error('invalid Codex launch args response');
  }
  return data.args;
};

const main = async () => {
  const { prompt, ...payload } = parseArgs(process.argv.slice(2));
  const args = await fetchArgs(payload);
  if (prompt) args.push(prompt);
  const token = readTrim(path.join(baseDir, {{js .TokenFile}}));
  const env = token ? { ...process.env, [{{js .TokenEnv}}]: token } : process.env;
  const child = spawn('codex', args, { stdio: 'inherit', env });
  child.on('exit', (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
      setTimeout(() => process.exit(1), 100);
      return;
    }
    process.exit(code ?? 1);
  });
  child.on('error', (err) => {
    console.error(err.message || String(err));
    process.exit(1);
  });
};

main().catch((err) => {
  console.error(err && err.message ? err.message : String(err));
  process.exit(1);
});
`))

// LauncherScript is the Node script the pane runs: it asks the runtime for codex's arguments (POST LaunchArgsPath
// with {workspaceId, resumeSessionId, model, effort}), appends the prompt, and runs codex with the token in TokenEnv,
// mirroring its exit.
func LauncherScript(c LauncherScriptConfig) []byte {
	var b strings.Builder
	if err := launcherTemplate.Execute(&b, c); err != nil {
		return nil
	}
	return []byte(b.String())
}

// MCPURL is the address of the runtime's MCP route on loopback.
func MCPURL(port int, path string) string {
	if port <= 0 {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(port) + path
}
