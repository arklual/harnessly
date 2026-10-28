package notifybots

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits of the person's input and of the agents' sends.
const (
	MaxName        = 64
	MaxInstruction = 2000
	// BindFor is how long a binding waits for the person's /start.
	BindFor = 10 * time.Minute
	// SendsPerMinute is how many messages an agent may send through one bot in a minute.
	SendsPerMinute = 20
	pollWait       = 5 * time.Second
)

// pollPause is the wait before the next poll after a failure or an answer that came back at once and empty (a long
// poll should not; this keeps a misbehaving server from being asked in a tight loop).
var pollPause = 2 * time.Second

// Reasons of a refusal that is this runtime's own.
const (
	ReasonBadToken       = "bad-token"       // the text is not shaped like a bot token
	ReasonDuplicate      = "duplicate"       // this bot is linked already
	ReasonNotFound       = "not-found"       // no bot of that id or name
	ReasonAmbiguous      = "ambiguous"       // the name matches more than one bot
	ReasonNotBound       = "not-bound"       // the bot has no chat yet
	ReasonEmpty          = "empty"           // nothing to send
	ReasonTooLong        = "too-long"        // more than MaxText characters
	ReasonBadName        = "bad-name"        // a name that is empty or too long
	ReasonBadInstruction = "bad-instruction" // an instruction that is too long
	ReasonFailed         = "failed"          // the file could not be read or written
)

// Refusal is a reason the caller is told, with Telegram's own words when it has them.
type Refusal struct {
	Reason string
	Detail string
}

func (r *Refusal) Error() string { return "notifybots: " + r.Reason }

func refuse(reason string) error { return &Refusal{Reason: reason} }

// RefusalOf is what to tell the caller of any error of the service.
func RefusalOf(err error) Refusal {
	var r *Refusal
	if errors.As(err, &r) {
		return *r
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return Refusal{Reason: ae.Reason, Detail: ae.Detail}
	}
	if errors.Is(err, ErrNotFound) {
		return Refusal{Reason: ReasonNotFound}
	}
	return Refusal{Reason: ReasonFailed}
}

var tokenRe = regexp.MustCompile(`^\d{5,16}:[A-Za-z0-9_-]{30,64}$`)

// CleanToken is a pasted token without the spaces and quotes around it.
func CleanToken(s string) string { return strings.Trim(strings.TrimSpace(s), "\"'`") }

// Binding is a wait for the person's /start with a one-time code.
type Binding struct {
	Code      string
	ExpiresAt time.Time
	// Failed is why the wait stopped without a chat ("" while it waits).
	Failed string
	cancel context.CancelFunc
}

// Service is the bots of this runtime: the page links and edits them, the agents list and send.
type Service struct {
	Store    *Store
	Telegram *Client
	Now      func() time.Time
	// Confirm is the message the bot writes to a chat once it is bound.
	Confirm string

	mu       sync.Mutex
	bindings map[string]*Binding
	sends    map[string][]time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- the page's views ----------------------------------------------------------------------------------------------

// View is a bot as the page sees it: never its token, only that one is set and its last characters.
type View struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Username    string       `json:"username"`
	Title       string       `json:"title"`
	TokenSet    bool         `json:"tokenSet"`
	TokenTail   string       `json:"tokenTail"`
	Chat        *ChatView    `json:"chat"`
	Instruction string       `json:"instruction"`
	Binding     *BindingView `json:"binding"`
	CreatedAt   int64        `json:"createdAt"`
	UpdatedAt   int64        `json:"updatedAt"`
}

// ChatView is the bound chat without its id.
type ChatView struct {
	Title string `json:"title"`
	Type  string `json:"type"`
}

// BindingView is a binding in progress, or how it ended.
type BindingView struct {
	State string `json:"state"` // waiting | failed
	Code  string `json:"code"`
	// Link opens the bot with the code; Command is what to send in a group.
	Link      string `json:"link"`
	Command   string `json:"command"`
	ExpiresAt int64  `json:"expiresAt"`
	Reason    string `json:"reason"`
}

