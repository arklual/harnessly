package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// Registration at the service (docs/connectors.md, «Incoming webhook»): for GitHub and GitLab the person may
// give the repository (or group, or organization) and a token that may add webhooks there; the connection then puts
// its own address at the service by itself, checks every hour that it is still there (and puts it back if someone
// removed it), and takes it off when the connection is removed. Without a token the address is given by hand, as before.

// Fields of the registration.
const (
	fieldTarget  = "target"
	fieldService = "serviceURL"
	fieldToken   = "apiToken"
)

// registerEvery is how often a registered webhook is looked for at the service.
const registerEvery = time.Hour

// hookMemo is what the connection keeps of its registration (Instance.Store).
type hookMemo struct {
	Target string `json:"target"`
	ID     int64  `json:"id"`
	API    string `json:"api"`
}

func registrationFields() []connectors.Field {
	when := &connectors.When{Field: "preset", Values: []string{PresetGitHub, PresetGitLab}}
	return []connectors.Field{
		{ID: fieldTarget, Type: connectors.FieldText, Optional: true, When: when, Placeholder: "owner/repo",
			Label: connectors.T("Register at", "Зарегистрировать в"),
			Hint: connectors.T("Optional: the repository (owner/repo, group/project), or group:<path> on GitLab, org:<name> on GitHub — the webhook is put there by itself",
				"Необязательно: репозиторий (owner/repo, group/project), или group:<путь> в GitLab, org:<имя> в GitHub — вебхук поставится туда сам")},
		{ID: fieldService, Type: connectors.FieldText, Optional: true, When: when, Placeholder: "https://gitlab.example.com",
			Label: connectors.T("Service address", "Адрес сервиса"),
			Hint:  connectors.T("Your own GitLab or GitHub Enterprise; empty for gitlab.com and github.com", "Свой GitLab или GitHub Enterprise; пусто — gitlab.com и github.com")},
		{ID: fieldToken, Type: connectors.FieldPassword, Secret: true, Optional: true, When: when,
			Label: connectors.T("Token to register it", "Токен для регистрации"),
			Hint: connectors.T("A token that may add webhooks: GitLab — Maintainer of the project (Owner of a group), GitHub — admin:repo_hook (admin:org_hook)",
				"Токен с правом добавлять вебхуки: GitLab — Maintainer проекта (Owner группы), GitHub — admin:repo_hook (admin:org_hook)")},
	}
}

// selfRegistering says the connection registers its webhook itself: GitHub or GitLab with a target and a token.
func selfRegistering(in connectors.Instance) (target, token string, ok bool) {
	if p := preset(in); p != PresetGitHub && p != PresetGitLab {
		return "", "", false
	}
	target = strings.TrimSpace(in.Config[fieldTarget])
	if target == "" || in.Secret == nil {
		return "", "", false
	}
	token, err := in.Secret(fieldToken)
	if err != nil || strings.TrimSpace(token) == "" {
		return "", "", false
	}
	return target, strings.TrimSpace(token), true
}

// Interval: the registration is looked after once an hour.
func (hookConn) Interval(connectors.Instance) time.Duration { return registerEvery }

// Poll keeps the webhook registered at the service when the connection registers itself; it never emits events (they
// come by Receive). An error says what the service answered, so the page shows why the address is not there.
func (h hookConn) Poll(ctx context.Context, in connectors.Instance, from connectors.Cursor) ([]connectors.Event, connectors.Cursor, error) {
	target, token, ok := selfRegistering(in)
	if !ok {
		return nil, from, nil
	}
	addr := address(in)
	if addr == "" {
		return nil, from, nil // not linked to a hub yet: the address is not known
	}
	var memo hookMemo
	if in.Load != nil {
		_ = in.Load(&memo)
	}
	api := apiBase(in)
	if memo.ID != 0 && memo.Target == target && memo.API == api {
		there, err := hookExists(ctx, in, api, target, token, memo.ID)
		if err != nil || there {
			return nil, from, err
		}
	}
	id, err := registerHook(ctx, in, api, target, token, addr)
	if err != nil {
		return nil, from, err
	}
	if in.Store != nil {
		if err := in.Store(hookMemo{Target: target, ID: id, API: api}); err != nil {
			return nil, from, err
		}
	}
	return nil, from, nil
}

