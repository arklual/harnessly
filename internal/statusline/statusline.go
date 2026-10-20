// Package statusline is the sink of Claude Code's status line: the platform's statusline script posts the JSON Claude
// gives it on every refresh, the sink keeps the subscription limits and the session's figures from it, and answers the
// line to show, which is the output of the person's own statusLine command when they have one.
package statusline

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/limits"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

const (
	bodyLimit = 1 << 20
	// DefaultTimeout is how long the person's command may run: the script's curl gives up after 2 s anyway.
	DefaultTimeout = 2 * time.Second
	outputLimit    = 4 << 20
	// The effort Claude reports for an ultracode session, and the one shown for it.
	reportedUltracode = "xhigh"
	ultracodeEffort   = "ultracode"
)

// Sink answers POST /api/status/statusline.
type Sink struct {
	// Authorized is the old API's own check (a page session or the cli-token); TokenOK is the cli-token the handler
	// asks for besides.
	Authorized, TokenOK func(*http.Request) bool
	// Home is the person's home ($HOME in a command), ClaudeDir Claude Code's folder (its settings.json).
	Home, ClaudeDir string
	Limits          *limits.Cache
	Stats           *history.Stats
	// Ultracode says whether a session belongs to a tab whose remembered launch asked for ultracode; nil for none.
	Ultracode func(sessionID any) bool
	// Broadcast hands the session's stored figures to the conversation views that show it.
	Broadcast func(stats *jsvalue.Object)
	// Script is the platform's own statusline script: a statusLine command that runs it would post here again.
	Script string
	// Timeout bounds the person's command (DefaultTimeout when 0).
	Timeout time.Duration
	// Now is the clock (tests freeze it); nil is the real one.
	Now func() time.Time

	warned sync.Map
}

// warnOnce logs a problem of a command once: the status line refreshes every few seconds.
func (s *Sink) warnOnce(key, msg string, fields ...agent.F) {
	if _, seen := s.warned.LoadOrStore(key, true); !seen {
		agent.Log("warn", msg, fields...)
	}
}

func (s *Sink) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func writeText(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any, h http.Header) {
	status, body, h := nodejson.Answer(r, status, v, h)
	for k, vs := range h {
		w.Header()[k] = vs
	}
	if body == nil {
		w.WriteHeader(status)
		return
	}
	raw := body.(json.RawMessage)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// ReadBody reads a request body as the old API did before any handler: JSON (an empty one is {}), a form as an object,
// anything else as its text; 1 MB at most. A non-empty refusal is the plain-text answer to send instead.
func ReadBody(r *http.Request) (any, int, string) {
	if n, err := strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64); err == nil && n > bodyLimit {
		return nil, http.StatusRequestEntityTooLarge, "Body exceeded 1mb limit"
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit+1))
	if err != nil {
		return nil, http.StatusBadRequest, "Invalid body"
	}
	if len(raw) > bodyLimit {
		return nil, http.StatusRequestEntityTooLarge, "Body exceeded 1mb limit"
	}
	dec := jscompat.UTF8Decoder{StripBOM: true}
	text := dec.Decode(raw, false)
	header := r.Header.Get("Content-Type")
	if header == "" {
		header = "text/plain"
	}
	kind, _, _ := strings.Cut(header, ";")
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "application/json", "application/ld+json":
		if text == "" {
			return jsvalue.NewObject(), 0, ""
		}
		v, err := jsvalue.Parse([]byte(text))
		if err != nil {
			return nil, http.StatusBadRequest, "Invalid JSON"
		}
		return v, 0, ""
	case "application/x-www-form-urlencoded":
		form := jsvalue.NewObject()
		for _, pair := range strings.Split(text, "&") {
			if pair == "" {
				continue
			}
			for k, vs := range jscompat.Query(pair) {
				prev, had := form.Get(k)
				switch {
				case !had:
					form.Set(k, vs[0])
				case isArray(prev):
					form.Set(k, append(prev.([]any), vs[0]))
				default:
					form.Set(k, []any{prev, vs[0]})
				}
			}
		}
		return form, 0, ""
	}
	return text, 0, ""
}

func isArray(v any) bool { _, ok := v.([]any); return ok }

// field is v[k] of a value read from JSON and whether it is there; anything but an object has no fields here.
func field(v any, k string) (any, bool) {
	if o, ok := v.(*jsvalue.Object); ok {
		return o.Get(k)
	}
	return nil, false
}

func get(v any, k string) any { x, _ := field(v, k); return x }

// or is `v ?? def`.
func or(v any, def any) any {
	if v == nil {
		return def
	}
	return v
}