func tail(token string) string {
	if len(token) < 4 {
		return ""
	}
	return token[len(token)-4:]
}

func (s *Service) view(b Bot) View {
	v := View{ID: b.ID, Name: b.Name, Username: b.Username, Title: b.Title, TokenSet: b.Token != "", TokenTail: tail(b.Token),
		Instruction: b.Instruction, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt}
	if b.Chat != nil {
		v.Chat = &ChatView{Title: b.Chat.Title, Type: b.Chat.Type}
	}
	s.mu.Lock()
	if bd := s.bindings[b.ID]; bd != nil {
		state := "waiting"
		if bd.Failed != "" {
			state = "failed"
		}
		v.Binding = &BindingView{State: state, Code: bd.Code, Link: "https://t.me/" + b.Username + "?start=" + bd.Code,
			Command: "/start@" + b.Username + " " + bd.Code, ExpiresAt: bd.ExpiresAt.UnixMilli(), Reason: bd.Failed}
	}
	s.mu.Unlock()
	return v
}

// Views are every bot for the page.
func (s *Service) Views() ([]View, error) {
	bots, err := s.Store.List()
	if err != nil {
		return nil, err
	}
	out := make([]View, len(bots))
	for i, b := range bots {
		out[i] = s.view(b)
	}
	return out, nil
}

// ViewOf is one bot for the page.
func (s *Service) ViewOf(id string) (View, error) {
	b, err := s.Store.Get(id)
	if err != nil {
		return View{}, err
	}
	return s.view(b), nil
}

// ---- linking and editing -------------------------------------------------------------------------------------------

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxName || strings.ContainsAny(name, "\n\r\t") {
		return "", refuse(ReasonBadName)
	}
	return name, nil
}

func checkInstruction(text string) (string, error) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > MaxInstruction {
		return "", refuse(ReasonBadInstruction)
	}
	return text, nil
}

func (s *Service) checkToken(ctx context.Context, token string) (Me, error) {
	if !tokenRe.MatchString(token) {
		return Me{}, refuse(ReasonBadToken)
	}
	me, err := s.Telegram.GetMe(ctx, token)
	if err != nil {
		return Me{}, err
	}
	if !me.IsBot || me.Username == "" {
		return Me{}, refuse(ReasonBadToken)
	}
	return me, nil
}

