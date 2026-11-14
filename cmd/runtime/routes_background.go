package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/bgmodel"
	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/settingsctl"
)

// The background tasks of the Settings screen (internal/bgmodel): which provider and model each background feature
// runs on. The person's page: a page session, same origin.
//
//	GET <platform>/background-tasks             {features: [backgroundFeature], providers: [backgroundProvider]}
//	PUT <platform>/background-tasks/<feature>   {provider: "<id>" | null, model?: "<name>"} -> {feature: backgroundFeature}
//
// provider null is «as now»: the chat's own agent decides, and no provider is called.

// backgroundFeature is a feature with the choice made for it.
type backgroundFeature struct {
	bgmodel.Feature
	// Provider and Model are the choice; both null is «as now».
	Provider *string `json:"provider"`
	Model    *string `json:"model"`
	// Effort is the reasoning effort the model is asked for; "" is the model's own.
	Effort string `json:"effort,omitempty"`
	// Agents: the feature may also be given a model of an agent of the machine (provider "claude" or "codex"), or
	// turned off (provider "off").
	Agents bool `json:"agents,omitempty"`
	// Last is how the feature's last call to its provider went; absent when it was not called since the runtime
	// started.
	Last *bgmodel.Outcome `json:"last,omitempty"`
}

// backgroundProvider is a provider a feature may be given.
type backgroundProvider struct {
	ID     string               `json:"id"`
	Name   string               `json:"name"`
	API    extproviders.API     `json:"api"`
	Models []extproviders.Model `json:"models"`
}

var (
	bgRunnersMu sync.Mutex
	bgRunners   = map[string]*bgmodel.Runner{}
)

// bgRunnerFor is the process's one runner of a data folder's background features.
func bgRunnerFor(d deps) *bgmodel.Runner {
	providers := extProvidersFor(d)
	bgRunnersMu.Lock()
	defer bgRunnersMu.Unlock()
	if r := bgRunners[d.DataDir]; r != nil {
		return r
	}
	r := &bgmodel.Runner{
		Store:     &bgmodel.Store{Path: filepath.Join(d.DataDir, "v2", "background-tasks.json")},
		Providers: providers,
		Client:    &bgmodel.Client{},
		Log: func(feature string, c bgmodel.Choice, err error) {
			// The record of a fallback: the feature did what it did before. Never the key, never the provider's body.
			agent.Log("warn", "background: the provider failed, the feature fell back", agent.F{K: "feature", V: feature},
				agent.F{K: "provider", V: c.Provider}, agent.F{K: "model", V: c.Model}, agent.F{K: "reason", V: bgmodel.ReasonOf(err)})
		},
	}
	bgRunners[d.DataDir] = r
	// A removed account leaves no feature pointing at it: what ran on it goes on with the default account of the same
	// provider when that one has the model, and back to «as now» otherwise.
	onExtProviderChange(d.DataDir, func(what string, before, _ *extproviders.Provider) {
		if what != "removed" || before == nil {
			return
		}
		if next, ok := providers.DefaultOf(*before); ok {
			r.Move(before.ID, next.ID, next.HasModel)
		}
		r.Forget(before.ID)
	})
	return r
}

// bgRunnerOf is the runner of a data folder when the routes made one; nil before that.
func bgRunnerOf(dataDir string) *bgmodel.Runner {
	bgRunnersMu.Lock()
	defer bgRunnersMu.Unlock()
	return bgRunners[dataDir]
}

// pushSummary is the notification summaries of a data folder for the push sender. It looks the runner up at each
// notification: the sender is made before the routes are.
type pushSummary struct{ dataDir string }

func (p pushSummary) Picked() bool {
	return bgRunnerOf(p.dataDir).Picked(bgmodel.FeatureNotifySummary)
}

func (p pushSummary) Summarize(ctx context.Context, answer string) (string, error) {
	r := bgRunnerOf(p.dataDir)
	if r == nil {
		return "", nil
	}
	summary, _, err := r.NotifySummary(ctx, answer)
	return summary, err
}

func backgroundView(r *bgmodel.Runner, summary noticeSummary) []backgroundFeature {
	choices, last := r.Store.All(), r.Last()
	out := []backgroundFeature{}
	for _, f := range bgmodel.Features() {
		v := backgroundFeature{Feature: f}
		if c, ok := choices[f.ID]; ok {
			v.Provider, v.Model, v.Effort = &c.Provider, &c.Model, c.Effort
		}
		// The summaries may also be an agent's model, or off (notice_summary.go).
		if f.ID == bgmodel.FeatureNotifySummary {
			v.Agents = true
			if provider, model := summary.Pick(); provider != "" {
				v.Provider, v.Model = &provider, &model
			}
		}
		if l, ok := last[f.ID]; ok {
			v.Last = &l
		}
		out = append(out, v)
	}
	return out
}

