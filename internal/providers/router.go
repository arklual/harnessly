package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Router is the switch of the auto profile and the endpoint its chats talk to. A chat of the profile is Claude Code
// sent to the runtime's own address (RouterPath) in Anthropic's Messages format; the router puts each turn on one of
// the vendor subscriptions the person has a key for (GLM Coding Plan, Kimi Code), with that profile's key, and streams
// the answer back as it came. Nothing of the person's Anthropic account goes anywhere: the chat's settings stub its
// OAuth token like any vendor profile's.
//
// Which vendor takes a request is decided at every step of the agent: when the person writes (a user message that is
// no tool result), a short call to the light model reads the ask and says whether it needs the heavy model or the
// light one; at every step of the tool loop after it, the same call reads the ask, what the agent just did and what
// the tools gave back, and says what the next step needs. A plan or a failing test can go to the heavy model and
// reading files or running a given command to the light one within one task; when the step's classifier does not
// answer, the step stays where the turn is. A target that answers 429, 5xx or refuses the key before a byte was streamed
// is put aside for a while and the next one of the tier takes the request, so a used-up weekly quota of one
// subscription moves the work to the other. Every decision is a line of Dir/decisions.jsonl.
type Router struct {
	Dir string
	// OnFile keeps the switch across restarts: it exists while the switch is on.
	OnFile string
	// Base is the router as Claude Code reaches it (http://127.0.0.1:<port>/api/cli/router).
	Base string
	// Store holds the vendors' keys.
	Store *Store
	// Client sends the requests up; nil is one without a total timeout (a turn streams for minutes).
	Client *http.Client
	// Classify decides the tier of a new turn; nil asks the light model (classifyByModel).
	Classify func(ctx context.Context, req ClassifyRequest) (Tier, string, error)
	// Targets are the candidates of each tier in order; nil is RouterTargets.
	Targets map[Tier][]Target
	// Upstream gives a profile's Messages address; nil is the preset's BaseURL + /v1/messages.
	Upstream func(id string) string

	mu       sync.Mutex
	convs    map[string]*routedConv
	down     map[string]downState
	logMu    sync.Mutex
	now      func() time.Time
	onChange atomic.Pointer[func()]
}

// Tier is how much a turn needs.
type Tier string

const (
	TierHeavy Tier = "heavy"
	TierLight Tier = "light"
)

// Target is a model of a vendor profile a turn can go to.
type Target struct {
	Profile string `json:"profile"`
	Model   string `json:"model"`
}

// RouterTargets are the subscriptions' models by tier, in the order they are tried: the heavy tier is Kimi Code's
// strongest model, then GLM's; the light tier is GLM's fast model, then Kimi's everyday one. A profile without a key is
// skipped, so either subscription alone is enough.
var RouterTargets = map[Tier][]Target{
	TierHeavy: {{"kimi-code", "k3.1-pro"}, {"glm", "glm-5.3"}, {"glm-cn", "glm-5.3"}},
	TierLight: {{"glm", "glm-5.3-flash"}, {"glm-cn", "glm-5.3-flash"}, {"kimi-code", "kimi-for-coding"}},
}

// The models of the auto profile: one decided per turn, one always light (Claude Code's small model: titles, summaries).
const (
	RouterModelAuto = "auto"
	RouterModelFast = "auto-fast"
)

// routerClassifyTry is one light target's share of routerClassifyWait, so a slow one leaves time for the next.
var routerClassifyTry = 7 * time.Second

// RouterPath is where the runtime mounts the router.
const RouterPath = "/api/cli/router"

const (
	// routerConvTTL is how long a conversation's turn is remembered without a request.
	routerConvTTL = 6 * time.Hour
	// routerClassifyWait bounds the decision; past it the turn goes heavy.
	routerClassifyWait = 15 * time.Second
	// routerClassifyText is how much of the request the classifier reads.
	routerClassifyText = 6000
	// routerStepAsk and routerStepDid are how much of the ask and of the last step a step's classifier reads;
	// routerStepPart is the share of one tool call or result in it.
	routerStepAsk  = 2000
	routerStepDid  = 4000
	routerStepPart = 600
	// routerLogMax is the size past which decisions.jsonl is moved to decisions.jsonl.1.
	routerLogMax = 4 << 20
)

