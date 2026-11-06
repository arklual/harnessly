package main

import (
	"context"
	"os"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/envsetup"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// The machine setup of the hub's environment list (docs/hub/environment.md, «Machine setup») is done here, by the
// runtime, and not by the machine agent: the agent is sandboxed by systemd and must not run a program of anyone's
// choosing, while this process runs as the person and holds the person's safe. The agent keeps the list and tells the
// hub; the two meet in the link folders (internal/envsetup). RUNTIME_ENV_SETUP=off keeps the runtime from doing it: the
// lists are still kept, the setup items just stay «машина ещё настраивается».

// safeSecrets is the runtime's safe as the setup sees it.
type safeSecrets struct{ sf *safe }

func (s safeSecrets) Has(name string) bool { return s.sf.store.Has(name) }

func (s safeSecrets) Reveal(names []string) (map[string]string, error) {
	return s.sf.store.Reveal(names, secrets.Use{Title: "Окружение команды"})
}

// Ask puts the secret on the person's page («Положить в сейф») with the reason; an ask that is open already is the same ask.
func (s safeSecrets) Ask(name, why string) error {
	_, _, err := s.sf.asks.Open(name, why, "", "Окружение команды", false)
	return err
}

// envSetupRunners are the runners of this machine's hub link folders; none when the setup is switched off.
func envSetupRunners(d deps, observeFile, version string) []*envsetup.Runner {
	if os.Getenv("RUNTIME_ENV_SETUP") == "off" || d.Home == "" {
		return nil
	}
	sec := safeSecrets{safeFor(d)}
	var out []*envsetup.Runner
	for _, dir := range hubLinkDirs(d.DataDir, observeFile) {
		out = append(out, &envsetup.Runner{LinkDir: dir, Home: d.Home, Secrets: sec, Log: setupLog, Version: version,
			OnUpdate: func() {
				if err := askBasepkg(d.DataDir); err != nil {
					setupLog("warn", "base package not asked", "error", err.Error())
				}
			}})
	}
	return out
}

func setupLog(level, msg string, kv ...string) {
	fields := make([]agent.F, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		fields = append(fields, agent.F{K: kv[i], V: kv[i+1]})
	}
	agent.Log(level, msg, fields...)
}

// startEnvSetup starts one goroutine per link folder; they end with ctx and never block the start.
func (f *runtimeGate) startEnvSetup(ctx context.Context) {
	for _, r := range f.envSetup {
		go r.Run(ctx)
	}
}
