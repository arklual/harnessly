package codex

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/wire"
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

// MCPArgs configures a server already authorized by the runtime or by the person's connections.
// Server-level approval avoids a hidden terminal prompt in otherwise unattended conversations.
func MCPArgs(m MCP) []string {
	return []string{
		"-c", "mcp_servers." + m.Server + ".url=" + string(nodejson.Marshal(m.URL)),
		"-c", "mcp_servers." + m.Server + `.default_tools_approval_mode="approve"`,
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
		flags = append(flags, "--model", agents.ShellQuote(agents.LauncherModel(l)))
	}
	if l.Effort != nil && *l.Effort != "" {
		flags = append(flags, "--effort", agents.ShellQuote(*l.Effort))
	}
	if len(l.Apps) > 0 {
		// Checked by CheckLaunch: ids are [a-z0-9.-].
		flags = append(flags, "--apps", agents.ShellQuote(strings.Join(l.Apps, ",")))
		if l.Conversation != "" {
			// The chat the apps' grant names, so the guard's journal finds it.
			flags = append(flags, "--conversation", agents.ShellQuote(l.Conversation))
		}
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

// ContainerSandboxArgs let Codex run commands without its own sandbox, which needs user namespaces a container does not
// give; the container is the boundary.
var ContainerSandboxArgs = []string{"-c", `sandbox_mode="danger-full-access"`, "-c", `approval_policy="never"`}

// LaunchConfig is what a Codex launch is built from; the deployment fills it.
type LaunchConfig struct {
	// LauncherScript is where the launcher script lives (written by EnsureLauncher), LauncherContent its text.
	LauncherScript  string
	LauncherContent []byte
	Hooks           *HookConfig
	// MCP is the runtime's MCP server; its URL needs the port the runtime listens on.
	MCP MCP
	// ExtraMCP gives, at every launch, more servers reached through the runtime with the same token (the person's MCP
	// servers from Settings → Connections); nil adds none.
	ExtraMCP func() []MCP
	// WorkspacePrompt is a workspace's developer-instructions file; nil when workspaces have none.
	WorkspacePrompt func(workspaceID string) string
	// SkipPermissions adds --yolo when it says so.
	SkipPermissions func() bool
	// ContainerSandbox: the runtime runs in a container that is the sandbox, where Codex's own (bubblewrap) cannot start.
	// Codex then runs with sandbox_mode "danger-full-access" and approval_policy "never" instead.
	ContainerSandbox bool
	// PassServiceEnv lets the commands Codex runs see the variables the workspace's services need (BOARDS_TOKEN,
	// ARTIFACTS_TOKEN): by default Codex drops every variable whose name has KEY, SECRET or TOKEN in it. ShellEnvExclude
	// are variables that still stay out (the runtime's own CLI token).
	PassServiceEnv  bool
	ShellEnvExclude []string
	// Profiles points a launch on a backend profile at the vendor; nil refuses such a launch.
	Profiles agents.ProfileLauncher
	// Apps gives a launch with apps their guard, servers, instructions and wall; nil refuses such a launch.
	Apps agents.AppLauncher
}

// Start is what a Codex launch is built to: codex's own arguments, and the shell words it runs behind (the apps' token
// and wall, which only the pane's shell can put in front of it); Prefix is "" for a launch without apps.
type Start struct {
	Args   []string
	Prefix string
}

// ErrNoProfiles: a launch names a backend profile and this deployment has none.
var ErrNoProfiles = errors.New("Backend profiles are not available on this machine")

// profile is what a launch on a backend profile adds; the zero value for a launch on the CLI's own account.
func (c LaunchConfig) profile(l *agents.Launch) (agents.CodexProfile, error) {
	if l == nil || l.Profile == "" {
		return agents.CodexProfile{}, nil
	}
	if c.Profiles == nil {
		return agents.CodexProfile{}, ErrNoProfiles
	}
	if err := agents.CheckLaunch(l); err != nil {
		return agents.CodexProfile{}, err
	}
	if l.Effort != nil {
		m, ok := c.Profiles.Model(l.Profile, *l.Model)
		if !ok || !slices.Contains(m.Efforts, wire.Effort(*l.Effort)) {
			return agents.CodexProfile{}, agents.ErrLaunchEffort
		}
	}
	return c.Profiles.Codex(l.Profile)
}

func invalidID(id string) error { return errors.New("Invalid codex session ID format: " + id) }

// RuntimeArgs are the arguments the launcher script receives from the runtime and passes to codex: resume, hooks, the
// MCP server, hook trust and update check off (their dialogs are invisible to the web composer), the workspace's
// instructions, permissions, model and effort. ConfigParseError reports an unreadable config.toml.
func (c LaunchConfig) RuntimeArgs(workspaceID, resumeID string, launch *agents.Launch) (args []string, configParseError bool, err error) {
	return c.argsOnly(c.RuntimeStart(workspaceID, resumeID, launch))
}

// RuntimeStart is RuntimeArgs with the words codex runs behind (Start.Prefix), which a launch with apps has.
func (c LaunchConfig) RuntimeStart(workspaceID, resumeID string, launch *agents.Launch) (Start, bool, error) {
	return c.runtimeArgs(workspaceID, "resume", resumeID, launch)
}

// ErrNeedsStart: a launch with apps was built where only the arguments are kept, which would drop the apps' wall.
var ErrNeedsStart = errors.New("a launch with apps needs RuntimeStart: its arguments alone would leave out the wall")

func (c LaunchConfig) argsOnly(s Start, parseErr bool, err error) ([]string, bool, error) {
	if err == nil && s.Prefix != "" {
		return nil, false, ErrNeedsStart
	}
	return s.Args, parseErr, err
}

// ForkArgs are RuntimeArgs that start codex on a fork of the session (`codex fork <id>`): a session of its own that
// begins with the other's history.
func (c LaunchConfig) ForkArgs(workspaceID, sessionID string, launch *agents.Launch) (args []string, configParseError bool, err error) {
	return c.argsOnly(c.ForkStart(workspaceID, sessionID, launch))
}

// ForkStart is ForkArgs with the words codex runs behind (Start.Prefix).
func (c LaunchConfig) ForkStart(workspaceID, sessionID string, launch *agents.Launch) (Start, bool, error) {
	if sessionID == "" {
		return Start{}, false, invalidID(sessionID)
	}
	return c.runtimeArgs(workspaceID, "fork", sessionID, launch)
}

// runtimeArgs start codex on session by verb (resume, fork), or anew when session is "".
func (c LaunchConfig) runtimeArgs(workspaceID, verb, session string, launch *agents.Launch) (start Start, configParseError bool, err error) {
	if session != "" && !agents.IsUUID(session) {
		return Start{}, false, invalidID(session)
	}
	prof, err := c.profile(launch)
	if err != nil {
		return Start{}, false, err
	}
	var apps agents.CodexApps
	launch = agents.WithActiveApps(launch, c.Apps)
	if launch != nil && len(launch.Apps) > 0 {
		if c.Apps == nil {
			return Start{}, false, agents.ErrNoApps
		}
		if err := agents.CheckLaunch(launch); err != nil {
			return Start{}, false, err
		}
		if apps, err = c.Apps.Codex(launch.Conversation, launch.Apps); err != nil {
			return Start{}, false, err
		}
	}
	var hookArgs []string
	if c.Hooks != nil {
		if hookArgs, configParseError, err = c.Hooks.FlagsWithGuard(apps.Guard); err != nil {
			return Start{}, false, err
		}
	} else if apps.Guard != "" {
		// Apps without the hook that holds their rules would run unguarded.
		return Start{}, false, errors.New("the apps' guard has no hook to run in")
	}
	var devArgs []string
	instructions := ""
	if workspaceID != "" && c.WorkspacePrompt != nil {
		if raw, err := os.ReadFile(c.WorkspacePrompt(workspaceID)); err == nil {
			instructions = strings.ToValidUTF8(string(raw), "�")
		}
	}
	if apps.Instructions != "" {
		if instructions != "" {
			instructions += "\n\n"
		}
		instructions += apps.Instructions
	}
	if instructions != "" {
		devArgs = []string{"-c", "developer_instructions=" + TOMLBasicString(instructions)}
	}
	if c.MCP.URL == "" {
		return Start{}, false, errors.New("Runtime port is unknown")
	}
	var args []string
	if session != "" {
		args = append(args, verb, session)
	}
	args = append(args, hookArgs...)
	args = append(args, MCPArgs(c.MCP)...)
	if c.ExtraMCP != nil {
		for _, m := range c.ExtraMCP() {
			if m.Server != "" && m.URL != "" && m.Server != c.MCP.Server {
				args = append(args, MCPArgs(m)...)
			}
		}
	}
	for _, srv := range apps.Servers {
		if srv.Name == c.MCP.Server {
			continue
		}
		args = append(args, AppServerArgs(srv, c.MCP.TokenEnv)...)
	}
	args = append(args, "--dangerously-bypass-hook-trust", "-c", "check_for_update_on_startup=false")
	args = append(args, devArgs...)
	args = append(args, c.shellEnvArgs(prof.KeyEnv)...)
	args = append(args, c.permissionArgs(launch)...)
	args = append(args, FastArgs(launch)...)
	launchArgs, err := RuntimeLaunchArgs(launch)
	if err != nil {
		return Start{}, false, err
	}
	// The vendor's provider comes with the model: Codex starts on the profile and asks it for the model.
	args = append(append(args, prof.Args...), launchArgs...)
	return Start{Args: args, Prefix: appsPrefix(apps, c.MCP.TokenEnv)}, configParseError, nil
}

// appsPrefix are the shell words the CLI of a chat with apps runs behind: the apps' environment, the launch's own token
// in the variable the runtime's MCP servers read (the launcher script put the person's there), and the wall.
func appsPrefix(apps agents.CodexApps, tokenEnv string) string {
	var words []string
	if apps.Env != "" {
		words = append(words, apps.Env)
	}
	if apps.TokenFile != "" && tokenEnv != "" {
		words = append(words, tokenEnv+`="$(cat `+agents.ShellQuote(apps.TokenFile)+`)"`)
	}
	if apps.Wrap != "" {
		words = append(words, apps.Wrap)
	}
	return strings.Join(words, " ")
}

// AppServerArgs are the `-c` flags of one MCP server of an app. The headers that carry the launch's token read it from
// tokenEnv, so it is on no command line; like the runtime's own, the server's tools need no approval of Codex's (the apps'
// guard decides every call).
func AppServerArgs(s agents.CodexAppServer, tokenEnv string) []string {
	key := "mcp_servers." + s.Name + "."
	args := []string{}
	add := func(k, v string) { args = append(args, "-c", key+k+"="+v) }
	if s.URL != "" {
		add("url", TOMLBasicString(s.URL))
	} else {
		add("command", TOMLBasicString(s.Command))
		if len(s.Args) > 0 {
			parts := make([]string, len(s.Args))
			for i, a := range s.Args {
				parts[i] = TOMLBasicString(a)
			}
			add("args", "["+strings.Join(parts, ",")+"]")
		}
		if t := inlineStrings(s.Env); t != "" {
			add("env", t)
		}
	}
	add("default_tools_approval_mode", `"approve"`)
	if t := inlineStrings(s.Headers); t != "" && s.URL != "" {
		add("http_headers", t)
	}
	if len(s.TokenHeaders) > 0 && tokenEnv != "" {
		bound := map[string]string{}
		for _, h := range s.TokenHeaders {
			bound[h] = tokenEnv
		}
		add("env_http_headers", inlineStrings(bound))
	}
	return args
}

// inlineStrings is a TOML inline table of strings, keys sorted; "" for none.
func inlineStrings(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = TOMLBasicString(k) + "=" + TOMLBasicString(m[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// shellEnvArgs are the two flags that keep Codex from dropping the services' tokens from its commands' environment.
//
// The variable that carries a profile's key stays out of the commands too (keyEnv), also when the services' tokens do not.
func (c LaunchConfig) shellEnvArgs(keyEnv string) []string {
	if !c.PassServiceEnv {
		return nil
	}
	exclude := make([]string, 0, len(c.ShellEnvExclude)+1)
	for _, name := range append(slices.Clone(c.ShellEnvExclude), keyEnv) {
		if name != "" {
			exclude = append(exclude, TOMLBasicString(name))
		}
	}
	return []string{"-c", "shell_environment_policy.ignore_default_excludes=true",
		"-c", "shell_environment_policy.exclude=[" + strings.Join(exclude, ", ") + "]"}
}

// LauncherLine is what is typed into the pane: `node '<launcher>' [--workspace-id …] [--resume-session-id …]
// [--model …] [--effort …]`. The launcher script is written first when it differs.
func (c LaunchConfig) LauncherLine(workspaceID, resumeID string, launch *agents.Launch) (string, error) {
	return c.launcherLine(workspaceID, "--resume-session-id", resumeID, launch)
}

// ForkLauncherLine is the LauncherLine of a fork of the session: `--fork-session-id <id>`.
func (c LaunchConfig) ForkLauncherLine(workspaceID, sessionID string, launch *agents.Launch) (string, error) {
	if sessionID == "" {
		return "", invalidID(sessionID)
	}
	return c.launcherLine(workspaceID, "--fork-session-id", sessionID, launch)
}

func (c LaunchConfig) launcherLine(workspaceID, flag, session string, launch *agents.Launch) (string, error) {
	if session != "" && !agents.IsUUID(session) {
		return "", invalidID(session)
	}
	prof, err := c.profile(launch)
	if err != nil {
		return "", err
	}
	if err := EnsureLauncher(c.LauncherScript, c.LauncherContent); err != nil {
		return "", err
	}
	parts := []string{"node", agents.ShellQuote(c.LauncherScript)}
	if prof.KeyEnv != "" {
		// The pane's shell reads the key from its file into the variable of this one command: the key is not in the
		// line that is typed, and not in the shell's own environment.
		parts = append([]string{prof.KeyEnv + `="$(cat ` + agents.ShellQuote(prof.KeyFile) + `)"`}, parts...)
	}
	if workspaceID != "" {
		parts = append(parts, "--workspace-id", agents.ShellQuote(workspaceID))
	}
	if session != "" {
		parts = append(parts, flag, agents.ShellQuote(session))
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
	// DataDirs are data folders, relative to the home or absolute; the first that exists is used (the last when none does).
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

const dataDirs = [{{range $i, $d := .DataDirs}}{{if $i}}, {{end}}{{js $d}}{{end}}].map((dir) => path.resolve(os.homedir(), dir));
const baseDir = dataDirs.find((dir) => fs.existsSync(dir)) || dataDirs[dataDirs.length - 1];

const readTrim = (filePath) => {
  try {
    return fs.readFileSync(filePath, 'utf8').trim();
  } catch {
    return '';
  }
};

const parseArgs = (argv) => {
  const result = { workspaceId: null, resumeSessionId: null, forkSessionId: null, model: null, effort: null, apps: null, conversation: null, prompt: null };
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
    } else if (item === '--fork-session-id' && next) {
      result.forkSessionId = next;
      i += 1;
    } else if (item === '--model' && next) {
      result.model = next;
      i += 1;
    } else if (item === '--effort' && next) {
      result.effort = next;
      i += 1;
    } else if (item === '--apps' && next) {
      result.apps = next.split(',').filter(Boolean);
      i += 1;
    } else if (item === '--conversation' && next) {
      result.conversation = next;
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
  // A chat with apps runs behind words of the pane's shell: its own token and the wall (prefix).
  const prefix = typeof data.prefix === 'string' ? data.prefix : '';
  return { args: data.args, prefix };
};

const main = async () => {
  const { prompt, ...payload } = parseArgs(process.argv.slice(2));
  const { args, prefix } = await fetchArgs(payload);
  if (prompt) args.push(prompt);
  const token = readTrim(path.join(baseDir, {{js .TokenFile}}));
  const env = token ? { ...process.env, [{{js .TokenEnv}}]: token } : process.env;
  const child = prefix
    ? spawn('sh', ['-c', prefix + ' codex "$@"', 'codex', ...args], { stdio: 'inherit', env })
    : spawn('codex', args, { stdio: 'inherit', env });
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
// with {workspaceId, resumeSessionId, forkSessionId, model, effort}), appends the prompt, and runs codex with the token in TokenEnv,
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

// permissionArgs are how the launch's Codex asks before it acts. A launch that names a permission mode gets
// it whatever the machine's default:
//
//   - bypass: --yolo, nothing asked and no sandbox;
//   - ask: approvals for anything but plain reads (`untrusted`), in the workspace-write sandbox;
//   - plan: changes nothing on its own: a read-only sandbox, and asks before anything past it (`on-request`). Codex has
//     no way to start in its Plan mode, and this is what that mode is for.
//
// In a container Codex's sandbox cannot start (ContainerSandbox): the container is the boundary, and plan and ask
// both ask before every command that is not a plain read. Without a mode the machine's default stands: --yolo when the
// runtime skips permissions, the container's args in a container, else Codex's own config.
func (c LaunchConfig) permissionArgs(launch *agents.Launch) []string {
	mode := wire.PermissionMode("")
	if launch != nil {
		mode = launch.Permission
	}
	sandbox := func(own string) []string {
		if c.ContainerSandbox {
			return []string{"-c", `sandbox_mode="danger-full-access"`, "-c", `approval_policy="untrusted"`}
		}
		return []string{"-c", `sandbox_mode="` + own + `"`}
	}
	switch mode {
	case wire.PermissionBypass:
		return []string{"--yolo"}
	case wire.PermissionAsk:
		if c.ContainerSandbox {
			return sandbox("")
		}
		return append(sandbox("workspace-write"), "-c", `approval_policy="untrusted"`)
	case wire.PermissionPlan:
		if c.ContainerSandbox {
			return sandbox("")
		}
		return append(sandbox("read-only"), "-c", `approval_policy="on-request"`)
	}
	if c.SkipPermissions != nil && c.SkipPermissions() {
		return []string{"--yolo"}
	}
	if c.ContainerSandbox {
		return ContainerSandboxArgs
	}
	return nil
}

// FastArgs are the service tier of a launch that chose it: `fast` when it is on; `default` when a launch
// that names its modes has it off, so a `service_tier = "fast"` of the person's config does not turn it on. A launch
// that names no mode leaves the config alone.
func FastArgs(launch *agents.Launch) []string {
	switch {
	case launch == nil:
		return nil
	case launch.Fast:
		return []string{"-c", `service_tier="fast"`}
	case launch.Permission != "":
		return []string{"-c", `service_tier="default"`}
	}
	return nil
}
