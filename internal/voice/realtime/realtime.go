package realtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

// Token is what a call is made with. It is never logged or answered to the page.
type Token struct {
	Access  string
	Account string
}

// TokenSource gives the token of the subscription route: the runtime reads the machine's Codex sign-in each time, so a
// token Codex refreshed is picked up and nothing is rotated here. ok is false when there is no ChatGPT sign-in.
type TokenSource interface {
	Token() (Token, bool)
}

// TokenFunc adapts a function to TokenSource.
type TokenFunc func() (Token, bool)

// Token implements TokenSource.
func (f TokenFunc) Token() (Token, bool) { return f() }

// KeyFunc gives the OpenAI API key of the key route; ok is false when the machine keeps none.
type KeyFunc func() (string, bool)

// Endpoints are where the calls and their control sockets go.
type Endpoints struct {
	Call    string // the subscription route's call base, …/backend-api/codex
	Live    string // the subscription route's control sockets, wss://…/v1/live
	KeyCall string // the key route's calls, …/v1/realtime/calls
	KeyLive string // the key route's control sockets, wss://…/v1/realtime
}

// DefaultEndpoints are OpenAI's own.
func DefaultEndpoints() Endpoints {
	return Endpoints{
		Call:    "https://chatgpt.com/backend-api/codex",
		Live:    "wss://api.openai.com/v1/live",
		KeyCall: "https://api.openai.com/v1/realtime/calls",
		KeyLive: "wss://api.openai.com/v1/realtime",
	}
}

// Config of a Service.
type Config struct {
	Dir       string // where the settings live
	Tokens    TokenSource
	Key       KeyFunc
	Endpoints Endpoints
	Client    *http.Client
	// Handoff hears every task the voice handed over (step "task") and every result told back for one (step
	// "result"), with the call's mode: the runtime writes them into its journal.
	Handoff func(step, mode, task, text string)
	// Notes gives the person's agents' notes about them for the chat a call was opened from (MemoryNotes of its
	// project); nil gives none.
	Notes func(conv string) []string
	// Primed hears the sizes of what a call started knowing (Primer.Sizes): the runtime journals them, never the words.
	Primed func(route string, sizes map[string]int)
	// Relayed hears how the machine's own connection of a call over the machine goes (relay.go): its states, the
	// page joining, the frames counted; never the sound or the words. ctx is the call's start request, without its
	// end: its scope (trace and chat) lets the chat's agent read the lines with its own journal tool.
	Relayed func(ctx context.Context, level, text string)
	// Live hears when a call opened from a chat begins (true) and ends (false): while it lasts, the chat's agent asks
	// the person before it reaches outside (internal/voice/consent).
	Live func(conv string, on bool)
	// SubscriptionOn is the installation's default of the subscription route for a person who never turned it
	// (RUNTIME_VOICE_SUBSCRIPTION=on, the team's own deployment): off for any other installation, whose admin decides.
	// A person's own off stays off.
	SubscriptionOn bool
	// WakeModelURL and WakeModelSHA256 replace WakeModel's (tests).
	WakeModelURL    string
	WakeModelSHA256 string
}

// Mode says what the voice does with a request.
const (
	ModeAgent = "agent" // hands work over to the chat's agent as tasks
	ModeChat  = "chat"  // a plain conversation, answered by the voice itself
	// ModeDigest reads the day's digest out phrase by phrase, as the page sends them, and answers questions about it
	// itself; it hands nothing over.
	ModeDigest = "digest"
)

// Routes a call can take.
const (
	RouteSubscription = "subscription" // ChatGPT's voice route of Codex, with the machine's Codex sign-in (unofficial)
	RouteKey          = "key"          // the public Realtime API, with the machine's OpenAI key
)

// Model is the subscription route's own model.
const Model = "gpt-live-1.1-codex"

// Models each route offers; the first is the default.
var Models = map[string][]string{
	RouteSubscription: {Model},
	RouteKey:          {"gpt-realtime-2.1", "gpt-realtime-2", "gpt-realtime", "gpt-realtime-mini"},
}

