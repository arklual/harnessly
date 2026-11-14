package main

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/providers"
)

// extRelay is the relay the runtime's proxy reaches an OpenAI-compatible provider through: Claude Code →
// the proxy (Anthropic's /v1/messages) → here, at <ExtRelayPath>/<provider id>/chat/completions with the proxy's
// secret of this runtime → the provider's address with its key, read from the machine's safe for each request. So the
// proxy's config holds no provider's key, and the key goes nowhere but to the provider it was saved for.
func extRelay(dataDir string, gpt *providers.GPTProxy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(gpt.Secret())) != 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, providers.ExtRelayPath+"/")
		id, _, _ := strings.Cut(rest, "/")
		extProvidersMu.Lock()
		svc := extProvidersAll[dataDir]
		extProvidersMu.Unlock()
		if svc == nil || id == "" {
			http.NotFound(w, r)
			return
		}
		p, err := svc.Get(id)
		if err != nil || !p.Proxied() {
			http.NotFound(w, r)
			return
		}
		relay := &providers.Relay{
			Upstream: p.BaseURL, Prefix: providers.ExtRelayPath + "/" + id, Plain: true,
			Forward:    []string{"Content-Type", "Accept"},
			Credential: func(context.Context) (string, error) { return svc.Key(p) },
		}
		relay.ServeHTTP(w, r)
	})
}

// extAlias is the name Claude Code asks the proxy for a model of an OpenAI-compatible provider by: the provider's id
// before it, so two providers with a model of one name stay apart.
func extAlias(provider, model string) string { return provider + "--" + model }

// extUpstreams are the OpenAI-compatible providers the proxy relays, with the models a chat may run on.
func extUpstreams(list []extproviders.Provider) []providers.ExtUpstream {
	var out []providers.ExtUpstream
	for _, p := range list {
		pr, ok := extProfile(p)
		if !ok || pr.Proxied == "" {
			continue
		}
		u := providers.ExtUpstream{ID: p.ID}
		for _, m := range pr.Models {
			u.Models = append(u.Models, providers.ExtModel{Name: strings.TrimPrefix(m.ID, extAlias(p.ID, "")), Alias: m.ID, Efforts: m.Efforts})
		}
		out = append(out, u)
	}
	return out
}
