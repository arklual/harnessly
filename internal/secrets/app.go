package secrets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// sharedMissFresh is how long a secret the shared safe did not have is not asked for again by HasApp.
const sharedMissFresh = 30 * time.Second

// An app's secrets (internal/apps/setup.go) live in the same safe under app/<id>/<name>. The slash keeps them out of
// every route that takes a plain name (ValidName refuses it): the page's safe list, the agent's `harnessly secret run`
// and `file` never see one. Only the app's own calls below put, ask about and reveal them, and the runtime reveals one
// only to fill a header or an environment of the app's MCP server on the machine.

const appPrefix = "app/"

var appPartRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// IsAppName says whether name is the safe's name of an app's secret.
func IsAppName(name string) bool { return strings.HasPrefix(name, appPrefix) }

// ValidAppName says whether name is a whole, valid app/<id>/<name>: the only name with slashes a shared safe keeps
// always two parts below app/, so it cannot leave the owner's folder.
func ValidAppName(name string) bool {
	rest, ok := strings.CutPrefix(name, appPrefix)
	if !ok {
		return false
	}
	app, n, ok := strings.Cut(rest, "/")
	return ok && AppName(app, n) == name
}

// ValidSharedName is a name a shared safe (the hub's, a server's) keeps: a person's plain name, an app's secret or one of
// harnessly's own records of the person.
func ValidSharedName(name string) bool {
	return ValidName(name) || ValidAppName(name) || ValidSysName(name)
}

// SysConnections is harnessly's record of the person's connections (Settings → Connections) in their shared safe: the
// list without credentials, so a new machine of the person has them. Like an app's secret it has a slash, so
// no route that takes a plain name (the page's safe list, `harnessly secret run`) ever sees it.
const SysConnections = "sys/connections"

// ValidSysName says whether name is one of harnessly's own records a shared safe keeps.
func ValidSysName(name string) bool { return name == SysConnections }

// AppShared is the shared safe an app's secrets are mirrored to, so every machine of the person has them:
// a VM is only a runner, an app's token put on one is there on the next.
type AppShared interface {
	ReadApp(full string) (string, error)
	WriteApp(full, value string) error
	DeleteApp(full string) error
}

// SetShared mirrors the app's secrets of this file to a shared safe; nil keeps them in the file alone.
func (s *Store) SetShared(sh AppShared) {
	s.mu.Lock()
	s.shared = sh
	s.mu.Unlock()
}

func (s *Store) sharedSafe() AppShared {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shared
}

// AppName is the name an app's secret has in the safe; "" when the app id or the secret name is not one.
func AppName(app, name string) string {
	if !appPartRe.MatchString(app) || !appPartRe.MatchString(name) {
		return ""
	}
	return appPrefix + app + "/" + name
}

// PutApp keeps the value of an app's secret.
func (s *Store) PutApp(app, name, value string) error {
	full := AppName(app, name)
	if full == "" {
		return refuse(ReasonBadName, nil)
	}
	if _, _, err := s.put(full, value, nil); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.sharedMiss, full)
	s.mu.Unlock()
	if sh := s.sharedSafe(); sh != nil {
		// The file has it already; a shared safe that is down now gets it on the next reveal.
		if sh.WriteApp(full, Clean(value)) == nil {
			s.mark(full, true)
		}
	}
	return nil
}

// HasApp says whether an app's secret has a value.
func (s *Store) HasApp(app, name string) bool {
	full := AppName(app, name)
	if full == "" {
		return false
	}
	if s.Has(full) {
		return true
	}
	_, ok := s.pull(full)
	return ok
}

// sharedMarks is the file of the app's secrets this file has shared: one of them the shared safe no longer keeps was
// removed on another machine, and leaves this file too, instead of going up again.
func (s *Store) sharedMarks() string { return filepath.Join(s.dir, "app-shared.json") }

func (s *Store) marked(full string) bool {
	raw, err := os.ReadFile(s.sharedMarks())
	var names []string
	return err == nil && json.Unmarshal(raw, &names) == nil && slices.Contains(names, full)
}

func (s *Store) mark(full string, on bool) {
	var names []string
	if raw, err := os.ReadFile(s.sharedMarks()); err == nil {
		_ = json.Unmarshal(raw, &names)
	}
	names = slices.DeleteFunc(names, func(n string) bool { return n == full })
	if on {
		names = append(names, full)
	}
	if raw, err := json.Marshal(names); err == nil && os.MkdirAll(s.dir, 0o700) == nil {
		_ = os.WriteFile(s.sharedMarks(), raw, 0o600)
	}
}

