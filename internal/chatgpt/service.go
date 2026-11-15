package chatgpt

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Config is what a runtime gives the integration.
type Config struct {
	// Dir is the integration's folder in the runtime's data folder: the chats and their attachments.
	Dir   string
	Vault Vault
	// Endpoints default to OpenAI's own.
	Endpoints Endpoints
	// Transport carries the calls to OpenAI; nil is the default one.
	Transport http.RoundTripper
	Now       func() time.Time
	// Listen opens the loopback callback's socket; nil listens on 127.0.0.1.
	Listen func(port int) (net.Listener, error)
}

// Service is the ChatGPT integration of one runtime.
type Service struct {
	ep     Endpoints
	http   *http.Client // short calls: tokens, keys, models
	stream *http.Client // the Responses stream, bounded by its turn's context
	accs   *accounts
	oauth  *oauth
	chats  *chats
	models modelCache
	now    func() time.Time

	runsMu sync.Mutex
	runs   map[string]*run
}

// turnMax is how long one answer may take.
const turnMax = 15 * time.Minute

// New opens the integration.
func New(cfg Config) *Service {
	if cfg.Endpoints.Auth == "" || cfg.Endpoints.API == "" {
		cfg.Endpoints = DefaultEndpoints()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Listen == nil {
		cfg.Listen = defaultListen
	}
	tr := cfg.Transport
	if tr == nil {
		tr = http.DefaultTransport
	}
	// No redirect is followed: a token goes only to the address it was meant for.
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &Service{
		ep:     cfg.Endpoints,
		http:   &http.Client{Transport: tr, Timeout: 30 * time.Second, CheckRedirect: noRedirect},
		stream: &http.Client{Transport: tr, CheckRedirect: noRedirect},
		accs:   newAccounts(cfg.Vault),
		chats:  openChats(cfg.Dir),
		models: modelCache{byAc: map[string]modelsAt{}},
		now:    cfg.Now,
		runs:   map[string]*run{},
	}
	s.oauth = &oauth{ep: cfg.Endpoints, http: s.http, accs: s.accs, now: cfg.Now, listen: cfg.Listen, logins: map[string]*login{}}
	return s
}

// State is what the settings card shows.
type State struct {
	Accounts []AccountView `json:"accounts"`
	Pending  bool          `json:"pending"`
}

func (s *Service) State() (State, error) {
	v, err := s.accs.views()
	if err != nil {
		return State{}, err
	}
	return State{Accounts: v, Pending: s.oauth.pending()}, nil
}

// StartLogin opens a sign-in; account "" registers a new account.
func (s *Service) StartLogin(account string) (LoginStart, error) { return s.oauth.start(account) }

// CompleteLogin ends a sign-in from the callback's address pasted from the browser.
func (s *Service) CompleteLogin(ctx context.Context, callback string) (AccountView, error) {
	return s.oauth.completePasted(ctx, callback)
}

// CancelLogin drops the waiting sign-ins.
func (s *Service) CancelLogin() { s.oauth.cancel() }

// SignOut ends an account's session; revoked says OpenAI confirmed it.
func (s *Service) SignOut(ctx context.Context, key string) (bool, error) {
	s.forgetModels(key)
	return s.oauth.signOut(ctx, key)
}

// Forget signs an account out and removes its registration.
func (s *Service) Forget(ctx context.Context, key string) (bool, error) {
	revoked, err := s.SignOut(ctx, key)
	if err != nil {
		return false, err
	}
	return revoked, s.accs.remove(key)
}

// Activate makes chats use an account.
func (s *Service) Activate(key string) error {
	if err := s.accs.setActive(key); err != nil {
		return fail(CodeNoAccount, http.StatusNotFound, "")
	}
	return nil
}

func (s *Service) forgetModels(key string) {
	s.models.mu.Lock()
	delete(s.models.byAc, key)
	s.models.mu.Unlock()
}

// Models are the active account's models.
func (s *Service) Models(ctx context.Context) ([]Model, error) {
	key, err := s.accs.active()
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, fail(CodeSignedOut, http.StatusUnauthorized, "")
	}
	return s.listModels(ctx, key)
}

// Chats are the conversations, newest first.
func (s *Service) Chats() []ChatSummary {
	out := s.chats.list()
	for i := range out {
		out[i].Running = s.runOf(out[i].ID) != nil
	}
	return out
}

// Chat is one conversation whole; running says an answer is being written.
func (s *Service) Chat(id string) (Chat, bool, error) {
	ch, err := s.chats.get(id)
	if err != nil {
		return Chat{}, false, fail(CodeNotFound, http.StatusNotFound, "")
	}
	return ch, s.runOf(id) != nil, nil
}

