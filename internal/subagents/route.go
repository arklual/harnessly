package subagents

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

// Paths are where the agents keep their transcripts: only a jsonl under one of them may be read.
type Paths struct {
	ClaudeProjects, CodexSessions string
}

func resolve(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}

// Codex says whether a path is a Codex transcript.
func (p Paths) Codex(file string) bool {
	r := resolve(file)
	return strings.HasPrefix(r, p.CodexSessions+string(filepath.Separator)) && strings.HasSuffix(r, ".jsonl")
}

// Allowed says whether a path is a transcript of either agent.
func (p Paths) Allowed(file string) bool {
	r := resolve(file)
	if !strings.HasSuffix(r, ".jsonl") {
		return false
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(r, p.ClaudeProjects+sep) || strings.HasPrefix(r, p.CodexSessions+sep)
}

// Route answers GET <platform API>/subagents?jsonlPath=<transcript>: the subagents of a Claude conversation; a Codex
// one has none ({subagents: [], supported: false}).
type Route struct {
	Lister *Lister
	Paths  Paths
	// Authorized is the old API's own check (a page session or the cli-token), SessionOK the page session the handler
	// asks for besides.
	Authorized, SessionOK func(*http.Request) bool
}

// refusal is the answer to a request the handlers turn away before reading its transcript, ok when there is none; path
// is the conversation's transcript.
func (rt *Route) refusal(r *http.Request) (status int, v any, h http.Header, path string, ok bool) {
	if !rt.Authorized(r) {
		return http.StatusUnauthorized, gate.Raw{ContentType: "application/json", Body: []byte(`{"error":"Unauthorized"}`)}, nil, "", false
	}
	if !rt.SessionOK(r) {
		status, v, h = nodejson.Answer(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
		return status, v, h, "", false
	}
	if r.Method != http.MethodGet {
		return http.StatusMethodNotAllowed, nil, http.Header{"Allow": {"GET"}}, "", false
	}
	if v := jscompat.Query(r.URL.RawQuery)["jsonlPath"]; len(v) == 1 {
		path = v[0]
	}
	if path == "" || !rt.Paths.Allowed(path) {
		status, v, h = nodejson.Answer(r, http.StatusBadRequest, map[string]string{"error": "Invalid session"}, nil)
		return status, v, h, "", false
	}
	return 0, nil, nil, path, true
}

// Answer is the whole answer, refusals included.
func (rt *Route) Answer(r *http.Request) (int, any, http.Header) {
	status, v, h, path, ok := rt.refusal(r)
	if !ok {
		return status, v, h
	}
	type answer struct {
		Subagents []Summary `json:"subagents"`
		Supported bool      `json:"supported"`
	}
	if rt.Paths.Codex(path) {
		return nodejson.Answer(r, http.StatusOK, answer{[]Summary{}, false}, nil)
	}
	return nodejson.Answer(r, http.StatusOK, answer{rt.Lister.List(path), true}, http.Header{"Cache-Control": {"no-store"}})
}

var subagentID = regexp.MustCompile(`^[\w-]+$`)

// AnswerTranscript answers GET <platform API>/subagents/<id>?jsonlPath=<transcript>: the entries of one subagent's
// transcript, flattened (every line of it is a sidechain, which the parser folds into one agent group).
func (rt *Route) AnswerTranscript(r *http.Request, id string) (int, any, http.Header) {
	status, v, h, path, ok := rt.refusal(r)
	if !ok {
		return status, v, h
	}
	if rt.Paths.Codex(path) {
		return nodejson.Answer(r, http.StatusBadRequest, map[string]string{"error": "Invalid session"}, nil)
	}
	if !subagentID.MatchString(id) {
		return nodejson.Answer(r, http.StatusBadRequest, map[string]string{"error": "Invalid subagent"}, nil)
	}
	entries := []any{}
	for _, e := range transcript.ParseFile(filepath.Join(DirFor(path), "agent-"+id+".jsonl")) {
		if e.String("type") != "agent-group" {
			entries = append(entries, e)
			continue
		}
		inner, _ := e.Get("entries")
		if list, isList := inner.([]any); isList {
			entries = append(entries, list...)
		} else {
			entries = append(entries, inner)
		}
	}
	return nodejson.Answer(r, http.StatusOK, map[string]any{"entries": entries}, http.Header{"Cache-Control": {"no-store"}})
}
