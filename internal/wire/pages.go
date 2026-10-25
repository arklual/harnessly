package wire

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// Prefix is the path of the v2 API; the runtime answers it under /agents as well, like the rest of its API.
const Prefix = "/api/v2/"

// Matches reports whether r is for the v2 API.
func Matches(r *http.Request) bool {
	_, ok := v2Path(r)
	return ok
}

// v2Path is r's path below /api/v2/.
func v2Path(r *http.Request) (string, bool) {
	p := strings.TrimPrefix(r.URL.Path, "/agents")
	return strings.CutPrefix(p, Prefix)
}

// ServeHTTP routes /api/v2/*. A page that cannot answer sends a Refusal with its status: 400 bad-request, 404
// not-found, 413 attachment-too-large, 415 attachment-type, 503 timeout, 500 internal. The machine parameter is the
// cluster's: a runtime is one machine and does not read it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := v2Path(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if guarded(p, r) && crossSite(r) {
		refuse(w, RefuseForbidden)
		return
	}
	switch {
	case p == "hello":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, Hello{Proto: Proto, Version: s.Version, Features: s.Features})
	case p == "machines":
		s.machines(w, r)
	case p == "me":
		s.me(w, r)
	case p == "me/settings":
		s.meSettings(w, r)
	case p == "socket":
		s.Socket(w, r)
	case strings.HasPrefix(p, "cmd/"):
		s.Cmd(w, r, Op(strings.TrimPrefix(p, "cmd/")))
	case p == "uploads":
		s.Upload(w, r)
	case strings.HasPrefix(p, "uploads/"):
		s.uploadFile(w, r, strings.TrimPrefix(p, "uploads/"))
	case p == "dirs":
		s.dirs(w, r)
	case p == "sessions":
		s.sessions(w, r)
	case p == "voice":
		s.voice(w, r)
	case strings.HasPrefix(p, "designly/"):
		s.designly(w, r, strings.TrimPrefix(p, "designly/"))
	case p == "terminal":
		s.terminalSocket(w, r)
	case p == "apps":
		s.apps(w, r)
	case p == "runtime-settings":
		s.runtimeSettings(w, r)
	case strings.HasPrefix(p, "agent-settings/"):
		s.agentSettings(w, r, strings.TrimPrefix(p, "agent-settings/"))
	case strings.HasPrefix(p, "machine/"):
		s.machineTool(w, r, strings.TrimPrefix(p, "machine/"))
	case strings.HasPrefix(p, "conversations/"):
		s.page(w, r, strings.Split(strings.TrimPrefix(p, "conversations/"), "/"))
	default:
		refuse(w, RefuseNotFound)
	}
}

// machines serves GET /api/v2/machines of a runtime on its own: the fleet is this one machine (spec §5). In the cluster
// the gateway answers this path itself, from the fleet and its heartbeats.
func (s *Server) machines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Limits.CmdBudgetMs)*time.Millisecond)
	defer cancel()
	snap, err := s.Machine.Snapshot(ctx, false)
	if err != nil {
		agent.Log("warn", "v2: no machine for the fleet", agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
		return
	}
	writeJSON(w, http.StatusOK, []Machine{snap.Machine})
}