// How long a target is put aside after an answer of each kind, when the answer does not say itself (Retry-After).
const (
	downQuota = 15 * time.Minute
	downError = time.Minute
	downAuth  = 10 * time.Minute
)

var _ Switch = (*Router)(nil)

type routedConv struct {
	tier   Tier
	reason string
	target Target
	// step is the number of the request within the person's turn: 0 for the turn itself, 1 and on for its tool loop;
	// since is the step at which the turn came onto its tier.
	step, since int
	// vendors are the profiles that answered in this conversation: more than one means the history mixes them.
	vendors map[string]bool
	seen    time.Time
}

type downState struct {
	until  time.Time
	status int
}

func (r *Router) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *Router) keyFile() string { return filepath.Join(r.Dir, "client.key") }

// LogFile is the journal of decisions, one JSON line each.
func (r *Router) LogFile() string { return filepath.Join(r.Dir, "decisions.jsonl") }

// Notify sets what is told when the router's readiness may have changed.
func (r *Router) Notify(f func()) { r.onChange.Store(&f) }

func (r *Router) On() bool {
	_, err := os.Stat(r.OnFile)
	return err == nil
}

// Ready: the switch is on and at least one subscription has a key.
func (r *Router) Ready() bool { return r.On() && len(r.configured()) > 0 }

// Endpoint is the router's address and the file of the key Claude Code sends it.
func (r *Router) Endpoint() (string, string, bool) {
	if !r.On() || r.Base == "" {
		return "", "", false
	}
	if _, err := os.Stat(r.keyFile()); err != nil {
		return "", "", false
	}
	return r.Base, r.keyFile(), true
}

