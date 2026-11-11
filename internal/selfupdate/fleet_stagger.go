package selfupdate

// One service at a time. A machine that runs several runtimes from one release (one per person) must not start them
// together: a runtime's start is the heaviest minute of its life, and four of them at once take the machine down with
// the chats of everyone on it. With Services each one is restarted on its own, the next only after the one before
// serves the release and its chats are back; the first that fails stops the rollout, and only the services that were
// restarted go back, the rest never left the previous release.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	// ChatsStarting is a runtime whose conversations are not loaded yet: it listens and serves nothing.
	ChatsStarting = "starting"
	// ChatsReady is a runtime that finished the work of its start.
	ChatsReady = "ready"
	// DefaultSettleTimeout is how long the next service waits for the chats of the one before it.
	DefaultSettleTimeout = 5 * time.Minute
)

// ErrTooSoon: the last activation is closer than the machine's window allows; nothing was changed.
var ErrTooSoon = errors.New("the machine took a release too recently")

// Health is what a service answers about itself.
type Health struct {
	Version string
	// Chats is starting, restoring or ready; empty from a service that does not say, which has nothing to wait for.
	Chats string
}

// Service is one of the services that run from current.
type Service struct {
	Name string
	// Probe answers what the service serves now, or an error while it does not answer.
	Probe func(ctx context.Context) (Health, error)
}

func (f *Fleet) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// until polls ok until it holds or limit passes; it reports whether it held and the last reason it did not.
func (f *Fleet) until(ctx context.Context, limit time.Duration, ok func() (bool, string)) (bool, string, error) {
	every := f.PollEvery
	if every <= 0 {
		every = 2 * time.Second
	}
	deadline := time.Now().Add(limit)
	for {
		held, why := ok()
		if held {
			return true, "", nil
		}
		if time.Now().After(deadline) {
			return false, why, nil
		}
		select {
		case <-ctx.Done():
			return false, why, ctx.Err()
		case <-time.After(every):
		}
	}
}

// serving waits until the service answers with the expected version and its conversations loaded. It returns the
// state of its chats at that moment.
func (f *Fleet) serving(ctx context.Context, s Service, expected string) (string, error) {
	limit := f.HealthTimeout
	if limit <= 0 {
		limit = DefaultHealthTimeout
	}
	var chats string
	held, why, err := f.until(ctx, limit, func() (bool, string) {
		h, err := s.Probe(ctx)
		switch {
		case err != nil:
			return false, err.Error()
		case expected != "" && h.Version != expected:
			return false, "serves " + h.Version
		case h.Chats == ChatsStarting:
			return false, "its chats are not loaded"
		}
		chats = h.Chats
		return true, ""
	})
	if err != nil {
		return "", err
	}
	if !held {
		return "", fmt.Errorf("did not pass the check in %s: %s (want %s)", limit, why, expected)
	}
	return chats, nil
}

// settled waits, after serving, for the service to finish the work of its start, so the next one does not start on
// top of it. A service that never finishes is not a failed release: the wait is bounded and the rollout goes on.
func (f *Fleet) settled(ctx context.Context, s Service, chats string) error {
	if chats == "" || chats == ChatsReady {
		return nil
	}
	limit := f.SettleTimeout
	if limit <= 0 {
		limit = DefaultSettleTimeout
	}
	held, _, err := f.until(ctx, limit, func() (bool, string) {
		h, err := s.Probe(ctx)
		return err == nil && (h.Chats == "" || h.Chats == ChatsReady), ""
	})
	if err != nil {
		return err
	}
	if !held {
		f.say("%s still restores its chats after %s; going on", s.Name, limit)
	}
	return nil
}

func (f *Fleet) staggered() bool { return len(f.Services) > 0 }

// check waits for what runs now to serve expected, restarting nothing.
func (f *Fleet) check(ctx context.Context, expected string) error {
	if !f.staggered() {
		return f.wait(ctx, expected)
	}
	for _, s := range f.Services {
		if _, err := f.serving(ctx, s, expected); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
	}
	return nil
}

// cycle restarts the services and waits until they serve expected. Without Services that is one restart of everything
// and one check. With them it is one service at a time, in their order, only those named when only is not nil. It
// returns the services it restarted, the failed one included. Strict stops at the first failure; otherwise every
// service is tried and the first failure is the answer.
func (f *Fleet) cycle(ctx context.Context, expected string, only []string, strict bool) ([]string, error) {
	if !f.staggered() {
		if err := f.restart(ctx); err != nil {
			return nil, err
		}
		return nil, f.wait(ctx, expected)
	}
	restarted := []string{}
	var first error
	for _, s := range f.Services {
		if only != nil && !slices.Contains(only, s.Name) {
			continue
		}
		restarted = append(restarted, s.Name)
		err := error(nil)
		if f.RestartOne != nil {
			err = f.RestartOne(ctx, s.Name)
		}
		var chats string
		if err == nil {
			chats, err = f.serving(ctx, s, expected)
		}
		if err == nil {
			f.say("%s serves %s", s.Name, expected)
			err = f.settled(ctx, s, chats)
		}
		if err == nil {
			continue
		}
		err = fmt.Errorf("%s: %w", s.Name, err)
		if strict {
			return restarted, err
		}
		f.say("%v", err)
		if first == nil {
			first = err
		}
	}
	return restarted, first
}

// tooSoon is the time left until the machine's window lets the next release in; zero when it may come now.
func (f *Fleet) tooSoon() time.Duration {
	if f.MinGap <= 0 || f.Force {
		return 0
	}
	raw, err := os.ReadFile(filepath.Join(f.Root, "deployed.json"))
	if err != nil {
		return 0
	}
	var rec struct {
		ActivatedAt string `json:"activatedAt"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return 0
	}
	at, err := time.Parse(time.RFC3339, rec.ActivatedAt)
	if err != nil {
		return 0
	}
	if left := f.MinGap - f.now().Sub(at); left > 0 {
		return left
	}
	return 0
}