// chatsMax is how many conversations a machine keeps.
const chatsMax = 2000

// CreateChat starts an empty conversation, or gives back the one that stands empty: a new chat is one draft, however
// many times «New chat» is pressed.
func (s *Service) CreateChat(model, effort string) (Chat, error) {
	if s.chats.count() >= chatsMax {
		return Chat{}, fail(CodeTooMany, http.StatusConflict, "")
	}
	now := nowMs(s.now())
	ch := &Chat{ID: newKey(), Model: strings.TrimSpace(model), Effort: strings.TrimSpace(effort), CreatedAt: now, UpdatedAt: now, Messages: []Message{}}
	return s.chats.createOrReuse(ch, func(id string) bool { return s.runOf(id) == nil })
}

// Rename names a conversation.
func (s *Service) Rename(id, title string) error {
	t := cleanTitle(title)
	if t == "" {
		return fail(CodeBadRequest, http.StatusBadRequest, "")
	}
	err := s.chats.change(id, func(ch *Chat) error { ch.Title = t; return nil })
	if errors.Is(err, errNoChat) {
		return fail(CodeNotFound, http.StatusNotFound, "")
	}
	return err
}

// Delete removes a conversation and its attachments, stopping its answer first.
func (s *Service) Delete(id string) error {
	if r := s.runOf(id); r != nil {
		r.cancel()
		<-r.over
	}
	if err := s.chats.remove(id); err != nil {
		if errors.Is(err, errNoChat) {
			return fail(CodeNotFound, http.StatusNotFound, "")
		}
		return err
	}
	return nil
}

// FileOf is an attachment's bytes and type.
func (s *Service) FileOf(chat, file string) ([]byte, string, string, error) {
	ch, err := s.chats.get(chat)
	if err != nil {
		return nil, "", "", fail(CodeNotFound, http.StatusNotFound, "")
	}
	for _, m := range ch.Messages {
		for _, f := range m.Files {
			if f.ID == file {
				b, err := os.ReadFile(s.chats.filePath(chat, f.ID))
				if err != nil {
					return nil, "", "", fail(CodeNotFound, http.StatusNotFound, "")
				}
				return b, f.Mime, f.Name, nil
			}
		}
	}
	return nil, "", "", fail(CodeNotFound, http.StatusNotFound, "")
}

// Upload is an attachment of a message being sent.
type Upload struct {
	Name string
	Mime string
	Data []byte
}

// SendInput is the person's message.
type SendInput struct {
	Text   string
	Files  []Upload
	Model  string
	Effort string
}

// Send keeps the person's message and starts the answer; the page follows it with Follow.
func (s *Service) Send(id string, in SendInput) (Message, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" && len(in.Files) == 0 || utf8.RuneCountInString(text) > textMax || len(in.Files) > filesMax {
		return Message{}, fail(CodeBadRequest, http.StatusBadRequest, "")
	}
	total := 0
	for _, f := range in.Files {
		total += len(f.Data)
		if total > filesTotalMax {
			return Message{}, fail(CodeTooMany, http.StatusRequestEntityTooLarge, "")
		}
		if len(f.Data) == 0 || len(f.Data) > fileMax || !allowedMime(f.Mime) {
			return Message{}, fail(CodeUnsupported, http.StatusBadRequest, f.Mime)
		}
	}
	key, err := s.accs.active()
	if err != nil {
		return Message{}, err
	}
	if key == "" {
		return Message{}, fail(CodeSignedOut, http.StatusUnauthorized, "")
	}
	ch, err := s.chats.get(id)
	if err != nil {
		return Message{}, fail(CodeNotFound, http.StatusNotFound, "")
	}
	if len(ch.Messages) >= messageMax {
		return Message{}, fail(CodeTooMany, http.StatusConflict, "")
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		model = ch.Model
	}
	if model == "" {
		// The page sent before the plan's models came: the plan's first one answers.
		if list, err := s.listModels(context.Background(), key); err == nil && len(list) > 0 {
			model = list[0].Slug
		}
	}
	if model == "" {
		return Message{}, fail(CodeBadRequest, http.StatusBadRequest, "model")
	}
	effort := strings.TrimSpace(in.Effort)
	if effort == "" {
		effort = ch.Effort
	}

	r, err := s.claim(id)
	if err != nil {
		return Message{}, err
	}
	now := nowMs(s.now())
	msg := Message{ID: newKey(), Role: "user", Text: text, At: now}
	for _, f := range in.Files {
		fid := newKey()
		if err := s.chats.keepFile(id, fid, f.Data); err != nil {
			for _, kept := range msg.Files {
				s.chats.dropFile(id, kept.ID)
			}
			s.release(id, r)
			return Message{}, err
		}
		msg.Files = append(msg.Files, File{ID: fid, Name: safeName(f.Name), Mime: f.Mime, Size: int64(len(f.Data))})
	}
	err = s.chats.change(id, func(c *Chat) error {
		c.Messages = append(c.Messages, msg)
		c.Model, c.Effort, c.UpdatedAt = model, effort, now
		if c.Title == "" {
			c.Title = titleOf(text)
			if c.Title == "" && len(msg.Files) > 0 {
				c.Title = msg.Files[0].Name
			}
		}
		return nil
	})
	if err != nil {
		for _, f := range msg.Files {
			s.chats.dropFile(id, f.ID)
		}
		s.release(id, r)
		return Message{}, err
	}
	go s.answer(id, key, model, effort, r)
	return msg, nil
}

