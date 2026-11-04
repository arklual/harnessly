package main

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	browsertools "github.com/ly-solutions/harnessly/internal/agentbrowser/tools"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

func browserSecretRig(t *testing.T) (*safeAccessRig, *browsertools.Service) {
	t.Helper()
	rig := newSafeAccessRig(t)
	safesMu.Lock()
	safesFor[rig.d.CLI] = rig.sf
	safesMu.Unlock()
	t.Cleanup(func() { safesMu.Lock(); delete(safesFor, rig.d.CLI); safesMu.Unlock() })
	svc := &browsertools.Service{}
	rig.d.BrowserTools = svc
	configureBrowserSecrets(rig.d, &core{})
	if svc.Secrets != rig.sf.store || svc.RequestSecret == nil || svc.Identity == nil {
		t.Fatal("browser callbacks were not connected to the runtime's shared safe")
	}
	if _, err := svc.Identity("unknown-chat"); err == nil {
		t.Fatal("browser identity accepted an unknown conversation")
	}
	// The callbacks can be exercised without a manager, CDP, or a browser process.
	svc.Identity = func(id string) (secrets.Use, error) {
		if id != "chat-a" {
			return secrets.Use{}, errors.New("unknown chat")
		}
		return secrets.Use{Conv: id, Project: "project-a", Title: "Verified a"}, nil
	}
	return rig, svc
}

func TestBrowserSecretsRequestReusesEquivalentCLICard(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-value", true: "foreign-grant"}[foreign], func(t *testing.T) {
			rig, svc := browserSecretRig(t)
			if foreign {
				rig.put("service-key", []string{"https://other.example.com"}, []string{"project-b"})
			}
			w := rig.call("POST", "/api/cli/secrets/ask", map[string]any{"name": "service-key", "session": "session-a", "origin": "HTTPS://EXAMPLE.COM:443/", "why": "sign in", "replace": foreign, "wait": 0}, "a", nil)
			if w.Code != 200 {
				t.Fatalf("CLI ask status: %d", w.Code)
			}
			before := rig.sf.asks.Pending()
			if len(before) != 1 {
				t.Fatalf("CLI opened %d cards", len(before))
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			state, err := svc.RequestSecret(ctx, "chat-a", "service-key", "https://example.com", "sign in")
			if err != nil || state != secrets.AskWaiting {
				t.Fatalf("browser retry: %s, %v", state, err)
			}
			after := rig.sf.asks.Pending()
			if len(after) != 1 || after[0].ID != before[0].ID || after[0].Origin != "https://example.com" || after[0].Grant != foreign || after[0].Replace {
				t.Fatalf("equivalent browser/CLI requests did not share a safe card: %+v", after)
			}
			for _, origin := range []string{"https://example.com:8443", "https://sub.example.com", "http://localhost:3000"} {
				state, err := svc.RequestSecret(ctx, "chat-a", "service-key", origin, "sign in")
				if err != nil || state != secrets.AskWaiting {
					t.Errorf("distinct origin request: %s, %v", state, err)
				}
			}
			pending := rig.sf.asks.Pending()
			if len(pending) != 4 {
				t.Fatalf("distinct origins collapsed into %d approval cards", len(pending))
			}
			var origins []string
			for _, a := range pending {
				origins = append(origins, a.Origin)
			}
			slices.Sort(origins)
			want := []string{"http://localhost:3000", "https://example.com", "https://example.com:8443", "https://sub.example.com"}
			if !slices.Equal(origins, want) {
				t.Errorf("approval origins = %v, want %v", origins, want)
			}
		})
	}
}

func TestBrowserSecretsRequestReadyOnlyForAllowedOrigin(t *testing.T) {
	rig, svc := browserSecretRig(t)
	rig.put("service-key", []string{"https://example.com"}, []string{"project-a"})
	state, err := svc.RequestSecret(context.Background(), "chat-a", "service-key", "HTTPS://EXAMPLE.COM:443/", "sign in")
	if err != nil || state != secrets.AskReady || len(rig.sf.asks.Pending()) != 0 {
		t.Fatalf("authorized origin: %s, %v", state, err)
	}
	for _, origin := range []string{"http://example.com", "https://example.com/path", "https://user@example.com", ""} {
		if _, err := svc.RequestSecret(context.Background(), "chat-a", "service-key", origin, "sign in"); err == nil {
			t.Errorf("invalid origin %q accepted", origin)
		}
	}
	if len(rig.sf.asks.Pending()) != 0 {
		t.Error("invalid origins opened approval cards")
	}
	views, err := rig.sf.store.List()
	if err != nil || len(views) != 1 || views[0].LastUsedAt != 0 || len(views[0].UsedBy) != 0 {
		t.Errorf("request revealed a value or recorded use: %v", err)
	}
}

func TestBrowserSecretsWaitingRequestRejectsMovedProject(t *testing.T) {
	for _, change := range []string{"unchanged", "moved", "deleted"} {
		t.Run(change, func(t *testing.T) {
			rig, svc := browserSecretRig(t)
			var changed atomic.Bool
			svc.Identity = func(id string) (secrets.Use, error) {
				if changed.Load() && change == "deleted" {
					return secrets.Use{}, errors.New("chat removed")
				}
				project := "project-a"
				if changed.Load() && change == "moved" {
					project = "project-b"
				}
				return secrets.Use{Conv: id, Project: project, Title: "Verified a"}, nil
			}
			type answer struct {
				state string
				err   error
			}
			done := make(chan answer, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			go func() {
				state, err := svc.RequestSecret(ctx, "chat-a", "service-key", "https://example.com", "sign in")
				done <- answer{state, err}
			}()
			var pending []secrets.Ask
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				pending = rig.sf.asks.Pending()
				if len(pending) == 1 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if len(pending) != 1 {
				t.Fatal("browser request did not open a card")
			}
			rig.put("service-key", []string{"https://example.com"}, []string{"project-a"})
			changed.Store(true)
			rig.sf.asks.Ready(pending[0].ID)
			select {
			case got := <-done:
				want := secrets.AskDeclined
				if change == "unchanged" {
					want = secrets.AskReady
				}
				if got.err != nil || got.state != want {
					t.Errorf("browser wait after %s = %q, %v; want %q", change, got.state, got.err, want)
				}
			case <-ctx.Done():
				t.Fatal("browser request did not settle after the card was answered")
			}
		})
	}
}
