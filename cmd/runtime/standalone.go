package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/hooksetup"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/machines"
	"github.com/ly-solutions/harnessly/internal/runlock"
	"github.com/ly-solutions/harnessly/internal/settings"
	"github.com/ly-solutions/harnessly/internal/telemetry"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// A standalone gate (RUNTIME_GATE_UPSTREAM empty) runs a machine with no old runtime behind it. What the old runtime did
// on start and stop is then this process's: the single-instance lock, the config with the pair's secret, the cli-token,
// the port file, the agents' hook and status line scripts, their settings and MCP files, the prompt editor, the Codex
// launcher, the tmux config, the workspace prompts and the clean-ups. In front of the old runtime it does them and the
// gate leaves them alone: one writer per file. The files are the old runtime's own, byte for byte, since agents already
// running hold their paths and read them.

type standalone struct {
	dataDir string
	lock    *runlock.Lock
	hooks   *hooksetup.Writer
	token   string
}

// lockFile is the single-instance lock (RUNTIME_LOCK_FILE: a path, or a name in the data folder).
func lockFile(dataDir string) string {
	f := env("RUNTIME_LOCK_FILE", "runtime.lock")
	if filepath.IsAbs(f) {
		return f
	}
	return filepath.Join(dataDir, f)
}

func healthApp() string { return env("RUNTIME_HEALTH_APP", "harnessly") }

// standaloneStart takes the lock for port, makes the config and its secret when missing, and the cli-token; it runs
// before the gate is built, which reads the secret.
func standaloneStart(cfg gate.Config, configFile string, port int) (*standalone, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o777); err != nil {
		return nil, err
	}
	lock, err := runlock.Acquire(runlock.Config{File: lockFile(cfg.DataDir), App: healthApp()}, port)
	if err != nil {
		var held *runlock.HeldError
		if errors.As(err, &held) {
			return nil, errors.New(held.Error() + ": stop it, or run this gate in front of it (RUNTIME_GATE_UPSTREAM)")
		}
		return nil, err
	}
	s := &standalone{dataDir: cfg.DataDir, lock: lock}
	if err := ensureConfig(configFile); err != nil {
		lock.Release()
		return nil, err
	}
	s.hooks = hooksetup.New(hookFilesConfig(cfg.DataDir))
	if s.token, err = s.hooks.EnsureToken(); err != nil {
		agent.Log("warn", "standalone: cli-token not written", agent.F{K: "err", V: err.Error()})
	}
	return s, nil
}

// ensureConfig is the old runtime's first start: an empty config when there is none (or it does not parse), and,
// unlike there (it waited for the person's onboarding, which a machine of the cluster never has), the pair's secret
// when the config has none and no secret file is named.
func ensureConfig(configFile string) error {
	store := &settings.Store{DataDir: filepath.Dir(configFile), ConfigFile: configFile}
	if store.ReadConfig() == nil {
		if err := store.UpdateConfig(jsvalue.NewObject()); err != nil {
			return err
		}
		agent.Log("info", "standalone: config created", agent.F{K: "file", V: configFile})
	}
	if os.Getenv("AGENT_SECRET_FILE") != "" {
		return nil
	}
	if s, ok := get(store.ReadConfig(), "authSecret").(string); ok && len([]rune(s)) >= 32 {
		return nil
	}
	agent.Log("info", "standalone: the pair's secret created", agent.F{K: "file", V: configFile})
	return store.UpdateConfig(jsvalue.NewObject("authSecret", store.Secret()))
}

// defaultMCPTokenEnv is the variable the Codex launcher puts the cli-token in when RUNTIME_MCP_TOKEN_ENV names none: the
// MCP config Codex is given reads its header value from it.
const defaultMCPTokenEnv = "HARNESSLY_CLI_TOKEN"

// mcpTokenEnv is RUNTIME_MCP_TOKEN_ENV, else the neutral default.
func mcpTokenEnv() string { return env("RUNTIME_MCP_TOKEN_ENV", defaultMCPTokenEnv) }

