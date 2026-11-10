package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The organization's rule for apps «Не спрашивать разрешений» (HRN-915). The hub sends it with the environment list
// (env.set, hubproto.EnvSet.AppsConsent); the agent keeps it in environment.json of its link folder, beside the list, and
// this runtime reads it from there. The rule is on when one link of the machine says auto: then an install, an update or
// a dev edit that would wait for the person gets its rights at once (apps.ConsentPolicy), and what waited when the rule
// came on is granted. A machine without a hub has no such file and asks as before. Turned off, the rule takes nothing
// back. Nothing on the machine writes the rule but the agent from the hub's word: the runtime has no route for it.

// appsPolicyEvery is how often the rule and the dev folders are looked at while the runtime runs.
const appsPolicyEvery = 3 * time.Second

// appsConsentPolicy reads the rule from the link folders, each file read again only when it changed.
type appsConsentPolicy struct {
	files []string

	mu   sync.Mutex
	seen map[string]policySeen
}

type policySeen struct {
	mod  time.Time
	size int64
	auto bool
}

func newAppsConsentPolicy(linkDirs []string) *appsConsentPolicy {
	p := &appsConsentPolicy{seen: map[string]policySeen{}}
	for _, d := range linkDirs {
		p.files = append(p.files, filepath.Join(d, "environment.json"))
	}
	return p
}

// On says whether one link's list carries the rule auto. A file that is missing, unreadable or broken says ask.
func (p *appsConsentPolicy) On() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	on := false
	for _, f := range p.files {
		fi, err := os.Stat(f)
		if err != nil {
			delete(p.seen, f)
			continue
		}
		if s, ok := p.seen[f]; ok && s.mod.Equal(fi.ModTime()) && s.size == fi.Size() {
			on = on || s.auto
			continue
		}
		auto := readAppsConsent(f) == hubproto.AppsConsentAuto
		p.seen[f] = policySeen{mod: fi.ModTime(), size: fi.Size(), auto: auto}
		on = on || auto
	}
	return on
}

// readAppsConsent is the rule of one list file: the one the agent wrote with a list whose hash holds (envkeeper.go).
func readAppsConsent(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var h struct {
		Hash        string             `json:"hash"`
		AppsConsent string             `json:"appsConsent"`
		Items       []hubproto.EnvItem `json:"items"`
	}
	if json.Unmarshal(raw, &h) != nil || h.Hash == "" || hubproto.EnvHash(h.Items) != h.Hash {
		return ""
	}
	return h.AppsConsent
}

// watchAppsConsent grants, while the rule is on, what waits: at once when the rule comes on, and the dev folders that
// grow afterwards. The registry was handed the rule (SetConsentPolicy) before. It returns when ctx ends.
func watchAppsConsent(ctx context.Context, reg *apps.Registry, every time.Duration) {
	grantByPolicy(reg)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			grantByPolicy(reg)
		}
	}
}

func grantByPolicy(reg *apps.Registry) {
	got, err := reg.GrantWaiting(time.Now())
	if err != nil {
		agent.Log("warn", "apps: the organization's rule was not applied", agent.F{K: "error", V: err.Error()})
		return
	}
	for _, a := range got {
		agent.Log("info", "apps: rights given by the organization's rule", agent.F{K: "id", V: a.ID}, agent.F{K: "version", V: a.Version})
	}
}
