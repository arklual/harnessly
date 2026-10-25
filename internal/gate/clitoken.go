package gate

import (
	"crypto/subtle"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// CLIToken checks the runtime's cli-token, kept in cli-token in the data folder: the CLIs and the agent hooks carry it.
type CLIToken struct {
	DataDir string
	// Headers carry the cli-token; a request is let in by the first one it has.
	Headers []string
}

func (t *CLIToken) token() string {
	raw, _ := os.ReadFile(filepath.Join(t.DataDir, "cli-token"))
	return strings.TrimSpace(string(raw))
}

// OK checks a request to the internal routes: the runtime's cli-token in one of the headers.
func (t *CLIToken) OK(r *http.Request) bool {
	token := t.token()
	if token == "" {
		return false
	}
	for _, h := range t.Headers {
		if v := r.Header.Get(h); v != "" && subtle.ConstantTimeCompare([]byte(v), []byte(token)) == 1 {
			return true
		}
	}
	return false
}
