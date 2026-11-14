package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Store keeps the keys of the profiles, one file each: <Dir>/<profile id>.key, 0600 in a folder of 0700, written
// whole under a temporary name and renamed over. A key is the person's, on this machine, and is read only by the file
// the CLI is told to read (apiKeyHelper, `cat`), by a test of the connection and by the mask the wire puts over the
// feed. It is never logged and never put in a launch line.
type Store struct {
	Dir string
	// Logins are the vendor CLIs' own sign-ins a profile runs on when no key is saved for it, by profile id (the Grok
	// CLI's for xai); nil for none.
	Logins map[string]Login
	// Switches are the switched profiles' switches, by profile id (GPTProxy for gpt); nil for none, and a switched
	// profile without one is never ready.
	Switches map[string]Switch
	// Secrets turns the name of a secret of the safe (Preset.KeyRef of a profile of the profiles file) into its value; nil
	// for none, and a profile with a KeyRef then takes the key typed on the Settings screen like any other.
	Secrets func(name string) (string, error)
	// Safe reads a secret of the machine's safe: the key and the secret variables of a custom profile (Preset.Custom).
	Safe func(name string) (string, error)
	// Proxy is the runtime's proxy a profile of an OpenAI-compatible provider runs on (Preset.Proxied); nil for none,
	// and such a profile is then never ready.
	Proxy ExtProxy

	mu sync.Mutex
	// refreshed is when each profile's key was last taken from the safe.
	refreshed map[string]time.Time
	// masks are the keys the feed hides, each with the form it has in a JSON string; read on every frame, so kept here
	// and rebuilt when a key is saved or removed. nil until the first use.
	masks atomic.Pointer[[]mask]
	// launched are the values of the secret variables put into a chat's launch (a custom profile's token): the feed
	// hides them as it hides the keys.
	launched []string
}

// hide adds values a launch took from the safe to what the feed hides.
func (s *Store) hide(values []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	added := false
	for _, v := range values {
		// A value shorter than a key cannot be told from a word and would mask words.
		if len(v) >= wire.ProfileKeyMin && !slices.Contains(s.launched, v) {
			s.launched, added = append(s.launched, v), true
		}
	}
	if added {
		s.rebuildLocked()
	}
}

type mask struct{ key, escaped, bullets []byte }

// ErrNoKey: no key is saved for the profile.
var ErrNoKey = errors.New("providers: no key saved")

// Switch is a profile that runs on a local proxy the runtime keeps instead of a key (Preset.Switched): the person turns
// it on and off, and while it is on Claude Code is pointed at the proxy.
type Switch interface {
	// On is the person's choice, kept across restarts.
	On() bool
	// SetOn turns the switch: on waits until the proxy answers (a *wire.ProfileSwitchError when it can not, and the
	// switch stays off), off stops it.
	SetOn(ctx context.Context, on bool) error
	// Ready: on, the vendor CLI signed in, and the proxy answers or can be started again.
	Ready() bool
	// Endpoint is where Claude Code sends its requests and the file of the key it sends; ok is false while the switch
	// is off or the proxy has never had an address.
	Endpoint() (baseURL, keyFile string, ok bool)
}

// ExtProxy is the proxy Claude Code reaches an OpenAI-compatible provider through (GPTProxy).
type ExtProxy interface {
	// ExtReady: the proxy relays the provider of that id.
	ExtReady(provider string) bool
	// ExtEndpoint is the proxy's address and the file of its key, the proxy started when it does not run.
	ExtEndpoint(ctx context.Context) (baseURL, keyFile string, err error)
}

// SwitchOf is the switch of a switched profile; nil for another profile, or when the runtime keeps none.
func (s *Store) SwitchOf(id string) Switch {
	if p, ok := Find(id); !ok || !p.Switched {
		return nil
	}
	return s.Switches[id]
}

// KeyFile is the file the key of a profile is kept in (it may not exist).
func (s *Store) KeyFile(id string) string { return filepath.Join(s.Dir, id+".key") }

// Set saves the key of a profile, replacing a saved one, and answers what the page may know of it.
func (s *Store) Set(id, key string) (wire.ProfileKey, error) {
	if p, ok := Find(id); !IsSpeechKey(id) && (!ok || p.Switched || p.Backed) {
		// A switched profile has no key; a backed one takes its account's, changed among the providers.
		return wire.ProfileKey{}, wire.ErrProfileUnknown
	}
	if err := (wire.ProfileKeyEdit{Key: key}).Validate(); err != nil {
		return wire.ProfileKey{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeLocked(id, key); err != nil {
		return wire.ProfileKey{}, err
	}
	return tailOf(key), nil
}

// writeLocked writes the key file of a profile whole under a temporary name and renames it over.
func (s *Store) writeLocked(id, key string) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return err
	}
	file := s.KeyFile(id)
	tmp := file + ".tmp"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	// No newline: the file is read by `cat` into a header.
	if _, err := f.WriteString(key); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.rebuildLocked()
	return nil
}