// SetOn turns the switch. On needs a key of GLM or Kimi Code (a *wire.ProfileSwitchError without one).
func (r *Router) SetOn(_ context.Context, on bool) error {
	if !on {
		if err := os.Remove(r.OnFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if len(r.configured()) == 0 {
		return &wire.ProfileSwitchError{Reason: wire.SwitchNoLogin, Message: "no key of GLM or Kimi Code is saved"}
	}
	if r.Base == "" {
		return &wire.ProfileSwitchError{Reason: wire.SwitchStart, Message: "the runtime's port is unknown"}
	}
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	if _, err := r.clientKey(); err != nil {
		return err
	}
	return writePrivate(r.OnFile, nil)
}

// clientKey is made once and kept, so the settings of running chats stay right.
func (r *Router) clientKey() (string, error) {
	if raw, err := os.ReadFile(r.keyFile()); err == nil && len(strings.TrimSpace(string(raw))) >= 32 {
		return strings.TrimSpace(string(raw)), nil
	}
	key := "hrn-" + randomHex(24)
	return key, writePrivate(r.keyFile(), []byte(key))
}

// configured are the profiles of the targets that have a key, each once.
func (r *Router) configured() []string {
	var out []string
	seen := map[string]bool{}
	for _, tier := range []Tier{TierHeavy, TierLight} {
		for _, t := range r.targets()[tier] {
			if !seen[t.Profile] && r.Store != nil && r.Store.Configured(t.Profile) {
				out = append(out, t.Profile)
			}
			seen[t.Profile] = true
		}
	}
	return out
}

func (r *Router) targets() map[Tier][]Target {
	if r.Targets != nil {
		return r.Targets
	}
	return RouterTargets
}

func (r *Router) upstream(id string) string {
	if r.Upstream != nil {
		return r.Upstream(id)
	}
	p, ok := Find(id)
	if !ok {
		return ""
	}
	return strings.TrimRight(p.BaseURL, "/") + "/v1/messages"
}

func (r *Router) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{
		Transport:     &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 5 * time.Minute, TLSHandshakeTimeout: 15 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// allow: the request carries the router's key, as x-api-key or a bearer token (Claude Code sends the helper's key as
// both, depending on its version).
func (r *Router) allow(req *http.Request) bool {
	key, err := r.clientKey()
	if err != nil || key == "" {
		return false
	}
	got := req.Header.Get("X-Api-Key")
	if got == "" {
		got, _ = strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	}
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1
}

// routerForwarded are the headers of Claude Code's request that go up with it.
var routerForwarded = []string{"Content-Type", "Accept", "User-Agent", "Anthropic-Version", "Anthropic-Beta", "X-App"}

// ServeHTTP takes /v1/messages and /v1/messages/count_tokens of a chat on the auto profile.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, RouterPath)
	if rest == req.URL.Path || !r.On() {
		http.NotFound(w, req)
		return
	}
	if !r.allow(req) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if req.Method != http.MethodPost || (rest != "/v1/messages" && rest != "/v1/messages/count_tokens") {
		relayError(w, http.StatusNotFound, "the router takes POST /v1/messages only")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, req.Body, RelayBodyMax))
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		relayError(w, http.StatusBadRequest, "the body is not a JSON object")
		return
	}
	var msgs []message
	_ = json.Unmarshal(body["messages"], &msgs)
	model := rawString(body["model"])
	conv := convKey(body, msgs)
	counting := strings.HasSuffix(rest, "/count_tokens")
	started := r.clock()

	tier, reason, newTurn, sticky, step := r.decide(req.Context(), conv, model, msgs, counting)
	cands := r.candidates(tier, sticky)
	if len(cands) == 0 {
		r.record(decision{Conv: conv, NewTurn: newTurn, Step: step, Tier: tier, Reason: reason, Status: 0, Error: "no target with a key is up"})
		relayError(w, http.StatusServiceUnavailable, "no subscription of the auto profile is available now (no key, or every one is resting after its limit)")
		return
	}
	headers := http.Header{}
	for _, h := range routerForwarded {
		if v := req.Header.Get(h); v != "" {
			headers.Set(h, v)
		}
	}
	for i, t := range cands {
		last := i == len(cands)-1
		out, err := r.bodyFor(body, msgs, t, conv, newTurn)
		if err != nil {
			relayError(w, http.StatusBadRequest, err.Error())
			return
		}
		res, err := r.send(req.Context(), t, rest, out, headers)
		d := decision{Conv: conv, NewTurn: newTurn, Step: step, Tier: tier, Reason: reason, Target: t, Model: model}
		if err != nil {
			d.Error = "no answer: " + err.Error()
			d.Ms = r.clock().Sub(started).Milliseconds()
			r.record(d)
			if req.Context().Err() != nil {
				return
			}
			r.putAside(t.Profile, 0, nil)
			if last {
				relayError(w, http.StatusBadGateway, "the vendor did not answer")
				return
			}
			continue
		}
		d.Status = res.StatusCode
		d.Ms = r.clock().Sub(started).Milliseconds()
		if retryable(res.StatusCode) && !last {
			r.putAside(t.Profile, res.StatusCode, res.Header)
			_ = res.Body.Close()
			d.Error = "set aside"
			r.record(d)
			continue
		}
		if retryable(res.StatusCode) {
			r.putAside(t.Profile, res.StatusCode, res.Header)
		}
		r.record(d)
		if !counting && res.StatusCode < 300 {
			r.answered(conv, t)
		}
		stream(w, res)
		_ = res.Body.Close()
		return
	}
}

// retryable: the target cannot take the request now, another one may.
func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusUnauthorized || status == http.StatusForbidden ||
		status == http.StatusPaymentRequired || status >= 500
}

