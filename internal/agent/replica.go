package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// The chats of the person leave the machine (part 1): the agent reads the runtime's replica socket
// (/api/v2/replica, loopback) and forwards the events of the conversation channels to the cluster as `v2.events`
// frames, which the cluster stores in its database so the chat list and the history are the same from every machine and
// readable while this one is off. The cluster says how far each channel is stored (`v2.ack`), and the agent keeps what is
// not stored yet; what it no longer has, it sends again from the pages of the runtime (`v2.backfill`).
//
// Nothing is sent, and nothing is kept, until the cluster says `v2.start{enabled:true}` (the `central-history` switch).
//
// a chat is backfilled whole, by a cursor the cluster keeps. `v2.start.pending` names the chats the cluster does
// not hold whole yet, each with the turn its backfill stopped before ("" for one never read); the agent walks each of
// them from there to its first turn, and every page it sends says where it was read from (`history.from`), so the
// cluster moves the cursor and, on the page with no older turn, marks the chat whole. A walk cut short (the link went,
// the runtime restarted) goes on from the cursor next time. A cluster without `pending` gets the old backfill.
const (
	replicaBuffer    = 5000  // events kept per channel while the cluster has not stored them
	replicaInflight  = 2000  // events sent per channel that the cluster has not acknowledged
	replicaBatch     = 200   // events in one frame
	replicaConvBatch = 100   // chat records in one backfill frame
	replicaPages     = 40    // pages of turns read for a chat the cluster has never seen (a cluster without cursors)
	replicaWalkPages = 10000 // pages of one chat walked in one go by its cursor: 200 000 turns, a guard against a loop
	replicaWalkTurns = 20    // turns in a page of a walk: it carries the steps of each, so the copy opens a folded turn
	replicaPause     = 150 * time.Millisecond
	// replicaConvsWait is how long the agent waits for the runtime's chat list before it asks again, doubled up to
	// replicaConvsWaitMax: a runtime too busy to make the list in its budget only logs it and answers nothing, and the
	// backfill of the cluster waited for the next link (an hour and more on a loaded machine).
	replicaConvsWait    = 30 * time.Second
	replicaConvsWaitMax = 5 * time.Minute
	// replicaRetryWait is how long a chat whose page the runtime did not give waits before its backfill goes on from
	// where it stopped, doubled up to replicaRetryWaitMax: a backfill runs when a link starts, and a chat cut by one
	// busy answer stayed behind until the next link.
	replicaRetryWait    = time.Minute
	replicaRetryWaitMax = 15 * time.Minute
)

// evV2Events carries events `fromSeq`.. of one channel of one epoch.
type evV2Events struct {
	Event   string            `json:"event"`
	Ch      string            `json:"ch"`
	Epoch   string            `json:"epoch"`
	FromSeq int64             `json:"fromSeq"`
	Reset   bool              `json:"reset,omitempty"`
	Events  []json.RawMessage `json:"events"`
}

// evV2Backfill carries events made from what the runtime holds now; they have no seq.
type evV2Backfill struct {
	Event  string            `json:"event"`
	Ch     string            `json:"ch"`
	Events []json.RawMessage `json:"events"`
}

type cursor struct {
	Epoch string `json:"epoch"`
	Seq   int64  `json:"seq"`
}

type replicaEvent struct {
	seq int64
	raw json.RawMessage
}

type replicaChan struct {
	events []replicaEvent // contiguous, oldest first; everything after `acked`
	last   int64          // the last seq seen
	sent   int64          // the last seq sent
	acked  int64
	reset  bool // the next frame is a new baseline, not a continuation
}

