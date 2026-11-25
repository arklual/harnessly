package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents/cliupdate"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/providers"
)

// The agent CLIs keep themselves up to date and the model lists follow the vendors: a run every
// cliupdate.Every and on a page open (POST <platform>/agents/refresh, at most once in cliupdate.PokeAfter) checks
// Claude Code and Codex, installs a new version beside the running one and switches the link (internal/agents/cliupdate),
// and then refreshes the lists: `codex debug models` writes ~/.codex/models_cache.json (the Codex picker, the GPT
// profile), the presets ask their vendors' /models. RUNTIME_CLI_AUTOUPDATE=off leaves the CLIs alone (the lists are
// still refreshed).

var (
	cliUpdatersMu sync.Mutex
	cliUpdaters   = map[string]*cliupdate.Updater{}
)

func cliAutoUpdate() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_CLI_AUTOUPDATE")))
	return v != "off" && v != "0" && v != "false"
}

// agentCLIs is the updater of the home's CLIs, one per process.
func agentCLIs(d deps) *cliupdate.Updater {
	cliUpdatersMu.Lock()
	defer cliUpdatersMu.Unlock()
	if u := cliUpdaters[d.Home]; u != nil {
		return u
	}
	tools := d.Tools
	if tools == nil {
		tools = preflight.Shared(d.Home)
	}
	u := cliupdate.New(cliupdate.Deps{
		Shell: tools, EnvDir: agentEnvDir(d.DataDir), Home: d.Home,
		Log: log.Printf,
		Changed: func(cli string) {
			tools.Invalidate()
			if cli == "codex" {
				go refreshCodexModels(context.Background(), d, true)
			}
		},
	})
	cliUpdaters[d.Home] = u
	return u
}

var modelsRefresh struct {
	sync.Mutex
	codexAt time.Time
	busy    bool
}

// refreshCodexModels runs `codex debug models`, which fetches the list for this Codex's version into
// ~/.codex/models_cache.json (Codex keeps its own few-minute TTL); at most once in cliupdate.PokeAfter unless forced.
func refreshCodexModels(ctx context.Context, d deps, force bool) {
	modelsRefresh.Lock()
	if modelsRefresh.busy || (!force && time.Since(modelsRefresh.codexAt) < cliupdate.PokeAfter) {
		modelsRefresh.Unlock()
		return
	}
	modelsRefresh.busy = true
	modelsRefresh.Unlock()
	defer func() {
		modelsRefresh.Lock()
		modelsRefresh.busy, modelsRefresh.codexAt = false, time.Now()
		modelsRefresh.Unlock()
	}()
	tools := d.Tools
	if tools == nil {
		tools = preflight.Shared(d.Home)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	_, _ = tools.RunLogin(ctx, "command -v codex >/dev/null 2>&1 || exit 0\ncodex debug models >/dev/null 2>&1 </dev/null; exit 0")
	if d.GPTModelsChanged != nil {
		d.GPTModelsChanged()
	}
}

// refreshModelLists refreshes every list that follows a vendor, in the background of the caller.
func refreshModelLists(ctx context.Context, d deps, force bool) {
	refreshCodexModels(ctx, d, force)
	providers.RefreshVendorModels(ctx, voiceStore(d.DataDir), nil, force)
	voiceLiveService(d).RefreshModels(ctx)
}

// runAgentCLIs is the background loop of main: the CLIs, then the lists, every cliupdate.Every.
func runAgentCLIs(ctx context.Context, d deps) {
	t := time.NewTimer(cliupdate.FirstAfter)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if cliAutoUpdate() {
			agentCLIs(d).Run(ctx)
		}
		refreshModelLists(ctx, d, false)
		t.Reset(cliupdate.Every)
	}
}

// pokeAgentCLIs is a page open: a check of the CLIs when the last is old enough, and the lists.
func pokeAgentCLIs(d deps) {
	if cliAutoUpdate() {
		agentCLIs(d).Poke(context.Background())
	}
	go refreshModelLists(context.Background(), d, false)
}

func agentCLIRoutes(d deps) []gate.Route {
	return []gate.Route{{
		Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/agents/refresh") },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			if !d.SessionOK(r) {
				peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			pokeAgentCLIs(d)
			peerJSON(w, http.StatusAccepted, map[string]any{"clis": agentCLIs(d).Status()})
		}),
	}}
}

// cliChecks are the machine checks of the CLIs' versions: what runs, the latest, the last update.
func cliChecks(list []cliupdate.Status) []machineCheck {
	var out []machineCheck
	for _, s := range list {
		if s.State == cliupdate.StateMissing {
			continue
		}
		c := machineCheck{ID: "agent_cli_version_" + s.CLI, OK: true, Title: s.Label}
		if s.Version != "" {
			c.Title += " " + s.Version
		}
		var parts []string
		switch s.State {
		case cliupdate.StateOK:
			if s.Latest != "" {
				parts = append(parts, "последняя версия")
			}
		case cliupdate.StateUpdating:
			parts = append(parts, "обновляется до "+s.Latest)
		case cliupdate.StateManaged:
			parts = append(parts, s.Detail)
		case cliupdate.StateShadowed, cliupdate.StateFailed:
			c.OK = false
			parts = append(parts, s.Detail)
			c.Fixable = true
		}
		if !s.UpdatedAt.IsZero() && s.From != s.To {
			parts = append(parts, "обновлён "+s.From+" → "+s.To+" "+s.UpdatedAt.Local().Format("02.01 15:04"))
		}
		if s.Path != "" {
			parts = append(parts, s.Path)
		}
		c.Detail = strings.Join(parts, " · ")
		if !c.OK && s.State == cliupdate.StateShadowed {
			c.FixCommand = "ln -sfn <новая копия> " + s.Path
		}
		out = append(out, c)
	}
	return out
}

// codexLiveModels are the «gpt-live» models of the Codex a chat starts (cliupdate.LiveModels).
func codexLiveModels(d deps) []string {
	for _, s := range agentCLIs(d).Status() {
		if s.CLI == "codex" && s.Real != "" {
			return cliupdate.LiveModels(s.Real)
		}
	}
	tools := d.Tools
	if tools == nil {
		tools = preflight.Shared(d.Home)
	}
	if p, err := lookPathIn("codex", tools.ShellPath()); err == nil {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return cliupdate.LiveModels(real)
		}
	}
	return nil
}

// lookPathIn finds a program along a PATH value.
func lookPathIn(name, path string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}
