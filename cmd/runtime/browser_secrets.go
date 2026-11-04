package main

import (
	"context"
	"errors"
	"slices"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

func configureBrowserSecrets(d deps, c *core) {
	sf := safeFor(d)
	d.BrowserTools.Secrets = sf.store
	d.BrowserTools.Identity = func(id string) (secrets.Use, error) {
		use := secretIdentity(c, id)
		if use.Conv == "" {
			return use, errors.New("Cannot identify this chat")
		}
		return use, nil
	}
	d.BrowserTools.RequestSecret = func(ctx context.Context, id, name, origin, why string) (string, error) {
		use, err := d.BrowserTools.Identity(id)
		if err != nil {
			return "", err
		}
		origin = secrets.Origin(origin)
		if origin == "" {
			return "", errors.New("origin must be an HTTPS site or HTTP on localhost")
		}
		existing, lookupErr := sf.store.Lookup(name, use.Project)
		if lookupErr == nil && slices.Contains(existing.Origins, origin) {
			return secrets.AskReady, nil
		}
		grant := sf.store.Has(name) && lookupErr != nil
		if grant && use.Project == "" {
			return "", errors.New("Secret is unavailable to this chat")
		}
		a, state, err := sf.asks.OpenScoped(secrets.Ask{Name: name, Origin: origin, Why: why,
			Conv: id, Title: use.Title, Project: use.Project, Grant: grant})
		if err != nil {
			return "", err
		}
		if state == secrets.AskWaiting {
			state = sf.asks.Wait(ctx, a.ID, secretWaitMax)
		}
		current, err := d.BrowserTools.Identity(id)
		if err != nil || current.Project != use.Project {
			return secrets.AskDeclined, nil
		}
		if state == secrets.AskReady {
			view, err := sf.store.Lookup(name, current.Project)
			if err != nil || !slices.Contains(view.Origins, origin) {
				return secrets.AskDeclined, nil
			}
		}
		return state, nil
	}
}
