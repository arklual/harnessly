package limits

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// The limits of the vendors' profiles: provider-limits.json, one entry per account of a profile, asked when someone
// looks (Touch) and no oftener than MinInterval per account. Nothing runs on a timer: a machine nobody watches asks
// nothing. An account whose vendor has no request, or whose request failed before it ever answered, has no windows:
// the page says the limits are unknown, never zero.

const (
	// ProfileMinInterval is the shortest time between two requests for one account.
	ProfileMinInterval = 5 * time.Minute
	// ProfileFailedInterval is how long a failed request is left alone.
	ProfileFailedInterval = 15 * time.Minute
	profileTimeout        = 15 * time.Second
	profileMaxBody        = 1 << 20
)

// ProfileKey is the key of an account's entry: the profile, and the account when the profile has more than one.
func ProfileKey(profile, account string) string {
	if account == "" {
		return profile
	}
	return profile + "/" + account
}

// Account is one account of a profile whose limits can be asked.
type Account struct {
	Profile string
	// Account names the account among the profile's (an email of a sign-in, the name of a saved key); empty for the
	// only one.
	Account string
	// Credential is read when the request is made and never kept.
	Credential func(ctx context.Context) (Credential, error)
}

// Key is the key of the account's entry.
func (a Account) Key() string { return ProfileKey(a.Profile, a.Account) }

// ProfileCache is provider-limits.json: {"ts": …, "entries": {<key>: entry}}. The file is replaced, never rewritten in
// place.
type ProfileCache struct {
	File string
	mu   sync.Mutex
}

// Read is the stored entries; a missing or broken file is empty.
func (c *ProfileCache) Read() map[string]Data {
	out := map[string]Data{}
	raw, err := os.ReadFile(c.File)
	if err != nil {
		return out
	}
	var parsed map[string]any
	if json.Unmarshal(raw, &parsed) != nil {
		return out
	}
	entries, _ := object(parsed["entries"])
	for k, v := range entries {
		if m, ok := object(v); ok {
			out[k] = m
		}
	}
	return out
}