// Voices each route speaks with; the first is the default. The subscription route speaks only ChatGPT's own voices:
// with any other name (the API's marin, cedar, verse…) the call is made and answered, but OpenAI's media server
// never answers a connectivity check, so the call dies with no error anywhere (live probes, 15.11).
var Voices = map[string][]string{
	RouteSubscription: {"cove", "arbor", "breeze", "ember", "juniper", "maple", "sol", "spruce", "vale"},
	RouteKey:          {"marin", "cedar", "alloy", "ash", "ballad", "coral", "echo", "sage", "shimmer", "verse"},
}

// Errors answered to the page by code.
const (
	CodeOff        = "voice_off"          // no route: the subscription route is off and the machine has no OpenAI key
	CodeNoLogin    = "voice_no_login"     // the subscription route is on, but there is no Codex sign-in and no key
	CodeRefused    = "voice_refused"      // OpenAI refused the call
	CodeUnreached  = "voice_unreached"    // OpenAI did not answer
	CodeNoCall     = "voice_no_call"      // no such call (ended or never made)
	CodeBadRequest = "voice_bad_request"  // the page sent something malformed
	CodeEnded      = "voice_call_ended"   // the call is over
	CodeStore      = "voice_store_failed" // the settings could not be written
)

// Error carries a code for the page and the HTTP status it goes with.
type Error struct {
	Code   string
	Status int
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

// Settings the person keeps.
type Settings struct {
	Subscription bool   `json:"subscription"` // the unofficial subscription route is on; always written once saved
	Voice        string `json:"voice,omitempty"`
	Model        string `json:"model,omitempty"`
	// Talk is how a call shows in the field: TalkText (the words written on its line) or TalkVoice (nothing written,
	// only heard, like ChatGPT's voice-only mode). Empty is TalkText.
	Talk string `json:"talk,omitempty"`
	// Head is the voice agent's look in the field: HeadH (the default) or HeadFace.
	Head string `json:"head,omitempty"`
	// NoBarge keeps the voice talking when the person speaks over it (barge-in is on by default).
	NoBarge bool `json:"noBarge,omitempty"`
	// About is what the person tells the voice about themselves and how to answer them, like ChatGPT's custom
	// instructions; it goes only into their own calls.
	About string `json:"about,omitempty"`
	// Notes: the voice also knows their agents' notes about them (memories of type user or feedback). Nil is on.
	Notes *bool `json:"notes,omitempty"`
	// Delivery is how the work is told while it goes by default: DeliveryNarrate (empty) or DeliveryResult.
	Delivery string `json:"delivery,omitempty"`
	// Effort is the default step of Efforts; empty is EffortInstant.
	Effort string `json:"effort,omitempty"`
	// Wake starts the voice by a spoken phrase, heard in the page itself (wake.go). Nil when never set: a full save
	// that leaves it out keeps the one on the disk.
	Wake *Wake `json:"wake,omitempty"`
}

// The voice agent's look in the field (Settings.Head).
const (
	HeadH    = "h"
	HeadFace = "face"
)

func (st Settings) notesOn() bool { return st.Notes == nil || *st.Notes }

// How a call shows in the field (Settings.Talk).
const (
	TalkText  = "text"
	TalkVoice = "voice"
)

// State is what Settings and the voice button show.
type State struct {
	Subscription bool     `json:"subscription"`
	SignedIn     bool     `json:"signedIn"` // Codex is signed in with ChatGPT on this machine
	HasKey       bool     `json:"hasKey"`   // the machine keeps an OpenAI key
	Route        string   `json:"route"`    // the route a call takes now; "" when none
	Ready        bool     `json:"ready"`
	Voice        string   `json:"voice"`
	Voices       []string `json:"voices"`
	Model        string   `json:"model"`
	Models       []string `json:"models"`
	Talk         string   `json:"talk"`
	Head         string   `json:"head"`
	Barge        bool     `json:"barge"` // speaking over the voice stops it
	About        string   `json:"about"`
	Notes        bool     `json:"notes"`
	Delivery     string   `json:"delivery"`
	Effort       string   `json:"effort"`
	Efforts      []string `json:"efforts"`
	Wake         Wake     `json:"wake"`
	// Relay: the machine can carry a call's sound itself (relay.go), for networks without UDP to the voice.
	Relay bool `json:"relay"`
}

// Service makes calls and keeps their control sockets.
type Service struct {
	cfg Config

	mu    sync.Mutex
	calls map[string]*Call
	// wakeMu lets one request fetch the wake model while the others wait for it.
	wakeMu sync.Mutex
}

// New makes a Service.
func New(cfg Config) *Service {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 30 * time.Second}
	}
	def := DefaultEndpoints()
	for _, p := range []struct {
		got *string
		def string
	}{
		{&cfg.Endpoints.Call, def.Call}, {&cfg.Endpoints.Live, def.Live},
		{&cfg.Endpoints.KeyCall, def.KeyCall}, {&cfg.Endpoints.KeyLive, def.KeyLive},
	} {
		if *p.got == "" {
			*p.got = p.def
		}
	}
	return &Service{cfg: cfg, calls: map[string]*Call{}}
}