// decide is the tier of a request: a new turn of model auto asks the classifier, a tool loop keeps its turn's tier,
// auto-fast and count_tokens are light without asking. sticky is the target of the turn, when it has one.
func (r *Router) decide(ctx context.Context, conv, model string, msgs []message, counting bool) (Tier, string, bool, *Target, int) {
	newTurn := startsTurn(msgs)
	if counting {
		return TierLight, "count_tokens", false, nil, 0
	}
	if model == RouterModelFast {
		return TierLight, "auto-fast", newTurn, nil, 0
	}
	cr := ClassifyRequest{Text: turnAsk(msgs), Turns: countTurns(msgs)}
	r.mu.Lock()
	c := r.convs[conv]
	var sticky *Target
	if !newTurn && c != nil {
		c.step++
		cr.Step = c.step
		cr.Current = c.tier
		cr.OnCurrent = c.step - c.since
		if c.target.Profile != "" {
			cp := c.target
			sticky = &cp
		}
	}
	r.mu.Unlock()
	if !newTurn {
		cr.Did = stepText(msgs)
		cr.Errors = failedSteps(msgs)
		if cr.Step == 0 {
			// A tool loop of a turn the router has not seen (the runtime restarted in it): decided as a step anyway.
			cr.Step = 1
		}
	}
	classify := r.Classify
	if classify == nil {
		classify = r.classifyByModel
	}
	cctx, cancel := context.WithTimeout(ctx, routerClassifyWait)
	defer cancel()
	tier, reason, err := classify(cctx, cr)
	if err != nil || (tier != TierHeavy && tier != TierLight) {
		why := "classifier failed"
		if err != nil {
			why += " (" + err.Error() + ")"
		}
		if !newTurn && cr.Current != "" {
			// A step keeps the turn's tier and target rather than jumping heavy on a slow classifier.
			tier, reason = cr.Current, why+": stays "+string(cr.Current)
		} else {
			tier, reason = TierHeavy, why+": heavy"
		}
	}
	if !newTurn {
		reason = fmt.Sprintf("step %d: %s", cr.Step, reason)
		if sticky != nil && tier != cr.Current {
			sticky = nil
		}
	}
	r.mu.Lock()
	r.gcLocked()
	c = r.convs[conv]
	if c == nil {
		c = &routedConv{vendors: map[string]bool{}}
		r.convs[conv] = c
	}
	if newTurn {
		c.step, c.since = 0, 0
		c.target = Target{}
	} else {
		if tier != cr.Current {
			c.since = cr.Step
		}
		c.step = cr.Step
		if sticky == nil {
			c.target = Target{}
		}
	}
	c.tier, c.reason, c.seen = tier, reason, r.clock()
	r.mu.Unlock()
	return tier, reason, newTurn, sticky, cr.Step
}

// candidates are the targets of the tier with a key and not put aside, the turn's own first; when every one of the
// tier rests, the other tier's take the request rather than none.
func (r *Router) candidates(tier Tier, sticky *Target) []Target {
	now := r.clock()
	r.mu.Lock()
	defer r.mu.Unlock()
	up := func(t Target) bool {
		if r.Store == nil || !r.Store.Configured(t.Profile) {
			return false
		}
		d, ok := r.down[t.Profile]
		return !ok || now.After(d.until)
	}
	var out []Target
	add := func(t Target) {
		for _, have := range out {
			if have == t {
				return
			}
		}
		if up(t) {
			out = append(out, t)
		}
	}
	if sticky != nil && sticky.Profile != "" {
		add(*sticky)
	}
	for _, t := range r.targets()[tier] {
		add(t)
	}
	if len(out) == 0 {
		other := TierLight
		if tier == TierLight {
			other = TierHeavy
		}
		for _, t := range r.targets()[other] {
			add(t)
		}
	}
	return out
}

// putAside rests a profile after an answer that says it cannot take requests now.
func (r *Router) putAside(id string, status int, h http.Header) {
	wait := downError
	switch {
	case status == http.StatusTooManyRequests || status == http.StatusPaymentRequired:
		wait = downQuota
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		wait = downAuth
	}
	if h != nil {
		if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
	}
	r.mu.Lock()
	if r.down == nil {
		r.down = map[string]downState{}
	}
	r.down[id] = downState{until: r.clock().Add(wait), status: status}
	r.mu.Unlock()
	r.changed()
}

