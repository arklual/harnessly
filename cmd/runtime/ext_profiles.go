package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/providers"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A provider the person added by API that speaks the Anthropic API is a profile a Claude Code chat starts on
// its address and models, its key from the safe, and over them the variables the person typed for it. The
// profiles are made again from the registry whenever it changes; a chat that runs already keeps the settings file it
// was started with until it is started again.

// extProfileID is the id of the profile of a provider: «x-» and the provider's id, or a digest of it when that would
// be longer than a profile id may be.
func extProfileID(provider string) string {
	if id := "x-" + provider; wire.ValidProfileID(id) {
		return id
	}
	sum := sha256.Sum256([]byte(provider))
	return "x-" + hex.EncodeToString(sum[:])[:16]
}

// extProfile is the launch profile of a provider; ok is false for one Claude Code cannot run on (no model a launch
// would take). An Anthropic-compatible provider is reached straight at its address; an OpenAI-compatible one through
// the runtime's proxy, which knows its models by extAlias.
func extProfile(p extproviders.Provider) (providers.Preset, bool) {
	if !p.ClaudeCLI() && !p.Proxied() {
		return providers.Preset{}, false
	}
	out := providers.Preset{ID: extProfileID(p.ID), Label: p.Name, Vendor: wire.VendorOwn, Via: wire.ProviderClaude,
		BaseURL: p.BaseURL, Host: p.Host(), KeyRef: p.KeyRef, Custom: true, Env: map[string]string{}, SecretEnv: map[string]string{}}
	name := func(id string) string { return id }
	if p.Proxied() {
		// The proxy holds the provider's key (through the runtime's relay): the profile names none.
		out.Proxied, out.KeyRef = p.ID, ""
		name = func(id string) string { return extAlias(p.ID, id) }
	}
	for _, m := range p.Models {
		id := name(m.ID)
		if !agents.IsProfileModelID(id) {
			continue
		}
		label := m.Label
		if label == "" {
			label = m.ID
		}
		pm := providers.Model{ID: id, Label: label, Window: m.Window}
		for _, e := range m.Efforts {
			pm.Efforts = append(pm.Efforts, wire.Effort(e))
		}
		if m.DefaultEffort != "" {
			pm.DefaultEffort = wire.Effort(m.DefaultEffort)
		}
		out.Models = append(out.Models, pm)
	}
	if len(out.Models) == 0 {
		return providers.Preset{}, false
	}
	out.Small, out.Test = out.Models[0].ID, out.Models[0].ID
	for _, v := range p.Env {
		if p.Proxied() && slices.Contains(proxiedOwnEnv, v.Name) {
			// The chat goes to the proxy with the proxy's key: a variable that would send it elsewhere is not taken.
			continue
		}
		// A value that is the sign-in of a Claude subscription makes no profile: the machine has one such sign-in.
		if !v.Secret && agents.ClaudeSubscriptionToken(v.Value) {
			return providers.Preset{}, false
		}
		if v.Secret {
			out.SecretEnv[v.Name] = v.Ref
		} else {
			out.Env[v.Name] = v.Value
		}
	}
	// The model the person typed is what every chat on the provider runs (providers.ClaudeModelPinned), so it is the
	// one model the picker offers: a list to pick from would name a model the chat is not on.
	if pinned := strings.TrimSpace(out.Env["ANTHROPIC_MODEL"]); agents.IsProfileModelID(pinned) {
		k := slices.IndexFunc(out.Models, func(m providers.Model) bool { return m.ID == name(pinned) })
		if k >= 0 {
			out.Models = []providers.Model{out.Models[k]}
		} else {
			out.Models = []providers.Model{{ID: name(pinned), Label: pinned}}
		}
		if p.Proxied() {
			// The proxy is asked for the model by its alias.
			out.Env["ANTHROPIC_MODEL"] = name(pinned)
		}
		out.Small, out.Test = out.Models[0].ID, out.Models[0].ID
	}
	return out, true
}

// proxiedOwnEnv are the variables of a chat through the proxy the runtime sets itself: where it goes and its key.
var proxiedOwnEnv = []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"}

// modelLabel is the name the provider's list gives the model, or its id when the list does not have it.
func modelLabel(models []providers.Model, id string) string {
	for _, m := range models {
		if m.ID == id {
			return m.Label
		}
	}
	return id
}

// vendorOf is the vendor's profile an account was made from: the template is one of providers.AccountVendors and
// the account still points at the vendor's own address. ok is false for every other provider.
func vendorOf(p extproviders.Provider) (providers.Preset, bool) {
	if !providers.IsAccountVendor(p.Preset) || !p.ClaudeCLI() {
		return providers.Preset{}, false
	}
	v, ok := providers.Find(p.Preset)
	if !ok || !strings.EqualFold(p.Host(), v.Host) {
		return providers.Preset{}, false
	}
	return v, true
}