func (s *Service) settingsPath() string { return filepath.Join(s.cfg.Dir, "settings.json") }

// Settings read from the disk; the subscription route is off when nothing was saved.
func (s *Service) Settings() Settings {
	var st Settings
	raw, err := os.ReadFile(s.settingsPath())
	if err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	// Never turned: the installation's default. Turned once: the person's own choice, kept even when off.
	var seen map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &seen) != nil || seen["subscription"] == nil {
		st.Subscription = s.cfg.SubscriptionOn
	}
	return st
}

// SaveSettings writes the settings.
func (s *Service) SaveSettings(st Settings) error {
	raw, _ := json.Marshal(st)
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	tmp := s.settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	if err := os.Rename(tmp, s.settingsPath()); err != nil {
		return &Error{Code: CodeStore, Status: http.StatusInternalServerError, Detail: err.Error()}
	}
	return nil
}

func (s *Service) token() (Token, bool) {
	if s.cfg.Tokens == nil {
		return Token{}, false
	}
	return s.cfg.Tokens.Token()
}

func (s *Service) key() (string, bool) {
	if s.cfg.Key == nil {
		return "", false
	}
	k, ok := s.cfg.Key()
	return k, ok && k != ""
}

// route is the route a call takes now: the subscription one when it is on and Codex is signed in, else the key one
// when the machine keeps a key.
func route(st Settings, signed, hasKey bool) string {
	if st.Subscription && signed {
		return RouteSubscription
	}
	if hasKey {
		return RouteKey
	}
	return ""
}

func pick(want, saved string, from []string) string {
	for _, v := range []string{want, saved} {
		if slices.Contains(from, v) {
			return v
		}
	}
	if len(from) == 0 {
		return ""
	}
	return from[0]
}

// State of the voice mode.
func (s *Service) State() State {
	st := s.Settings()
	_, signed := s.token()
	_, hasKey := s.key()
	r := route(st, signed, hasKey)
	view := r
	if view == "" {
		view = RouteSubscription
	}
	return State{
		Subscription: st.Subscription, SignedIn: signed, HasKey: hasKey, Route: r, Ready: r != "",
		Voice: pick("", st.Voice, Voices[view]), Voices: Voices[view],
		Model: pick("", st.Model, Models[view]), Models: Models[view],
		Talk: pick("", st.Talk, []string{TalkText, TalkVoice}),
		Head: pick("", st.Head, []string{HeadH, HeadFace}), Barge: !st.NoBarge,
		About: st.About, Notes: st.notesOn(),
		Delivery: pick("", st.Delivery, []string{DeliveryNarrate, DeliveryResult}),
		Effort:   pick("", st.Effort, Efforts), Efforts: Efforts,
		Wake: st.Wake.orDefault(), Relay: true,
	}
}