// Add links a bot by its token: Telegram says whose it is. The name is the bot's own when none is given.
func (s *Service) Add(ctx context.Context, token, name, instruction string) (View, error) {
	token = CleanToken(token)
	me, err := s.checkToken(ctx, token)
	if err != nil {
		return View{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = me.FirstName
	}
	if name, err = checkName(name); err != nil {
		return View{}, err
	}
	if instruction, err = checkInstruction(instruction); err != nil {
		return View{}, err
	}
	bots, err := s.Store.List()
	if err != nil {
		return View{}, err
	}
	for _, b := range bots {
		if b.TelegramID == me.ID {
			return View{}, refuse(ReasonDuplicate)
		}
	}
	at := s.now().UnixMilli()
	b := Bot{ID: "nb_" + randomHex(6), Name: name, Username: me.Username, Title: me.FirstName, TelegramID: me.ID, Token: token,
		Instruction: instruction, CreatedAt: at, UpdatedAt: at}
	if err := s.Store.Add(b); err != nil {
		return View{}, err
	}
	return s.view(b), nil
}

// Change is an edit of a bot from the page; nil fields stay as they are.
type Change struct {
	Name        *string `json:"name"`
	Instruction *string `json:"instruction"`
	// Token replaces the token; another bot's token forgets the chat (that bot has not been added to it).
	Token *string `json:"token"`
}

// Edit changes a bot's name, instruction or token.
func (s *Service) Edit(ctx context.Context, id string, c Change) (View, error) {
	var me *Me
	if c.Token != nil {
		token := CleanToken(*c.Token)
		m, err := s.checkToken(ctx, token)
		if err != nil {
			return View{}, err
		}
		c.Token, me = &token, &m
		bots, err := s.Store.List()
		if err != nil {
			return View{}, err
		}
		for _, b := range bots {
			if b.ID != id && b.TelegramID == m.ID {
				return View{}, refuse(ReasonDuplicate)
			}
		}
	}
	var name, instruction string
	var err error
	if c.Name != nil {
		if name, err = checkName(*c.Name); err != nil {
			return View{}, err
		}
	}
	if c.Instruction != nil {
		if instruction, err = checkInstruction(*c.Instruction); err != nil {
			return View{}, err
		}
	}
	rebound := false
	b, err := s.Store.Update(id, func(b *Bot) error {
		if c.Name != nil {
			b.Name = name
		}
		if c.Instruction != nil {
			b.Instruction = instruction
		}
		if me != nil {
			if me.ID != b.TelegramID {
				b.Chat = nil
				rebound = true
			}
			b.Token, b.TelegramID, b.Username, b.Title = *c.Token, me.ID, me.Username, me.FirstName
		}
		b.UpdatedAt = s.now().UnixMilli()
		return nil
	})
	if err != nil {
		return View{}, err
	}
	if rebound || me != nil {
		// A wait started with the old token reads the old bot's messages.
		s.StopBinding(id)
	}
	return s.view(b), nil
}

// Remove unlinks a bot: its token is gone from the machine.
func (s *Service) Remove(id string) error {
	s.StopBinding(id)
	s.mu.Lock()
	delete(s.sends, id)
	s.mu.Unlock()
	return s.Store.Remove(id)
}

// ---- binding a chat ------------------------------------------------------------------------------------------------

// StartBinding makes a new one-time code for the bot and waits in the background, up to BindFor, for a message with it.
func (s *Service) StartBinding(id string) (View, error) {
	b, err := s.Store.Get(id)
	if err != nil {
		return View{}, err
	}
	s.StopBinding(id)
	ctx, cancel := context.WithTimeout(context.Background(), BindFor)
	bd := &Binding{Code: randomHex(8), ExpiresAt: s.now().Add(BindFor), cancel: cancel}
	s.mu.Lock()
	if s.bindings == nil {
		s.bindings = map[string]*Binding{}
	}
	s.bindings[id] = bd
	s.mu.Unlock()
	go s.watch(ctx, b, bd)
	return s.view(b), nil
}

// StopBinding ends a bot's wait, if it has one.
func (s *Service) StopBinding(id string) {
	s.mu.Lock()
	bd := s.bindings[id]
	delete(s.bindings, id)
	s.mu.Unlock()
	if bd != nil {
		bd.cancel()
	}
}

func (s *Service) fail(id string, bd *Binding, reason string) {
	s.mu.Lock()
	if s.bindings[id] == bd {
		bd.Failed = reason
	}
	s.mu.Unlock()
}

// HasCode says whether a message carries the code: "/start <code>", "/start@bot <code>" or the code alone.
func HasCode(text, code string) bool {
	for _, f := range strings.Fields(text) {
		if f == code {
			return true
		}
		if rest, ok := strings.CutPrefix(f, "/start"); ok && strings.HasSuffix(rest, code) {
			return true
		}
	}
	return false
}

func chatOf(m *Message) Chat {
	c := Chat{ID: m.Chat.ID, Type: m.Chat.Type, Title: m.Chat.Title}
	if c.Title == "" {
		c.Title = strings.TrimSpace(m.Chat.FirstName + " " + m.Chat.LastName)
	}
	if c.Title == "" && m.Chat.Username != "" {
		c.Title = "@" + m.Chat.Username
	}
	c.Title = label(c.Title)
	return c
}

// label is a name from outside (a Telegram chat's title, set by whoever made the chat) as it may appear in a prompt:
// one line of printable runes, at most 64 of them.
func label(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 64 {
		s = strings.TrimSpace(string(r[:64])) + "…"
	}
	return s
}

func (s *Service) watch(ctx context.Context, b Bot, bd *Binding) {
	defer bd.cancel()
	var offset int64
	pause := func() {
		select {
		case <-ctx.Done():
		case <-time.After(pollPause):
		}
	}
	for ctx.Err() == nil {
		asked := time.Now()
		updates, err := s.Telegram.GetUpdates(ctx, b.Token, offset, pollWait)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			switch r := ReasonOf(err); r {
			case ReasonWebhook, ReasonInvalidToken:
				s.fail(b.ID, bd, r)
				return
			}
			// A network hiccup or Telegram's 429: try again a little later.
			pause()
			continue
		}
		if len(updates) == 0 && time.Since(asked) < pollWait/10 {
			pause()
		}
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			m := u.Message
			if m == nil {
				m = u.ChannelPost
			}
			if m == nil || !HasCode(m.Text, bd.Code) {
				continue
			}
			chat := chatOf(m)
			if _, err := s.Store.Update(b.ID, func(x *Bot) error {
				x.Chat = &chat
				x.UpdatedAt = s.now().UnixMilli()
				return nil
			}); err != nil {
				s.fail(b.ID, bd, ReasonFailed)
				return
			}
			// Bound: the page sees the chat and no wait from now on, in one step.
			s.mu.Lock()
			if s.bindings[b.ID] == bd {
				delete(s.bindings, b.ID)
			}
			s.mu.Unlock()
			// The updates read so far are confirmed, so the next wait does not see them again.
			_, _ = s.Telegram.GetUpdates(context.Background(), b.Token, offset, 0)
			if s.Confirm != "" {
				cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = s.Telegram.SendMessage(cctx, b.Token, chat.ID, s.Confirm, false, false)
				cancel()
			}
			return
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		s.fail(b.ID, bd, "expired")
	}
}