// update rewrites the entries under the lock.
func (c *ProfileCache) update(now float64, change func(entries map[string]Data)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries := c.Read()
	change(entries)
	if err := os.MkdirAll(filepath.Dir(c.File), 0o755); err != nil {
		return err
	}
	var id [8]byte
	_, _ = rand.Read(id[:])
	tmp := c.File + "." + strconv.Itoa(os.Getpid()) + "." + hex.EncodeToString(id[:]) + ".tmp"
	if err := os.WriteFile(tmp, nodejson.Marshal(map[string]any{"ts": now, "entries": entries}), 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, c.File); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Put stores an answer as the account's entry, whole: the vendor listed every window it has.
func (c *ProfileCache) Put(a Account, data Data, now float64) error {
	return c.update(now, func(entries map[string]Data) {
		next := copyObject(data)
		next["profile"], next["account"] = a.Profile, a.Account
		entries[a.Key()] = next
	})
}

// Fail notes a failed request on the account's entry and keeps what it last answered; with forget, nothing of it.
func (c *ProfileCache) Fail(a Account, message string, forget bool, now float64) error {
	return c.update(now, func(entries map[string]Data) {
		next := Data{"profile": a.Profile, "account": a.Account}
		if prev := entries[a.Key()]; prev != nil && !forget {
			next = copyObject(prev)
		}
		failure := map[string]any{"message": message, "at": now}
		if message == keyRefused() {
			failure["rejected"] = true
		}
		next["error"] = failure
		entries[a.Key()] = next
	})
}

// Keep drops the entries of accounts that are no more (a removed key, a signed-out CLI).
func (c *ProfileCache) Keep(keys map[string]bool, now float64) error {
	stale := false
	for k := range c.Read() {
		stale = stale || !keys[k]
	}
	if !stale {
		return nil
	}
	return c.update(now, func(entries map[string]Data) {
		for k := range entries {
			if !keys[k] {
				delete(entries, k)
			}
		}
	})
}

// askedAt is when the entry was last asked: the later of its answer and its failure.
func askedAt(d Data) (at float64, failed bool) {
	at = orZero(d["ts"])
	if e, ok := object(d["error"]); ok {
		if f := orZero(e["at"]); f >= at {
			return f, true
		}
	}
	return at, false
}

// Profiles asks the vendors.
type Profiles struct {
	Cache *ProfileCache
	// Accounts lists the accounts that have a credential now.
	Accounts func() []Account
	// Client sends the requests; nil is one that follows no redirect (a redirect would carry the credential to the host
	// it points at).
	Client *http.Client
	// URL overrides a vendor's address (tests).
	URL   func(profile string, v Vendor) string
	Clock func() float64
	Warn  func(msg string, err error)

	mu      sync.Mutex
	running bool
}

var profileClient = &http.Client{
	Timeout:       profileTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (p *Profiles) clock() float64 {
	if p.Clock != nil {
		return p.Clock()
	}
	return float64(time.Now().UnixMicro()) / 1e6
}

// Touch says someone looks at the limits: the accounts that were not asked lately are asked, in the background and one
// at a time. A pass that still runs is not doubled.
func (p *Profiles) Touch() {
	if p == nil || p.Accounts == nil {
		return
	}
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()
	go func() {
		defer func() {
			p.mu.Lock()
			p.running = false
			p.mu.Unlock()
		}()
		p.Refresh(context.Background())
	}()
}

// Refresh is one pass over the accounts.
func (p *Profiles) Refresh(ctx context.Context) {
	accounts := p.Accounts()
	stored := p.Cache.Read()
	keys := map[string]bool{}
	for _, a := range accounts {
		v, ok := VendorOf(a.Profile)
		if !ok {
			continue
		}
		keys[a.Key()] = true
		now := p.clock()
		if prev := stored[a.Key()]; prev != nil {
			at, failed := askedAt(prev)
			wait := ProfileMinInterval
			if failed {
				wait = ProfileFailedInterval
			}
			if now-at < wait.Seconds() {
				continue
			}
		}
		data, failure := p.ask(ctx, a, v, now)
		var err error
		switch {
		case data != nil:
			if v.Unofficial {
				data["unofficial"] = true
			}
			err = p.Cache.Put(a, data, now)
		case failure != "":
			err = p.Cache.Fail(a, failure, v.Unofficial, now)
		default: // the credential cannot ask (an API key where only a sign-in can): unknown, and not a failure
			delete(keys, a.Key())
		}
		if err != nil && p.Warn != nil {
			p.Warn("failed to write provider limits", err)
		}
	}
	if err := p.Cache.Keep(keys, p.clock()); err != nil && p.Warn != nil {
		p.Warn("failed to write provider limits", err)
	}
}

func (p *Profiles) ask(ctx context.Context, a Account, v Vendor, now float64) (Data, string) {
	ctx, cancel := context.WithTimeout(ctx, profileTimeout)
	defer cancel()
	cred, err := a.Credential(ctx)
	if err != nil || cred.Token == "" || (v.LoginOnly && !cred.Login) {
		return nil, ""
	}
	url := v.URL
	if p.URL != nil {
		url = p.URL(a.Profile, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "fetch failed"
	}
	req.Header.Set("Accept", "application/json")
	v.Auth(req, cred.Token)
	if v.AccountHeader != "" && cred.Account != "" {
		req.Header.Set(v.AccountHeader, cred.Account)
	}
	client := p.Client
	if client == nil {
		client = profileClient
	}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, locale.Pick("the provider did not answer in time", "провайдер не ответил вовремя")
		}
		return nil, locale.Pick("the provider could not be reached", "до провайдера не удалось достучаться")
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, keyRefused()
	case res.StatusCode < 200 || res.StatusCode > 299:
		return nil, "HTTP " + strconv.Itoa(res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, profileMaxBody))
	if err != nil {
		return nil, "fetch failed"
	}
	var body any
	if json.Unmarshal(raw, &body) != nil {
		return nil, locale.Pick("the answer could not be read", "ответ не удалось прочитать")
	}
	if v.Refused != nil && v.Refused(body) {
		return nil, keyRefused()
	}
	if data := v.Parse(body, now); data != nil {
		return data, ""
	}
	return nil, locale.Pick("the answer has no limits", "в ответе нет лимитов")
}

// keyRefused is the failure of an account whose key the vendor did not accept (401, 403, or its own refusal); the entry
// notes it apart from the words so the page can offer another key.
func keyRefused() string {
	return locale.Pick("the provider did not accept the key", "провайдер не принял ключ")
}