// scriptDataDirs are the home-relative data folders the Codex launcher looks for the runtime in:
// RUNTIME_SCRIPT_DATA_DIRS, else the runtime's own data folder, relative to the home when it lies under it (the
// default ~/.harnessly is then ".harnessly") and absolute when it does not.
func scriptDataDirs(dataDir, home string) []string {
	if dirs := splitList(os.Getenv("RUNTIME_SCRIPT_DATA_DIRS")); len(dirs) > 0 {
		return dirs
	}
	if home != "" {
		if rel, err := filepath.Rel(home, dataDir); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return []string{rel}
		}
	}
	return []string{dataDir}
}

// hookFilesConfig names the product's parts of the agents' files, from the deployment.
func hookFilesConfig(dataDir string) hooksetup.Config {
	home, _ := os.UserHomeDir()
	scriptDirs := scriptDataDirs(dataDir, home)
	scriptDir := scriptDirs[0]
	if !filepath.IsAbs(scriptDir) {
		scriptDir = "$HOME/" + scriptDir
	}
	server := env("RUNTIME_MCP_SERVER", "harnessly")
	headers := splitList(env("RUNTIME_CLI_TOKEN_HEADERS", "x-cli-token"))
	return hooksetup.Config{
		DataDir:          dataDir,
		ScriptDataDir:    scriptDir,
		TokenHeaders:     headers,
		MCPServer:        server,
		MCPPath:          env("RUNTIME_MCP_PATH", "/api/cli/preview/mcp"),
		Allow:            splitList(env("RUNTIME_MCP_ALLOW", "mcp__"+server)),
		Editor:           promptEditorScript(dataDir),
		EditorTitle:      env("RUNTIME_PROMPT_EDITOR_TITLE", "Prompt editor"),
		HumanEditorVar:   promptHumanEditorVar(),
		Handoff:          env("RUNTIME_PROMPT_HANDOFF_DIR", filepath.Join(dataDir, hooksetup.HandoffDir)),
		LegacyTmuxSocket: env("RUNTIME_PROMPT_HANDOFF_UNKEYED_SOCKET", os.Getenv("RUNTIME_TMUX_LEGACY_SOCKET")),
		Codex: &codex.LauncherScriptConfig{DataDirs: scriptDirs, PortFile: hooksetup.PortFile, TokenFile: hooksetup.TokenFile,
			LaunchArgsPath: "/api/codex/launch-args", TokenHeaders: headers, TokenEnv: mcpTokenEnv()},
	}
}

// files writes what the agents call the runtime through, once the port is known (the old runtime's ensureHookSettings,
// prompt editor and Codex launcher), and re-reads the tmux config on the running servers.
func (s *standalone) files(port int, tm *tmux.Tmux) {
	res, err := s.hooks.Ensure(port, s.token)
	if err != nil {
		agent.Log("error", "standalone: the agents' hook files not written", agent.F{K: "err", V: err.Error()})
	}
	if res.CodexHookFailed {
		agent.Log("warn", "standalone: the Codex hook script not written; Codex tabs report no state")
	}
	if promptEditorScript(s.dataDir) != "" {
		if _, err := s.hooks.EnsureEditor(); err != nil {
			agent.Log("warn", "standalone: prompt editor script not written", agent.F{K: "err", V: err.Error()})
		}
	}
	if content := codexLauncherContent(s.dataDir); content != nil {
		if err := codex.EnsureLauncher(s.hooks.LauncherPath(), content); err != nil {
			agent.Log("warn", "standalone: Codex launcher not written", agent.F{K: "err", V: err.Error()})
		}
	}
	provisionAgentSkills(s.dataDir)
	if tm != nil {
		tm.ApplyConfig("")
	}
}

// codexLauncherContent is the Codex launcher: the release's copy of the platform's own script (RUNTIME_CODEX_LAUNCHER_FILE,
// else <release>/runtime-static/codex-launcher.js), else the neutral one.
func codexLauncherContent(dataDir string) []byte {
	if f := releaseStatic("RUNTIME_CODEX_LAUNCHER_FILE", "codex-launcher.js"); f != "" {
		if raw, err := os.ReadFile(f); err == nil {
			return raw
		}
	}
	return codex.LauncherScript(*hookFilesConfig(dataDir).Codex)
}

