package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CodexLogin is the Codex CLI's sign-in by a ChatGPT subscription: the `auth.json` in its home (~/.codex), an access
// token of about ten days, the account it is of and the refresh token the CLI renews it with. As with Grok, the runtime
// never uses the refresh token itself (it rotates, and two renewers would log each other out): Codex renews the token
// in the last minutes before it runs out, on the first request it makes then, so when the token is that close the
// runtime runs the CLI once (`codex debug models`, which asks the backend for the models and so renews first) and reads
// the file again. A sign-in by an API key is no subscription and does not count.
type CodexLogin struct {
	// Path is the CLI's auth.json.
	Path string
	// Renew runs the CLI so that it renews the token; nil runs `codex debug models` (Bin, else `codex` on PATH).
	Renew func(ctx context.Context) error
	// Bin is the CLI; empty finds it.
	Bin string
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu sync.Mutex
}

// CodexRenewAhead is how long before its expiry the Codex token is renewed: inside the five minutes in which the CLI
// itself renews it, else the CLI would find it fresh and leave it.
const CodexRenewAhead = 4 * time.Minute

// CodexHome is the Codex CLI's home for a user's home: $CODEX_HOME, else ~/.codex.
func CodexHome(home string) string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".codex")
}

type codexAuthFile struct {
	AuthMode string  `json:"auth_mode"`
	APIKey   *string `json:"OPENAI_API_KEY"`
	Tokens   *struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}

// codexEntry is what the runtime reads of the sign-in.
type codexEntry struct {
	token, refresh, account, email string
	expires                        time.Time
}

// codexClaims are the claims of the tokens the runtime reads: whom they are of and when the access token runs out. The
// tokens are not verified here; the backend does that, and the runtime only reads its own user's file.
type codexClaims struct {
	Exp     int64  `json:"exp"`
	Email   string `json:"email"`
	Profile struct {
		Email string `json:"email"`
	} `json:"https://api.openai.com/profile"`
	Auth struct {
		AccountID string `json:"chatgpt_account_id"`
	} `json:"https://api.openai.com/auth"`
}

func jwtClaims(token string) (codexClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return codexClaims{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return codexClaims{}, false
	}
	var c codexClaims
	if json.Unmarshal(raw, &c) != nil {
		return codexClaims{}, false
	}
	return c, true
}

func (c *CodexLogin) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// entry is the subscription sign-in of the file; false for none, or for a sign-in by an API key.
func (c *CodexLogin) entry() (codexEntry, bool) {
	raw, err := os.ReadFile(c.Path)
	if err != nil {
		return codexEntry{}, false
	}
	var f codexAuthFile
	if json.Unmarshal(raw, &f) != nil || f.Tokens == nil || f.Tokens.AccessToken == "" {
		return codexEntry{}, false
	}
	// Older files have no mode: tokens without an API key are a ChatGPT sign-in.
	if mode := strings.ToLower(f.AuthMode); mode != "" && !strings.HasPrefix(mode, "chatgpt") {
		return codexEntry{}, false
	}
	if f.AuthMode == "" && f.APIKey != nil && *f.APIKey != "" {
		return codexEntry{}, false
	}
	e := codexEntry{token: f.Tokens.AccessToken, refresh: f.Tokens.RefreshToken, account: f.Tokens.AccountID}
	access, _ := jwtClaims(f.Tokens.AccessToken)
	if access.Exp > 0 {
		e.expires = time.Unix(access.Exp, 0)
	}
	if e.account == "" {
		e.account = access.Auth.AccountID
	}
	if id, ok := jwtClaims(f.Tokens.IDToken); ok {
		e.email = id.Email
		if e.account == "" {
			e.account = id.Auth.AccountID
		}
	}
	if e.email == "" {
		e.email = access.Profile.Email
	}
	return e, true
}

func (c *CodexLogin) usable(e codexEntry) bool {
	return e.refresh != "" || e.expires.IsZero() || e.expires.After(c.now())
}

func (c *CodexLogin) fresh(e codexEntry) bool {
	return e.expires.IsZero() || e.expires.After(c.now().Add(CodexRenewAhead))
}

func (c *CodexLogin) Account() (string, bool) {
	e, ok := c.entry()
	if !ok || !c.usable(e) {
		return "", false
	}
	return e.email, true
}

// AccountID is the ChatGPT account the sign-in is of: the backend wants it beside the token.
func (c *CodexLogin) AccountID() string {
	e, _ := c.entry()
	return e.account
}

func (c *CodexLogin) Token(ctx context.Context) (string, error) {
	e, ok := c.entry()
	if !ok {
		return "", ErrNoLogin
	}
	if c.fresh(e) {
		return e.token, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Another request may have renewed it while this one waited.
	if e, ok = c.entry(); ok && c.fresh(e) {
		return e.token, nil
	}
	if !ok || e.refresh == "" {
		return c.lastResort(e, ok)
	}
	ctx, cancel := context.WithTimeout(ctx, renewTimeout)
	defer cancel()
	renewErr := c.renew(ctx)
	if e2, ok2 := c.entry(); ok2 && e2.expires.After(e.expires) {
		return e2.token, nil
	}
	if renewErr != nil && !e.expires.After(c.now()) {
		return "", errors.Join(ErrNoLogin, renewErr)
	}
	return c.lastResort(e, ok)
}

// lastResort is a token that has not run out yet, though it is due for renewal.
func (c *CodexLogin) lastResort(e codexEntry, ok bool) (string, error) {
	if ok && e.expires.After(c.now()) {
		return e.token, nil
	}
	return "", ErrNoLogin
}

func (c *CodexLogin) renew(ctx context.Context) error {
	if c.Renew != nil {
		return c.Renew(ctx)
	}
	bin := c.Bin
	if bin == "" {
		p, err := exec.LookPath("codex")
		if err != nil {
			return err
		}
		bin = p
	}
	cmd := exec.CommandContext(ctx, bin, "debug", "models")
	// The home the file is in, so a CODEX_HOME of the runtime's own does not send the CLI elsewhere.
	cmd.Env = append(os.Environ(), "CODEX_HOME="+filepath.Dir(c.Path))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd.Run()
}
