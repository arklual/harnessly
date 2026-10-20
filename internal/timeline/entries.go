package timeline

import (
	"math"
	"net/http"
	"os"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/subagents"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

// entriesPage is how many entries a page holds when the request names no limit.
const entriesPage = 256

// EntriesRoute answers GET /api/timeline/entries?jsonlPath=&beforeByte=&limit=&untilByte=: the page of a transcript
// before a byte offset, as the timeline scrolls back. The cluster answers it from its copy of the transcript first;
// the machine is asked when that copy has no such lines.
type EntriesRoute struct {
	Paths subagents.Paths
	Codex transcript.CodexOptions
	// Authorized is the old API's check: a page session or the cli-token.
	Authorized func(*http.Request) bool
}

// pastEnd is how far past a Claude transcript's end a read is still answered here.
const pastEnd = 1 << 20

var entriesKeys = []string{"jsonlPath", "beforeByte", "limit", "untilByte"}

// Takes says whether this route answers the request as the previous runtime did; the rest stays with it: a repeated
// parameter (its handler read an array), a negative limit (it answered offsets of NaN), a Claude transcript read far
// past its end (it allocated zeros up to the offset; the bytes past the end are read as zeros here too, but only so
// many).
func (e *EntriesRoute) Takes(r *http.Request) bool {
	q := jscompat.Query(r.URL.RawQuery)
	for _, k := range entriesKeys {
		if len(q[k]) > 1 {
			return false
		}
	}
	if limit := jscompat.ParseInt(q.Get("limit"), 10); limit < 0 {
		return false
	}
	path := q.Get("jsonlPath")
	if path == "" || !e.Paths.Allowed(path) || e.Paths.Codex(path) {
		return true
	}
	before := jscompat.ParseInt(q.Get("beforeByte"), 10)
	if st, err := os.Stat(path); err == nil && before > float64(st.Size()+pastEnd) {
		return false
	}
	return true
}

// offset is a byte offset read as a number, capped where no file reaches.
func offset(v float64) int64 { return int64(math.Min(v, 1<<53)) }

// Answer is the whole answer, refusals included.
func (e *EntriesRoute) Answer(r *http.Request) (int, any, http.Header) {
	if !e.Authorized(r) {
		return http.StatusUnauthorized, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}
	}
	if r.Method != http.MethodGet {
		return nodejson.Answer(r, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"}, http.Header{"Allow": {"GET"}})
	}
	q := jscompat.Query(r.URL.RawQuery)
	path := q.Get("jsonlPath")
	if path == "" {
		return nodejson.Answer(r, http.StatusBadRequest, map[string]string{"error": "jsonlPath parameter required"}, nil)
	}
	if !e.Paths.Allowed(path) {
		return nodejson.Answer(r, http.StatusForbidden, map[string]string{"error": "Path not allowed"}, nil)
	}
	before := jscompat.ParseInt(q.Get("beforeByte"), 10)
	if math.IsNaN(before) || before < 0 {
		return nodejson.Answer(r, http.StatusBadRequest, map[string]string{"error": "beforeByte parameter required"}, nil)
	}
	limit := entriesPage
	if l := jscompat.ParseInt(q.Get("limit"), 10); !math.IsNaN(l) && l != 0 {
		limit = int(math.Min(l, math.MaxInt32))
	}
	until := int64(-1)
	if u := jscompat.ParseInt(q.Get("untilByte"), 10); !math.IsNaN(u) && !math.IsInf(u, 0) && u >= 0 {
		until = offset(u)
	}
	codex := e.Paths.Codex(path)
	var page transcript.Chunk
	if codex {
		page = transcript.ReadCodexBefore(path, offset(before), limit, until, e.Codex)
	} else {
		page = transcript.ReadBefore(path, offset(before), limit)
	}
	return nodejson.Answer(r, http.StatusOK, entriesAnswer(page, codex), nil)
}

type entriesBody struct {
	Entries         []*jsvalue.Object `json:"entries"`
	StartByteOffset int64             `json:"startByteOffset"`
	HasMore         bool              `json:"hasMore"`
	// ReplaceEntries: a Codex page is read on to the calls' ends, so it replaces what the page shows from its start.
	ReplaceEntries bool `json:"replaceEntries"`
}

func entriesAnswer(page transcript.Chunk, codex bool) entriesBody {
	entries := page.Entries
	if entries == nil {
		entries = []*jsvalue.Object{}
	}
	return entriesBody{entries, page.StartByteOffset, page.HasMore, codex}
}