// refSettle is how long a key taken from the safe is trusted before the safe is asked again.
const refSettle = time.Minute

// refresh puts the key of a profile that names a secret of the safe (Preset.KeyRef) into its key file, so that every
// reader of the file (the CLI's apiKeyHelper, a test) sees the safe's current value. A safe that does not answer leaves
// the file as it was.
func (s *Store) refresh(id string) {
	p, ok := Find(id)
	read := s.Secrets
	if p.Custom || p.Backed {
		read = s.Safe
	}
	if ok && (p.Custom || p.Backed) && p.KeyRef == "" {
		// The person took the provider's key away: no file of it is left for a launch to read.
		_ = os.Remove(s.KeyFile(id))
		return
	}
	if !ok || p.KeyRef == "" || read == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if at, ok := s.refreshed[id]; ok && time.Since(at) < refSettle {
		return
	}
	val, err := read(p.KeyRef)
	val = strings.TrimSpace(val)
	if err != nil || len(val) < wire.ProfileKeyMin {
		return
	}
	if (p.Custom || p.Backed) && agents.ClaudeSubscriptionToken(val) {
		// The secret the provider names is the sign-in of a subscription, not a key: the provider has no key to run on.
		_ = os.Remove(s.KeyFile(id))
		return
	}
	if s.refreshed == nil {
		s.refreshed = map[string]time.Time{}
	}
	s.refreshed[id] = time.Now()
	if raw, err := os.ReadFile(s.KeyFile(id)); err == nil && strings.TrimSpace(string(raw)) == val {
		return
	}
	_ = s.writeLocked(id, val)
}

// Stale makes the next reader of a profile's key ask the safe again: the provider was changed.
func (s *Store) Stale(id string) {
	s.mu.Lock()
	delete(s.refreshed, id)
	s.mu.Unlock()
}

// Remove deletes the saved key; a profile with none stays as it is.
func (s *Store) Remove(id string) (wire.ProfileKey, error) {
	p, ok := Find(id)
	if (!ok || p.Backed) && !IsSpeechKey(id) {
		return wire.ProfileKey{}, wire.ErrProfileUnknown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.KeyFile(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wire.ProfileKey{}, err
	}
	s.rebuildLocked()
	return wire.ProfileKey{}, nil
}

// Key is the saved key of a profile; ErrNoKey when there is none. Callers use it for the request of a test and never
// keep it.
func (s *Store) Key(id string) (string, error) {
	if _, ok := Find(id); !ok && !IsSpeechKey(id) {
		return "", wire.ErrProfileUnknown
	}
	s.refresh(id)
	raw, err := os.ReadFile(s.KeyFile(id))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoKey
	}
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", ErrNoKey
	}
	return key, nil
}

// State is whether a key is saved for the profile, and its tail.
func (s *Store) State(id string) wire.ProfileKey {
	key, err := s.Key(id)
	if err != nil {
		return wire.ProfileKey{}
	}
	return tailOf(key)
}

// Configured says whether a key is saved for the profile.
func (s *Store) Configured(id string) bool { return s.State(id).Configured }

// LoginOf is the account of the vendor CLI's sign-in the profile can run on; ok is false while there is none.
func (s *Store) LoginOf(id string) (account string, ok bool) {
	if p, found := Find(id); found && p.Home != "" {
		return (&CodexLogin{Path: filepath.Join(p.Home, "auth.json")}).Account()
	}
	l := s.Logins[id]
	if l == nil {
		return "", false
	}
	return l.Account()
}

// Ready says whether a conversation can run on the profile: a key is saved, or the vendor CLI is signed in; for a
// switched profile, its switch says.
func (s *Store) Ready(id string) bool {
	if p, ok := Find(id); ok && p.Proxied != "" {
		return s.Proxy != nil && s.Proxy.ExtReady(p.Proxied)
	}
	if p, ok := Find(id); ok && p.Switched {
		sw := s.SwitchOf(id)
		return sw != nil && sw.Ready()
	}
	if p, ok := Find(id); ok && p.Home != "" {
		// Another sign-in of Codex: ready when its home holds a sign-in. No key counts for it.
		return codexSignedIn(p.Home)
	}
	if s.Configured(id) {
		return true
	}
	if p, ok := Find(id); ok && p.Custom && p.ownCredential() {
		return true
	}
	_, ok := s.LoginOf(id)
	return ok
}