// CallInput is what the page brings to start a call.
type CallInput struct {
	SDP   string `json:"sdp"`
	Mode  string `json:"mode"`
	Voice string `json:"voice"`
	Model string `json:"model"`
	// Context is a few lines about where the voice was opened (the chat's title, the open design), for the voice to
	// know what "this" means.
	Context string `json:"context"`
	// Conv is the chat the voice was opened from: its project's notes about the person go in.
	Conv string `json:"conv"`
	// Screen is what is in front of the person (Primer.Screen).
	Screen string `json:"screen"`
	// Delivery and Effort of this call; empty takes the settings'.
	Delivery string `json:"delivery"`
	Effort   string `json:"effort"`
	// Relay: the sound goes over the machine (relay.go); the page brings no offer, it joins calls/<id>/audio.
	Relay bool `json:"relay"`
}

// Started is the answer to a call: the page applies the SDP to its peer connection.
type Started struct {
	ID    string `json:"id"`
	SDP   string `json:"sdp"`
	Route string `json:"route"`
	Model string `json:"model"`
	// Relay: the machine holds the peer connection; SDP is empty, the page joins calls/<id>/audio.
	Relay bool `json:"relay,omitempty"`
}

var callIDRe = regexp.MustCompile(`^(rtc_[A-Za-z0-9_-]+|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

const maxSDP = 48 << 10

// Start makes the call with the page's offer and joins its control socket.
func (s *Service) Start(ctx context.Context, in CallInput) (*Started, error) {
	st := s.Settings()
	tok, signed := s.token()
	key, hasKey := s.key()
	r := route(st, signed, hasKey)
	switch {
	case r == "" && st.Subscription:
		return nil, &Error{Code: CodeNoLogin, Status: http.StatusConflict, Detail: "no Codex sign-in with ChatGPT and no OpenAI key"}
	case r == "":
		return nil, &Error{Code: CodeOff, Status: http.StatusConflict, Detail: "the subscription route is off and there is no OpenAI key"}
	}
	var rl *relay
	if in.Relay {
		var err error
		var note func(level, text string)
		if s.cfg.Relayed != nil {
			scope := context.WithoutCancel(ctx)
			note = func(level, text string) { s.cfg.Relayed(scope, level, text) }
		}
		if rl, err = newRelay(note); err == nil {
			in.SDP, err = rl.offer(ctx)
		}
		if err != nil {
			if rl != nil {
				rl.close()
			}
			return nil, &Error{Code: CodeUnreached, Status: http.StatusBadGateway, Detail: "relay: " + err.Error()}
		}
		defer func() {
			if rl != nil {
				rl.close() // the call was not made: kept ones are set to nil below
			}
		}()
	}
	if !strings.HasPrefix(in.SDP, "v=0") || len(in.SDP) > maxSDP {
		return nil, &Error{Code: CodeBadRequest, Status: http.StatusBadRequest, Detail: "bad offer"}
	}
	voice := pick(in.Voice, st.Voice, Voices[r])
	model := pick(in.Model, st.Model, Models[r])
	mode := in.Mode
	if mode != ModeChat && mode != ModeDigest {
		mode = ModeAgent
	}
	style := Style{
		Delivery: pick(in.Delivery, st.Delivery, []string{DeliveryNarrate, DeliveryResult}),
		Effort:   pick(in.Effort, st.Effort, Efforts),
	}
	instructions := Instructions(mode, in.Context, style)
	primer := Primer{About: st.About, Screen: in.Screen}
	if st.notesOn() && s.cfg.Notes != nil && in.Conv != "" {
		primer.Notes = s.cfg.Notes(in.Conv)
	}
	if s.cfg.Primed != nil {
		s.cfg.Primed(r, primer.Sizes())
	}
	var (
		proto protocol
		req   *http.Request
		err   error
	)
	if r == RouteSubscription {
		session := newID()
		sess := map[string]any{
			"instructions": instructions,
			"audio":        map[string]any{"output": map[string]any{"voice": voice}},
			// ack_filler: the voice says a short "секунду, передаю" of its own as it hands a task over, as ChatGPT's does.
			"delegation": map[string]any{"type": "client", "ack_filler": true},
			"model":      model,
		}
		// The call starts knowing the person and the screen (the instructions can not change once it is made).
		if items := primer.Items(); items != nil {
			sess["initial_items"] = items
		}
		body, _ := json.Marshal(map[string]any{"sdp": in.SDP, "session": sess})
		u := strings.TrimRight(s.cfg.Endpoints.Call, "/") + "/realtime/calls?intent=quicksilver&architecture=avas"
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			setHeaders(req.Header, tok, session)
		}
		proto = &subscriptionWire{tok: tok, session: session, live: s.cfg.Endpoints.Live}
	} else {
		if !primer.Empty() {
			instructions += "\n\n" + primer.Text()
		}
		ks := keySession(model, voice, mode, instructions)
		if strings.HasPrefix(model, "gpt-realtime-2") {
			ks["reasoning"] = map[string]any{"effort": reasoningOf[style.Effort]}
		}
		req, err = keyCallRequest(ctx, s.cfg.Endpoints.KeyCall, key, in.SDP, ks)
		proto = &keyWire{key: key, live: s.cfg.Endpoints.KeyLive}
	}
	if err != nil {
		return nil, &Error{Code: CodeUnreached, Status: http.StatusBadGateway, Detail: err.Error()}
	}
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		return nil, &Error{Code: CodeUnreached, Status: http.StatusBadGateway, Detail: "call: " + scrub(err.Error())}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxSDP*2))
	if resp.StatusCode/100 != 2 {
		return nil, &Error{Code: CodeRefused, Status: http.StatusBadGateway,
			Detail: fmt.Sprintf("call: %d %s", resp.StatusCode, scrub(short(string(raw))))}
	}
	id := callID(resp.Header.Get("Location"))
	if id == "" {
		return nil, &Error{Code: CodeRefused, Status: http.StatusBadGateway, Detail: "call: no call id in Location"}
	}
	if rl != nil {
		if err := rl.answer(string(raw)); err != nil {
			return nil, &Error{Code: CodeRefused, Status: http.StatusBadGateway, Detail: "relay answer: " + err.Error()}
		}
	}
	c := &Call{id: id, mode: mode, route: r, model: model, proto: proto, handoff: s.cfg.Handoff, started: time.Now(),
		wake: make(chan struct{}), relay: rl}
	c.ctx, c.cancel = context.WithTimeout(context.Background(), maxCall)
	s.mu.Lock()
	for _, old := range s.calls {
		old.end("replaced")
	}
	s.calls = map[string]*Call{id: c}
	s.mu.Unlock()
	if s.cfg.Live != nil && in.Conv != "" {
		conv, live := in.Conv, s.cfg.Live
		live(conv, true)
		c.mu.Lock()
		c.over = func() { live(conv, false) }
		c.mu.Unlock()
	}
	go c.run(s)
	if rl != nil {
		rl = nil // the call keeps it now
		return &Started{ID: id, Route: r, Model: model, Relay: true}, nil
	}
	return &Started{ID: id, SDP: string(raw), Route: r, Model: model}, nil
}

func setHeaders(h http.Header, tok Token, session string) {
	h.Set("Authorization", "Bearer "+tok.Access)
	if tok.Account != "" {
		h.Set("chatgpt-account-id", tok.Account)
	}
	h.Set("openai-alpha", "quicksilver=v2")
	h.Set("originator", "codex_cli_rs")
	h.Set("x-session-id", session)
	h.Set("session-id", session)
	h.Set("thread-id", session)
}

// callID is the last rtc_… or UUID segment of the call's Location.
func callID(loc string) string {
	if u, err := url.Parse(loc); err == nil {
		loc = u.Path
	}
	parts := strings.Split(strings.Trim(loc, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if callIDRe.MatchString(parts[i]) {
			return parts[i]
		}
	}
	return ""
}

// Call returns a live call.
func (s *Service) Call(id string) (*Call, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.calls[id]; c != nil {
		return c, nil
	}
	return nil, &Error{Code: CodeNoCall, Status: http.StatusNotFound, Detail: "no such call"}
}

// Hang ends a call's control socket; the page closes the peer connection itself.
func (s *Service) Hang(id string) {
	s.mu.Lock()
	c := s.calls[id]
	delete(s.calls, id)
	s.mu.Unlock()
	if c != nil {
		c.end("hung up")
	}
}

const (
	maxCall    = 30 * time.Minute
	maxEvents  = 4000
	chunkBytes = 500
)

// Event is what the page sees of the control socket.
type Event struct {
	Seq     int    `json:"seq"`
	Type    string `json:"type"` // ready, user, assistant, turn, task, error, ended
	Role    string `json:"role,omitempty"`
	Text    string `json:"text,omitempty"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message,omitempty"`
}

