package storelink

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/capabilities"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Machine is a hub's machine as its machine.json names it: who it is, where it dials and the key it proves itself
// with.
type Machine struct {
	ID      string
	DialURL string
	Key     ed25519.PrivateKey
}

// origin is the hub's https (or http) root of a dial URL: wss://hub.example.org/agent/v1 → https://hub.example.org (a
// hub under a base path keeps it: wss://example.org/hub/agent/v1 → https://example.org/hub).
func origin(dialURL string) (string, error) {
	u, err := url.Parse(dialURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("not a dial url: %q", dialURL)
	}
	switch u.Scheme {
	case "wss", "https":
		u.Scheme = "https"
	case "ws", "http":
		u.Scheme = "http"
	default:
		return "", fmt.Errorf("not a dial url: %q", dialURL)
	}
	return u.Scheme + "://" + u.Host + strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/agent/v1"), nil
}

// errNoStore is a hub that answers and has no store: an older hub, or HUB_STORE=off.
var errNoStore = errors.New("the hub has no store")

// HubHasStore asks the hub's GET /api/capabilities whether it keeps a store. errNoStore when it answers without one.
func HubHasStore(ctx context.Context, hc *http.Client, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/capabilities", nil)
	if err != nil {
		return err
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return errNoStore // a hub from before capabilities
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("hub capabilities: HTTP %d", res.StatusCode)
	}
	var doc capabilities.Document
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return fmt.Errorf("hub capabilities: %w", err)
	}
	for _, c := range doc.Capabilities {
		if c.ID == hubproto.StoreCapability && c.Status == capabilities.On {
			return nil
		}
	}
	return errNoStore
}

// RequestSession asks the hub for a store session (POST /agent/store), signed with the machine's key.
func RequestSession(ctx context.Context, hc *http.Client, m Machine, now time.Time) (hubproto.StoreSession, error) {
	var out hubproto.StoreSession
	base, err := origin(m.DialURL)
	if err != nil {
		return out, err
	}
	host, err := hubproto.DialHost(m.DialURL)
	if err != nil {
		return out, err
	}
	auth, err := hubproto.SignStore(m.Key, m.ID, now, host)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+hubproto.StorePath, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", auth)
	res, err := hc.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return out, errNoStore
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusLocked:
		return out, fmt.Errorf("the hub gave this machine no store session (HTTP %d): check the clock, or the machine is not in this hub", res.StatusCode)
	default:
		return out, fmt.Errorf("the hub gave no store session: HTTP %d", res.StatusCode)
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" || !strings.HasPrefix(out.Path, "/") {
		return out, errors.New("the hub gave an answer that is not a store session")
	}
	return out, nil
}

// sessions keeps one session and asks for the next before it runs out, or when the store refused it.
type sessions struct {
	hc      *http.Client
	machine Machine
	now     func() time.Time

	mu  sync.Mutex
	cur hubproto.StoreSession
	exp time.Time
}

func (s *sessions) token(ctx context.Context, refresh bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !refresh && s.cur.Token != "" && s.now().Add(5*time.Minute).Before(s.exp) {
		return s.cur.Token, nil
	}
	sess, err := RequestSession(ctx, s.hc, s.machine, s.now())
	if err != nil {
		return "", err
	}
	exp, err := time.Parse(time.RFC3339, sess.ExpiresAt)
	if err != nil {
		exp = s.now().Add(10 * time.Minute)
	}
	s.cur, s.exp = sess, exp
	return sess.Token, nil
}