// ---- sending -------------------------------------------------------------------------------------------------------

// Find is a bot by its id, its name (any case) or its @username.
func (s *Service) Find(ref string) (Bot, error) {
	ref = strings.TrimSpace(ref)
	bots, err := s.Store.List()
	if err != nil {
		return Bot{}, err
	}
	for _, b := range bots {
		if b.ID == ref {
			return b, nil
		}
	}
	var found []Bot
	for _, b := range bots {
		if strings.EqualFold(b.Name, ref) || strings.EqualFold("@"+b.Username, ref) || strings.EqualFold(b.Username, ref) {
			found = append(found, b)
		}
	}
	switch len(found) {
	case 0:
		return Bot{}, refuse(ReasonNotFound)
	case 1:
		return found[0], nil
	}
	return Bot{}, refuse(ReasonAmbiguous)
}

// take counts one message against the bot's minute; false when the minute is full.
func (s *Service) take(id string, n int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sends == nil {
		s.sends = map[string][]time.Time{}
	}
	now := s.now()
	recent := s.sends[id][:0]
	for _, at := range s.sends[id] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	if len(recent)+n > SendsPerMinute {
		s.sends[id] = recent
		return false
	}
	for range n {
		recent = append(recent, now)
	}
	s.sends[id] = recent
	return true
}

// Sent is what a send did.
type Sent struct {
	Bot      string `json:"bot"`
	Chat     string `json:"chat"`
	Messages int    `json:"messages"`
}

// Send writes the text to the bot's chat, as several messages when it is long. The text is the agent's markdown-ish
// text; a part Telegram cannot parse as HTML goes as plain text.
func (s *Service) Send(ctx context.Context, ref, text string, silent bool) (Sent, error) {
	b, err := s.Find(ref)
	if err != nil {
		return Sent{}, err
	}
	return s.send(ctx, b, text, silent, true)
}

// Test sends the page's test message.
func (s *Service) Test(ctx context.Context, id, text string) (Sent, error) {
	b, err := s.Store.Get(id)
	if err != nil {
		return Sent{}, err
	}
	return s.send(ctx, b, text, false, false)
}