// Call is one call's control socket and what came over it.
type Call struct {
	id      string
	mode    string
	route   string
	model   string
	proto   protocol
	handoff func(step, mode, task, text string)
	over    func() // the call's chat is told it ended
	started time.Time
	ctx     context.Context
	cancel  context.CancelFunc

	mu     sync.Mutex
	events []Event
	wake   chan struct{}
	conn   *websocket.Conn
	done   bool
	// relay is the machine's peer connection when the call's sound goes over the machine (relay.go).
	relay *relay
	queue [][]byte // frames written before the socket joined
}

// ID of the call.
func (c *Call) ID() string { return c.id }

func (c *Call) push(e Event) {
	if e.Type == "task" && c.handoff != nil {
		c.handoff("task", c.mode, e.ID, e.Text)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done && e.Type != "ended" {
		return
	}
	e.Seq = len(c.events) + 1
	if len(c.events) < maxEvents || e.Type == "ended" {
		c.events = append(c.events, e)
	}
	close(c.wake)
	c.wake = make(chan struct{})
}

// Events since seq (exclusive), a channel closed on the next one, and whether the call is over.
func (c *Call) Events(since int) ([]Event, <-chan struct{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Event
	if since < len(c.events) {
		out = append(out, c.events[since:]...)
	}
	return out, c.wake, c.done
}

func (c *Call) end(why string) {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return
	}
	conn := c.conn
	c.mu.Unlock()
	c.push(Event{Type: "ended", Message: why})
	c.mu.Lock()
	c.done = true
	over := c.over
	c.mu.Unlock()
	c.cancel()
	if c.relay != nil {
		c.relay.close()
	}
	if over != nil {
		over()
	}
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
}

// run joins the control socket and relays it until the call ends; it rejoins a dropped socket.
func (c *Call) run(s *Service) {
	defer func() {
		s.mu.Lock()
		if s.calls[c.id] == c {
			delete(s.calls, c.id)
		}
		s.mu.Unlock()
	}()
	u := c.proto.liveURL(c.id)
	backoff := 200 * time.Millisecond
	for attempt := 0; c.ctx.Err() == nil; attempt++ {
		conn, status, err := c.dial(u)
		if err != nil {
			if status == http.StatusGone || status == http.StatusNotFound && attempt >= 3 {
				c.end("call ended")
				return
			}
			if status == http.StatusUnauthorized || status == http.StatusForbidden || attempt > 8 {
				c.push(Event{Type: "error", Message: fmt.Sprintf("control socket: %d %s", status, scrub(err.Error()))})
				c.end("control socket refused")
				return
			}
			wait := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, 800 * time.Millisecond}
			d := backoff
			if status == http.StatusNotFound && attempt < len(wait) {
				d = wait[attempt]
			} else {
				backoff = min(backoff*2, 5*time.Second)
			}
			select {
			case <-time.After(d):
			case <-c.ctx.Done():
			}
			continue
		}
		joined := time.Now()
		c.mu.Lock()
		c.conn = conn
		queue := c.queue
		c.queue = nil
		c.mu.Unlock()
		for _, f := range queue {
			_ = conn.Write(c.ctx, websocket.MessageText, f)
		}
		err = c.read(conn)
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		if c.ctx.Err() != nil {
			return
		}
		if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			c.end("call ended")
			return
		}
		if time.Since(joined) > 30*time.Second {
			backoff = 200 * time.Millisecond
			attempt = 0
		}
	}
	c.end("call over")
}