// credentialEnv are the variables Claude Code takes a credential from.
var credentialEnv = []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"}

// ownCredential says whether the profile's variables carry a credential of their own, so it needs no saved key.
func (p Preset) ownCredential() bool {
	for _, name := range credentialEnv {
		if _, ok := p.SecretEnv[name]; ok {
			return true
		}
	}
	return false
}

// Credential is what a request of the profile is authorised with: the saved key, else the token of the vendor CLI's
// sign-in. A saved key is the person's choice on the Settings screen and goes first.
func (s *Store) Credential(ctx context.Context, id string) (string, error) {
	if p, ok := Find(id); ok && p.Switched {
		return s.switchKey(id)
	}
	if p, ok := Find(id); ok && p.Proxied != "" {
		// The proxy's key goes only to the proxy, from its file (the chat's apiKeyHelper); a caller here would send it to
		// the profile's BaseURL, the provider's own address. The provider is checked by the registry (extproviders).
		return "", ErrNoKey
	}
	key, err := s.Key(id)
	if err == nil || !errors.Is(err, ErrNoKey) {
		return key, err
	}
	if l := s.Logins[id]; l != nil {
		if _, ok := l.Account(); ok {
			return l.Token(ctx)
		}
	}
	return "", ErrNoKey
}

// switchKey is the key of a switched profile's proxy, the one Claude Code sends it.
func (s *Store) switchKey(id string) (string, error) {
	sw := s.SwitchOf(id)
	if sw == nil {
		return "", ErrNoKey
	}
	_, file, ok := sw.Endpoint()
	if !ok {
		return "", ErrNoKey
	}
	raw, err := os.ReadFile(file)
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return "", ErrNoKey
	}
	return strings.TrimSpace(string(raw)), nil
}

// tailOf is «…» and the last four characters of key.
func tailOf(key string) wire.ProfileKey {
	runes := []rune(key)
	tail := "…" + string(runes[max(0, len(runes)-4):])
	return wire.ProfileKey{Configured: true, Tail: &tail}
}

// Mask is the form a saved key takes in the feed: bullets and the last four characters.
func Mask(key string) string {
	runes := []rune(key)
	return "•••" + string(runes[max(0, len(runes)-4):])
}

// rebuildLocked reads every saved key again into the masks.
func (s *Store) rebuildLocked() {
	masks := []mask{}
	hidden := slices.Clone(s.launched)
	// After a restart the chats started before it still run with their variables: the values are read again from the
	// safe by the names the custom profiles keep.
	if s.Safe != nil {
		for _, p := range all() {
			if !p.Custom {
				continue
			}
			for _, ref := range p.SecretEnv {
				if v, err := s.Safe(ref); err == nil {
					if v = strings.TrimSpace(v); len(v) >= wire.ProfileKeyMin && !slices.Contains(hidden, v) {
						hidden = append(hidden, v)
					}
				}
			}
		}
	}
	for _, v := range hidden {
		esc, _ := json.Marshal(v)
		masks = append(masks, mask{key: []byte(v), escaped: esc[1 : len(esc)-1], bullets: []byte(Mask(v))})
	}
	ids := SpeechKeys
	for _, p := range all() {
		ids = append(ids[:len(ids):len(ids)], p.ID)
	}
	for _, id := range ids {
		raw, err := os.ReadFile(s.KeyFile(id))
		key := strings.TrimSpace(string(raw))
		// A key shorter than the least a page may save cannot be one of ours, and would mask words.
		if err != nil || len(key) < wire.ProfileKeyMin {
			continue
		}
		esc, _ := json.Marshal(key)
		masks = append(masks, mask{key: []byte(key), escaped: esc[1 : len(esc)-1], bullets: []byte(Mask(key))})
	}
	s.masks.Store(&masks)
}

// Redact puts the mask over every saved key in b (a frame as it goes on the wire), in the form it has in a JSON string
// too. It is for a key an agent printed: a `cat` of the key file, an environment dump. It returns b itself when there
// is nothing to hide.
func (s *Store) Redact(b []byte) []byte {
	masks := s.masks.Load()
	if masks == nil {
		s.mu.Lock()
		if s.masks.Load() == nil {
			s.rebuildLocked()
		}
		s.mu.Unlock()
		masks = s.masks.Load()
	}
	for _, m := range *masks {
		if bytes.Contains(b, m.key) {
			b = bytes.ReplaceAll(b, m.key, m.bullets)
		}
		if !bytes.Equal(m.escaped, m.key) && bytes.Contains(b, m.escaped) {
			b = bytes.ReplaceAll(b, m.escaped, m.bullets)
		}
	}
	return b
}