func (r *Router) changed() {
	if f := r.onChange.Load(); f != nil && *f != nil {
		(*f)()
	}
}

// answered remembers the target that took the conversation's turn and that its vendor is in the history now.
func (r *Router) answered(conv string, t Target) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.convs == nil {
		r.convs = map[string]*routedConv{}
	}
	c := r.convs[conv]
	if c == nil {
		c = &routedConv{tier: TierHeavy, vendors: map[string]bool{}}
		r.convs[conv] = c
	}
	c.target, c.seen = t, r.clock()
	c.vendors[t.Profile] = true
}

func (r *Router) gcLocked() {
	if r.convs == nil {
		r.convs = map[string]*routedConv{}
	}
	cut := r.clock().Add(-routerConvTTL)
	for k, c := range r.convs {
		if c.seen.Before(cut) {
			delete(r.convs, k)
		}
	}
}

// bodyFor is the request as target t takes it: its model, and, once the history mixes vendors, without the reasoning
// blocks of the turns before this one (signed by another vendor, they would be refused or misread).
func (r *Router) bodyFor(body map[string]json.RawMessage, msgs []message, t Target, conv string, newTurn bool) ([]byte, error) {
	out := make(map[string]json.RawMessage, len(body))
	for k, v := range body {
		out[k] = v
	}
	model, _ := json.Marshal(t.Model)
	out["model"] = model
	r.mu.Lock()
	mixed := false
	if c := r.convs[conv]; c != nil {
		mixed = len(c.vendors) > 1 || (len(c.vendors) == 1 && !c.vendors[t.Profile])
		if !newTurn && c.target.Profile != "" && c.target.Profile != t.Profile {
			// The turn itself changed vendor (a failover in its tool loop): no reasoning block of it stays either.
			mixed = true
		}
	}
	r.mu.Unlock()
	if mixed {
		if stripped, ok := stripThinking(body["messages"], msgs); ok {
			out["messages"] = stripped
		}
	}
	return json.Marshal(out)
}

