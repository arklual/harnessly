package main

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/services"
)

const (
	// mrParallel is how many requests to a GitLab run at once for the opens of every conversation together.
	mrParallel = 10
	// mrFoundTTL: a merge request that is there stays there; mrMissTTL: one that is not may be made a minute later.
	mrFoundTTL = 30 * time.Minute
	mrMissTTL  = 2 * time.Minute
	// mrKept bounds the answers remembered.
	mrKept = 2048
)

type mrAnswer struct {
	done chan struct{}
	info mrInfo
	ok   bool
	at   time.Time
}

// mrChecker asks the GitLab, with the token the merge request tab reads with (services), whether a merge request is
// there. It remembers the answers, shares one request between the askers of the same merge request and keeps the
// requests in flight to mrParallel.
type mrChecker struct {
	svc   *services.Service
	slots chan struct{}
	mu    sync.Mutex
	known map[string]*mrAnswer
	named map[string]namedProjects
}

// namedProjects are the projects a search for a name found.
type namedProjects struct {
	paths []string
	at    time.Time
}

func newMRChecker(svc *services.Service) *mrChecker {
	return &mrChecker{svc: svc, slots: make(chan struct{}, mrParallel), known: map[string]*mrAnswer{}, named: map[string]namedProjects{}}
}

func (c *mrChecker) lookup(ctx context.Context, host, project string, iid int) (mrInfo, bool) {
	key := host + "/" + project + "!" + strconv.Itoa(iid)
	c.mu.Lock()
	a := c.known[key]
	if a != nil {
		select {
		case <-a.done:
			ttl := mrMissTTL
			if a.ok {
				ttl = mrFoundTTL
			}
			if time.Since(a.at) > ttl {
				a = nil
			}
		default:
		}
	}
	if a == nil {
		if len(c.known) >= mrKept {
			c.known = map[string]*mrAnswer{}
		}
		a = &mrAnswer{done: make(chan struct{})}
		c.known[key] = a
		go c.ask(a, host, project, iid)
	}
	c.mu.Unlock()
	select {
	case <-a.done:
		return a.info, a.ok
	case <-ctx.Done():
		return mrInfo{}, false
	}
}

// ask runs apart from the asker, so one that gives up does not leave the others waiting on an answer that never comes.
func (c *mrChecker) ask(a *mrAnswer, host, project string, iid int) {
	c.slots <- struct{}{}
	defer func() { <-c.slots }()
	info, ok := c.svc.MergeRequest(context.Background(), host, project, iid)
	a.info, a.ok, a.at = mrInfo{url: info.URL, title: info.Title, updatedAt: info.UpdatedAt}, ok, time.Now()
	close(a.done)
}

// projects are the projects of a GitLab called name (its search, asked once in mrFoundTTL, or in mrMissTTL when it found
// none); the search shares the requests in flight with the merge request checks.
func (c *mrChecker) projects(ctx context.Context, host, name string) []string {
	key := host + "/" + name
	c.mu.Lock()
	have, ok := c.named[key]
	c.mu.Unlock()
	ttl := mrMissTTL
	if len(have.paths) > 0 {
		ttl = mrFoundTTL
	}
	if ok && time.Since(have.at) < ttl {
		return have.paths
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil
	}
	paths := c.svc.ProjectPaths(ctx, host, name)
	if ctx.Err() != nil {
		return nil
	}
	c.mu.Lock()
	if len(c.named) >= mrKept {
		c.named = map[string]namedProjects{}
	}
	c.named[key] = namedProjects{paths: paths, at: time.Now()}
	c.mu.Unlock()
	return paths
}
