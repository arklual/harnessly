// Package usagefeed fills the machine's usage outbox (internal/usageoutbox) from the facts of the agent CLIs'
// transcripts (internal/usagereport): a `call` record for every model call and a `session` record for every session
// that is new or changed (docs/hub/usage.md). It copies numbers and ids, never the text of a conversation: the facts it
// reads hold none, and a record has no field that could carry any.
package usagefeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
	"github.com/ly-solutions/harnessly/internal/usageoutbox"
	"github.com/ly-solutions/harnessly/internal/usagereport"
)

const (
	// settle: a transcript written to this recently may still be growing its last call (a streamed answer repeats its
	// usage with a rising output count), and a record never changes once it has a seq, so that call waits.
	settle = 30 * time.Second
	// settleMax: a call this old is final whatever the file does.
	settleMax = 30 * time.Minute
	// ended: a session quiet this long gets an end.
	ended = 30 * time.Minute
	// flushEvery bounds the records one write holds, so a long backfill does not sit in memory.
	flushEvery = 2000
	stateFile  = "producer.json"

	// DefaultMaxPending and DefaultMaxBytes bound what waits for the hub's acknowledgement: a hub that stops reading
	// does not make the outbox grow without end. Reaching either pauses the feed; nothing is dropped.
	DefaultMaxPending = 200_000
	DefaultMaxBytes   = 64 << 20
)

// Source is the transcripts' facts; usagereport.Collector is one.
type Source interface {
	Refresh()
	Entries() []usagereport.Entry
}

// Config is what the feed needs.
type Config struct {
	Outbox *usageoutbox.Outbox
	Source Source
	// StatePath keeps what was already written, so a restart does not write it all again.
	StatePath string
	// Identity says which account and endpoint a provider's CLI uses now; nil knows nothing.
	Identity func(provider string) Identity
	// Now is the clock (tests move it); nil is the real one.
	Now func() time.Time
	// MaxPending and MaxBytes cap the unacknowledged records (count, bytes on disk); zero is the default.
	MaxPending int
	MaxBytes   int64
}

// Feed writes the outbox.
type Feed struct {
	cfg Config

	mu     sync.Mutex // one pass at a time
	paused bool       // the cap was reached and said so
	st     state
	seen   map[string]bool // msgKeys written or known to be written, per provider
	// known are the transcripts whose written rows were put in seen.
	known map[string]bool
}

type state struct {
	Epoch    string                `json:"epoch"`
	Files    map[string]fileState  `json:"files"`
	Sessions map[string]sessState  `json:"sessions"`
	Idents   map[string]identState `json:"idents"`
	// Tools are the tool facts written, by a hash of (session, day, kind, name): the totals as last written.
	Tools map[string]toolState `json:"tools,omitempty"`
}

type toolState struct {
	Calls  int64 `json:"calls"`
	Errors int64 `json:"errors"`
}

// fileState: the first N rows of a transcript are written; Last is the key of row N-1, to notice a file that was
// rewritten rather than appended to.
type fileState struct {
	N    int    `json:"n"`
	Last string `json:"last"`
}

type sessState struct {
	Calls  int   `json:"calls"`
	LastTs int64 `json:"lastTs"`
	Ended  bool  `json:"ended"`
	// Metrics is the signature of the metrics last written; a state from before metrics has none, so every session
	// is written once more with them.
	Metrics string `json:"metrics,omitempty"`
}

// identState: what a provider's identity was when first seen and since when. Facts older than Since are not given an
// account or an endpoint they may not have had.
type identState struct {
	Sig   string `json:"sig"`
	Since int64  `json:"since"`
}

