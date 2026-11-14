package main

import (
	"path/filepath"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/providers"
	"github.com/ly-solutions/harnessly/internal/sessionaccounts"
	"github.com/ly-solutions/harnessly/internal/usagefeed"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The account each agent session ran on, kept beside the conversations of the data folder: the report of
// spend reads it, the conversations write it. One store a folder, so that what a launch notes the report sees at once.

var sessionAccountStores sync.Map

func sessionAccountsOf(dataDir string) *sessionaccounts.Store {
	path := filepath.Join(dataDir, "v2", "session-accounts.jsonl")
	s, _ := sessionAccountStores.LoadOrStore(path, &sessionaccounts.Store{Path: path})
	return s.(*sessionaccounts.Store)
}

// noteSessionAccount is convo.Deps.SessionAccount: the session is counted to the profile it was started on, under the
// name the profile has now (an account's name, or the vendor's for a key typed before accounts).
// For the hub the account is named as its other accounts are (docs/hub/usage.md): a digest of the key, never the key,
// and the host its calls go to.
func noteSessionAccount(dataDir string, keys *providers.Store) func(wire.Provider, string, string) {
	store := sessionAccountsOf(dataDir)
	return func(p wire.Provider, session, profile string) {
		e := sessionaccounts.Entry{Provider: string(p), Session: session, Account: profile, Label: profile}
		if pr, ok := providers.Find(profile); ok {
			if pr.Label != "" {
				e.Label = pr.Label
			}
			e.Host = pr.Host
		}
		if key, err := keys.Key(profile); err == nil && key != "" {
			e.Ref = usagefeed.KeyRef(key)
		}
		if pr, ok := providers.Find(profile); ok && pr.Proxied != "" {
			// A chat through the proxy sends the proxy's key; the account is the provider's, named by its key.
			e.Ref = proxiedKeyRef(dataDir, pr.Proxied)
		}
		if home := codexLoginHomeOf(profile); home != "" {
			// Another sign-in of the Codex CLI: the account is the one its own home is signed in with, named as the
			// main sign-in's is, and its calls go to the provider's own endpoint.
			id := usagefeed.Local{Getenv: func(_, name string) string {
				if name == "CODEX_HOME" {
					return home
				}
				return ""
			}}.Identity(string(wire.ProviderCodex))
			e.Ref, e.Host = id.AccountRef, ""
		}
		if err := store.Note(e); err != nil {
			agent.Log("warn", "usage: the account of a session was not noted", agent.F{K: "error", V: err.Error()})
		}
	}
}

// codexLoginHomeOf is the CODEX_HOME of a profile that is another sign-in of the Codex CLI; "" for any other profile.
func codexLoginHomeOf(profile string) string {
	pr, ok := providers.Find(profile)
	if !ok {
		return ""
	}
	return pr.Home
}

// accountOfSession is usagereport.Config.AccountOf: the name noted for the session, "" for one nobody noted.
func accountOfSession(dataDir string) func(provider, session string) string {
	store := sessionAccountsOf(dataDir)
	return func(provider, session string) string {
		e, _ := store.Of(provider, session)
		return e.Label
	}
}

// proxiedKeyRef is the digest of the key of an OpenAI-compatible provider a chat ran on through the proxy; "" when the
// registry or the key is not there.
func proxiedKeyRef(dataDir, provider string) string {
	extProvidersMu.Lock()
	svc := extProvidersAll[dataDir]
	extProvidersMu.Unlock()
	if svc == nil {
		return ""
	}
	p, err := svc.Get(provider)
	if err != nil {
		return ""
	}
	if key, err := svc.Key(p); err == nil && key != "" {
		return usagefeed.KeyRef(key)
	}
	return ""
}
