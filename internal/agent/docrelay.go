package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/docsecret"
)

// The runtime and the CLI reach the cluster's documents through this agent: a document is published
// ("doc.publish") and its access is changed ("doc.acl") by an event with a request id, and the cluster answers with the
// op "doc.result" {requestId, status, body}. Loopback routes, same token as the doc threads:
//
//	POST /docs/publish {title, format: "md"|"html", content, slug?, visibility?, note?, source?{path, cwd, conversation}}
//	POST /docs/acl     {slug, method, route, body?}
//
// Both answer {status, body}: the cluster's status and body as they were. The agent itself answers 503 with no link,
// 422 for a document that holds a secret (the first line is named) and 504 when the cluster does not answer.

const (
	docRelayTO      = 60 * time.Second
	docPublishMax   = 4 * 1024 * 1024
	docAclMax       = 64 * 1024
	docContentLimit = 1_000_000
)

// DocResult is the cluster's answer to doc.publish and doc.acl.
type DocResult struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

type evDocPublish struct {
	Event      string          `json:"event"`
	RequestID  string          `json:"requestId"`
	Title      string          `json:"title"`
	Format     string          `json:"format"`
	Content    string          `json:"content"`
	Slug       *string         `json:"slug,omitempty"`
	Visibility string          `json:"visibility"`
	Note       *string         `json:"note,omitempty"`
	Source     json.RawMessage `json:"source,omitempty"`
}

type evDocAcl struct {
	Event     string          `json:"event"`
	RequestID string          `json:"requestId"`
	Slug      string          `json:"slug"`
	Method    string          `json:"method"`
	Route     string          `json:"route"`
	Body      json.RawMessage `json:"body,omitempty"`
}

func (a *Agent) settleDoc(requestID string, res DocResult) {
	a.mu.Lock()
	ch := a.pendingDocs[requestID]
	a.mu.Unlock()
	if ch != nil {
		select {
		case ch <- res:
		default:
		}
	}
}

// askCluster sends the event and waits up to wait for its doc.result (or notify.result).
func (a *Agent) askCluster(id string, ev any, wait time.Duration) DocResult {
	ch := make(chan DocResult, 1)
	a.mu.Lock()
	a.pendingDocs[id] = ch
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.pendingDocs, id)
		a.mu.Unlock()
	}()
	a.broadcast(ev)
	select {
	case res := <-ch:
		return res
	case <-time.After(wait):
		return DocResult{Status: http.StatusGatewayTimeout, Body: json.RawMessage(`{"error":"the cluster did not answer in time"}`)}
	}
}

// docRelayRoute serves /docs/publish and /docs/acl.
func (a *Agent) docRelayRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	publish := r.URL.Path == "/docs/publish"
	limit := int64(docAclMax)
	if publish {
		limit = docPublishMax
	}
	obj := readObject(r, limit)
	if obj == nil {
		reply(w, 400, map[string]string{"error": "bad json"})
		return
	}
	if !a.connected() {
		reply(w, 503, map[string]string{"error": "the platform is not connected, try again later"})
		return
	}
	id := newRequestID()
	if !publish {
		slug := str(obj, "slug", 128)
		method, _ := field[string](obj, "method")
		route, _ := field[string](obj, "route")
		if slug == nil || method == "" || !strings.HasPrefix(route, "/") {
			reply(w, 400, map[string]string{"error": "slug, method and route are required"})
			return
		}
		reply(w, 200, a.askCluster(id, evDocAcl{Event: "doc.acl", RequestID: id, Slug: *slug, Method: strings.ToUpper(method), Route: route, Body: obj["body"]}, docRelayTO))
		return
	}
	title := str(obj, "title", 300)
	content, _ := field[string](obj, "content")
	format, _ := field[string](obj, "format")
	if title == nil || strings.TrimSpace(content) == "" || (format != "md" && format != "html") {
		reply(w, 400, map[string]string{"error": "title, content and format (md or html) are required"})
		return
	}
	if len(content) > docContentLimit {
		reply(w, 413, map[string]string{"error": "a document is at most 1 MB"})
		return
	}
	if hit, found := docsecret.Find(content); found {
		reply(w, 422, map[string]any{"error": "secret", "kind": hit.Kind, "line": hit.Line})
		return
	}
	visibility, _ := field[string](obj, "visibility")
	if visibility == "" {
		visibility = "private"
	}
	ev := evDocPublish{Event: "doc.publish", RequestID: id, Title: *title, Format: format, Content: content, Slug: str(obj, "slug", 128), Visibility: visibility, Note: str(obj, "note", 200), Source: obj["source"]}
	reply(w, 200, a.askCluster(id, ev, docRelayTO))
}