// dirs serves GET /api/v2/dirs: the folders the machine offers for a new project.
func (s *Server) dirs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.Dirs == nil {
		refuse(w, RefuseNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Limits.CmdBudgetMs)*time.Millisecond)
	defer cancel()
	list, err := s.Dirs(ctx)
	if err != nil {
		agent.Log("warn", "v2: no folders to offer", agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
		return
	}
	out := Dirs(list)
	if out == nil {
		out = Dirs{}
	}
	if verr := out.Validate(); verr != nil {
		agent.Log("error", "v2: the folders were out of contract", agent.F{K: "error", V: verr.Error()})
		refuse(w, RefuseInternal)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// sessions serves GET /api/v2/sessions: the agent sessions on the machine the person may continue.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.Sessions == nil {
		refuse(w, RefuseNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Limits.CmdBudgetMs)*time.Millisecond)
	defer cancel()
	list, err := s.Sessions(ctx)
	if err != nil {
		agent.Log("warn", "v2: no sessions to offer", agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
		return
	}
	out := AgentSessions(list)
	if out == nil {
		out = AgentSessions{}
	}
	if verr := out.Validate(); verr != nil {
		agent.Log("error", "v2: the sessions were out of contract", agent.F{K: "error", V: verr.Error()})
		refuse(w, RefuseInternal)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

var refusalStatus = map[RefuseCode]int{
	RefuseBadRequest: http.StatusBadRequest, RefuseConflict: http.StatusConflict, RefuseNotFound: http.StatusNotFound, RefuseForbidden: http.StatusForbidden, RefuseAttachmentTooLarge: http.StatusRequestEntityTooLarge,
	RefuseAttachmentType: http.StatusUnsupportedMediaType, RefuseTimeout: http.StatusServiceUnavailable, RefuseInternal: http.StatusInternalServerError,
}

func refuse(w http.ResponseWriter, code RefuseCode) {
	status := refusalStatus[code]
	if status == 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, Refused(code))
}

// page serves the pages of a conversation: turns before a turn, the entries of one turn, the whole output of one
// entry.
func (s *Server) page(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) >= 2 && parts[1] == "terminals" {
		if !ValidConvID(parts[0]) {
			refuse(w, RefuseBadRequest)
			return
		}
		s.terminals(w, r, parts[0], parts[2:])
		return
	}
	if len(parts) == 2 && parts[1] == "side-chat" {
		s.sideChat(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "handoff-draft" {
		s.handoffDraft(w, r, parts[0])
		return
	}
	saving := len(parts) == 2 && parts[1] == "file" && r.Method == http.MethodPut
	if r.Method != http.MethodGet && !saving {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if len(parts) < 2 || !ValidConvID(parts[0]) {
		refuse(w, RefuseBadRequest)
		return
	}
	id := parts[0]
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Limits.CmdBudgetMs)*time.Millisecond)
	defer cancel()
	var v interface{ Validate() error }
	var err error
	switch {
	case len(parts) == 2 && (parts[1] == "files" || parts[1] == "file"):
		s.files(ctx, w, r, id, parts[1] == "file")
		return
	case len(parts) == 2 && parts[1] == "shot":
		s.shot(ctx, w, r, id)
		return
	case len(parts) == 2 && parts[1] == "tasks":
		s.tasks(ctx, w, r, id)
		return
	case len(parts) == 2 && parts[1] == "context":
		s.context(ctx, w, r, id)
		return
	case len(parts) == 2 && parts[1] == "legacy":
		s.legacy(ctx, w, r, id)
		return
	case len(parts) == 2 && parts[1] == "open":
		s.open(ctx, w, r, id)
		return
	case len(parts) == 2 && parts[1] == "open-refs":
		s.openRefs(ctx, w, r, id)
		return
	case len(parts) == 2 && parts[1] == "turns":
		q := r.URL.Query()
		before, limit := q.Get("before"), TurnsPageDefault
		if l := q.Get("limit"); l != "" {
			n, perr := strconv.Atoi(l)
			if perr != nil || n < 1 || n > TurnsPageMax {
				refuse(w, RefuseBadRequest)
				return
			}
			limit = n
		}
		if before != "" && !ValidTurnID(before) {
			refuse(w, RefuseBadRequest)
			return
		}
		var page TurnsPage
		page, err = s.Service.Turns(ctx, id, before, limit)
		page.Entries = trimOutputs(page.Entries, ToolOutputMax)
		v = page
	case len(parts) == 2 && parts[1] == "rewind" && s.Rewind != nil:
		entry := r.URL.Query().Get("entryId")
		if !ValidTurnID(entry) {
			refuse(w, RefuseBadRequest)
			return
		}
		v, err = s.Rewind.Plan(ctx, id, entry)
	case len(parts) == 4 && parts[1] == "rewinds" && parts[3] == "turns" && s.Rewind != nil:
		n, perr := strconv.Atoi(parts[2])
		if perr != nil || n < 0 || n >= RewindsMax {
			refuse(w, RefuseBadRequest)
			return
		}
		var page TurnsPage
		page, err = s.Rewind.Rewound(ctx, id, n)
		page.Entries = trimOutputs(page.Entries, ToolOutputMax)
		v = page
	case len(parts) == 4 && parts[1] == "turns" && parts[3] == "entries":
		if !ValidTurnID(parts[2]) {
			refuse(w, RefuseBadRequest)
			return
		}
		var page EntriesPage
		page, err = s.Service.TurnEntries(ctx, id, parts[2])
		page.Entries = trimOutputs(page.Entries, ToolOutputMax)
		v = page
	case len(parts) == 4 && parts[1] == "entries" && parts[3] == "output":
		if !ValidEntryID(parts[2]) {
			refuse(w, RefuseBadRequest)
			return
		}
		v, err = s.Service.EntryOutput(ctx, id, parts[2])
	default:
		refuse(w, RefuseNotFound)
		return
	}
	switch {
	case err != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case err != nil && ctx.Err() != nil:
		refuse(w, RefuseTimeout)
	case err != nil:
		agent.Log("warn", "v2: page failed", agent.F{K: "path", V: r.URL.Path}, agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		if verr := v.Validate(); verr != nil {
			agent.Log("error", "v2: a page answered out of contract", agent.F{K: "path", V: r.URL.Path}, agent.F{K: "error", V: verr.Error()})
			refuse(w, RefuseInternal)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

// --- uploads -----------------------------------------------------------------------------------------------------

// Uploads keeps the files the page uploads before it sends the message that carries them: <dir>/<id> and its
// AttachmentRef in <dir>/<id>.json.
type Uploads struct {
	Dir    string
	Limits Limits
}

// imageMimes are the images an agent takes pasted; any other type is a file.
var imageMimes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// Upload serves PUT /api/v2/uploads?name=<file name>: the body is the file, Content-Type its type. It answers the
// AttachmentRef, or a Refusal: bad-request without a name, attachment-type for a type that does not parse,
// attachment-too-large past limits.maxImageBytes or maxFileBytes.
func (s *Server) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ref, code := s.Uploads.Put(r.URL.Query().Get("name"), r.Header.Get("Content-Type"), r.ContentLength, r.Body)
	if code != "" {
		refuse(w, code)
		return
	}
	writeJSON(w, http.StatusOK, ref)
}

// uploadFile serves GET /api/v2/uploads/{id}: the file an upload holds, for the page's thumbnails. An image is shown,
// anything else is a download; neither runs as a page of the app.
func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ref, path, ok := s.Uploads.Get(id)
	if !ok {
		refuse(w, RefuseNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		refuse(w, RefuseNotFound)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, max-age=86400, immutable")
	if imageMimes[ref.Mime] {
		h.Set("Content-Type", ref.Mime)
	} else {
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": ref.Name}))
	}
	http.ServeContent(w, r, "", time.Time{}, f)
}

// Put stores one upload; size is the declared length, -1 when unknown.
func (u *Uploads) Put(name, contentType string, size int64, body io.Reader) (AttachmentRef, RefuseCode) {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == "/" || len(name) > 255 || !utf8.ValidString(name) {
		return AttachmentRef{}, RefuseBadRequest
	}
	typ := "application/octet-stream"
	if contentType != "" {
		mt, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return AttachmentRef{}, RefuseAttachmentType
		}
		typ = mt
	}
	ref := AttachmentRef{ID: newUploadID(), Name: name, Kind: AttachmentFile, Mime: typ}
	limit := u.Limits.MaxFileBytes
	if imageMimes[typ] {
		ref.Kind, limit = AttachmentImage, u.Limits.MaxImageBytes
	}
	if size > limit {
		return AttachmentRef{}, RefuseAttachmentTooLarge
	}
	if err := os.MkdirAll(u.Dir, 0o700); err != nil {
		return AttachmentRef{}, RefuseInternal
	}
	f, err := os.CreateTemp(u.Dir, ".up-*")
	if err != nil {
		return AttachmentRef{}, RefuseInternal
	}
	defer os.Remove(f.Name())
	n, err := io.Copy(f, io.LimitReader(body, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return AttachmentRef{}, RefuseBadRequest
	case n > limit:
		return AttachmentRef{}, RefuseAttachmentTooLarge
	}
	meta, _ := json.Marshal(ref)
	if err := writeFileAtomic(filepath.Join(u.Dir, ref.ID+".json"), meta); err != nil {
		return AttachmentRef{}, RefuseInternal
	}
	if err := os.Rename(f.Name(), filepath.Join(u.Dir, ref.ID+uploadExt(ref))); err != nil {
		return AttachmentRef{}, RefuseInternal
	}
	return ref, ""
}

// imageExts name a picture's file by its type: Claude takes a pasted path for a picture only by its extension, and a
// bare <id> went in as text — the message's words were lost behind it.
var imageExts = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

var fileExtRe = regexp.MustCompile(`^\.[a-z0-9]{1,8}$`)

// uploadExt is the extension an upload's file carries: its picture type's, else its name's when it is a plain one.
func uploadExt(ref AttachmentRef) string {
	if ext, ok := imageExts[ref.Mime]; ok {
		return ext
	}
	if ext := strings.ToLower(filepath.Ext(ref.Name)); fileExtRe.MatchString(ext) {
		return ext
	}
	return ""
}

// Get is an upload by its id and the file that holds it.
func (u *Uploads) Get(id string) (ref AttachmentRef, path string, ok bool) {
	if !strings.HasPrefix(id, "up_") || strings.ContainsAny(id, "/\\.") {
		return ref, "", false
	}
	raw, err := os.ReadFile(filepath.Join(u.Dir, id+".json"))
	if err != nil || json.Unmarshal(raw, &ref) != nil || ref.ID != id {
		return AttachmentRef{}, "", false
	}
	named := filepath.Join(u.Dir, id+uploadExt(ref))
	if _, err := os.Stat(named); err != nil {
		// An upload from before the files had extensions: it gets one, so its picture still goes in as a picture.
		bare := filepath.Join(u.Dir, id)
		if named == bare || os.Link(bare, named) != nil {
			return ref, bare, true
		}
	}
	return ref, named, true
}

func newUploadID() string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	return "up_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
}

// --- budgets -----------------------------------------------------------------------------------------------------

// The budgets of the snapshots (§2), in bytes of raw JSON: a convs snapshot ConvsBudget per 100 conversations (never
// less than that), a conv snapshot ConvBudget.
const (
	ConvsBudget = 72 << 10 // was 64: lastPromptAt is 30 B a record
	ConvBudget  = 256 << 10
)

// envelope is room for the snapshot frame around its data.
const envelope = 128

func size(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}

// ConvsBudgetFor is the budget of a convs snapshot of n conversations.
func ConvsBudgetFor(n int) int { return max(ConvsBudget, ConvsBudget*n/100) }

// FitConvs keeps a convs snapshot within its budget: the turn snippets get shorter (they are one line of the
// sidebar), then, for records at their largest, the titles. The events that follow carry the whole records.
func FitConvs(s ConvsSnapshot) ConvsSnapshot {
	budget := ConvsBudgetFor(len(s.Conversations)) - envelope
	if size(s) <= budget {
		return s
	}
	out := s
	out.Conversations = append([]Conversation(nil), s.Conversations...)
	for _, n := range []int{80, 40, 0} {
		for i := range out.Conversations {
			c := &out.Conversations[i]
			if c.LastTurn != nil {
				t := *c.LastTurn
				t.Snippet = cutRunes(t.Snippet, n)
				c.LastTurn = &t
			}
		}
		if size(out) <= budget {
			return out
		}
	}
	for _, n := range []int{40, 20} {
		for i := range out.Conversations {
			out.Conversations[i].Title = cutRunes(out.Conversations[i].Title, n)
		}
		if size(out) <= budget {
			break
		}
	}
	return out
}

// FitConv keeps a conv snapshot within ConvBudget. Step by step, until it fits: tool outputs are cut to
// ToolOutputMax, then shorter; the steps of the older full turns go (their answer entries stay, a folded turn needs
// only those); the oldest steps of the last turn go while they take more than half the budget; the oldest turns go,
// and beforeTurnId moves to the oldest kept one; at last the rest of the last turn's steps but its user entry, its
// answer and its live reply. What went is loaded by the pages: a turn with fewer entries than its stepCount is
// opened through /turns/{id}/entries.
func FitConv(s ConvSnapshot) ConvSnapshot {
	budget := ConvBudget - envelope
	entries := trimOutputs(s.Entries, ToolOutputMax)
	// Each entry is encoded once and counted with its comma; what goes is subtracted, nothing is encoded again.
	s.Entries = []Entry{}
	total := size(s)
	sizes := make([]int, len(entries))
	for i, e := range entries {
		sizes[i] = size(e) + 1
		total += sizes[i]
	}
	done := func() ConvSnapshot {
		s.Entries = entries
		return s
	}
	if total <= budget {
		return done()
	}
	entries = slices.Clone(entries)
	for _, n := range []int{1024, 256, 0} {
		for i, e := range entries {
			if e.Output != nil && len(e.Output.Text) > n {
				cut := trimOutputs(entries[i:i+1], n)[0]
				total += size(cut) + 1 - sizes[i]
				entries[i], sizes[i] = cut, size(cut)+1
			}
		}
		if total <= budget {
			return done()
		}
	}
	if len(s.Turns) == 0 {
		return done()
	}
	answers := map[string]bool{}
	for _, t := range s.Turns {
		if t.AnswerEntryID != nil {
			answers[*t.AnswerEntryID] = true
		}
	}
	keep := func(ok func(Entry) bool) {
		j := 0
		for i, e := range entries {
			if ok(e) {
				entries[j], sizes[j] = e, sizes[i]
				j++
			} else {
				total -= sizes[i]
			}
		}
		entries, sizes = entries[:j], sizes[:j]
	}
	last := s.Turns[len(s.Turns)-1].ID
	inLast := func(e Entry) bool { return e.TurnID != nil && *e.TurnID == last }
	// The older full turns keep only what opened them and their answers, as a folded turn shows them; an opening
	// message's pictures carried inline in the transcript (data: URLs, the bulk of such a turn) go, and the turn's page
	// brings them back when it opens.
	opens := func(e Entry) bool { return e.T == EntryUser && e.TurnID != nil && *e.TurnID == e.ID }
	keep(func(e Entry) bool { return e.TurnID == nil || inLast(e) || answers[e.ID] || opens(e) })
	for i, e := range entries {
		if inLast(e) || !opens(e) || !slices.ContainsFunc(e.Attachments, inlinePicture) {
			continue
		}
		e.Attachments = slices.DeleteFunc(slices.Clone(e.Attachments), inlinePicture)
		total += size(e) + 1 - sizes[i]
		entries[i], sizes[i] = e, size(e)+1
	}
	if total <= budget {
		return done()
	}
	lastBytes := 0
	for i, e := range entries {
		if inLast(e) {
			lastBytes += sizes[i]
		}
	}
	// dropSteps takes the last turn's oldest steps — never its user entry, its answer or its live reply — until the
	// snapshot fits or the turn is down to floor bytes.
	dropSteps := func(floor int) {
		gone := map[int]bool{}
		for i, e := range entries {
			if total <= budget || lastBytes <= floor {
				break
			}
			if inLast(e) && e.T != EntryUser && !answers[e.ID] && !strings.HasPrefix(e.ID, "live:") {
				gone[i] = true
				total, lastBytes = total-sizes[i], lastBytes-sizes[i]
			}
		}
		j := 0
		for i := range entries {
			if !gone[i] {
				entries[j], sizes[j] = entries[i], sizes[i]
				j++
			}
		}
		entries, sizes = entries[:j], sizes[:j]
	}
	// The last turn's oldest steps while they take more than half the budget, then the oldest turns, then the
	// rest of the last turn's steps.
	dropSteps(budget / 2)
	for len(s.Turns) > 1 && total > budget {
		gone := s.Turns[0].ID
		total -= size(s.Turns[0]) + 1
		s.Turns = s.Turns[1:]
		before := s.Turns[0].ID
		s.BeforeTurnID = &before
		keep(func(e Entry) bool { return e.TurnID == nil || *e.TurnID != gone })
	}
	dropSteps(0)
	return done()
}

// inlinePicture is a picture the transcript carries in the message itself, its bytes in its id.
func inlinePicture(a AttachmentRef) bool { return strings.HasPrefix(a.ID, "data:") }

// trimOutputs cuts every tool output past n bytes (on a character boundary) and says how much was shown of how much.
func trimOutputs(es []Entry, n int) []Entry {
	var out []Entry
	for i, e := range es {
		if e.Output == nil || len(e.Output.Text) <= n {
			continue
		}
		if out == nil {
			out = append([]Entry(nil), es...)
		}
		total := int64(len(e.Output.Text))
		if e.Output.Truncated != nil {
			total = e.Output.Truncated.Total
		}
		text := strings.ToValidUTF8(e.Output.Text[:n], "")
		out[i].Output = &ToolOutput{Text: text, Truncated: &OutputTrimmed{Shown: int64(len(text)), Total: total}}
	}
	if out == nil {
		return es
	}
	return out
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	if n == 0 {
		return ""
	}
	return string(r[:n-1]) + "…"
}
