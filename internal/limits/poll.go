package limits

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Pollers ask the usage endpoints on their own schedule. Each one is single-flight: a slow run delays the next instead
// of piling up.
type Pollers struct {
	Cache       *Cache
	CodexbarBin string // absolute path, or empty for no codexbar poller
	CodexHome   string
	Client      *http.Client
	URL         string // the Codex usage endpoint
	Clock       func() float64
	RunCLI      RunCodexbar
	// Warn gets the failures that are only worth a log line.
	Warn func(msg string, err error)
}

func (p *Pollers) clock() float64 {
	if p.Clock != nil {
		return p.Clock()
	}
	return float64(time.Now().UnixMicro()) / 1e6
}

func (p *Pollers) warn(msg string, err error) {
	if p.Warn != nil && err != nil {
		p.Warn(msg, err)
	}
}

func every(ctx context.Context, interval time.Duration, tick func()) {
	for {
		tick()
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// Run polls until ctx ends: codexbar every minute when configured, Codex's usage every two minutes while the machine
// has a ChatGPT login of Codex (re-read every time, Codex refreshes it).
func (p *Pollers) Run(ctx context.Context) {
	var wg sync.WaitGroup
	if p.CodexbarBin != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			every(ctx, CodexbarInterval, func() {
				_, err := PollCodexbarOnce(ctx, p.CodexbarBin, p.RunCLI, p.Cache, p.clock)
				p.warn("failed to write codexbar usage", err)
			})
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		every(ctx, CodexUsageInterval, func() { p.pollCodexUsage(ctx) })
	}()
	wg.Wait()
}

func (p *Pollers) pollCodexUsage(ctx context.Context) {
	login := ReadCodexLogin(p.CodexHome)
	if login == nil {
		return
	}
	// A value this endpoint gave less than half an interval ago (from another writer of the file) is fresh enough.
	if cached := p.Cache.Read()["codex"]; cached != nil && cached["source"] == SourceCodexUsage &&
		p.clock()-num(cached["ts"]) < CodexUsageInterval.Seconds()/2 {
		return
	}
	url := p.URL
	if url == "" {
		url = CodexUsageURL
	}
	_, err := PollCodexUsageOnce(ctx, *login, p.Client, url, p.Cache, p.clock)
	p.warn("failed to write codex usage", err)
}
