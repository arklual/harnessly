package main

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/selfupdate"
	"github.com/ly-solutions/harnessly/internal/updatecheck"
)

// The "new version" notice (internal/updatecheck):
//
//	GET <platform>/update   the person's page: a page session. {current, latest, url, publishedAt, updateAvailable,
//	                        machine?}; machine is the state of the programs of a machine a hub carries (selfupdate.go)
//
//	GET /api/cli/update     the same answer for `harnessly doctor update`: the CLI's token
//
// A standalone runtime asks GitHub for the release of its channel at start and every 12 hours, and says nothing else to
// anyone. RUNTIME_UPDATE_CHANNEL is the channel: stable (the default: the release the project's stable pointer names) or
// latest (every release). RUNTIME_UPDATE_CHECK=0 turns the check off; RUNTIME_UPDATE_CHECK_URL names another address of
// the latest release (a mirror, a test) and RUNTIME_UPDATE_STABLE_URL of the stable pointer. A runtime a hub carries
// never asks, whatever the channel: the hub serves its machines' binaries, and a machine in fleet mode takes the
// releases its rollout brings. A "dev" build has nothing to compare.

// updateCheckOn says whether this runtime looks for releases.
func updateCheckOn(standalone bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_UPDATE_CHECK"))) {
	case "0", "off", "false", "no":
		return false
	}
	return standalone && !hubMode() && updatecheck.Comparable(agent.BuildVersion)
}

// newUpdateChecker is the checker of this runtime, and whether it should run (when not, its answer is "no update").
func newUpdateChecker(standalone bool) (c *updatecheck.Checker, on bool) {
	on = updateCheckOn(standalone)
	channel, known := updatecheck.ParseChannel(os.Getenv("RUNTIME_UPDATE_CHANNEL"))
	if !known && on {
		agent.Log("warn", "RUNTIME_UPDATE_CHANNEL is neither stable nor latest: using stable",
			agent.F{K: "value", V: strings.TrimSpace(os.Getenv("RUNTIME_UPDATE_CHANNEL"))})
	}
	c = &updatecheck.Checker{Current: agent.BuildVersion, URL: env("RUNTIME_UPDATE_CHECK_URL", updatecheck.DefaultURL),
		Channel: channel, StableURL: strings.TrimSpace(os.Getenv("RUNTIME_UPDATE_STABLE_URL")),
		Log: func(msg string, err error) { agent.Log("debug", msg, agent.F{K: "error", V: err.Error()}) }}
	return c, on
}

// startUpdateCheck runs the checks until ctx ends; it returns at once.
func (f *runtimeGate) startUpdateCheck(ctx context.Context) {
	if f.update != nil {
		go f.update.Run(ctx)
	}
	if f.self != nil {
		go f.self.Run(ctx)
	}
}

// updateAnswer is the release check's answer, with the hub machine's state when there is one.
type updateAnswer struct {
	updatecheck.Status
	// Checking: this runtime looks for releases (a standalone one built from a release), so the page may show them.
	Checking bool `json:"checking"`
	// Source says where this runtime's updates come from: "releases" (the published releases of its channel), "hub"
	// (the programs its hub serves), "fleet" (a rollout brings them) or "none" (it looks nowhere). The channel is
	// only of "releases".
	Source  string             `json:"source"`
	Machine *selfupdate.Status `json:"machine,omitempty"`
}

// updateState is the answer of both routes. A runtime that does not look for releases has no channel to show.
func updateState(c *updatecheck.Checker, checking bool, self *selfupdate.Updater) updateAnswer {
	answer := updateAnswer{Status: c.Status(), Checking: checking, Source: "none"}
	if checking {
		answer.Source = "releases"
	} else {
		answer.Channel, answer.Note = "", ""
	}
	if self != nil {
		st := self.Status()
		answer.Machine = &st
		answer.Source = "hub"
		if self.Fleet != nil {
			answer.Source = "fleet"
		}
	}
	return answer
}

func updateRoutes(d deps, c *updatecheck.Checker, checking bool, self *selfupdate.Updater) []gate.Route {
	return append(selfUpdateRoutes(d, self), gate.Route{
		Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/update") },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			if !d.SessionOK(r) {
				peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			peerJSON(w, http.StatusOK, updateState(c, checking, self))
		}),
	}, gate.Route{
		Match: func(r *http.Request) bool { return pathIs(r, "/api/cli/update") },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d.CLI == nil || !d.CLI.OK(r) {
				peerJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
				return
			}
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			peerJSON(w, http.StatusOK, updateState(c, checking, self))
		}),
	})
}