// asVendor gives the profile of an account of a vendor what the vendor's own profile has and a list of models from
// the address does not: the models with their depths and windows, the small model and the vendor's variables.
func asVendor(pr providers.Preset, v providers.Preset) providers.Preset {
	pr.Vendor, pr.Models, pr.Small, pr.Test = v.Vendor, v.Models, v.Small, v.Test
	for name, value := range v.Env {
		if _, typed := pr.Env[name]; !typed {
			pr.Env[name] = value
		}
	}
	if pinned := strings.TrimSpace(pr.Env["ANTHROPIC_MODEL"]); agents.IsProfileModelID(pinned) {
		pr.Models = []providers.Model{{ID: pinned, Label: modelLabel(v.Models, pinned)}}
	}
	return pr
}

// followExtProviders has the launch profiles follow a data folder's registry from now on: every change of it makes
// them again and tells the pages, so an account the person just added is in the picker of the next chat at once.
func followExtProviders(dataDir string, svc *extproviders.Service, store *providers.Store, republish func()) {
	onExtProviderChange(dataDir, func(string, *extproviders.Provider, *extproviders.Provider) {
		syncExtProfiles(svc, store)
		republish()
	})
}

// syncExtProfiles makes the launch profiles of a data folder's providers again and has the store read their keys anew.
// The default account of a vendor (Kimi, GLM) is the vendor's own profile, under the account's name: the chats that
// run on that profile go on with the account's key. Every account is a profile of its own as well, so a chat started
// on one keeps it when the person makes another account the default.
func syncExtProfiles(svc *extproviders.Service, store *providers.Store) {
	list, err := svc.Store.List()
	if err != nil {
		return
	}
	backing := map[string]providers.Backing{}
	if vendorKeysMoved(svc.Store.Dir) {
		// Once the keys typed for the vendors were moved to accounts, a vendor without an account has no key.
		for _, id := range providers.AccountVendors {
			backing[id] = providers.Backing{}
		}
	}
	var out []providers.Preset
	for _, p := range list {
		v, vendor := vendorOf(p)
		_, moved := backing[p.Preset]
		main := vendor && moved && p.Default && p.KeyRef != ""
		if main {
			backing[p.Preset] = providers.Backing{KeyRef: p.KeyRef, Label: p.Name}
		}
		pr, ok := extProfile(p)
		if !ok {
			continue
		}
		if vendor {
			pr = asVendor(pr, v)
		}
		// The default account is picked as the vendor's profile; its own profile stays for the chats started on it
		// while another account was the default.
		pr.Unlisted = main
		out = append(out, pr)
	}
	providers.SetBacked(backing)
	providers.SetCustom(out)
	if ext, ok := store.Proxy.(interface{ SetExt([]providers.ExtUpstream) }); ok {
		ext.SetExt(extUpstreams(list))
	}
	for _, pr := range out {
		store.Stale(pr.ID)
	}
	for id := range backing {
		store.Stale(id)
	}
}

// vendorKeysFile marks a data folder whose keys typed for the vendors (Kimi, GLM) were moved to accounts.
func vendorKeysFile(registryDir string) string {
	return filepath.Join(registryDir, "vendor-keys-moved")
}

func vendorKeysMoved(registryDir string) bool {
	_, err := os.Stat(vendorKeysFile(registryDir))
	return err == nil
}

// moveVendorKeys makes the key a person typed for a vendor's profile the first account of that vendor among their
// providers, once: the same key, now in the safe, under the vendor's name. The profile goes on with it (the default
// account of a vendor is the vendor's profile, syncExtProfiles), so the chats that run on it notice nothing and the
// key is not typed again. An account that holds the key already is left as it is. A failure leaves the
// folder unmarked: the next start tries again, and until then the profiles keep the keys typed for them.
func moveVendorKeys(svc *extproviders.Service, store *providers.Store) {
	dir := svc.Store.Dir
	if vendorKeysMoved(dir) {
		return
	}
	list, err := svc.Store.List()
	if err != nil {
		return
	}
	templates := extproviders.Presets()
	for _, id := range providers.AccountVendors {
		key, err := store.Key(id)
		if err != nil || strings.TrimSpace(key) == "" {
			continue
		}
		// An account that holds this very key already (an earlier start got that far): nothing to carry over. Another
		// account of the vendor does not stand for the typed key: it becomes an account beside it.
		if slices.ContainsFunc(list, func(p extproviders.Provider) bool {
			have, err := svc.Key(p)
			return p.Preset == id && err == nil && have == strings.TrimSpace(key)
		}) {
			continue
		}
		k := slices.IndexFunc(templates, func(t extproviders.Preset) bool { return t.ID == id })
		if k < 0 {
			continue
		}
		t := templates[k]
		_, err = svc.Create(extproviders.Input{Name: t.Name, BaseURL: t.BaseURL, API: t.API, Preset: t.ID, Key: key})
		if extproviders.ReasonOf(err) == extproviders.ReasonSubscriptionToken {
			// No key of a vendor: nothing to carry over.
			continue
		}
		if err != nil {
			agent.Log("warn", "providers: a vendor's key was not moved to an account", agent.F{K: "vendor", V: id}, agent.F{K: "error", V: err.Error()})
			return
		}
		agent.Log("info", "providers: a vendor's key is an account now", agent.F{K: "vendor", V: id})
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(vendorKeysFile(dir), []byte("1\n"), 0o600)
}