// pull copies an app's secret the shared safe keeps into the file; ok when there was one.
func (s *Store) pull(full string) (string, bool) {
	sh := s.sharedSafe()
	if sh == nil {
		return "", false
	}
	// A page asks whether an app waits for its secret on every draw: a miss is not asked again for a while.
	s.mu.Lock()
	missed := s.Now().Sub(s.sharedMiss[full]) < sharedMissFresh
	s.mu.Unlock()
	if missed {
		return "", false
	}
	v, err := sh.ReadApp(full)
	if err != nil || v == "" {
		s.mu.Lock()
		if s.sharedMiss == nil {
			s.sharedMiss = map[string]time.Time{}
		}
		s.sharedMiss[full] = s.Now()
		s.mu.Unlock()
		return "", false
	}
	if _, _, err := s.put(full, v, nil); err != nil {
		return "", false
	}
	s.mark(full, true)
	return v, true
}

// RemoveApp forgets an app's secret.
func (s *Store) RemoveApp(app, name string) error {
	full := AppName(app, name)
	if full == "" {
		return refuse(ReasonBadName, nil)
	}
	if sh := s.sharedSafe(); sh != nil {
		if err := sh.DeleteApp(full); err != nil && ReasonOf(err) != ReasonNotFound {
			return err
		}
	}
	s.mark(full, false)
	err := s.Remove(full)
	if ReasonOf(err) == ReasonNotFound && s.sharedSafe() != nil {
		return nil
	}
	return err
}

// RevealApp opens the values of an app's secrets for the machine to put into the app's MCP server config. The use is
// recorded under the app's name; names are the secrets' own names (not the safe's). A missing one is a refusal.
func (s *Store) RevealApp(app string, names []string) (map[string]string, error) {
	full := make([]string, 0, len(names))
	for _, n := range names {
		f := AppName(app, n)
		if f == "" {
			return nil, refuse(ReasonBadName, nil)
		}
		full = append(full, f)
	}
	// The shared value wins (the last one the person put, on any machine); a value only the file has goes up.
	shared := map[string]string{}
	if sh := s.sharedSafe(); sh != nil {
		for _, f := range full {
			v, err := sh.ReadApp(f)
			switch {
			case err == nil && v != "":
				shared[f] = v
			case ReasonOf(err) == ReasonNotFound:
				shared[f] = ""
			}
		}
		for f, v := range shared {
			switch {
			case v != "" && !s.Has(f):
				_, _, _ = s.put(f, v, nil)
				s.mark(f, true)
			case v == "" && s.marked(f):
				// Removed on another machine: the copy here goes too.
				_ = s.Remove(f)
				s.mark(f, false)
				delete(shared, f)
			}
		}
	}
	got, err := s.Reveal(full, Use{Title: "app " + app})
	if err != nil {
		return nil, err
	}
	for f, v := range shared {
		switch {
		case v == "" && got[f] != "":
			// A token the file kept before it was shared goes up once.
			if s.sharedSafe().WriteApp(f, got[f]) == nil {
				s.mark(f, true)
			}
		case v != "" && got[f] != v:
			_, _, _ = s.put(f, v, nil)
			s.mark(f, true)
			got[f] = v
		}
	}
	out := make(map[string]string, len(names))
	for i, n := range names {
		out[n] = got[full[i]]
	}
	return out, nil
}

// ShareApps sends up every app's secret this file keeps and has not shared yet: a token put on this machine
// before the safe was shared reaches the other machines at once, not only on its first use here. One the shared safe
// already keeps is only marked (its value wins on the next reveal). Errors leave the secret for the next start.
func (s *Store) ShareApps() int {
	sh := s.sharedSafe()
	if sh == nil {
		return 0
	}
	s.mu.Lock()
	f, err := s.read()
	s.mu.Unlock()
	if err != nil {
		return 0
	}
	var names []string
	for _, e := range f.Secrets {
		if ValidAppName(e.Name) && !s.marked(e.Name) {
			names = append(names, e.Name)
		}
	}
	shared := 0
	for _, full := range names {
		_, err := sh.ReadApp(full)
		switch {
		case err == nil:
			s.mark(full, true)
		case ReasonOf(err) == ReasonNotFound:
			got, rerr := s.Reveal([]string{full}, Use{Title: "shared safe"})
			if rerr == nil && sh.WriteApp(full, got[full]) == nil {
				s.mark(full, true)
				shared++
			}
		}
	}
	return shared
}
