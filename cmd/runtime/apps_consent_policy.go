package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The organization's rule for apps «Не спрашивать разрешений». The hub sends it with the environment list
// (env.set, hubproto.EnvSet.AppsConsent); the agent keeps it in environment.json of its link folder, beside the list, and
// this runtime reads it from there. The rule is on when one link of the machine says auto: then an install, an update or
// a dev edit that would wait for the person gets its rights at once (apps.ConsentPolicy), and what waited when the rule
// came on is granted. A machine without a hub has no such file and asks as before. Turned off, the rule takes nothing
// back. Nothing on the machine writes the rule but the agent from the hub's word: the runtime has no route for it.
//
// The same list carries the organization's switch of an app item (source.off): an environment app whose item is off is
// not listed on the machine (no menu item, no tools, no views) and stays installed, so turning it on again is instant.

// appsPolicyEvery is how often the rule and the dev folders are looked at while the runtime runs.
const appsPolicyEvery = 3 * time.Second

// appsConsentPolicy reads the rule from the link folders, each file read again only when it changed.
type appsConsentPolicy struct {
	files []string
	// links are the link folders, in the order of files.
	links []string

	mu   sync.Mutex
	seen map[string]policySeen
}

type policySeen struct {
	mod  time.Time
	size int64
	auto bool
	// off are the app items the organization turned off.
	off []string
}

func newAppsConsentPolicy(linkDirs []string) *appsConsentPolicy {
	p := &appsConsentPolicy{seen: map[string]policySeen{}}
	for _, d := range linkDirs {
		p.files = append(p.files, filepath.Join(d, "environment.json"))
		p.links = append(p.links, d)
	}
	return p
}

// On says whether one link's list carries the rule auto. A file that is missing, unreadable or broken says ask.
func (p *appsConsentPolicy) On() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	on := false
	for _, f := range p.files {
		if s, ok := p.readLocked(f); ok {
			on = on || s.auto
		}
	}
	return on
}

// AppOff says whether the organization turned off the app item of the link's list (apps.Registry.SetOrgOff).
func (p *appsConsentPolicy) AppOff(link, itemID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, f := range p.files {
		if p.links[i] != link {
			continue
		}
		if s, ok := p.readLocked(f); ok && slices.Contains(s.off, itemID) {
			return true
		}
	}
	return false
}

// readLocked is what one list file says, read again only when it changed; false for a file that is not there.
func (p *appsConsentPolicy) readLocked(f string) (policySeen, bool) {
	fi, err := os.Stat(f)
	if err != nil {
		delete(p.seen, f)
		return policySeen{}, false
	}
	if s, ok := p.seen[f]; ok && s.mod.Equal(fi.ModTime()) && s.size == fi.Size() {
		return s, true
	}
	consent, off := readEnvList(f)
	s := policySeen{mod: fi.ModTime(), size: fi.Size(), auto: consent == hubproto.AppsConsentAuto, off: off}
	p.seen[f] = s
	return s, true
}

// readEnvList is the rule for apps of one list file and its app items the organization turned off: the list the agent
// wrote, whose hash holds (envkeeper.go). A file that is missing, unreadable, broken or whose hash does not hold says
// ask and nothing off.
func readEnvList(path string) (string, []string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	var h struct {
		Hash        string             `json:"hash"`
		AppsConsent string             `json:"appsConsent"`
		Items       []hubproto.EnvItem `json:"items"`
	}
	if json.Unmarshal(raw, &h) != nil || h.Hash == "" || hubproto.EnvHash(h.Items) != h.Hash {
		return "", nil
	}
	var off []string
	for _, it := range h.Items {
		var src struct {
			Off bool `json:"off"`
		}
		if it.Kind == hubproto.EnvKindApp && json.Unmarshal(it.Source, &src) == nil && src.Off {
			off = append(off, it.ItemID)
		}
	}
	return h.AppsConsent, off
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
