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
// A standalone runtime asks GitHub for the latest release at start and every 12 hours, and says nothing else to anyone.
// RUNTIME_UPDATE_CHECK=0 turns that off; RUNTIME_UPDATE_CHECK_URL names another address (a mirror, a test). A runtime a
// hub carries never asks: the hub serves its machines' binaries. A "dev" build has nothing to compare.

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
	c = &updatecheck.Checker{Current: agent.BuildVersion, URL: env("RUNTIME_UPDATE_CHECK_URL", updatecheck.DefaultURL),
		Log: func(msg string, err error) { agent.Log("debug", msg, agent.F{K: "error", V: err.Error()}) }}
	return c, updateCheckOn(standalone)
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
	Checking bool               `json:"checking"`
	Machine  *selfupdate.Status `json:"machine,omitempty"`
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
			answer := updateAnswer{Status: c.Status(), Checking: checking}
			if self != nil {
				st := self.Status()
				answer.Machine = &st
			}
			peerJSON(w, http.StatusOK, answer)
		}),
	})
}
