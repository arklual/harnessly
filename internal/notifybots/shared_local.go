package notifybots

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LocalFile keeps the person's chat with the installation's own shared bot (self-hosted, RUNTIME_NOTIFY_BOT_TOKEN).
const LocalFile = "notify-shared-local.json"

// Local is the shared bot of a self-hosted runtime: its token is in the runtime's configuration, the runtime reads
// the bot's messages all the time (a /start with the code links the chat, /stop forgets it) and sends itself.
type Local struct {
	Token    string
	Telegram *Client
	// Path is LocalFile in the runtime's v2 data folder.
	Path string
	Now  func() time.Time
	// The bot's answers: to a /start with the code, to /stop, and to anything else.
	Linked, Stopped, Hello string
	// Log is told of a poll that cannot go on (a webhook on the bot, a token Telegram does not know).
	Log func(reason string)

	mu    sync.Mutex
	me    Me
	sends []time.Time
}

type localBody struct {
	Chat        *Chat  `json:"chat,omitempty"`
	Instruction string `json:"instruction"`
	Code        string `json:"code,omitempty"`
	CodeExpires int64  `json:"codeExpires,omitempty"`
}

func (l *Local) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Local) read() (localBody, error) {
	var b localBody
	raw, err := os.ReadFile(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	return b, json.Unmarshal(raw, &b)
}

func (l *Local) write(b localBody) error {
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	tmp := l.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.Path)
}

// update changes the file under the lock; the change may refuse.
func (l *Local) update(change func(*localBody) error) (localBody, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, err := l.read()
	if err != nil {
		return b, refuse(ReasonFailed)
	}
	if err := change(&b); err != nil {
		return b, err
	}
	if err := l.write(b); err != nil {
		return b, refuse(ReasonFailed)
	}
	return b, nil
}

func (l *Local) username(ctx context.Context) (string, error) {
	l.mu.Lock()
	name := l.me.Username
	l.mu.Unlock()
	if name != "" {
		return name, nil
	}
	me, err := l.Telegram.GetMe(ctx, l.Token)
	if err != nil {
		return "", err
	}
	l.mu.Lock()
	l.me = me
	l.mu.Unlock()
	return me.Username, nil
}

func (l *Local) view(ctx context.Context, b localBody) (SharedState, error) {
	name, err := l.username(ctx)
	if err != nil {
		return SharedState{}, err
	}
	st := SharedState{Enabled: true, Bot: name, Instruction: b.Instruction}
	if b.Chat != nil {
		st.Chat = &SharedChat{Title: b.Chat.Title}
	}
	if b.Code != "" && l.now().UnixMilli() < b.CodeExpires {
		st.Binding = &SharedBinding{Link: "https://t.me/" + name + "?start=" + b.Code, ExpiresAt: b.CodeExpires}
	}
	return st, nil
}

func (l *Local) State(ctx context.Context) (SharedState, error) {
	l.mu.Lock()
	b, err := l.read()
	l.mu.Unlock()
	if err != nil {
		return SharedState{}, refuse(ReasonFailed)
	}
	return l.view(ctx, b)
}

func (l *Local) Link(ctx context.Context) (SharedState, error) {
	if _, err := l.username(ctx); err != nil {
		return SharedState{}, err
	}
	b, err := l.update(func(b *localBody) error {
		b.Code, b.CodeExpires = randomHex(8), l.now().Add(BindFor).UnixMilli()
		return nil
	})
	if err != nil {
		return SharedState{}, err
	}
	return l.view(ctx, b)
}

func (l *Local) Unlink(ctx context.Context) (SharedState, error) {
	b, err := l.update(func(b *localBody) error {
		b.Chat, b.Code, b.CodeExpires = nil, "", 0
		return nil
	})
	if err != nil {
		return SharedState{}, err
	}
	return l.view(ctx, b)
}

func (l *Local) SetInstruction(ctx context.Context, text string) (SharedState, error) {
	b, err := l.update(func(b *localBody) error {
		b.Instruction = text
		return nil
	})
	if err != nil {
		return SharedState{}, err
	}
	return l.view(ctx, b)
}