// Spoken is one line of a voice conversation kept in a chat's history.
type Spoken struct {
	Role string `json:"role"` // user | assistant
	Text string `json:"text"`
}

// AppendSpoken keeps what was said in the voice mode in the chat, as its messages; nothing is sent to
// ChatGPT for it. Empty lines are skipped; a chat with no title takes it from the first thing the person said.
func (s *Service) AppendSpoken(id string, lines []Spoken) ([]Message, error) {
	var add []Message
	now := nowMs(s.now())
	size := 0
	for _, l := range lines {
		text := strings.TrimSpace(l.Text)
		if text == "" || l.Role != "user" && l.Role != "assistant" {
			continue
		}
		size += utf8.RuneCountInString(text)
		m := Message{ID: newKey(), Role: l.Role, Text: text, At: now + int64(len(add))}
		if l.Role == "assistant" {
			m.Status = "done"
		}
		add = append(add, m)
	}
	if len(add) == 0 || size > textMax {
		return nil, fail(CodeBadRequest, http.StatusBadRequest, "")
	}
	err := s.chats.change(id, func(c *Chat) error {
		if len(c.Messages)+len(add) > messageMax {
			return fail(CodeTooMany, http.StatusConflict, "")
		}
		c.Messages = append(c.Messages, add...)
		c.UpdatedAt = now
		if c.Title == "" {
			for _, m := range add {
				if m.Role == "user" {
					c.Title = titleOf(m.Text)
					break
				}
			}
		}
		return nil
	})
	if errors.Is(err, errNoChat) {
		return nil, fail(CodeNotFound, http.StatusNotFound, "")
	}
	if err != nil {
		return nil, err
	}
	return add, nil
}

// Stop ends the answer being written; what it wrote stays.
func (s *Service) Stop(id string) bool {
	r := s.runOf(id)
	if r == nil {
		return false
	}
	r.cancel()
	return true
}

func allowedMime(m string) bool {
	switch m {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "application/pdf", "text/plain", "text/markdown", "text/csv", "application/json":
		return true
	}
	return false
}

func safeName(n string) string {
	n = strings.Map(func(r rune) rune {
		if r < 0x20 || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, n)
	if utf8.RuneCountInString(n) > 120 {
		n = string([]rune(n)[:120])
	}
	if strings.TrimSpace(n) == "" {
		n = "file"
	}
	return n
}

// --- the answer being written ---

// run is one answer: its text so far, and a channel closed at every change for whoever follows it.
type run struct {
	ctx     context.Context
	cancel  context.CancelFunc
	over    chan struct{} // closed once the answer is kept
	mu      sync.Mutex
	text    string
	done    *Message
	changed chan struct{}
	tooLong atomic.Bool
}

// answerMax is the longest answer kept; past it the answer stops as incomplete.
const answerMax = 2 << 20

var errTooLong = fail(CodeIncomplete, http.StatusBadGateway, "the answer is too long")

func (r *run) add(delta string) {
	r.mu.Lock()
	if len(r.text)+len(delta) > answerMax {
		r.mu.Unlock()
		r.tooLong.Store(true)
		r.cancel()
		return
	}
	r.text += delta
	close(r.changed)
	r.changed = make(chan struct{})
	r.mu.Unlock()
}

func (r *run) finish(m Message) {
	r.mu.Lock()
	r.done = &m
	close(r.changed)
	r.changed = make(chan struct{})
	r.mu.Unlock()
}

// look is the run's text, its kept message once done, and the channel that says it changed.
func (r *run) look() (string, *Message, <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.text, r.done, r.changed
}

func (s *Service) runOf(id string) *run {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	r := s.runs[id]
	if r == nil {
		return nil
	}
	if _, done, _ := r.look(); done != nil {
		return nil
	}
	return r
}

func (s *Service) claim(id string) (*run, error) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if r := s.runs[id]; r != nil {
		if _, done, _ := r.look(); done == nil {
			return nil, fail(CodeBusy, http.StatusConflict, "")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), turnMax)
	r := &run{ctx: ctx, cancel: cancel, over: make(chan struct{}), changed: make(chan struct{})}
	s.runs[id] = r
	return r, nil
}