// Forget takes the registered webhook off the service when the connection goes; best effort.
func (hookConn) Forget(ctx context.Context, in connectors.Instance) {
	target, token, ok := selfRegistering(in)
	if !ok || in.Load == nil {
		return
	}
	var memo hookMemo
	if in.Load(&memo) != nil || memo.ID == 0 || memo.Target != target {
		return
	}
	_, _ = call(ctx, in, http.MethodDelete, hookURL(in, memo.API, target, memo.ID), token, nil, nil)
}

// apiBase is the API address of the service.
func apiBase(in connectors.Instance) string {
	svc := strings.TrimRight(strings.TrimSpace(in.Config[fieldService]), "/")
	if preset(in) == PresetGitLab {
		if svc == "" {
			svc = "https://gitlab.com"
		}
		return svc + "/api/v4"
	}
	switch {
	case svc == "", svc == "https://github.com", svc == "https://api.github.com":
		return "https://api.github.com"
	case strings.HasSuffix(svc, "/api/v3"):
		return svc
	}
	return svc + "/api/v3"
}

// hooksURL is where the webhooks of the target are listed and added.
func hooksURL(in connectors.Instance, api, target string) string {
	if preset(in) == PresetGitLab {
		if g, ok := strings.CutPrefix(target, "group:"); ok {
			return api + "/groups/" + url.PathEscape(g) + "/hooks"
		}
		return api + "/projects/" + url.PathEscape(target) + "/hooks"
	}
	if o, ok := strings.CutPrefix(target, "org:"); ok {
		return api + "/orgs/" + url.PathEscape(o) + "/hooks"
	}
	return api + "/repos/" + target + "/hooks"
}

func hookURL(in connectors.Instance, api, target string, id int64) string {
	return fmt.Sprintf("%s/%d", hooksURL(in, api, target), id)
}

// registerHook puts the connection's address at the service with the events the presets read, signed or tokened with
// the connection's secret, and gives the webhook's id.
func registerHook(ctx context.Context, in connectors.Instance, api, target, token, addr string) (int64, error) {
	secret, err := in.Secret("secret")
	if err != nil || secret == "" {
		return 0, connectors.Sayf("The connection has no secret to sign the webhook with: reissue it")
	}
	var body any
	if preset(in) == PresetGitLab {
		body = map[string]any{"url": addr, "token": secret, "enable_ssl_verification": true,
			"push_events": true, "merge_requests_events": true, "pipeline_events": true, "job_events": false,
			"note_events": true, "issues_events": true, "releases_events": true}
	} else {
		body = map[string]any{"name": "web", "active": true,
			"events": []string{"pull_request", "pull_request_review", "issues", "issue_comment", "check_suite", "workflow_run", "push", "release"},
			"config": map[string]string{"url": addr, "content_type": "json", "secret": secret, "insecure_ssl": "0"}}
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if _, err := call(ctx, in, http.MethodPost, hooksURL(in, api, target), token, body, &out); err != nil {
		return 0, err
	}
	if out.ID == 0 {
		return 0, connectors.Sayf("The service took the webhook but gave no id")
	}
	return out.ID, nil
}

// hookExists says the registered webhook is still at the service.
func hookExists(ctx context.Context, in connectors.Instance, api, target, token string, id int64) (bool, error) {
	status, err := call(ctx, in, http.MethodGet, hookURL(in, api, target, id), token, nil, nil)
	if status == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

// call is one request to the service's API with the person's token; an error is words for the page, never the token.
func call(ctx context.Context, in connectors.Instance, method, u, token string, body, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, err
	}
	if preset(in) == PresetGitLab {
		req.Header.Set("PRIVATE-TOKEN", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := in.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return 0, connectors.Sayf("The service did not answer: %s", req.URL.Host)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return res.StatusCode, connectors.Sayf("The service refused the token (%d): it needs the right to add webhooks there", res.StatusCode)
	case res.StatusCode == http.StatusNotFound:
		return res.StatusCode, connectors.Sayf("The service does not know %s, or the token does not see it", req.URL.Path)
	case res.StatusCode >= 300:
		return res.StatusCode, connectors.Sayf("The service answered %d", res.StatusCode)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return res.StatusCode, connectors.Sayf("The service answered something unreadable")
		}
	}
	return res.StatusCode, nil
}