// replica is the agent's end of the chat replication. `emit` hands a frame to the cluster links, `connected` says whether
// any is up, `askConvs` asks the runtime for its chat list, `pages` reads a chat's pages of turns from the runtime.
type replica struct {
	emit      func(any)
	connected func() bool
	askConvs  func()
	pages     func(conv, before string, limit int, steps bool) (json.RawMessage, error)
	entries   func(conv, turn string) (json.RawMessage, error)
	output    func(conv, entry string) (json.RawMessage, error)
	pause     time.Duration
	convsWait time.Duration // the first wait for the chat list (replicaConvsWait)
	retryWait time.Duration // the first wait before a cut chat's backfill goes on (replicaRetryWait)

	// The tool outputs the cluster's copy gets whole (replica_outputs.go).
	outQ    chan outputJob
	outMu   sync.Mutex
	outSeen map[string]int64

	mu         sync.Mutex
	enabled    bool
	epoch      string
	chans      map[string]*replicaChan
	cursors    map[string]cursor
	known      map[string]int64
	pending    map[string]string // nil: the cluster keeps no backfill cursors (an older one)
	steps      map[string]string // the chats held whole without the steps of every turn, by their steps cursor
	generation int               // a new `v2.start` or a new runtime epoch starts a new backfill; the old one stops
	answered   int               // the generation the runtime's chat list came for
}

func newReplica(a *Agent) *replica {
	return &replica{
		emit: func(ev any) {
			a.broadcast(ev)
			if a.hub != nil && !a.hub.observe {
				a.hub.emitHistory(ev) // HRN-879: a hub keeps the chats as a cluster does
			}
		},
		connected: a.connected,
		askConvs:  func() {},
		pause:     replicaPause,
		convsWait: replicaConvsWait,
		retryWait: replicaRetryWait,
		chans:     map[string]*replicaChan{},
		outQ:      make(chan outputJob, outputQueue),
		outSeen:   map[string]int64{},
	}
}

// start is `v2.start`: the switch, where the cluster's copy stands per channel, and the revision of each chat it holds.
func (r *replica) start(enabled bool, cursors map[string]cursor, known map[string]int64, pending map[string]string) {
	r.startWith(enabled, cursors, known, pending, nil)
}

// startWith is start with `v2.start.steps`: the chats the cluster holds whole but not with the steps of every turn,
// each with where their steps walk stands.
func (r *replica) startWith(enabled bool, cursors map[string]cursor, known map[string]int64, pending, steps map[string]string) {
	r.mu.Lock()
	r.enabled = enabled
	r.cursors, r.known, r.pending, r.steps = cursors, known, pending, steps
	r.generation++
	generation := r.generation
	r.chans = map[string]*replicaChan{}
	r.mu.Unlock()
	// A new start may be another cluster or one that lost what it had: the outputs seen go again.
	r.outMu.Lock()
	r.outSeen = map[string]int64{}
	r.outMu.Unlock()
	if enabled {
		r.ask(generation) // the runtime answers with its chats, and `convs` below backfills them
	}
}

// ask asks the runtime for its chat list, and again while no list came for this generation: the runtime answers
// nothing when it could not make the list in its budget.
func (r *replica) ask(generation int) {
	r.askConvs()
	wait := r.convsWait
	if wait <= 0 {
		return
	}
	go func() {
		for {
			time.Sleep(wait)
			r.mu.Lock()
			waiting := r.enabled && r.generation == generation && r.answered != generation
			r.mu.Unlock()
			if !waiting {
				return
			}
			Log("info", "the runtime's chat list did not come, asked again", F{"wait", wait.String()})
			r.askConvs()
			wait = min(wait*2, replicaConvsWaitMax)
		}
	}()
}

// ack is `v2.ack`: the cluster has stored `ch` up to ackSeq (of `epoch`); gap: it got a frame that began after that.
func (r *replica) ack(ch, epoch string, ackSeq int64, gap bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.chans[ch]
	if !r.enabled || c == nil || epoch != r.epoch {
		return
	}
	c.acked = max(c.acked, ackSeq)
	keep := c.events[:0]
	for _, ev := range c.events {
		if ev.seq > ackSeq {
			keep = append(keep, ev)
		}
	}
	c.events = keep
	if gap || ackSeq < c.sent {
		c.sent = ackSeq
		c.reset = false
	}
	r.flushLocked(ch, c)
}

