package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/bgmodel"
	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/providers"
)

// inboxTriageSeen marks a machine Inbox has asked to triage (through the hub, whose «inbox-triage» rollout switch
// decides who). Settings show «Inbox triage» only there, so other machines see no new row.
func inboxTriageSeen(dataDir string) string { return filepath.Join(dataDir, "v2", "inbox-triage-seen") }

func inboxTriageEnabled(dataDir string) bool {
	_, err := os.Stat(inboxTriageSeen(dataDir))
	return err == nil
}

func inboxGPTModels() []extproviders.Model {
	p, _ := providers.Find("gpt")
	models := make([]extproviders.Model, 0, len(p.Models))
	for _, m := range p.Models {
		// External providers on the shared bridge use <provider>--<model>; never send data to one through @gpt.
		if strings.Contains(m.ID, "--") {
			continue
		}
		models = append(models, extproviders.Model{ID: m.ID, Label: m.Label})
	}
	return models
}

func inboxGPTModel(id string) bool {
	for _, m := range inboxGPTModels() {
		if m.ID == id {
			return true
		}
	}
	return false
}

func inboxTriageRoutes(d deps, proxy *providers.GPTProxy) []gate.Route {
	runner := bgRunnerFor(d)
	if proxy != nil {
		runner.InboxGPT = func(ctx context.Context, choice bgmodel.Choice, call bgmodel.Call) (string, error) {
			return inboxGPTComplete(ctx, proxy, choice, call)
		}
	}
	var busy sync.Mutex
	h := func(w http.ResponseWriter, r *http.Request) {
		claims := d.Claims(r)
		if claims["aud"] != hubproto.InboxTriageAudience || claims["sub"] == "" || claims["sub"] == nil {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method-not-allowed"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, hubproto.InboxTriageMaxBody)
		raw, err := io.ReadAll(r.Body)
		var b hubproto.InboxTriageInput
		if err != nil || json.Unmarshal(raw, &b) != nil || !b.Valid() {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid-request"})
			return
		}
		if !inboxTriageEnabled(d.DataDir) {
			_ = os.MkdirAll(filepath.Dir(inboxTriageSeen(d.DataDir)), 0o700)
			_ = os.WriteFile(inboxTriageSeen(d.DataDir), nil, 0o600)
		}
		if !busy.TryLock() {
			secretJSON(w, http.StatusTooManyRequests, map[string]string{"error": "triage-busy"})
			return
		}
		defer busy.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		text, used, err := runner.Run(ctx, bgmodel.FeatureInboxTriage, bgmodel.Call{System: b.System, Prompt: b.Prompt, MaxTokens: bgmodel.MaxMaxTokens, Timeout: time.Minute})
		out := hubproto.InboxTriageOutput{Used: used, Text: text}
		if err != nil {
			out.Error, out.Text = bgmodel.ReasonOf(err), ""
		}
		secretJSON(w, http.StatusOK, out)
	}
	return []gate.Route{{Match: func(r *http.Request) bool { return r.URL.Path == platformAPI+hubproto.InboxTriageSuffix }, Handler: http.HandlerFunc(h)}}
}

type inboxGPTBridge interface {
	Ready() bool
	Ensure(context.Context) error
	Endpoint() (string, string, bool)
}

func inboxGPTComplete(ctx context.Context, proxy inboxGPTBridge, choice bgmodel.Choice, call bgmodel.Call) (string, error) {
	if !inboxGPTModel(choice.Model) {
		return "", &bgmodel.Error{Reason: bgmodel.ReasonNoProvider}
	}
	if !proxy.Ready() {
		return "", &bgmodel.Error{Reason: bgmodel.ReasonUnauthorized}
	}
	if err := proxy.Ensure(ctx); err != nil {
		return "", &bgmodel.Error{Reason: bgmodel.ReasonUnreachable}
	}
	base, keyFile, ok := proxy.Endpoint()
	if !ok {
		return "", &bgmodel.Error{Reason: bgmodel.ReasonUnreachable}
	}
	key, err := os.ReadFile(keyFile)
	if err != nil || len(bytes.TrimSpace(key)) == 0 {
		return "", &bgmodel.Error{Reason: bgmodel.ReasonUnauthorized}
	}
	return inboxGPTRequest(ctx, base, string(bytes.TrimSpace(key)), choice, call)
}

// Responses through the managed loopback bridge, with no session, conversation or tools.
func inboxGPTRequest(ctx context.Context, base, key string, choice bgmodel.Choice, call bgmodel.Call) (string, error) {
	body := map[string]any{
		"model": choice.Model, "instructions": call.System, "store": false, "stream": false,
		"max_output_tokens": call.MaxTokens,
		"input":             []map[string]any{{"type": "message", "role": "user", "content": []map[string]string{{"type": "input_text", "text": call.Prompt}}}},
	}
	if choice.Effort != "" {
		body["reasoning"] = map[string]string{"effort": choice.Effort}
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/responses", bytes.NewReader(raw))
	if err != nil {
		return "", &bgmodel.Error{Reason: bgmodel.ReasonUnreachable}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	hc := http.Client{Transport: &http.Transport{DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", &bgmodel.Error{Reason: bgmodel.ReasonTimeout}
		}
		return "", &bgmodel.Error{Reason: bgmodel.ReasonUnreachable}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		reason := bgmodel.ReasonRejected
		switch res.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			reason = bgmodel.ReasonUnauthorized
		case http.StatusTooManyRequests:
			reason = bgmodel.ReasonRateLimited
		}
		return "", &bgmodel.Error{Reason: reason, Status: res.StatusCode}
	}
	var result struct {
		Status string `json:"status"`
		Model  string `json:"model"`
		Output []struct{ Content []struct{ Type, Text string } }
	}
	raw, err = io.ReadAll(io.LimitReader(res.Body, hubproto.InboxTriageMaxReply+1))
	if err != nil || len(raw) > hubproto.InboxTriageMaxReply || json.Unmarshal(raw, &result) != nil || result.Status != "completed" || result.Model != choice.Model {
		return "", &bgmodel.Error{Reason: bgmodel.ReasonRejected}
	}
	var text strings.Builder
	for _, o := range result.Output {
		for _, c := range o.Content {
			if c.Type == "output_text" {
				text.WriteString(c.Text)
			}
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", &bgmodel.Error{Reason: bgmodel.ReasonEmpty}
	}
	return text.String(), nil
}