// take counts n messages against the minute; false when it is full.
func (l *Local) take(n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	recent := l.sends[:0]
	for _, at := range l.sends {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	l.sends = recent
	if len(recent)+n > SendsPerMinute {
		return false
	}
	for range n {
		l.sends = append(l.sends, now)
	}
	return true
}

func (l *Local) Send(ctx context.Context, parts []Part, silent bool) (SharedSent, error) {
	l.mu.Lock()
	b, err := l.read()
	l.mu.Unlock()
	if err != nil {
		return SharedSent{}, refuse(ReasonFailed)
	}
	if b.Chat == nil {
		return SharedSent{}, refuse(ReasonNotLinked)
	}
	if !l.take(len(parts)) {
		return SharedSent{}, refuse(ReasonRateLimited)
	}
	chat := b.Chat
	for i, p := range parts {
		err := l.Telegram.SendMessage(ctx, l.Token, chat.ID, p.HTML, true, silent)
		var ae *APIError
		if errors.As(err, &ae) && ae.Reason == ReasonRejected && strings.Contains(strings.ToLower(ae.Detail), "parse") {
			err = l.Telegram.SendMessage(ctx, l.Token, chat.ID, p.Text, false, silent)
		}
		if ReasonOf(err) == ReasonChatGone {
			// The person blocked the bot: the chat is forgotten, as /stop would.
			_, _ = l.update(func(x *localBody) error {
				if x.Chat != nil && x.Chat.ID == chat.ID {
					x.Chat = nil
				}
				return nil
			})
		}
		if err != nil {
			return SharedSent{Chat: chat.Title, Messages: i}, err
		}
	}
	return SharedSent{Chat: chat.Title, Messages: len(parts)}, nil
}

// command is a private message's command and its argument: "/start abc" → ("start", "abc").
func command(text string) (string, string) {
	f := strings.Fields(text)
	if len(f) == 0 || !strings.HasPrefix(f[0], "/") {
		return "", ""
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(f[0], "/"), "@")
	arg := ""
	if len(f) > 1 {
		arg = f[1]
	}
	return strings.ToLower(name), arg
}

// handle answers one message of the bot: only private chats link.
func (l *Local) handle(ctx context.Context, m *Message) {
	if m == nil || m.Chat.Type != "private" {
		return
	}
	name, arg := command(m.Text)
	reply := ""
	switch name {
	case "start":
		reply = l.Hello
		_, err := l.update(func(b *localBody) error {
			if arg == "" || b.Code == "" || arg != b.Code || l.now().UnixMilli() >= b.CodeExpires {
				return refuse(ReasonExpired)
			}
			chat := chatOf(m)
			b.Chat, b.Code, b.CodeExpires = &chat, "", 0
			return nil
		})
		if err == nil {
			reply = l.Linked
		}
	case "stop":
		reply = l.Stopped
		_, _ = l.update(func(b *localBody) error {
			if b.Chat != nil && b.Chat.ID == m.Chat.ID {
				b.Chat = nil
			}
			return nil
		})
	default:
		reply = l.Hello
	}
	if reply != "" {
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = l.Telegram.SendMessage(sctx, l.Token, m.Chat.ID, reply, false, false)
		cancel()
	}
}

// Run reads the bot's messages until ctx ends.
func (l *Local) Run(ctx context.Context) {
	var offset int64
	pause := func(d time.Duration) {
		select {
		case <-ctx.Done():
		case <-time.After(d):
		}
	}
	for ctx.Err() == nil {
		asked := time.Now()
		updates, err := l.Telegram.GetUpdates(ctx, l.Token, offset, pollWait)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			switch r := ReasonOf(err); r {
			case ReasonWebhook, ReasonInvalidToken:
				if l.Log != nil {
					l.Log(r)
				}
				pause(time.Minute)
			default:
				pause(pollPause)
			}
			continue
		}
		if len(updates) == 0 && time.Since(asked) < pollWait/10 {
			pause(pollPause)
		}
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			l.handle(ctx, u.Message)
		}
	}
}