// send puts the request on target t with its profile's key.
func (r *Router) send(ctx context.Context, t Target, rest string, body []byte, headers http.Header) (*http.Response, error) {
	key, err := r.Store.Credential(ctx, t.Profile)
	if err != nil {
		return nil, err
	}
	url := r.upstream(t.Profile)
	if url == "" {
		return nil, fmt.Errorf("no address of %s", t.Profile)
	}
	if strings.HasSuffix(rest, "/count_tokens") {
		url += "/count_tokens"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, vs := range headers {
		req.Header[k] = append([]string(nil), vs...)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", key)
	req.Header.Set("Authorization", "Bearer "+key)
	return r.client().Do(req)
}

// stream copies the vendor's answer to Claude Code as it comes, an event at a time.
func stream(w http.ResponseWriter, res *http.Response) {
	for _, h := range []string{"Content-Type", "Retry-After", "Request-Id", "X-Request-Id"} {
		if v := res.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	if !strings.Contains(res.Header.Get("Content-Type"), "event-stream") {
		_, _ = io.Copy(w, res.Body)
		return
	}
	flusher, _ := w.(http.Flusher)
	br := bufio.NewReaderSize(res.Body, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				return
			}
			if flusher != nil && (len(line) == 1 || bytes.Equal(line, []byte("\r\n"))) {
				flusher.Flush()
			}
		}
		if err != nil {
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
	}
}

// decision is a line of the journal. It holds no key and no text of the chat.
type decision struct {
	At      string `json:"at"`
	Conv    string `json:"conv"`
	NewTurn bool   `json:"new_turn"`
	Step    int    `json:"step,omitempty"`
	Tier    Tier   `json:"tier"`
	Reason  string `json:"reason"`
	Model   string `json:"model,omitempty"`
	Target  Target `json:"target"`
	Status  int    `json:"status"`
	Ms      int64  `json:"ms"`
	Error   string `json:"error,omitempty"`
}

func (r *Router) record(d decision) {
	d.At = r.clock().UTC().Format(time.RFC3339)
	line, err := json.Marshal(d)
	if err != nil {
		return
	}
	r.logMu.Lock()
	defer r.logMu.Unlock()
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return
	}
	if st, err := os.Stat(r.LogFile()); err == nil && st.Size() > routerLogMax {
		_ = os.Rename(r.LogFile(), r.LogFile()+".1")
	}
	f, err := os.OpenFile(r.LogFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		jsonlog.Log("warn", "providers: the router's journal is not writable", jsonlog.F{K: "error", V: err.Error()})
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// message is a message of the Messages API as far as the router reads it.
type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (m message) blocks() []block {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	_ = json.Unmarshal(m.Content, &bs)
	return bs
}

// startsTurn: the last message is the person's, not the results of tools. Claude Code puts its own notes after the
// person's message as messages of role system; they do not end a turn, so they are passed over.
func startsTurn(msgs []message) bool {
	last, ok := lastSaid(msgs)
	if !ok {
		return true
	}
	return last.Role == "user" && startsTurnAt(last)
}

// lastSaid is the last message that is not a note of role system.
func lastSaid(msgs []message) (message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "system" {
			return msgs[i], true
		}
	}
	return message{}, false
}

// countTurns is how many messages of the person the conversation has.
func countTurns(msgs []message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "user" && startsTurnAt(m) {
			n++
		}
	}
	return n
}

func startsTurnAt(m message) bool {
	for _, b := range m.blocks() {
		if b.Type == "tool_result" {
			return false
		}
	}
	return true
}

// turnAsk is the text of the person's last message (the last user message that is no tool result), its end kept when
// it is long (the ask is usually at the end).
func turnAsk(msgs []message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && startsTurnAt(msgs[i]) {
			return blockText(msgs[i])
		}
	}
	return ""
}

func blockText(m message) string {
	var parts []string
	for _, b := range m.blocks() {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if r := []rune(text); len(r) > routerClassifyText {
		text = "…" + string(r[len(r)-routerClassifyText:])
	}
	return text
}

// stepText is the agent's last step as the step's classifier reads it: its text and tool calls, then what the tools
// answered, each cut short.
func stepText(msgs []message) string {
	end := len(msgs)
	for end > 0 && msgs[end-1].Role == "system" {
		end--
	}
	start := end
	for start > 0 && msgs[start-1].Role == "user" {
		start--
	}
	var parts []string
	if start > 0 && msgs[start-1].Role == "assistant" {
		for _, b := range msgs[start-1].stepBlocks() {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) != "" {
					parts = append(parts, "agent: "+cutHead(b.Text, routerStepPart))
				}
			case "tool_use":
				parts = append(parts, "call "+b.Name+": "+cutHead(string(b.Input), routerStepPart))
			}
		}
	}
	for _, m := range msgs[start:end] {
		for _, b := range m.stepBlocks() {
			if b.Type != "tool_result" {
				continue
			}
			head := "result: "
			if b.IsError {
				head = "error result: "
			}
			parts = append(parts, head+cutHead(b.resultText(), routerStepPart))
		}
	}
	return cutHead(strings.Join(parts, "\n"), routerStepDid)
}

// failedSteps is how many of the turn's last steps in a row got an error result from a tool.
func failedSteps(msgs []message) int {
	n := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != "user" {
			continue
		}
		results, failed := 0, false
		for _, b := range m.stepBlocks() {
			if b.Type == "tool_result" {
				results++
				failed = failed || b.IsError
			}
		}
		if results == 0 || !failed {
			return n
		}
		n++
	}
	return n
}

// stepBlock is a block of a tool loop as the step's classifier reads it.
type stepBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

func (m message) stepBlocks() []stepBlock {
	var bs []stepBlock
	_ = json.Unmarshal(m.Content, &bs)
	return bs
}

