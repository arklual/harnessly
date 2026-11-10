package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/docprovider"
	"github.com/ly-solutions/harnessly/internal/integrations"
	"github.com/ly-solutions/harnessly/internal/panel"
)

// linkResolver is panel.Targets.Resolve (HRN-890, GitHub #60): an address on an instance of one of the connections, or
// on the service of the documents provider, opens where that connection or provider reads it, never in the remote
// browser by default.
//
//   - A connection's issue opens in the task tab on that connection (panel.ConnTarget), so two trackers that share a key
//     each open their own; a wiki page opens in the same tab as a page. The key is the one the provider read out of the
//     address, case and hyphens as it wrote them.
//   - A connection whose last check was refused has no sign-in the pane could read with: the address is a "link" with the
//     reason `connect`.
//   - A document or a board of the documents provider (the optional `resolve` capability of harnessly.docs/v1) opens in
//     the artifacts tab by its local alias; a provider that says it cannot be framed gives a "link" with the reason
//     `embed`. A provider that declares no `resolve` is never asked.
//
// cs and docs may be nil; the resolver then claims nothing.
func linkResolver(cs *connections.Service, docs func() docprovider.Provider, targets panel.Targets) func(ctx context.Context, text string) panel.Link {
	return func(ctx context.Context, text string) panel.Link {
		u, err := url.Parse(strings.TrimSpace(text))
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return panel.Link{}
		}
		if cs != nil {
			if c, t, ok := cs.Resolve(text); ok {
				if l := connLink(c, t, text); l.Tab != "" {
					return l
				}
			}
		}
		if docs == nil {
			return panel.Link{}
		}
		p := docs()
		if p == nil {
			return panel.Link{}
		}
		r, ok := p.(docprovider.Resolver)
		if !ok {
			return panel.Link{}
		}
		ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		res, found, err := r.Resolve(ctx, text)
		var up *integrations.UpstreamError
		switch {
		case errors.As(err, &up) && up.Status == 403:
			return panel.Link{Tab: "link", Target: text, Reason: panel.ReasonConnect}
		case err != nil || !found:
			return panel.Link{}
		case !res.Embeddable():
			return panel.Link{Tab: "link", Target: text, Reason: panel.ReasonEmbed}
		}
		return panel.Link{Tab: "artifacts", Target: targets.DocTarget(docprovider.Alias(res.ID))}
	}
}

// docsProviderOf is the documents provider a link resolver asks, looked up on use (the adapters load once, lazily): the
// declared adapter, nil when there is none (the built-in catalog resolves nothing).
func docsProviderOf() func() docprovider.Provider {
	return func() docprovider.Provider {
		if !docprovider.Declared() {
			return nil
		}
		return docprovider.For(integrations.Config{}, nil)
	}
}

// connLink is where an address a connection claimed opens: its issue or page in the task tab on that connection, or a
// link to connect when the connection's sign-in was refused at its last check.
func connLink(c connections.Conn, t connections.LinkTarget, text string) panel.Link {
	if t.Key == "" || (t.Kind != "issue" && t.Kind != "page") {
		return panel.Link{}
	}
	if c.State == "denied" {
		return panel.Link{Tab: "link", Target: text, Reason: panel.ReasonConnect}
	}
	return panel.Link{Tab: "task", Target: panel.ConnTarget(c.ID, t.Kind, t.Key)}
}
