package gate

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// OldRuntime calls the previous runtime's internal routes for what the Go slices must tell it while it still keeps
// the state they touch. Calls carry the runtime's cli-token.
type OldRuntime struct {
	Upstream string
	DataDir  string
	// TokenHeaders carry the cli-token; the old runtime accepts the first one it knows.
	TokenHeaders []string
	Client       *http.Client
}

func (o *OldRuntime) token() string {
	raw, _ := os.ReadFile(filepath.Join(o.DataDir, "cli-token"))
	return strings.TrimSpace(string(raw))
}

// Post sends body to path and decodes a JSON answer into out (if given). False when the call did not succeed, and at
// once without an old runtime (a standalone gate).
func (o *OldRuntime) Post(path string, body, out any) bool {
	if o == nil || o.Upstream == "" {
		return false
	}
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+o.Upstream+path, bytes.NewReader(raw))
	if err != nil {
		return false
	}
	req.Header.Set("content-type", "application/json")
	token := o.token()
	for _, h := range o.TokenHeaders {
		req.Header.Set(h, token)
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		agent.Log("warn", "old runtime call failed", agent.F{K: "path", V: path}, agent.F{K: "error", V: err.Error()})
		return false
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		agent.Log("warn", "old runtime call refused", agent.F{K: "path", V: path}, agent.F{K: "status", V: res.StatusCode})
		return false
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out) == nil
	}
	return true
}

// CLITokenOK checks a request to the gate's internal routes: the runtime's cli-token in one of the headers.
func (o *OldRuntime) CLITokenOK(r *http.Request) bool {
	token := o.token()
	if token == "" {
		return false
	}
	for _, h := range o.TokenHeaders {
		if v := r.Header.Get(h); v != "" && subtle.ConstantTimeCompare([]byte(v), []byte(token)) == 1 {
			return true
		}
	}
	return false
}
