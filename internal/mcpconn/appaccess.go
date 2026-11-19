package mcpconn

// What the apps (a view through @harnessly/app-sdk, hl.connections) need of the person's connections that the
// hub does not: which group a catalog entry is in, what kind of connection a server is, whether a tool or an API request
// only reads, the tools with their hints, a way to hear that the connections changed, and a scrub that keeps a key out
// of an answer. Kept apart from the hub (what agents see) and the catalog (what the page lists).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// The kinds of connection an app sees, as Settings → Connections groups them.
const (
	AppKindMCP   = "mcp"   // a remote MCP server
	AppKindLocal = "local" // an MCP server the runtime starts on this machine
	AppKindAPI   = "api"   // a service with an HTTP API and no MCP server
)

// AppKindOf is the kind of a connection for an app: mcp, local or api.
func AppKindOf(srv Server) string {
	switch srv.Transport {
	case Stdio:
		return AppKindLocal
	case API:
		return AppKindAPI
	}
	return AppKindMCP
}

// CatalogKind is the group of catalog entry id (mail, tasks, …), or of the person's own service of that id (the one the
// service read last); "" for an id nobody knows.
func CatalogKind(id string) string {
	if e, ok := catalogEntry(id); ok {
		return string(e.Kind)
	}
	return customKindOf(id)
}

// KnownKind says whether k is one of the catalog's groups.
func KnownKind(k string) bool { return slices.Contains(Kinds, Kind(k)) }

// AppTool is a tool of a connection as an app sees it.
type AppTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	// ReadOnly: the server marks the tool readOnlyHint, so it changes nothing. A tool with no such mark is taken as one
	// that may change something.
	ReadOnly bool `json:"readOnly"`
}

type appToolCache struct {
	at    time.Time
	tools []AppTool
}

var (
	appToolsMu sync.Mutex
	appTools   = map[string]appToolCache{}
)

const appToolsFresh = 5 * time.Minute

// AppTools lists the tools of a connection with their read-only mark, kept for a few minutes.
func (s *Service) AppTools(ctx context.Context, id string) ([]AppTool, error) {
	appToolsMu.Lock()
	c, ok := appTools[id]
	appToolsMu.Unlock()
	if ok && time.Since(c.at) < appToolsFresh {
		return c.tools, nil
	}
	var tools []AppTool
	_, err := s.withServer(ctx, id, func(c caller) (json.RawMessage, error) {
		tools = nil
		cursor := ""
		for range maxToolPages {
			var params any
			if cursor != "" {
				params = map[string]string{"cursor": cursor}
			}
			raw, err := c.call(ctx, "tools/list", params)
			if err != nil {
				return nil, err
			}
			var page struct {
				Tools []struct {
					Name        string          `json:"name"`
					Description string          `json:"description"`
					InputSchema json.RawMessage `json:"inputSchema"`
					Annotations struct {
						ReadOnlyHint *bool `json:"readOnlyHint"`
					} `json:"annotations"`
				} `json:"tools"`
				NextCursor string `json:"nextCursor"`
			}
			if err := json.Unmarshal(raw, &page); err != nil {
				return nil, notMCP("bad tools/list result")
			}
			for _, t := range page.Tools {
				tools = append(tools, AppTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
					ReadOnly: t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint})
			}
			if page.NextCursor == "" || page.NextCursor == cursor {
				break
			}
			cursor = page.NextCursor
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	appToolsMu.Lock()
	appTools[id] = appToolCache{at: time.Now(), tools: tools}
	appToolsMu.Unlock()
	return tools, nil
}

// apiReadPaths are the requests of an API that read although the method is not GET (a search whose query is a body):
// "<METHOD> <path>", the path whole or ending in * for a prefix. GET and HEAD read everywhere.
var apiReadPaths = map[string][]string{
	"yandex-tracker": {"POST /issues/_search", "POST /issues/_count"},
}

// APIReads says whether a request to the API of catalog entry id only reads: GET and HEAD, and the searches the entry
// lists. Anything else may change something at the service.
func APIReads(catalogID, method, path string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" || method == http.MethodGet || method == http.MethodHead {
		return true
	}
	path = strings.SplitN(strings.TrimSpace(path), "?", 2)[0]
	path = "/" + strings.Trim(path, "/")
	for _, rule := range apiReadPaths[catalogID] {
		m, p, _ := strings.Cut(rule, " ")
		if m != method {
			continue
		}
		if p == path || strings.HasSuffix(p, "*") && strings.HasPrefix(path, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

// OnChange calls fn after the servers were written (added, removed, signed in, turned on or off, checked): what the
// apps' event `connections` is made from. fn runs on the writer's goroutine and must not block. It returns the function
// that stops the calls.
func (s *Service) OnChange(fn func()) (cancel func()) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if s.subs == nil {
		s.subs = map[int]func(){}
	}
	id := s.nextSub
	s.nextSub++
	s.subs[id] = fn
	return func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		delete(s.subs, id)
	}
}

func (s *Service) notifyChange() {
	s.subMu.Lock()
	fns := make([]func(), 0, len(s.subs))
	for _, fn := range s.subs {
		fns = append(fns, fn)
	}
	s.subMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Redact takes every key of connection id out of b (an answer headed for a view): the typed keys of a local server and
// an API, a typed header and its token, an OAuth token and the client secret. A key shorter than 8 characters is left,
// since it would cut ordinary words. The answer of an API already goes through this; a tool's answer and an error may
// echo a key as well.
func (s *Service) Redact(id string, b []byte) []byte {
	cr, err := s.readCred(id)
	if err != nil {
		return b
	}
	var keys []string
	for _, v := range cr.Env {
		keys = append(keys, v)
	}
	if cr.Header != "" {
		keys = append(keys, cr.Header)
		if _, tail, ok := strings.Cut(cr.Header, " "); ok {
			keys = append(keys, tail)
		}
	}
	if o := cr.OAuth; o != nil {
		keys = append(keys, o.AccessToken, o.RefreshToken, o.ClientSecret)
	}
	for _, k := range keys {
		if len(k) >= 8 {
			b = bytes.ReplaceAll(b, []byte(k), []byte("[key]"))
			// A key inside a JSON string is written with escapes.
			if esc, err := json.Marshal(k); err == nil && len(esc) > 2 {
				b = bytes.ReplaceAll(b, esc[1:len(esc)-1], []byte("[key]"))
			}
		}
	}
	return b
}