func (s *Sink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.Authorized(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"Unauthorized"}`)
		return
	}
	body, status, refusal := ReadBody(r)
	if refusal != "" {
		writeText(w, status, refusal)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, r, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"}, http.Header{"Allow": {"POST"}})
		return
	}
	if !s.TokenOK(r) {
		writeJSON(w, r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
		return
	}
	input := or(body, jsvalue.NewObject())
	s.writeLimits(input)
	s.writeStats(input)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	command, ok := s.userCommand(input)
	if !ok || command == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.refersToScript(command) {
		s.warnOnce("self:"+command, "statusline: the person's statusLine command runs the platform's own script; not run")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	out, ran := s.run(r.Context(), command, jsvalue.Stringify(input, ""))
	if !ran {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	payload := []byte(jscompat.DecodeUTF8(out))
	tag := nodejson.ETag(payload)
	w.Header().Set("ETag", tag)
	if nodejson.Fresh(r.Header, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

// writeLimits keeps the Claude limits the status line carries; its windows have no source (non-polled).
func (s *Sink) writeLimits(input any) {
	rl := get(input, "rate_limits")
	fiveHour, sevenDay := or(get(rl, "five_hour"), nil), or(get(rl, "seven_day"), nil)
	if !jsvalue.Truthy(fiveHour) && !jsvalue.Truthy(sevenDay) {
		return
	}
	data := limits.Data{"ts": float64(s.now().UnixMilli()) / 1000, "five_hour": plain(fiveHour), "seven_day": plain(sevenDay)}
	if _, err := s.Limits.Write("claude", data); err != nil {
		agent.Log("warn", "statusline: rate limits not written", agent.F{K: "err", V: err.Error()})
	}
}

// plain is a JSON value as encoding/json decodes it.
func plain(v any) any {
	if v == nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(jsvalue.Stringify(v, ""), &out)
	return out
}

// writeStats keeps the session's figures and shows them to the conversation views.
func (s *Sink) writeStats(input any) {
	sessionID := get(input, "session_id")
	if !jsvalue.Truthy(sessionID) {
		return
	}
	ctx := get(input, "context_window")
	cu := get(ctx, "current_usage")
	var current any = 0.0
	if jsvalue.Truthy(cu) {
		current = jsAdd(jsAdd(or(get(cu, "input_tokens"), 0.0), or(get(cu, "cache_creation_input_tokens"), 0.0)), or(get(cu, "cache_read_input_tokens"), 0.0))
	}
	var effort any
	if e, ok := get(get(input, "effort"), "level").(string); ok {
		effort = e
	}
	transcript, hasTranscript := field(input, "transcript_path")
	stats := jsvalue.NewObject("sessionId", sessionID)
	if hasTranscript {
		stats.Set("transcriptPath", transcript)
	}
	stats.Set("inputTokens", or(get(ctx, "total_input_tokens"), 0.0))
	stats.Set("outputTokens", or(get(ctx, "total_output_tokens"), 0.0))
	stats.Set("cost", get(get(input, "cost"), "total_cost_usd"))
	stats.Set("currentContextTokens", current)
	stats.Set("contextWindowSize", or(get(ctx, "context_window_size"), 0.0))
	stats.Set("usedPercentage", get(ctx, "used_percentage"))
	stats.Set("model", get(get(input, "model"), "display_name"))
	stats.Set("effort", effort)
	stats.Set("exceeds200k", or(get(input, "exceeds_200k_tokens"), false))
	stats.Set("receivedAt", float64(s.now().UnixMilli()))
	if effort == reportedUltracode && s.Ultracode != nil && s.Ultracode(sessionID) {
		stats.Set("effort", ultracodeEffort)
	}
	var drop []string
	if !hasTranscript {
		drop = []string{"transcriptPath"} // undefined in the patch: the stored path goes too
	}
	id := jscompat.String(sessionID)
	merged := stats
	if history.ValidSessionID(id) {
		stored, err := s.Stats.Update(id, stats, drop...)
		if err != nil {
			agent.Log("warn", "statusline: session stats not written", agent.F{K: "err", V: err.Error()})
			stored = s.Stats.Read(id)
		}
		if stored != nil {
			merged = stored
		}
	}
	if s.Broadcast != nil {
		s.Broadcast(merged)
	}
}

// jsAdd is JavaScript's + on two JSON values: text when either side is text (or an object or array), else numbers.
func jsAdd(a, b any) any {
	prim := func(v any) (any, bool) {
		switch x := v.(type) {
		case string:
			return x, true
		case *jsvalue.Object, []any:
			return jscompat.String(x), true
		}
		return v, false
	}
	pa, sa := prim(a)
	pb, sb := prim(b)
	if sa || sb {
		return jscompat.String(pa) + jscompat.String(pb)
	}
	toNum := func(v any) float64 {
		switch x := v.(type) {
		case float64:
			return x
		case bool:
			if x {
				return 1
			}
		}
		return 0
	}
	return toNum(pa) + toNum(pb)
}

// userCommand is the person's own statusLine command: the project's local settings, its settings, then Claude's own
// settings, first hit wins. ok is false where the old handler failed (a project folder that is not text).
func (s *Sink) userCommand(input any) (string, bool) {
	var files []string
	if dir := get(get(input, "workspace"), "project_dir"); jsvalue.Truthy(dir) {
		d, isText := dir.(string)
		if !isText {
			return "", false
		}
		files = append(files, filepath.Join(d, ".claude", "settings.local.json"), filepath.Join(d, ".claude", "settings.json"))
	}
	files = append(files, filepath.Join(s.ClaudeDir, "settings.json"))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		v, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(raw)))
		if err != nil {
			continue
		}
		if c, ok := get(get(v, "statusLine"), "command").(string); ok && jscompat.Trim(c) != "" {
			return c, true
		}
	}
	return "", true
}

// run runs the command under sh with the input on stdin and gives its output, whatever its exit code. It is killed,
// with everything it started, when the caller goes away or the time is up; ran is false then, and when it could not
// start.
func (s *Sink) run(parent context.Context, command string, input []byte) ([]byte, bool) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 500 * time.Millisecond
	cmd.Stdin = bytes.NewReader(input)
	out := &limitedBuffer{max: outputLimit}
	cmd.Stdout = out
	if err := cmd.Start(); err != nil {
		return nil, false
	}
	_ = cmd.Wait() // a failed command's output is still the line, as before
	if ctx.Err() != nil {
		s.warnOnce("stopped:"+command, "statusline: the person's statusLine command was stopped", agent.F{K: "why", V: ctx.Err().Error()})
		return nil, false
	}
	return out.Bytes(), true
}

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); room > 0 {
		if len(p) > room {
			b.Buffer.Write(p[:room])
		} else {
			b.Buffer.Write(p)
		}
	}
	return len(p), nil
}