// New loads the state. A state of another epoch than the outbox's (the folder was made anew) is forgotten.
func New(cfg Config) (*Feed, error) {
	if cfg.Outbox == nil || cfg.Source == nil {
		return nil, errors.New("usage feed: outbox and source are required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = DefaultMaxPending
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	f := &Feed{cfg: cfg, seen: map[string]bool{}, known: map[string]bool{}}
	if raw, err := os.ReadFile(cfg.StatePath); err == nil {
		_ = json.Unmarshal(raw, &f.st)
	}
	if f.st.Epoch != cfg.Outbox.Epoch() {
		f.st = state{Epoch: cfg.Outbox.Epoch()}
	}
	if f.st.Files == nil {
		f.st.Files = map[string]fileState{}
	}
	if f.st.Sessions == nil {
		f.st.Sessions = map[string]sessState{}
	}
	if f.st.Idents == nil {
		f.st.Idents = map[string]identState{}
	}
	if f.st.Tools == nil {
		f.st.Tools = map[string]toolState{}
	}
	return f, nil
}

// Run does a pass now and then every interval until ctx is done. written is told the outbox's epoch and newest seq
// after a pass that wrote something.
func (f *Feed) Run(ctx context.Context, every time.Duration, written func(epoch string, last int64)) {
	pass := func() {
		n, err := f.Pass(ctx)
		if err != nil && ctx.Err() == nil {
			jsonlog.Log("warn", "usage feed pass failed", jsonlog.F{K: "error", V: err.Error()})
		}
		if n > 0 && written != nil {
			written(f.cfg.Outbox.Epoch(), f.cfg.Outbox.Last())
		}
	}
	pass()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pass()
		}
	}
}

// session is what the transcripts say of one session, summed over its files.
type session struct {
	provider, key, parent string
	first, last           float64 // epoch seconds
	calls                 int
	m                     metrics
	tools                 map[toolDay]toolState
}

// sessionOf names the session a transcript belongs to. A Claude subagent's transcript carries its parent's session id
// inside; it is a session of its own, named after its parent and its file, with the parent set. Codex's facts do not
// say who a sub-thread's parent is.
func sessionOf(e usagereport.Entry) (key, parent string) {
	f := e.Facts
	file := strings.TrimSuffix(filepath.Base(e.Path), ".jsonl")
	if f.Provider == "claude" && (f.Sidechain || strings.Contains(filepath.ToSlash(e.Path), "/subagents/")) {
		if f.SessionID != "" && f.SessionID != file {
			return f.SessionID + "/" + file, f.SessionID
		}
		return file, ""
	}
	if f.SessionID != "" {
		return f.SessionID, ""
	}
	return file, ""
}

// toolDay is one tool on one UTC day of a session.
type toolDay struct{ day, kind, name string }

// toolKey is a hash of the session, then a hash of the day, kind and name, so that the entries of a session that is gone
// can be told by their start.
func toolKey(sid string, td toolDay) string {
	return toolSessionKey(sid) + sha256Hex(td.day+"\x00"+td.kind+"\x00"+td.name)
}

func toolSessionKey(sid string) string { return sha256Hex(sid) }

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// addTools sums the named tool calls of a transcript by UTC day, tool kind and name. Calls older than the cutoff (epoch
// ms) are left out, as are calls of no known time.
func (s *session) addTools(fa *usagereport.Facts, cutoff int64) {
	for _, c := range fa.ToolCalls {
		if c.Ts <= 0 || c.Name == "" || ms(c.Ts) < cutoff {
			continue
		}
		if s.tools == nil {
			s.tools = map[toolDay]toolState{}
		}
		td := toolDay{day: time.UnixMilli(ms(c.Ts)).UTC().Format("2006-01-02"), kind: c.Kind, name: c.Name}
		t := s.tools[td]
		n, errs := c.Count()
		t.Calls += int64(n)
		t.Errors += int64(errs)
		s.tools[td] = t
	}
}

// toolOrder is the session's tool days in a fixed order.
func (s *session) toolOrder() []toolDay {
	out := make([]toolDay, 0, len(s.tools))
	for td := range s.tools {
		out = append(out, td)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.day != b.day {
			return a.day < b.day
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.name < b.name
	})
	return out
}

func ms(sec float64) int64 { return int64(math.Round(sec * 1000)) }

func keyOf(m usagereport.MsgRow) string { return jscompat.String(m.Key) }