func (c *Call) dial(u string) (*websocket.Conn, int, error) {
	h := c.proto.headers()
	ctx, cancel := context.WithTimeout(c.ctx, 15*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: h})
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	if err != nil {
		return nil, status, err
	}
	conn.SetReadLimit(4 << 20)
	return conn, status, nil
}

func (c *Call) read(conn *websocket.Conn) error {
	for {
		typ, raw, err := conn.Read(c.ctx)
		if err != nil {
			return err
		}
		if typ != websocket.MessageText {
			continue
		}
		evs, replies := c.proto.parse(raw)
		for _, e := range evs {
			c.push(e)
		}
		for _, f := range replies {
			if err := conn.Write(c.ctx, websocket.MessageText, f); err != nil {
				return err
			}
		}
	}
}

// parseSubscription turns a control-socket frame into what the page sees; audio and unknown frames are dropped.
func parseSubscription(raw []byte) (Event, bool) {
	var f struct {
		Type    string          `json:"type"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
		Item    struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Target  string `json:"target"`
			Text    string `json:"text"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"item"`
		Turn struct {
			Role       string `json:"role"`
			Transcript string `json:"transcript"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return Event{}, false
	}
	switch f.Type {
	case "session.started":
		return Event{Type: "ready"}, true
	case "input_transcript.added":
		return Event{Type: "user", Text: f.Item.Text}, f.Item.Text != ""
	case "output_transcript.added":
		return Event{Type: "assistant", Text: f.Item.Text}, f.Item.Text != ""
	case "turn.done":
		return Event{Type: "turn", Role: f.Turn.Role, Text: f.Turn.Transcript}, f.Turn.Role != ""
	case "delegation.created":
		if f.Item.Type != "delegation" || f.Item.Target != "client" || f.Item.ID == "" {
			return Event{}, false
		}
		var b strings.Builder
		for _, p := range f.Item.Content {
			if p.Type == "input_text" {
				b.WriteString(p.Text)
			}
		}
		return Event{Type: "task", ID: f.Item.ID, Text: b.String()}, true
	case "error":
		msg := f.Message
		if msg == "" && len(f.Error) > 0 {
			var e struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(f.Error, &e) == nil && e.Message != "" {
				msg = e.Message
			} else {
				msg = string(f.Error)
			}
		}
		return Event{Type: "error", Message: short(msg)}, true
	}
	return Event{}, false
}

