package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/envsetup"
	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/locale"
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
		out = append(out, &envsetup.Runner{LinkDir: dir, Home: d.Home, Secrets: sec, Apps: setupApps{reg: apps.Open(d.DataDir), link: dir, version: version}, Providers: setupProviders{svc: extProvidersFor(d), ask: providerSecretAsk(safeFor(d))}, Log: setupLog, Version: version,
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
func (f *runtimeGate) startEnvSetup(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	var workers sync.WaitGroup
	for _, r := range f.envSetup {
		workers.Go(func() { r.Run(ctx) })
	}
	return func() {
		cancel()
		workers.Wait()
	}
}

// providerSecretAsk opens the safe's card for a secret a provider names, with the server it is for: a secret that is
// not there is put for that server, one the person has is allowed for it. The value never passes here.
func providerSecretAsk(sf *safe) func(name, origin, why string) {
	return func(name, origin, why string) {
		title := locale.Pick("Team environment", "Окружение команды")
		_, _, err := sf.asks.OpenScoped(secrets.Ask{Name: name, Why: why, Title: title, Origin: origin, OriginGrant: sf.store.Has(name)})
		if err != nil {
			setupLog("warn", "envsetup: ask not opened", "name", name, "error", err.Error())
		}
	}
}

// safeBound says whether the person allowed a secret of the safe for a site.
func safeBound(store *secrets.Safe) func(name, origin string) bool {
	return func(name, origin string) bool {
		site := secrets.Origin(origin)
		if site == "" {
			return false
		}
		views, err := store.List()
		if err != nil {
			return false
		}
		for _, v := range views {
			if v.Name == name {
				return slices.Contains(v.Origins, site)
			}
		}
		return false
	}
}

// setupProviders is the registry of model providers as the machine's setup keeps the environment's ones in it.
type setupProviders struct {
	svc *extproviders.Service
	// ask puts on the person's page the card of a secret the environment's provider names: to put it, or to allow one
	// they have for the provider's server. nil asks nothing.
	ask func(name, origin, why string)
}

func (s setupProviders) Apply(itemID, name string, source json.RawMessage) (bool, []string, []string, error) {
	src, err := extproviders.ParseManagedSource(source)
	if err != nil {
		return false, nil, nil, errors.New(extProviderMessage(extproviders.ReasonOf(err)))
	}
	changed, waiting, err := s.svc.ApplyManaged(itemID, name, src)
	if err != nil {
		return false, nil, nil, errors.New(extProviderMessage(extproviders.ReasonOf(err)))
	}
	var shared []string
	for _, ref := range src.SecretRefs() {
		if extproviders.SharedRef(ref) {
			shared = append(shared, ref)
		}
	}
	if s.ask != nil {
		origin := extproviders.OriginOf(src.BaseURL)
		for _, n := range waiting {
			s.ask(n, origin, locale.Pick("Team environment: the provider «", "Окружение команды: провайдер «")+name+"» ("+origin+")")
		}
	}
	return changed, waiting, shared, nil
}

func (s setupProviders) Has(itemID string) bool { return s.svc.HasManaged(itemID) }

func (s setupProviders) Prune(keep []string) ([]string, error) { return s.svc.PruneManaged(keep) }

// setupApps is the app registry as the machine's setup installs the environment's apps into it.
type setupApps struct {
	reg *apps.Registry
	// link is the runner's link folder: its installs are its own.
	link    string
	version string
}

func (s setupApps) Install(dir, itemID string, locked bool, install map[string]string, keep []string) (string, string, bool, []string, error) {
	got, changed, err := s.reg.InstallForEnv(dir, apps.EnvInstallOptions{Link: s.link, ItemID: itemID, Locked: locked, HostVersion: s.version, Now: time.Now(), Install: install, Keep: keep})
	if errors.Is(err, apps.ErrEnvOwnItem) {
		// The organization's own item brought the same app: the platform's item has nothing to do, and that is no error.
		return got.ID, got.Version, false, got.Missing, nil
	}
	return got.ID, got.Version, changed, got.Missing, err
}

func (s setupApps) Has(itemID string) bool { return s.reg.EnvHasItem(s.link, itemID) }

func (s setupApps) Prune(keep []string) ([]string, error) { return s.reg.PruneEnv(s.link, keep) }