// hello is the runtime's first frame on a replica socket: the epoch its seqs belong to.
func (r *replica) hello(epoch string) {
	r.mu.Lock()
	r.epoch = epoch
	r.chans = map[string]*replicaChan{}
	r.generation++
	generation := r.generation
	enabled := r.enabled
	r.mu.Unlock()
	if enabled {
		r.ask(generation)
	}
}

// event is one event the runtime published.
func (r *replica) event(ch string, seq int64, raw json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled || r.epoch == "" {
		return
	}
	r.noteOutputs(ch, raw)
	c := r.chans[ch]
	if c == nil {
		c = &replicaChan{}
		r.chans[ch] = c
		r.adopt(ch, c, seq)
	}
	if c.last != 0 && seq != c.last+1 {
		// The replica socket dropped events (it was closed for being behind): what is kept is no longer a run.
		c.events = nil
		c.sent = seq - 1
		c.reset = true
	}
	c.last = seq
	c.events = append(c.events, replicaEvent{seq: seq, raw: raw})
	if len(c.events) > replicaBuffer {
		dropped := c.events[0]
		c.events = c.events[1:]
		if c.sent < dropped.seq {
			c.sent = dropped.seq
			c.reset = true
		}
	}
	r.flushLocked(ch, c)
}

// adopt sets where a channel seen for the first time starts being sent from: right after what the cluster has stored of
// the same epoch, else as a new baseline.
func (r *replica) adopt(ch string, c *replicaChan, firstSeq int64) {
	cur, ok := r.cursors[ch]
	if ok && cur.Epoch == r.epoch && cur.Seq >= firstSeq-1 {
		c.sent, c.acked = cur.Seq, cur.Seq
		return
	}
	c.sent, c.acked = firstSeq-1, firstSeq-1
	c.reset = ok && cur.Epoch == r.epoch && cur.Seq < firstSeq-1
}

func (r *replica) flushLocked(ch string, c *replicaChan) {
	if !r.connected() {
		return
	}
	for {
		if c.sent-c.acked >= replicaInflight {
			return
		}
		var batch []json.RawMessage
		var from int64
		for _, ev := range c.events {
			if ev.seq <= c.sent {
				continue
			}
			if from == 0 {
				from = ev.seq
			}
			batch = append(batch, ev.raw)
			if len(batch) == replicaBatch {
				break
			}
		}
		if len(batch) == 0 {
			return
		}
		r.emit(evV2Events{Event: "v2.events", Ch: ch, Epoch: r.epoch, FromSeq: from, Reset: c.reset || from != c.sent+1, Events: batch})
		c.reset = false
		c.sent = from + int64(len(batch)) - 1
	}
}

