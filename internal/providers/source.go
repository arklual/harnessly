package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// TestTimeout bounds a test of a connection.
const TestTimeout = 15 * time.Second

// Source is the wire's ProfilesSource over a Store: the presets of the runtime, the keys and the test of a connection.
type Source struct {
	Store *Store
	// Client sends the test request; nil is a client with TestTimeout that follows no redirect (a redirect would carry
	// the key to the host it points at).
	Client *http.Client
	// BaseURL gives the address a test goes to; nil is the preset's own. It is for tests, which point it at a fake
	// server; the runtime never takes an address from a person.
	BaseURL func(Preset) string
	// OnChange is told after a key is saved or removed, so a machine can show the profile offered or not.
	OnChange func()
}

var _ wire.ProfilesSource = (*Source)(nil)

func (s *Source) Profiles(context.Context) (wire.ProfileList, error) {
	out := make([]wire.ProfileInfo, 0, len(presets))
	for _, p := range presets {
		info := wire.ProfileInfo{ID: p.ID, Label: p.Label, Vendor: p.Vendor, Via: p.Via, Host: p.Host, Hint: p.Hint,
			Key: s.Store.State(p.ID), Models: p.AgentModels()}
		if account, ok := s.Store.LoginOf(p.ID); ok {
			info.Login = &wire.ProfileLogin{Account: account}
		}
		out = append(out, info)
	}
	return wire.ProfileList{Profiles: out}, nil
}

func (s *Source) SetProfileKey(_ context.Context, id, key string) (wire.ProfileKey, error) {
	st, err := s.Store.Set(id, key)
	s.changed(err)
	return st, err
}

func (s *Source) RemoveProfileKey(_ context.Context, id string) (wire.ProfileKey, error) {
	st, err := s.Store.Remove(id)
	s.changed(err)
	return st, err
}

func (s *Source) changed(err error) {
	if err == nil && s.OnChange != nil {
		s.OnChange()
	}
}

func (s *Source) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: TestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// TestProfile makes one minimal request to the vendor with the saved key (else the vendor CLI's sign-in): a message of a token for the Anthropic-
// compatible vendors, a response of a few tokens for xAI. The answer says how it went and holds neither the key nor
// the vendor's body.
func (s *Source) TestProfile(ctx context.Context, id string) (wire.ProfileTest, error) {
	p, ok := Find(id)
	if !ok {
		return wire.ProfileTest{}, wire.ErrProfileUnknown
	}
	key, err := s.Store.Credential(ctx, id)
	if errors.Is(err, ErrNoKey) || errors.Is(err, ErrNoLogin) {
		r := wire.TestNoKey
		return wire.ProfileTest{Reason: &r}, nil
	}
	if err != nil {
		return wire.ProfileTest{}, err
	}
	base := p.BaseURL
	if s.BaseURL != nil {
		base = s.BaseURL(p)
	}
	base = strings.TrimRight(base, "/")
	var url string
	var body any
	hdr := http.Header{"Content-Type": {"application/json"}}
	if p.Via == wire.ProviderCodex {
		url = base + "/responses"
		body = map[string]any{"model": p.Test, "input": "ping", "max_output_tokens": 16}
		hdr.Set("Authorization", "Bearer "+key)
	} else {
		url = base + "/v1/messages"
		body = map[string]any{"model": p.Test, "max_tokens": 16, "messages": []map[string]string{{"role": "user", "content": "ping"}}}
		// The vendors read the key from one header or the other.
		hdr.Set("Authorization", "Bearer "+key)
		hdr.Set("x-api-key", key)
		hdr.Set("anthropic-version", "2023-06-01")
	}
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(ctx, TestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return wire.ProfileTest{}, err
	}
	req.Header = hdr
	start := time.Now()
	res, err := s.client().Do(req)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		reason := wire.TestUnreachable
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			reason = wire.TestTimeout
		}
		return wire.ProfileTest{Reason: &reason, Ms: ms}, nil
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	status := res.StatusCode
	if status >= 200 && status < 300 {
		return wire.ProfileTest{OK: true, Status: &status, Ms: ms}, nil
	}
	reason := wire.TestRejected
	switch status {
	case http.StatusUnauthorized:
		reason = wire.TestUnauthorized
	case http.StatusForbidden:
		reason = wire.TestForbidden
	case http.StatusTooManyRequests:
		reason = wire.TestRateLimited
	}
	return wire.ProfileTest{Reason: &reason, Status: &status, Ms: ms}, nil
}