func backgroundMessage(err error) (int, string, string) {
	switch {
	case errors.Is(err, bgmodel.ErrUnknownFeature):
		return http.StatusNotFound, "unknown-feature", locale.Pick("There is no such background task.", "Такой фоновой задачи нет.")
	case errors.Is(err, bgmodel.ErrUnknownProvider):
		return http.StatusBadRequest, "unknown-provider", locale.Pick("There is no such provider any more. Pick another.", "Такого провайдера уже нет. Выберите другого.")
	case errors.Is(err, bgmodel.ErrBadModel):
		return http.StatusBadRequest, "bad-model", locale.Pick("Pick a model.", "Выберите модель.")
	case errors.Is(err, bgmodel.ErrBadEffort):
		return http.StatusBadRequest, "bad-effort", locale.Pick("Pick a level of reasoning the model has.", "Выберите уровень рассуждения, который есть у модели.")
	}
	return http.StatusInternalServerError, "failed", locale.Pick("It did not work. Try again.", "Не получилось. Попробуйте ещё раз.")
}

func backgroundRoutes(d deps) []gate.Route {
	runner := bgRunnerFor(d)
	providers := extProvidersFor(d)
	summary := noticeSummaryFor(d)
	base := platformAPI + "/background-tasks"
	under := func(r *http.Request) (string, bool) {
		for _, prefix := range []string{base + "/", "/agents" + base + "/"} {
			if rest, ok := strings.CutPrefix(r.URL.Path, prefix); ok && rest != "" && !strings.Contains(rest, "/") {
				return rest, true
			}
		}
		return "", false
	}
	pageOK := func(w http.ResponseWriter, r *http.Request) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	list := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		views, err := providers.List()
		if err != nil {
			extProviderRefusal(w, err)
			return
		}
		offered := []backgroundProvider{}
		for _, v := range views {
			offered = append(offered, backgroundProvider{ID: v.ID, Name: v.Name, API: v.API, Models: v.Models})
		}
		secretJSON(w, http.StatusOK, map[string]any{"features": backgroundView(runner, summary), "providers": offered})
	}
	one := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodPut {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		feature, _ := under(r)
		var b struct {
			Provider *string `json:"provider"`
			Model    string  `json:"model"`
			Effort   string  `json:"effort"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<10))
		if err != nil || json.Unmarshal(raw, &b) != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		var choice *bgmodel.Choice
		if b.Provider != nil && *b.Provider != "" {
			choice = &bgmodel.Choice{Provider: *b.Provider, Model: b.Model, Effort: b.Effort}
		}
		value := func(c *bgmodel.Choice) string {
			if c == nil {
				return backgroundAsNow
			}
			return c.String()
		}
		var was, now string
		if feature == bgmodel.FeatureNotifySummary {
			// One setting over the agents' models and the providers' (notice_summary.go).
			was = summary.Value()
			provider := ""
			if b.Provider != nil {
				provider = *b.Provider
			}
			err = summary.Set(provider, b.Model)
			now = summary.Value()
		} else {
			var before *bgmodel.Choice
			before, err = runner.Pick(feature, choice)
			was, now = value(before), value(choice)
		}
		if err != nil {
			status, reason, message := backgroundMessage(err)
			secretJSON(w, status, map[string]string{"error": reason, "message": message})
			return
		}
		if ctl := settingsCtlOf(d.DataDir); ctl != nil {
			// The person's change goes into the journal of the settings' changes.
			ctl.Record("background."+feature, was, now, settingsctl.Actor{Kind: settingsctl.ByPerson}, settingsctl.ViaPage)
		}
		for _, v := range backgroundView(runner, summary) {
			if v.ID == feature {
				secretJSON(w, http.StatusOK, map[string]any{"feature": v})
				return
			}
		}
	}
	return []gate.Route{
		{Match: func(r *http.Request) bool { return pathIs(r, base) }, Handler: http.HandlerFunc(list)},
		{Match: func(r *http.Request) bool { _, ok := under(r); return ok }, Handler: http.HandlerFunc(one)},
	}
}