// convs is the runtime's answer to the chat list request: every chat goes to the cluster as an upsert, and the chats it
// does not hold at the revision they have now are read page by page.
func (r *replica) convs(records []json.RawMessage) {
	r.mu.Lock()
	generation := r.generation
	r.answered = generation
	known, pending, steps := r.known, r.pending, r.steps
	enabled := r.enabled
	r.mu.Unlock()
	if !enabled || !r.connected() {
		return
	}
	type head struct {
		ID  string `json:"id"`
		Rev int64  `json:"rev"`
	}
	var heads []head
	for start := 0; start < len(records); start += replicaConvBatch {
		end := min(start+replicaConvBatch, len(records))
		events := make([]json.RawMessage, 0, end-start)
		for _, rec := range records[start:end] {
			var h head
			if json.Unmarshal(rec, &h) != nil || h.ID == "" {
				continue
			}
			ev, _ := json.Marshal(map[string]json.RawMessage{"type": json.RawMessage(`"conversation_upsert"`), "conv": rec})
			events = append(events, ev)
			heads = append(heads, h)
		}
		r.emit(evV2Backfill{Event: "v2.backfill", Ch: "convs", Events: events})
	}
	var jobs []backfillJob
	if pending == nil {
		// An older cluster: the chats it has never seen first, whole as far as replicaPages; the changed ones by their newest page.
		var newest []backfillJob
		for _, h := range heads {
			if rev, ok := known[h.ID]; !ok {
				jobs = append(jobs, backfillJob{id: h.ID, pages: replicaPages})
			} else if rev != h.Rev {
				newest = append(newest, backfillJob{id: h.ID, pages: 1})
			}
		}
		jobs = append(jobs, newest...)
	} else {
		// The changed chats' newest pages first (cheap, and what people open), then the walks: the chats the cluster has
		// never seen, then the ones whose backfill stopped part way, last the chats held whole that lack steps.
		var newest, fresh, rest, stepWalks []backfillJob
		for _, h := range heads {
			rev, seen := known[h.ID]
			from, open := pending[h.ID]
			if stepsFrom, ok := steps[h.ID]; ok && seen && !open {
				stepWalks = append(stepWalks, backfillJob{id: h.ID, from: stepsFrom, pages: replicaWalkPages, walk: true, steps: true})
			}
			switch {
			case !seen:
				fresh = append(fresh, backfillJob{id: h.ID, pages: replicaWalkPages, walk: true})
			case open:
				if rev != h.Rev && from != "" {
					newest = append(newest, backfillJob{id: h.ID, pages: 1})
				}
				rest = append(rest, backfillJob{id: h.ID, from: from, pages: replicaWalkPages, walk: true})
			case rev != h.Rev:
				newest = append(newest, backfillJob{id: h.ID, pages: 1})
			}
		}
		jobs = append(append(append(newest, fresh...), rest...), stepWalks...)
	}
	go r.backfill(generation, jobs)
}

// backfillJob is one chat to read: from the turn `from` is before ("" for the newest), at most `pages` pages; a walk
// marks its pages so the cluster moves the chat's cursor.
type backfillJob struct {
	id    string
	from  string
	pages int
	walk  bool
	steps bool // a walk for the steps of a chat held whole: its pages move the steps cursor, not the history's
}

func (r *replica) current(generation int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled && r.generation == generation && r.connected()
}

// backfill reads the pages of turns of the chats the cluster lacks and hands them over as `feed_reset` events, one chat at
// a time with a pause between: the machine is somebody's working machine.
func (r *replica) backfill(generation int, jobs []backfillJob) {
	wait := r.retryWait
	for {
		var cut []backfillJob
		for _, job := range jobs {
			if !r.current(generation) {
				return
			}
			if rest := r.backfillChat(generation, job); rest != nil {
				cut = append(cut, *rest)
			}
			time.Sleep(r.pause)
		}
		if len(cut) == 0 || wait <= 0 {
			return
		}
		// The chats whose page did not come go on from where they stopped, a while later, while this backfill is the current one.
		time.Sleep(wait)
		if !r.current(generation) {
			return
		}
		Log("info", "backfill goes on with the chats a page stopped", F{"chats", strconv.Itoa(len(cut))})
		jobs, wait = cut, min(wait*2, replicaRetryWaitMax)
	}
}

