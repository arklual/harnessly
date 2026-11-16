package limits

import (
	"context"
	"errors"
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
	// ClaudeDirs are the folders of the machine's Claude sign-in, in order; none: no Claude usage poller.
	ClaudeDirs []string
	ClaudeURL  string
	Clock      func() float64
	RunCLI     RunCodexbar
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
	if len(p.ClaudeDirs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.runClaudeUsage(ctx)
		}()
	}
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

// runClaudeUsage asks Anthropic every ClaudeUsageInterval while a Claude chat runs (one goroutine: the interval is its
// own clock, the entry's ts moves with every statusline); a refusal leaves the account alone
// for as long as it asks (an expired sign-in until Claude Code renews it), doubling up to an hour while it repeats.
func (p *Pollers) runClaudeUsage(ctx context.Context) {
	var wait time.Duration
	for {
		next := ClaudeUsageInterval
		if d, err := p.pollClaudeUsage(ctx); err != nil {
			var refused *errClaudeUsage
			if errors.As(err, &refused) && refused.wait > 0 {
				wait = min(max(refused.wait, wait*2), claudeMaxBackoff)
				next = wait
			}
			p.warn("failed to read claude usage", err)
		} else if d {
			wait = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(next):
		}
	}
}

// pollClaudeUsage asks once when it is worth it; true when an answer was written.
func (p *Pollers) pollClaudeUsage(ctx context.Context) (bool, error) {
	now := p.clock()
	entry := p.Cache.Read()["claude"]
	if !claudeActive(entry, now) {
		return false, nil
	}
	login := ReadClaudeLogin(p.ClaudeDirs...)
	if login == nil || (login.ExpiresAt > 0 && login.ExpiresAt/1000 <= now) {
		return false, nil
	}
	url := p.ClaudeURL
	if url == "" {
		url = ClaudeUsageURL
	}
	data, err := FetchClaudeUsage(ctx, *login, p.Client, url, now)
	if err != nil {
		return false, err
	}
	_, err = p.Cache.Write("claude", data)
	return err == nil, err
}
