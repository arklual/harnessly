package chatgpt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// Vault keeps the integration's record in the machine's safe (secrets.Store does).
type Vault interface {
	PutIntegration(integration, name, value string) error
	RevealIntegration(integration, name string) (string, error)
	RemoveIntegration(integration, name string) error
}

const (
	vaultIntegration = "chatgpt"
	vaultRecord      = "accounts"
)

// Account is one ChatGPT sign-in of this machine: an issued client bound to the person's account and workspace, and
// its tokens. Label (the account's e-mail) is shown on the person's own page only; nothing of an account goes to the
// hub.
type Account struct {
	Key      string `json:"key"` // the runtime's own id of the registration
	Label    string `json:"label"`
	Subject  string `json:"subject"`
	ClientID string `json:"clientId"`
	Plan     string `json:"plan,omitempty"`

	IDToken      string   `json:"idToken,omitempty"`
	AccessToken  string   `json:"accessToken,omitempty"`
	RefreshToken string   `json:"refreshToken,omitempty"`
	ExpiresAt    int64    `json:"expiresAt,omitempty"` // unix ms
	Scopes       []string `json:"scopes,omitempty"`
	SavedAt      int64    `json:"savedAt,omitempty"`
}

// SignedIn says the account holds a session that can be refreshed.
func (a *Account) SignedIn() bool { return a.RefreshToken != "" || a.AccessToken != "" }

// PlanUse says the person allowed this app to use their ChatGPT plan.
func (a *Account) PlanUse() bool { return slices.Contains(a.Scopes, scopePlan) }

// clearTokens forgets the session and keeps the registration (client id, identity) for a later sign-in.
func (a *Account) clearTokens() {
	a.AccessToken, a.RefreshToken, a.ExpiresAt, a.Scopes = "", "", 0, nil
}

// record is what the safe holds: the machine's host id and every registration.
type record struct {
	HostID   string    `json:"hostId"`
	Active   string    `json:"active,omitempty"`
	Accounts []Account `json:"accounts"`
}

// AccountView is an account as the person's page sees it: no token.
type AccountView struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Plan     string `json:"plan,omitempty"`
	SignedIn bool   `json:"signedIn"`
	PlanUse  bool   `json:"planUse"`
	Active   bool   `json:"active"`
}

// accounts is the record in memory, read from the safe once and written back whole on every change. One process owns
// it: the runtime is single-instance per data folder (its start-up lock), so no other writer changes the record under
// it.
type accounts struct {
	vault Vault
	mu    sync.Mutex
	rec   *record
	// locks serialize the refreshes of one account: a rotating refresh token used twice ends the session.
	locks map[string]*sync.Mutex
}

var errNoAccount = errors.New("chatgpt: no such account")

func newAccounts(v Vault) *accounts { return &accounts{vault: v, locks: map[string]*sync.Mutex{}} }

// load reads the record once; a missing one is a new record with a fresh host id. Called with mu held.
func (s *accounts) load() (*record, error) {
	if s.rec != nil {
		return s.rec, nil
	}
	raw, err := s.vault.RevealIntegration(vaultIntegration, vaultRecord)
	switch {
	case err == nil:
		var r record
		if json.Unmarshal([]byte(raw), &r) != nil || r.HostID == "" {
			return nil, errors.New("chatgpt: the safe's record does not read")
		}
		s.rec = &r
	case secrets.ReasonOf(err) == secrets.ReasonNotFound:
		s.rec = &record{HostID: newHostID()}
	default:
		return nil, err
	}
	return s.rec, nil
}

// save writes the record back. Called with mu held.
func (s *accounts) save() error {
	raw, err := json.Marshal(s.rec)
	if err != nil {
		return err
	}
	return s.vault.PutIntegration(vaultIntegration, vaultRecord, string(raw))
}

// hostID is this machine's stable ext_agent_host_id, made and kept before the first sign-in.
func (s *accounts) hostID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return "", err
	}
	if len(r.Accounts) == 0 && r.Active == "" {
		// Persist the fresh id before its first use, so a sign-in that fails half way keeps the same host.
		if err := s.save(); err != nil {
			return "", err
		}
	}
	return r.HostID, nil
}

// get is a copy of one account.
func (s *accounts) get(key string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return Account{}, err
	}
	for _, a := range r.Accounts {
		if a.Key == key {
			return a, nil
		}
	}
	return Account{}, errNoAccount
}

// active is the key of the account chats use: the chosen one, else the first signed-in one.
func (s *accounts) active() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return "", err
	}
	return activeOf(r), nil
}

func activeOf(r *record) string {
	for _, a := range r.Accounts {
		if a.Key == r.Active && a.SignedIn() {
			return a.Key
		}
	}
	for _, a := range r.Accounts {
		if a.SignedIn() {
			return a.Key
		}
	}
	return ""
}

// views are the accounts for the page.
func (s *accounts) views() ([]AccountView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return nil, err
	}
	act := activeOf(r)
	out := make([]AccountView, 0, len(r.Accounts))
	for _, a := range r.Accounts {
		out = append(out, AccountView{Key: a.Key, Label: a.Label, Plan: a.Plan, SignedIn: a.SignedIn(), PlanUse: a.PlanUse(), Active: a.Key == act})
	}
	return out, nil
}

// put keeps a (new or updated) account; makeActive makes chats use it.
func (s *accounts) put(a Account, makeActive bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(r.Accounts, func(x Account) bool { return x.Key == a.Key })
	if i < 0 {
		r.Accounts = append(r.Accounts, a)
	} else {
		r.Accounts[i] = a
	}
	if makeActive {
		r.Active = a.Key
	}
	return s.save()
}

// update changes one account under the lock; fn sees the stored copy.
func (s *accounts) update(key string, fn func(a *Account)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return err
	}
	for i := range r.Accounts {
		if r.Accounts[i].Key == key {
			fn(&r.Accounts[i])
			return s.save()
		}
	}
	return errNoAccount
}

// setActive makes chats use key.
func (s *accounts) setActive(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(r.Accounts, func(a Account) bool { return a.Key == key }) {
		return errNoAccount
	}
	r.Active = key
	return s.save()
}

// remove forgets an account whole.
func (s *accounts) remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return err
	}
	n := len(r.Accounts)
	r.Accounts = slices.DeleteFunc(r.Accounts, func(a Account) bool { return a.Key == key })
	if len(r.Accounts) == n {
		return errNoAccount
	}
	if r.Active == key {
		r.Active = ""
	}
	return s.save()
}

// bySubject finds a registration of the same account and workspace client: a second sign-in to it updates it.
func (s *accounts) byClient(clientID, subject string) (Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return Account{}, false
	}
	for _, a := range r.Accounts {
		if a.ClientID == clientID && a.Subject == subject {
			return a, true
		}
	}
	return Account{}, false
}

// lockOf is the refresh lock of one account.
func (s *accounts) lockOf(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.locks[key]
	if m == nil {
		m = &sync.Mutex{}
		s.locks[key] = m
	}
	return m
}

func newHostID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 9562 variant
	h := hex.EncodeToString(b[:])
	return "urn:uuid:" + h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func newKey() string {
	var b [9]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func nowMs(t time.Time) int64 { return t.UnixMilli() }
