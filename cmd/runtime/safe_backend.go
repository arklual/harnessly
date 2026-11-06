package main

import (
	"errors"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/secrets/execkv"
	"github.com/ly-solutions/harnessly/internal/secrets/vaultkv"
)

// Where the safe keeps its values, by RUNTIME_SECRETS_MODE:
//
//	local (default)  this machine's sealed file, nothing else is needed
//	vault            a Vault-compatible server (OpenBao, HashiCorp Vault): RUNTIME_SECRETS_VAULT_*; the file stays
//	                 readable for the secrets put before the switch
//	hub              the hub's safe, through the machine agent: only the hub logs in to the server, the machine reaches
//	                 its owner's folder and the references the hub allows
//	exec             any other store, through the command RUNTIME_SECRETS_EXEC (harnessly.secrets-provider/v1)
//
// See docs/safe.md, "Keeping the safe on a server", "The hub's safe" and "Any other store".
const (
	safeModeLocal = "local"
	safeModeVault = "vault"
	safeModeHub   = "hub"
	safeModeExec  = "exec"
)

// safeStore is where the safe is, for the page: never an address of a secret's value, only of the server.
type safeStore struct {
	Mode   string `json:"mode"`
	Addr   string `json:"addr,omitempty"`
	Mount  string `json:"mount,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Auth   string `json:"auth,omitempty"`
	// Refs says whether vault: references may be used.
	Refs bool `json:"refs,omitempty"`
	// Error is why the server cannot be used (a bad setting); the safe then refuses new secrets as unavailable.
	Error string `json:"error,omitempty"`
}

// brokenProvider is a server safe whose settings do not work: every call is refused as unavailable, so a value is
// never quietly kept on the machine when the administrator asked for a server.
type brokenProvider struct{ err error }

func (b brokenProvider) refusal() error {
	return &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: b.err}
}
func (b brokenProvider) List() ([]secrets.View, error) { return nil, b.refusal() }
func (b brokenProvider) Has(string) bool               { return false }
func (b brokenProvider) Put(string, string) (secrets.View, bool, error) {
	return secrets.View{}, false, b.refusal()
}
func (b brokenProvider) Remove(string) error { return b.refusal() }
func (b brokenProvider) Reveal([]string, secrets.Use) (map[string]string, error) {
	return nil, b.refusal()
}
func (b brokenProvider) Lookup(string, string) (secrets.View, error) {
	return secrets.View{}, b.refusal()
}
func (b brokenProvider) Metadata(string, []string, []string) (secrets.View, error) {
	return secrets.View{}, b.refusal()
}
func (b brokenProvider) Grant(string, string, string) error       { return b.refusal() }
func (b brokenProvider) GrantOrigin(string, string, string) error { return b.refusal() }
func (b brokenProvider) PutAsked(secrets.Ask, string) (secrets.View, bool, error) {
	return secrets.View{}, false, b.refusal()
}

// newSafeBackend is the safe the settings ask for, around the machine's file; dir is the safe's folder (the ledger of
// a server safe lies there).
func newSafeBackend(getenv func(string) string, local *secrets.Store, dir, dataDir string) (*secrets.Safe, safeStore) {
	mode := strings.ToLower(strings.TrimSpace(getenv("RUNTIME_SECRETS_MODE")))
	if mode == "" || mode == safeModeLocal {
		return &secrets.Safe{Local: local}, safeStore{Mode: safeModeLocal}
	}
	env := func(k string) string { return strings.TrimSpace(getenv("RUNTIME_SECRETS_VAULT_" + k)) }
	info := safeStore{Mode: mode}
	broken := func(err error) (*secrets.Safe, safeStore) {
		info.Error = err.Error()
		return &secrets.Safe{Local: local, Primary: brokenProvider{err: err}, Where: secrets.WhereVault}, info
	}
	switch mode {
	case safeModeHub:
		// The hub checks each reference itself; the runtime lets them all through to it.
		remote := secrets.NewRemote(agent.LocalSafe{DataDir: dataDir, TokenHeader: getenv("RUNTIME_AGENT_TOKEN_HEADER")}, agent.SafeOwnFolder, dir)
		remote.RefPrefixes = []string{"*"}
		info.Refs = true
		return &secrets.Safe{Local: local, Primary: remote, Refs: remote, Where: secrets.WhereVault}, info
	case safeModeExec:
		kv, err := execkv.New(getenv("RUNTIME_SECRETS_EXEC"))
		if err != nil {
			return broken(errors.New("RUNTIME_SECRETS_EXEC: " + err.Error()))
		}
		info.Addr, info.Prefix = kv.Name(), strings.Trim(strings.TrimSpace(getenv("RUNTIME_SECRETS_EXEC_PREFIX")), "/")
		if info.Prefix == "" {
			info.Prefix = "harnessly"
		}
		if !vaultkv.ValidPath(info.Prefix) {
			return broken(errors.New("RUNTIME_SECRETS_EXEC_PREFIX is not a path"))
		}
		remote := secrets.NewRemote(kv, info.Prefix, dir)
		remote.RefPrefixes = vaultkv.SplitPrefixes(getenv("RUNTIME_SECRETS_REF_PREFIXES"))
		info.Refs = len(remote.RefPrefixes) > 0
		return &secrets.Safe{Local: local, Primary: remote, Refs: remote, Where: secrets.WhereVault}, info
	case safeModeVault:
	default:
		return broken(errors.New("RUNTIME_SECRETS_MODE must be local, vault, hub or exec"))
	}
	info.Addr, info.Mount, info.Prefix = env("ADDR"), env("MOUNT"), env("PREFIX")
	cfg, err := vaultkv.FromEnv("RUNTIME_SECRETS_VAULT_", getenv)
	if err != nil {
		return broken(err)
	}
	info.Auth = cfg.Auth.Method()
	client, err := vaultkv.New(cfg)
	if err != nil {
		return broken(err)
	}
	info.Addr, info.Mount = client.Addr(), client.Mount()
	if info.Prefix == "" {
		info.Prefix = "harnessly"
	}
	if !vaultkv.ValidPath(strings.Trim(info.Prefix, "/")) {
		return broken(errors.New("RUNTIME_SECRETS_VAULT_PREFIX is not a path"))
	}
	remote := secrets.NewRemote(client, info.Prefix, dir)
	remote.RefPrefixes = vaultkv.SplitPrefixes(getenv("RUNTIME_SECRETS_REF_PREFIXES"))
	info.Refs = len(remote.RefPrefixes) > 0
	return &secrets.Safe{Local: local, Primary: remote, Refs: remote, Where: secrets.WhereVault}, info
}