func (s *Service) send(ctx context.Context, b Bot, text string, silent, limited bool) (Sent, error) {
	if b.Chat == nil {
		return Sent{}, refuse(ReasonNotBound)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Sent{}, refuse(ReasonEmpty)
	}
	if utf8.RuneCountInString(text) > MaxText {
		return Sent{}, refuse(ReasonTooLong)
	}
	parts := Split(text, MessageLimit)
	if limited && !s.take(b.ID, len(parts)) {
		return Sent{}, refuse(ReasonRateLimited)
	}
	chat := b.Chat.ID
	for i, p := range parts {
		err := s.Telegram.SendMessage(ctx, b.Token, chat, ToHTML(p), true, silent)
		var ae *APIError
		if errors.As(err, &ae) && ae.MigrateTo != 0 {
			// The group became a supergroup: it has a new id from now on.
			chat = ae.MigrateTo
			_, _ = s.Store.Update(b.ID, func(x *Bot) error {
				if x.Chat != nil {
					x.Chat.ID, x.Chat.Type = chat, "supergroup"
				}
				return nil
			})
			err = s.Telegram.SendMessage(ctx, b.Token, chat, ToHTML(p), true, silent)
		}
		if errors.As(err, &ae) && ae.Reason == ReasonRejected && strings.Contains(strings.ToLower(ae.Detail), "parse") {
			err = s.Telegram.SendMessage(ctx, b.Token, chat, p, false, silent)
		}
		if err != nil {
			if i == 0 {
				return Sent{}, err
			}
			return Sent{Bot: b.Name, Chat: b.Chat.Title, Messages: i}, err
		}
	}
	return Sent{Bot: b.Name, Chat: b.Chat.Title, Messages: len(parts)}, nil
}

// ---- for the agents ------------------------------------------------------------------------------------------------

// AgentBot is a bot as an agent sees it: what it is called, where it writes and when to use it.
type AgentBot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Username    string `json:"username"`
	Chat        string `json:"chat"`
	Bound       bool   `json:"bound"`
	Instruction string `json:"instruction"`
}

// AgentBots are every bot for `harnessly notify list`.
func (s *Service) AgentBots() ([]AgentBot, error) {
	bots, err := s.Store.List()
	if err != nil {
		return nil, err
	}
	out := make([]AgentBot, len(bots))
	for i, b := range bots {
		out[i] = AgentBot{ID: b.ID, Name: b.Name, Username: b.Username, Bound: b.Chat != nil, Instruction: b.Instruction}
		if b.Chat != nil {
			out[i].Chat = b.Chat.Title
		}
	}
	return out, nil
}

// PromptBlock is the part of an agent's system prompt about the bots: those with a chat, each with its instruction;
// "" when there are none.
func PromptBlock(bots []Bot) string {
	var b strings.Builder
	for _, bot := range bots {
		if bot.Chat == nil {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("## Telegram notifications\n\n")
			b.WriteString("The person linked Telegram bots to reach them when they are away from the screen. Each bot has the person's " +
				"instruction of when to use it:\n\n")
		}
		instruction := strings.Join(strings.Fields(bot.Instruction), " ")
		if instruction == "" {
			instruction = "no instruction: use it only when the person asks you to"
		}
		// The chat's title comes from Telegram, not from the person: quoted, one line, as data.
		b.WriteString("- **" + label(bot.Name) + "** (chat " + strconv.Quote(label(bot.Chat.Title)) + "): " + instruction + "\n")
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("\nSend with `harnessly notify send --bot \"<name>\" \"<text>\"` (add `--silent` for a message without a sound; " +
		"`-` reads the text from stdin). Write as to a person: short, what happened and what you need from them; " +
		"**bold**, `code` and links work. Use a bot only as its instruction says, never for routine progress, and not twice for " +
		"the same thing. `harnessly notify list` shows the bots as they are now.\n" +
		"\nWhen you are stuck on something only a person knows or must decide, ask them through the same chats and wait for " +
		"the answer: `harnessly ask --to <name> \"<question>\"`, or `harnessly ask approve` for an approval with buttons " +
		"(`harnessly help ask` tells how; `harnessly ask people` lists whom you may tag).\n")
	return b.String()
}