// backfillChat reads one chat's pages; what is left of it when the runtime did not give a page (nil when nothing is).
func (r *replica) backfillChat(generation int, job backfillJob) *backfillJob {
	ch := "conv:" + job.id
	before := job.from
	limit := 100
	if job.walk {
		limit = replicaWalkTurns
	}
	for page := 0; page < job.pages; page++ {
		raw, err := r.pages(job.id, before, limit, job.walk)
		if err != nil {
			Log("warn", "chat page not read for the cluster", F{"conv", job.id}, F{"error", err.Error()})
			return &backfillJob{id: job.id, from: before, pages: job.pages - page, walk: job.walk}
		}
		var p struct {
			Turns        []json.RawMessage `json:"turns"`
			BeforeTurnID *string           `json:"beforeTurnId"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return nil
		}
		ev, ok := feedReset(raw)
		if ok && job.walk {
			ev, ok = markWalk(ev, job.steps, before)
		}
		if !ok || !r.current(generation) {
			return nil
		}
		r.emit(evV2Backfill{Event: "v2.backfill", Ch: ch, Events: []json.RawMessage{ev}})
		r.noteOutputs(ch, raw)
		if before == "" {
			// A page holds a folded turn as its first entry and its answer; the steps of the newest turns are read apart.
			r.backfillSteps(generation, job.id, p.Turns)
		}
		if p.BeforeTurnID == nil || *p.BeforeTurnID == "" || *p.BeforeTurnID == before {
			return nil
		}
		before = *p.BeforeTurnID
		time.Sleep(r.pause)
	}
	return nil
}

// markWalk says on a page of a walk where it was read from: the cluster moves the chat's cursor from `from` to the page's
// `beforeTurnId`, and a page with none is the chat's first.
func markWalk(ev json.RawMessage, steps bool, from string) (json.RawMessage, bool) {
	var p map[string]json.RawMessage
	if json.Unmarshal(ev, &p) != nil {
		return nil, false
	}
	mark, _ := json.Marshal(map[string]string{"from": from})
	key := "history"
	if steps {
		key = "steps" // a steps walk of a chat held whole moves the cluster's steps cursor (HRN-879)
	}
	p[key] = mark
	out, err := json.Marshal(p)
	return out, err == nil
}

func (r *replica) backfillSteps(generation int, id string, turns []json.RawMessage) {
	for i := len(turns) - 1; i >= 0 && i >= len(turns)-3; i-- {
		var t struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(turns[i], &t) != nil || t.ID == "" || !r.current(generation) {
			continue
		}
		raw, err := r.entries(id, t.ID)
		if err != nil {
			continue
		}
		var page struct {
			Entries []json.RawMessage `json:"entries"`
		}
		if json.Unmarshal(raw, &page) != nil || len(page.Entries) == 0 {
			continue
		}
		ev, _ := json.Marshal(map[string]any{"type": "feed_reset", "turns": []json.RawMessage{}, "entries": page.Entries, "beforeTurnId": nil})
		r.emit(evV2Backfill{Event: "v2.backfill", Ch: "conv:" + id, Events: []json.RawMessage{ev}})
		r.noteOutputs("conv:"+id, raw)
		time.Sleep(r.pause)
	}
}

// feedReset turns a page of turns into the event the cluster applies by its stable ids.
func feedReset(page json.RawMessage) (json.RawMessage, bool) {
	var p map[string]json.RawMessage
	if json.Unmarshal(page, &p) != nil || p["turns"] == nil || p["entries"] == nil {
		return nil, false
	}
	p["type"] = json.RawMessage(`"feed_reset"`)
	if p["beforeTurnId"] == nil {
		p["beforeTurnId"] = json.RawMessage(`null`)
	}
	ev, err := json.Marshal(p)
	return ev, err == nil
}

// ---- the loopback end -------------------------------------------------------------------------------------------

// startReplica reads the runtime's replica socket for as long as the agent runs.
func (a *Agent) startReplica(ctx context.Context) {
	r := a.replica
	r.askConvs = func() { a.replicaSend(`{"k":"convs"}`) }
	r.pages = func(conv, before string, limit int, steps bool) (json.RawMessage, error) {
		q := url.Values{"limit": {strconv.Itoa(limit)}}
		if before != "" {
			q.Set("before", before)
		}
		if steps {
			q.Set("steps", "all")
		}
		return a.runtimeGet(a.cfg.V2Path + "/conversations/" + url.PathEscape(conv) + "/turns?" + q.Encode())
	}
	r.entries = func(conv, turn string) (json.RawMessage, error) {
		return a.runtimeGet(a.cfg.V2Path + "/conversations/" + url.PathEscape(conv) + "/turns/" + url.PathEscape(turn) + "/entries")
	}
	r.output = func(conv, entry string) (json.RawMessage, error) {
		return a.runtimeGet(a.cfg.V2Path + "/conversations/" + url.PathEscape(conv) + "/entries/" + url.PathEscape(entry) + "/output")
	}
	go r.shipOutputs(ctx)
	go func() {
		wait := replicaRetryMin
		for ctx.Err() == nil {
			began := time.Now()
			a.replicaOnce(ctx)
			a.replicaMu.Lock()
			a.replicaConn = nil
			a.replicaMu.Unlock()
			if time.Since(began) > time.Minute {
				wait = replicaRetryMin
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = min(wait*2, replicaRetryMax)
		}
	}()
}

const (
	replicaRetryMin = 2 * time.Second
	replicaRetryMax = 30 * time.Second
)

func (a *Agent) replicaSend(text string) {
	a.replicaMu.Lock()
	conn := a.replicaConn
	a.replicaMu.Unlock()
	if conn == nil {
		return // asked again when the socket opens: its hello does
	}
	ctx, cancel := context.WithTimeout(context.Background(), statusSendLimit)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, []byte(text))
}

func (a *Agent) replicaOnce(ctx context.Context) {
	dialCtx, cancel := context.WithTimeout(ctx, relayHandshake)
	defer cancel()
	header := http.Header{}
	header.Set("cookie", a.runtimeCookie())
	conn, _, err := websocket.Dial(dialCtx, "ws://127.0.0.1:"+strconv.Itoa(a.cfg.RuntimePort)+a.cfg.V2Path+"/replica", &websocket.DialOptions{
		HTTPHeader:      header,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(relayReadLimit)
	a.replicaMu.Lock()
	a.replicaConn = conn
	a.replicaMu.Unlock()
	cctx, ccancel := context.WithCancel(ctx)
	defer ccancel()
	go every(cctx, 20*time.Second, false, func() { a.replicaSend(`{"k":"ping"}`) })
	for {
		_, data, err := conn.Read(cctx)
		if err != nil {
			return
		}
		a.replica.frame(data)
	}
}

// frame handles one frame of the runtime's replica socket.
func (r *replica) frame(data []byte) {
	var f struct {
		K             string            `json:"k"`
		Epoch         string            `json:"epoch"`
		Ch            string            `json:"ch"`
		Seq           int64             `json:"seq"`
		E             json.RawMessage   `json:"e"`
		Conversations []json.RawMessage `json:"conversations"`
	}
	if json.Unmarshal(data, &f) != nil {
		return
	}
	switch f.K {
	case "hello":
		r.hello(f.Epoch)
	case "ev":
		if f.Ch != "" && f.Seq > 0 && f.E != nil {
			r.event(f.Ch, f.Seq, f.E)
		}
	case "convs":
		r.convs(f.Conversations)
	}
}

// runtimePageTO is how long a page of the runtime may take to read: the first page of a long chat makes the runtime
// read its whole transcript (tens of megabytes), which can take longer than a command's runtimeCallTO; a page cut there
// stopped the chat's backfill.
const runtimePageTO = 90 * time.Second

// runtimeGet reads a page of the runtime over loopback, as a page would with its session.
func (a *Agent) runtimeGet(path string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runtimePageTO)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.runtimeURL(path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("cookie", a.runtimeCookie())
	res, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	out, err := io.ReadAll(io.LimitReader(res.Body, relayReadLimit))
	if err != nil {
		// Not «answered 200»: the answer was cut (the deadline, the runtime went away), and that is what the log says.
		return nil, fmt.Errorf("runtime answer not read whole (%d bytes): %w", len(out), err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runtime answered %d", res.StatusCode)
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("runtime answered %d with %d bytes that are not JSON", res.StatusCode, len(out))
	}
	return out, nil
}
