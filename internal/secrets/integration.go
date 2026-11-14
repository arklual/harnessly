package secrets

import (
	"regexp"
	"strings"
)

// A built-in integration's credentials (internal/chatgpt) live in the same safe under integration/<id>/<name>, like an
// app's under app/<id>/<name>, but in a namespace no installed app can name: an app whose id happened to be the
// integration's would otherwise read its tokens through RevealApp. The slash keeps them out of every route that takes
// a plain name (ValidName refuses it), and List leaves them out: the page's safe list, the agent's `harnessly secret
// run` and `file` never see one. Only the integration's own code in the runtime puts, reads and removes them.

const integrationPrefix = "integration/"

var integrationPartRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// IsIntegrationName says whether name is the safe's name of a built-in integration's secret.
func IsIntegrationName(name string) bool { return strings.HasPrefix(name, integrationPrefix) }

// IntegrationName is the name an integration's secret has in the safe; "" when either part is not one.
func IntegrationName(integration, name string) string {
	if !integrationPartRe.MatchString(integration) || !integrationPartRe.MatchString(name) {
		return ""
	}
	return integrationPrefix + integration + "/" + name
}

// PutIntegration keeps the value of an integration's secret.
func (s *Store) PutIntegration(integration, name, value string) error {
	full := IntegrationName(integration, name)
	if full == "" {
		return refuse(ReasonBadName, nil)
	}
	_, _, err := s.put(full, value, nil)
	return err
}

// RevealIntegration opens the value of an integration's secret; a missing one is a refusal (ReasonNotFound).
func (s *Store) RevealIntegration(integration, name string) (string, error) {
	full := IntegrationName(integration, name)
	if full == "" {
		return "", refuse(ReasonBadName, nil)
	}
	got, err := s.Reveal([]string{full}, Use{Title: "integration " + integration})
	if err != nil {
		return "", err
	}
	return got[full], nil
}

// RemoveIntegration forgets an integration's secret.
func (s *Store) RemoveIntegration(integration, name string) error {
	full := IntegrationName(integration, name)
	if full == "" {
		return refuse(ReasonBadName, nil)
	}
	return s.Remove(full)
}
