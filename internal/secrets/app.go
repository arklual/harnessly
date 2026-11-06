package secrets

import (
	"regexp"
	"strings"
)

// An app's secrets (internal/apps/setup.go) live in the same safe under app/<id>/<name>. The slash keeps them out of
// every route that takes a plain name (ValidName refuses it): the page's safe list, the agent's `harnessly secret run`
// and `file` never see one. Only the app's own calls below put, ask about and reveal them, and the runtime reveals one
// only to fill a header or an environment of the app's MCP server on the machine.

const appPrefix = "app/"

var appPartRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// IsAppName says whether name is the safe's name of an app's secret.
func IsAppName(name string) bool { return strings.HasPrefix(name, appPrefix) }

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
	_, _, err := s.put(full, value, nil)
	return err
}

// HasApp says whether an app's secret has a value.
func (s *Store) HasApp(app, name string) bool {
	full := AppName(app, name)
	return full != "" && s.Has(full)
}

// RemoveApp forgets an app's secret.
func (s *Store) RemoveApp(app, name string) error {
	full := AppName(app, name)
	if full == "" {
		return refuse(ReasonBadName, nil)
	}
	return s.Remove(full)
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
	got, err := s.Reveal(full, Use{Title: "app " + app})
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(names))
	for i, n := range names {
		out[n] = got[full[i]]
	}
	return out, nil
}
