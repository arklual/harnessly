package providers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Login is a sign-in of a vendor's own CLI on this machine that a profile can run on instead of a saved key: a
// subscription the person already pays for. The runtime reads the CLI's credential and never writes it; the CLI
// renews it.
type Login interface {
	// Account is whom the sign-in is of (an email); ok is false while there is no usable sign-in.
	Account() (account string, ok bool)
	// Token is a current access token, renewed through the CLI when it is about to run out.
	Token(ctx context.Context) (string, error)
}

// ErrNoLogin: the CLI is not signed in, or its sign-in can no longer be renewed.
var ErrNoLogin = errors.New("providers: the vendor's CLI is not signed in")

// GrokLogin is the Grok CLI's sign-in: the `auth.json` in its home (~/.grok), an access token of a few hours and the
// refresh token the CLI renews it with. The runtime does not use the refresh token itself: refresh tokens rotate, and
// two renewers of one sign-in would log each other out. When the token is about to run out it runs the CLI once
// (`grok models`, which renews before it asks) and reads the file again.
type GrokLogin struct {
	// Path is the CLI's auth.json.
	Path string
	// Renew runs the CLI so that it renews the token; nil runs `grok models` (Bin, else `grok` on PATH, else
	// ~/.grok/bin/grok next to Path).
	Renew func(ctx context.Context) error
	// Bin is the CLI; empty finds it.
	Bin string
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu sync.Mutex
}

// RenewAhead is how long before its expiry a token is renewed.
const RenewAhead = 10 * time.Minute

// renewTimeout bounds one run of the CLI.
const renewTimeout = 90 * time.Second

// GrokHome is the Grok CLI's home for a user's home: $GROK_HOME, else ~/.grok.
func GrokHome(home string) string {
	if dir := os.Getenv("GROK_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".grok")
}

type grokEntry struct {
	issuer       string
	Key          string    `json:"key"`
	Email        string    `json:"email"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshToken string    `json:"refresh_token"`
}

func (g *GrokLogin) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// entry is the sign-in of the file: the one of xAI's own issuer, else the one that runs out last.
func (g *GrokLogin) entry() (grokEntry, bool) {
	raw, err := os.ReadFile(g.Path)
	if err != nil {
		return grokEntry{}, false
	}
	var all map[string]json.RawMessage
	if json.Unmarshal(raw, &all) != nil {
		return grokEntry{}, false
	}
	var found []grokEntry
	for k, v := range all {
		var e grokEntry
		if json.Unmarshal(v, &e) != nil || e.Key == "" {
			continue
		}
		e.issuer, _, _ = strings.Cut(k, "::")
		found = append(found, e)
	}
	if len(found) == 0 {
		return grokEntry{}, false
	}
	sort.Slice(found, func(i, j int) bool {
		oi, oj := found[i].issuer == "https://auth.x.ai", found[j].issuer == "https://auth.x.ai"
		if oi != oj {
			return oi
		}
		return found[i].ExpiresAt.After(found[j].ExpiresAt)
	})
	return found[0], true
}

// usable says whether the sign-in gives a token now or can be renewed to give one.
func (g *GrokLogin) usable(e grokEntry) bool {
	return e.RefreshToken != "" || e.ExpiresAt.IsZero() || e.ExpiresAt.After(g.now())
}

func (g *GrokLogin) Account() (string, bool) {
	e, ok := g.entry()
	if !ok || !g.usable(e) {
		return "", false
	}
	return e.Email, true
}

func (g *GrokLogin) fresh(e grokEntry) bool {
	return e.ExpiresAt.IsZero() || e.ExpiresAt.After(g.now().Add(RenewAhead))
}

func (g *GrokLogin) Token(ctx context.Context) (string, error) {
	e, ok := g.entry()
	if !ok {
		return "", ErrNoLogin
	}
	if g.fresh(e) {
		return e.Key, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// Another request may have renewed it while this one waited.
	if e, ok = g.entry(); ok && g.fresh(e) {
		return e.Key, nil
	}
	if !ok || e.RefreshToken == "" {
		return g.lastResort(e, ok)
	}
	ctx, cancel := context.WithTimeout(ctx, renewTimeout)
	defer cancel()
	renewErr := g.renew(ctx)
	if e2, ok2 := g.entry(); ok2 && e2.ExpiresAt.After(g.now()) {
		return e2.Key, nil
	}
	if renewErr != nil {
		return "", errors.Join(ErrNoLogin, renewErr)
	}
	return g.lastResort(e, ok)
}

// lastResort is a token that has not run out yet, though it is due for renewal.
func (g *GrokLogin) lastResort(e grokEntry, ok bool) (string, error) {
	if ok && e.ExpiresAt.After(g.now()) {
		return e.Key, nil
	}
	return "", ErrNoLogin
}

func (g *GrokLogin) renew(ctx context.Context) error {
	if g.Renew != nil {
		return g.Renew(ctx)
	}
	bin := g.Bin
	if bin == "" {
		if p, err := exec.LookPath("grok"); err == nil {
			bin = p
		} else {
			bin = filepath.Join(filepath.Dir(g.Path), "bin", "grok")
		}
	}
	cmd := exec.CommandContext(ctx, bin, "models")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd.Run()
}