func (s *Service) release(id string, r *run) {
	r.cancel()
	close(r.over)
	s.runsMu.Lock()
	if s.runs[id] == r {
		delete(s.runs, id)
	}
	s.runsMu.Unlock()
}

// Follow is the answer being written in a chat; nil when none is.
func (s *Service) Follow(id string) *Follower {
	s.runsMu.Lock()
	r := s.runs[id]
	s.runsMu.Unlock()
	if r == nil {
		return nil
	}
	return &Follower{r: r}
}

// Follower reads one answer as it is written.
type Follower struct {
	r    *run
	sent int
}

// Next waits for more text or the end; it returns the new text and, at the end, the kept message.
func (f *Follower) Next(ctx context.Context) (string, *Message, error) {
	for {
		text, done, changed := f.r.look()
		if len(text) > f.sent || done != nil {
			delta := text[f.sent:]
			f.sent = len(text)
			return delta, done, nil
		}
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-changed:
		}
	}
}

// answer writes ChatGPT's answer to the chat's last message and keeps it.
func (s *Service) answer(id, key, model, effort string, r *run) {
	defer func() {
		close(r.over)
		r.cancel()
		// The finished run stays a moment for a page that follows it late, then goes.
		time.AfterFunc(30*time.Second, func() {
			s.runsMu.Lock()
			if s.runs[id] == r {
				delete(s.runs, id)
			}
			s.runsMu.Unlock()
		})
	}()
	msg := Message{ID: newKey(), Role: "assistant", Model: model, Status: "done"}
	err := s.turn(r.ctx, id, key, model, effort, r)
	if r.tooLong.Load() {
		err = errTooLong
	}
	text, _, _ := r.look()
	msg.Text = text
	switch {
	case err == nil:
	case errors.Is(err, context.Canceled):
		msg.Status = "stopped"
	case errors.Is(err, context.DeadlineExceeded):
		msg.Status = "failed"
		msg.Error = &Failed{Code: CodeIncomplete}
	default:
		msg.Status = "failed"
		var e *Error
		if errors.As(err, &e) {
			msg.Error = &Failed{Code: e.Code, Detail: e.Detail}
		} else {
			msg.Error = &Failed{Code: CodeUpstream}
		}
	}
	msg.At = nowMs(s.now())
	_ = s.chats.change(id, func(c *Chat) error {
		c.Messages = append(c.Messages, msg)
		c.UpdatedAt = msg.At
		return nil
	})
	r.finish(msg)
}

func (s *Service) turn(ctx context.Context, id, key, model, effort string, r *run) error {
	token, err := s.oauth.token(ctx, key)
	if err != nil {
		return err
	}
	ch, err := s.chats.get(id)
	if err != nil {
		return err
	}
	req := turnRequest{Model: model}
	if effort != "" {
		req.Reasoning = &reasoningParam{Effort: effort}
	}
	for _, m := range ch.Messages {
		item, ok := s.itemOf(id, m)
		if ok {
			req.Input = append(req.Input, item)
		}
	}
	return s.streamTurn(ctx, token, req, r.add)
}

// itemOf is a message as the Responses API takes it back as history.
func (s *Service) itemOf(chat string, m Message) (inputItem, bool) {
	if m.Role == "assistant" {
		if strings.TrimSpace(m.Text) == "" {
			return inputItem{}, false
		}
		return inputItem{Type: "message", Role: "assistant", Content: []contentPart{{Type: "output_text", Text: m.Text}}}, true
	}
	var parts []contentPart
	if m.Text != "" {
		parts = append(parts, contentPart{Type: "input_text", Text: m.Text})
	}
	for _, f := range m.Files {
		b, err := os.ReadFile(s.chats.filePath(chat, f.ID))
		if err != nil {
			continue
		}
		data := "data:" + f.Mime + ";base64," + base64.StdEncoding.EncodeToString(b)
		if strings.HasPrefix(f.Mime, "image/") {
			parts = append(parts, contentPart{Type: "input_image", ImageURL: data})
		} else {
			parts = append(parts, contentPart{Type: "input_file", Filename: f.Name, FileData: data})
		}
	}
	if len(parts) == 0 {
		return inputItem{}, false
	}
	return inputItem{Type: "message", Role: "user", Content: parts}, true
}