// Pass reads the transcripts' new facts and writes what is new to the outbox. It returns how many records it wrote. A
// pass that ctx stops saves nothing; the next one writes again what this one did, which the hub ignores.
func (f *Feed) Pass(ctx context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Full: nothing is scanned and the state stays as it is, so the pass after the hub's ack goes on from here.
	if f.full(0) {
		return 0, nil
	}
	f.paused = false
	f.cfg.Source.Refresh()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	now := f.cfg.Now()
	nowMs := now.UnixMilli()
	cutoff := nowMs - usagereport.WindowDays*86400*1000
	entries := f.cfg.Source.Entries()

	idents := map[string]Identity{}
	for _, p := range []string{hubproto.ProviderClaude, hubproto.ProviderCodex} {
		var id Identity
		if f.cfg.Identity != nil {
			id = f.cfg.Identity(p)
		}
		idents[p] = id
		sig := id.AccountRef + "|" + id.AuthMode + "|" + id.BaseURLHost
		if cur, ok := f.st.Idents[p]; !ok || cur.Sig != sig {
			f.st.Idents[p] = identState{Sig: sig, Since: nowMs}
		}
	}

	var batch []hubproto.UsageRecord
	written := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := f.cfg.Outbox.Append(batch); err != nil {
			return err
		}
		written += len(batch)
		batch = batch[:0]
		return nil
	}

	sessions := map[string]*session{}
	var order []string
	live := make(map[string]bool, len(entries))
	for _, e := range entries {
		fa := e.Facts
		if fa == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return written, err
		}
		if f.full(len(batch)) {
			// The files before this one are done and their state is kept; this one and the rest wait.
			if err := flush(); err != nil {
				return written, err
			}
			return written, f.save()
		}
		fkey := fileKey(e.Path)
		live[fkey] = true
		skey, parent := sessionOf(e)
		sid := fa.Provider + ":" + skey
		s := sessions[sid]
		if s == nil {
			s = &session{provider: fa.Provider, key: skey, parent: parent}
			sessions[sid] = s
			order = append(order, sid)
		}
		s.calls += len(fa.Messages)
		s.m.add(fa)
		s.addTools(fa, cutoff)
		if fa.FirstTs > 0 && (s.first == 0 || fa.FirstTs < s.first) {
			s.first = fa.FirstTs
		}
		s.last = max(s.last, fa.LastTs)

		id := idents[fa.Provider]
		since := f.st.Idents[fa.Provider].Since
		rows := fa.Messages
		fs := f.st.Files[fkey]
		start := 0
		if fs.N > 0 && fs.N <= len(rows) && keyOf(rows[fs.N-1]) == fs.Last {
			start = fs.N
		}
		// What an earlier run wrote is known, so a copy of it in a forked transcript is not written again.
		if !f.known[fkey] {
			for _, m := range rows[:start] {
				f.seen[fa.Provider+"\x00"+keyOf(m)] = true
			}
			f.known[fkey] = true
		}
		end := len(rows)
		// The last call of a file that is still being written may still be growing.
		if end > 0 && float64(nowMs)-e.MtimeMs < float64(settle.Milliseconds()) && float64(nowMs)-rows[end-1].Ts*1000 < float64(settleMax.Milliseconds()) {
			end--
		}
		for i := start; i < end; i++ {
			m := rows[i]
			mk := keyOf(m)
			if m.Ts <= 0 || ms(m.Ts) < cutoff || m.Input+m.Output+m.CacheRead+m.W5m+m.W1h == 0 {
				continue
			}
			if mk == "" || mk == ":" {
				mk = fmt.Sprintf("%s@%d", skey, ms(m.Ts))
			}
			dk := fa.Provider + "\x00" + mk
			if f.seen[dk] {
				continue
			}
			f.seen[dk] = true
			model := "unknown"
			if m.Model >= 0 && m.Model < len(fa.Models) {
				if name, ok := fa.Models[m.Model].(string); ok && name != "" {
					model = name
				}
			}
			host := ""
			if ms(m.Ts) >= since {
				host = id.BaseURLHost
			}
			batch = append(batch, hubproto.UsageRecord{Kind: hubproto.UsageCall, Call: &hubproto.UsageCallFact{
				Provider: fa.Provider, MsgKey: mk, SessionKey: skey, Ts: ms(m.Ts), Model: model, BaseURLHost: host,
				Input: round(m.Input), Output: round(m.Output), CacheRead: round(m.CacheRead),
				CacheWrite5m: round(m.W5m), CacheWrite1h: round(m.W1h),
			}})
			if len(batch) >= flushEvery {
				if err := flush(); err != nil {
					return written, err
				}
			}
		}
		if end > 0 {
			f.st.Files[fkey] = fileState{N: end, Last: keyOf(rows[end-1])}
		}
	}
	for p := range f.st.Files {
		if !live[p] {
			delete(f.st.Files, p)
			delete(f.known, p)
		}
	}

	// Sessions after the calls: a session is written when it is new or its calls, end or last call changed.
	for _, sid := range order {
		s := sessions[sid]
		if s.first == 0 && s.calls == 0 {
			continue
		}
		lastMs := ms(s.last)
		over := nowMs-lastMs >= ended.Milliseconds()
		met := s.m.values()
		mrKeys := s.m.mrKeys()
		cur := sessState{Calls: s.calls, LastTs: lastMs, Ended: over, Metrics: signature(met, mrKeys)}
		if prev, ok := f.st.Sessions[sid]; ok && prev == cur {
			continue
		}
		f.st.Sessions[sid] = cur
		id := idents[s.provider]
		startedAt := ms(s.first)
		fact := &hubproto.UsageSessionFact{
			Provider: s.provider, SessionKey: s.key, ParentKey: s.parent, StartedAt: startedAt,
			Origin: hubproto.OriginLive, Attribution: hubproto.AttributionGuessed, Metrics: met, MRKeys: mrKeys,
		}
		if startedAt >= f.st.Idents[s.provider].Since {
			fact.AccountRef, fact.AuthMode, fact.BaseURLHost = id.AccountRef, id.AuthMode, id.BaseURLHost
		}
		if over {
			end := lastMs
			fact.EndedAt = &end
		}
		batch = append(batch, hubproto.UsageRecord{Kind: hubproto.UsageSession, Session: fact})
		if len(batch) >= flushEvery {
			if err := flush(); err != nil {
				return written, err
			}
		}
	}
	// Tools after the sessions: the hub finds the session a tool fact belongs to.
	for _, sid := range order {
		s := sessions[sid]
		for _, td := range s.toolOrder() {
			cur := s.tools[td]
			tk := toolKey(sid, td)
			// A total never goes down: facts read back from the cache have no tool calls, and a session spread over
			// several transcripts may be seen in part.
			if prev, ok := f.st.Tools[tk]; ok && cur.Calls <= prev.Calls && (cur.Calls < prev.Calls || cur.Errors <= prev.Errors) {
				continue
			}
			if f.full(len(batch)) {
				if err := flush(); err != nil {
					return written, err
				}
				return written, f.save()
			}
			f.st.Tools[tk] = cur
			batch = append(batch, hubproto.UsageRecord{Kind: hubproto.UsageTool, Tool: &hubproto.UsageToolFact{
				Provider: s.provider, SessionKey: s.key, Day: td.day, ToolKind: td.kind, ToolName: td.name, Calls: cur.Calls, Errors: cur.Errors,
			}})
			if len(batch) >= flushEvery {
				if err := flush(); err != nil {
					return written, err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return written, err
	}
	// Sessions that are no longer in any transcript are forgotten with them.
	for sid := range f.st.Sessions {
		if sessions[sid] == nil {
			delete(f.st.Sessions, sid)
		}
	}
	// What was written of a session that is no longer in any transcript is forgotten with it. A session whose facts came
	// from the cache keeps its entries, though they hold no tool calls.
	liveTools := make(map[string]bool, len(sessions))
	for sid := range sessions {
		liveTools[toolSessionKey(sid)] = true
	}
	for k := range f.st.Tools {
		if !liveTools[k[:16]] {
			delete(f.st.Tools, k)
		}
	}
	return written, f.save()
}

// full says the outbox holds as much as it may and logs it once per stop. extra counts records not yet written.
func (f *Feed) full(extra int) bool {
	box := f.cfg.Outbox
	if box.Pending()+extra < f.cfg.MaxPending && box.PendingBytes() < f.cfg.MaxBytes {
		return false
	}
	if !f.paused {
		f.paused = true
		jsonlog.Log("warn", "usage outbox is full; no more spend facts are written until the hub acknowledges some",
			jsonlog.F{K: "pending", V: box.Pending()}, jsonlog.F{K: "bytes", V: box.PendingBytes()})
	}
	return true
}

// fileKey names a transcript in the state by a hash of its path: the folder names of a project are paths, and the state
// is kept to say what was written, not where the person works.
func fileKey(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8])
}

func round(v float64) int64 { return int64(math.Round(math.Max(0, v))) }

// save writes the state, after the records it describes are on disk: a stop in between writes some of them twice,
// which the hub ignores, and never loses one.
func (f *Feed) save() error {
	raw, err := json.Marshal(f.st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.cfg.StatePath), 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", f.cfg.StatePath, os.Getpid())
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.cfg.StatePath)
}

// StatePath is where the state of an outbox in dir is kept.
func StatePath(dir string) string { return filepath.Join(dir, stateFile) }