// workspacePrompts are the agents' per-workspace prompt templates (RUNTIME_WORKSPACE_PROMPTS_FILE, else the release's
// runtime-static/workspace-prompts.json); without one, the built-in prompts that describe the harnessly command.
func workspacePrompts() hooksetup.Prompts {
	f := releaseStatic("RUNTIME_WORKSPACE_PROMPTS_FILE", "workspace-prompts.json")
	if f == "" {
		return defaultWorkspacePrompts()
	}
	p, err := hooksetup.LoadPrompts(f)
	if err != nil {
		agent.Log("warn", "standalone: workspace prompts not read; the built-in ones are used", agent.F{K: "file", V: f}, agent.F{K: "err", V: err.Error()})
		return defaultWorkspacePrompts()
	}
	return p
}

// cleanUp removes the uploads older than a day and the statusline figures of conversations whose transcripts are gone,
// as the old runtime did after its start.
func (s *standalone) cleanUp(home, uploads string) {
	if r := (&files.Uploads{Dir: uploads}).Clean(false); r.Deleted > 0 {
		agent.Log("info", "uploads cleanup", agent.F{K: "deleted", V: r.Deleted}, agent.F{K: "freedBytes", V: r.FreedBytes})
	}
	projects := filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "projects")
	(&history.Stats{Dir: filepath.Join(s.dataDir, "session-stats")}).CleanupOrphans(projects)
}

// stop removes the port file (the scripts stop calling a runtime that is gone) and releases the lock.
func (s *standalone) stop() {
	if s == nil {
		return
	}
	_ = s.hooks.RemovePortFile()
	s.lock.Release()
}

// mismatchTelemetry reports corrected tab states of a released runtime to the deployment's batch endpoint
// (RUNTIME_TELEMETRY_URL, RUNTIME_TELEMETRY_KEY); nil sends nothing.
func mismatchTelemetry(configFile string) *telemetry.Queue {
	url := os.Getenv("RUNTIME_TELEMETRY_URL")
	release := machines.ReadRelease(os.Getenv("RUNTIME_RELEASE_FILE"))
	if url == "" || release == nil {
		return nil
	}
	common := jsvalue.NewObject()
	if raw := os.Getenv("RUNTIME_TELEMETRY_PROPERTIES"); raw != "" {
		if v, err := jsvalue.Parse([]byte(raw)); err == nil {
			if o, ok := v.(*jsvalue.Object); ok {
				common = o
			}
		}
	}
	var commit any
	if release.Commit != nil {
		commit = *release.Commit
	}
	host, _ := os.Hostname()
	common.Set("release", release.Version)
	common.Set("runtime_commit", commit)
	common.Set("host", host)
	return telemetry.New(telemetry.Config{URL: url, Key: os.Getenv("RUNTIME_TELEMETRY_KEY"), Common: common,
		DistinctID: func() string {
			store := &settings.Store{ConfigFile: configFile}
			if s, ok := get(store.ReadConfig(), "automationOwnerEmail").(string); ok {
				return s
			}
			return env("RUNTIME_OWNER_EMAIL", env("RUNTIME_TELEMETRY_ANONYMOUS_ID", "runtime"))
		},
		Log: machinesLog})
}

// standaloneFallback answers, with no old runtime behind the gate, what no route took: a route the old runtime had that
// is not answered here (or not yet: the core still starting) is unavailable, as a machine that is down looks to the
// cluster, which answers from what it keeps; anything else is the old runtime's "Not found".
func standaloneFallback() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUpgrade(r) {
			status := "HTTP/1.1 404 Not Found\r\n\r\n"
			p := strings.TrimPrefix(r.URL.Path, "/agents")
			if slicedSockets[p] {
				status = "HTTP/1.1 503 Service Unavailable\r\n\r\n"
			}
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_, _ = conn.Write([]byte(status))
					_ = conn.Close()
					return
				}
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if oldRouteMatches(r) {
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"Service unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Not found"}`))
	})
}