func (b stepBlock) resultText() string {
	var s string
	if json.Unmarshal(b.Content, &s) == nil {
		return s
	}
	var parts []string
	for _, x := range (message{Content: b.Content}).blocks() {
		if x.Type == "text" {
			parts = append(parts, x.Text)
		} else if x.Type != "" {
			parts = append(parts, "["+x.Type+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// cutHead keeps the start of a text, n runes at most.
func cutHead(text string, n int) string {
	if r := []rune(text); len(r) > n {
		return string(r[:n]) + "…"
	}
	return text
}

// convKey tells conversations apart: Claude Code's session (metadata.user_id) and the first message, so a sub-agent
// of the chat is a conversation of its own.
func convKey(body map[string]json.RawMessage, msgs []message) string {
	var meta struct {
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal(body["metadata"], &meta)
	h := sha256.New()
	h.Write([]byte(meta.UserID))
	h.Write([]byte{0})
	if len(msgs) > 0 {
		h.Write(msgs[0].Content)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func rawString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// stripThinking drops thinking and redacted_thinking blocks from the assistant messages; ok is false when there was
// none.
func stripThinking(raw json.RawMessage, msgs []message) (json.RawMessage, bool) {
	var all []map[string]json.RawMessage
	if json.Unmarshal(raw, &all) != nil || len(all) != len(msgs) {
		return raw, false
	}
	changed := false
	for i, m := range all {
		if msgs[i].Role != "assistant" {
			continue
		}
		var blocks []json.RawMessage
		if json.Unmarshal(m["content"], &blocks) != nil {
			continue
		}
		kept := blocks[:0:0]
		for _, b := range blocks {
			var head struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(b, &head)
			if head.Type == "thinking" || head.Type == "redacted_thinking" {
				changed = true
				continue
			}
			kept = append(kept, b)
		}
		if len(kept) == 0 {
			kept = append(kept, json.RawMessage(`{"type":"text","text":"…"}`))
		}
		content, _ := json.Marshal(kept)
		m["content"] = content
	}
	if !changed {
		return raw, false
	}
	out, err := json.Marshal(all)
	if err != nil {
		return raw, false
	}
	return out, true
}

// ClassifyRequest is what the classifier reads of a new turn.
type ClassifyRequest struct {
	// Text is the person's message (its end, when long).
	Text string
	// Turns is how many messages of the person the conversation has, this one included.
	Turns int
	// Step is the step of the turn's tool loop the request is (1 and on); 0 is the turn itself.
	Step int
	// Current is the tier the turn is on now, for a step.
	Current Tier
	// Did is what the agent did at the step before and what the tools gave back (each part cut short), for a step.
	Did string
	// OnCurrent is how many steps the turn has been on Current; Errors is how many of the last steps in a row got an
	// error back from a tool.
	OnCurrent, Errors int
}

// classifyPrompt is the instruction of the light model that decides a turn. It describes the two tiers by what the
// work needs; the decision is the model's.
const classifyPrompt = `You route turns of a coding agent between two models.
heavy: a strong slow model. Use it when the turn needs real reasoning or several steps: designing or changing code across files, debugging an unknown cause, reviewing, planning, migrations, infrastructure changes, anything risky or ambiguous, or a long task.
light: a fast cheap model. Use it when the turn is small and clear: a short answer, a lookup, renaming, formatting, a one-line fix that is already spelled out, running a given command, a status question, a confirmation like "ok, go on" of work already planned.
When unsure, choose heavy.
Answer with one JSON object and nothing else: {"tier":"heavy"|"light","reason":"<at most 12 words>"}`

// stepPrompt is the instruction of the light model that decides a step of a turn's tool loop.
const stepPrompt = `You route the steps of a coding agent between two models while it works on a task.
heavy: a strong slow model. The next step needs it when the agent has to think: plan or re-plan, design or write non-trivial code, find the cause of a failure or of an unexpected result, decide between options, review what was done, or write the final answer of a complex task.
light: a fast cheap model. The next step needs only it when it is mechanical and already clear from what was done: read or search more files, run the next command of a plan, apply an edit already decided, re-run tests after a fix, report a plain result.
The step is on the current model now. Moving to the other one drops its cached context and its reasoning, so move only when the next step clearly needs the other model. Tool errors several steps in a row or no progress on the light model mean it is stuck: move to heavy. Once the heavy model has decided what to do and the rest is plain execution, move to light.
Answer with one JSON object and nothing else: {"tier":"heavy"|"light","reason":"<at most 12 words>"}`

// classifyByModel asks the light targets with a key one by one: one that is slow or failing gives its time slice to
// the next instead of sending the turn heavy.
func (r *Router) classifyByModel(ctx context.Context, cr ClassifyRequest) (Tier, string, error) {
	cands := r.candidates(TierLight, nil)
	if len(cands) == 0 {
		return "", "", errors.New("no light target")
	}
	var errs []string
	for i, t := range cands {
		wait := routerClassifyTry
		if dl, ok := ctx.Deadline(); ok {
			// The last candidate gets whatever is left.
			if left := time.Until(dl); i == len(cands)-1 || left < wait {
				wait = left
			}
		}
		if wait <= 0 {
			break
		}
		tctx, cancel := context.WithTimeout(ctx, wait)
		tier, reason, err := r.classifyWith(tctx, t, cr)
		cancel()
		if err == nil {
			if i > 0 {
				reason += " (by " + t.Profile + ")"
			}
			return tier, reason, nil
		}
		errs = append(errs, t.Profile+": "+err.Error())
		if ctx.Err() != nil {
			break
		}
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}

func (r *Router) classifyWith(ctx context.Context, t Target, cr ClassifyRequest) (Tier, string, error) {
	system := classifyPrompt
	user := fmt.Sprintf("Message %d of the conversation:\n<message>\n%s\n</message>", cr.Turns, cr.Text)
	if cr.Step > 0 {
		system = stepPrompt
		user = fmt.Sprintf("The person's task:\n<task>\n%s\n</task>\n\nStep %d of the agent's work on it. What the agent did last and what came back:\n<step>\n%s\n</step>\n\nThe current model is %s, for %d steps. Steps in a row that got a tool error: %d. What does the next step need?",
			cutHead(cr.Text, routerStepAsk), cr.Step, cr.Did, cr.Current, cr.OnCurrent, cr.Errors)
	}
	body, _ := json.Marshal(map[string]any{
		"model":      t.Model,
		"max_tokens": 1024,
		"system":     system,
		"thinking":   map[string]string{"type": "disabled"},
		"messages":   []map[string]string{{"role": "user", "content": user}},
	})
	h := http.Header{}
	h.Set("Anthropic-Version", "2023-06-01")
	res, err := r.send(ctx, t, "/v1/messages", body, h)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		if retryable(res.StatusCode) {
			r.putAside(t.Profile, res.StatusCode, res.Header)
		}
		return "", "", fmt.Errorf("answered %d", res.StatusCode)
	}
	var out struct {
		Content []block `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", "", err
	}
	var text string
	for _, b := range out.Content {
		if b.Type == "text" {
			text += b.Text
		}
	}
	return parseVerdict(text)
}

// parseVerdict reads the classifier's JSON object out of its answer.
func parseVerdict(text string) (Tier, string, error) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return "", "", errors.New("no JSON in the classifier's answer")
	}
	var v struct {
		Tier   string `json:"tier"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &v); err != nil {
		return "", "", err
	}
	switch Tier(v.Tier) {
	case TierHeavy, TierLight:
		return Tier(v.Tier), v.Reason, nil
	}
	return "", "", fmt.Errorf("unknown tier %q", v.Tier)
}

// RouterStatus is what the router says of its targets now: which rest and until when.
func (r *Router) RouterStatus() map[string]time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]time.Time{}
	now := r.clock()
	for id, d := range r.down {
		if d.until.After(now) {
			out[id] = d.until
		}
	}
	return out
}