// Say gives the voice something to say or know: a task's result (with the task's id) or a note on what is going on.
// speak false puts it on the quiet channel: the voice knows it but does not read it out.
func (c *Call) Say(text, task string, speak bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return &Error{Code: CodeBadRequest, Status: http.StatusBadRequest, Detail: "empty text"}
	}
	if task != "" && c.handoff != nil {
		c.handoff("result", c.mode, task, text)
	}
	return c.send(c.proto.say(text, task, speak))
}

// Type gives the voice words the person typed in place of saying them: it answers them out loud.
func (c *Call) Type(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return &Error{Code: CodeBadRequest, Status: http.StatusBadRequest, Detail: "empty text"}
	}
	return c.send(c.proto.typed(text))
}

// send writes frames on the control socket, or queues them until it joins.
func (c *Call) send(frames [][]byte) error {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return &Error{Code: CodeEnded, Status: http.StatusConflict, Detail: "the call is over"}
	}
	conn := c.conn
	if conn == nil {
		c.queue = append(c.queue, frames...)
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	for _, f := range frames {
		if err := conn.Write(c.ctx, websocket.MessageText, f); err != nil {
			return &Error{Code: CodeUnreached, Status: http.StatusBadGateway, Detail: scrub(err.Error())}
		}
	}
	return nil
}

// chunks splits text into pieces of at most n bytes on rune boundaries.
func chunks(text string, n int) []string {
	var out []string
	for len(text) > n {
		cut := n
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		out = append(out, text[:cut])
		text = text[cut:]
	}
	return append(out, text)
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

var bearerRe = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+|eyJ[A-Za-z0-9._-]{20,}`)

// scrub takes anything that looks like a token out of a message before it is shown or logged.
func scrub(s string) string { return bearerRe.ReplaceAllString(s, "[token]") }

// ErrorOf is err as an *Error.
func ErrorOf(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
