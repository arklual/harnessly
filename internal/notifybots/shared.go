package notifybots

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The shared bot: one Telegram bot of the installation (in the cloud, or this runtime's own when self-hosted)
// that every person connects their Telegram to with one button, instead of a bot of their own. The person has one
// chat and one instruction of when agents should write; the page and the agents never see the bot's token, and with
// the cloud's bot the machine never holds it at all.

// Reasons of the shared bot.
const (
	ReasonNotLinked = "not-linked" // the person has not connected Telegram yet
	ReasonNoShared  = "no-shared"  // no shared bot here: the cloud has none or cannot be reached
	ReasonOffline   = "offline"    // the cloud did not answer
	ReasonDisabled  = "disabled"   // own bots are switched off while the shared bot is on
	ReasonExpired   = "expired"    // the one-time code is not (or no longer) the one waiting
)

// SharedCacheFile keeps the shared bot's state as last seen, for the agents' prompt and for a page while the cloud is away.
const SharedCacheFile = "notify-shared.json"

// SharedChat is the person's chat: its title only, made one plain line (it comes from Telegram).
type SharedChat struct {
	Title string `json:"title"`
}

// SharedBinding is a one-time code waiting for the person's Start: the link opens the bot with it.
type SharedBinding struct {
	Link      string `json:"link"`
	ExpiresAt int64  `json:"expiresAt"`
}

// SharedState is the person's Telegram as the page and the agents see it.
type SharedState struct {
	// Enabled: there is a shared bot here; everything else is empty when there is not.
	Enabled     bool           `json:"enabled"`
	Bot         string         `json:"bot"`
	Chat        *SharedChat    `json:"chat"`
	Instruction string         `json:"instruction"`
	Binding     *SharedBinding `json:"binding"`
}

// Part is one message of a send: Telegram HTML, and the same text plain for when Telegram cannot parse the HTML.
type Part struct {
	HTML string `json:"html"`
	Text string `json:"text"`
}

// SharedSent is what a send through the shared bot did.
type SharedSent struct {
	Chat     string `json:"chat"`
	Messages int    `json:"messages"`
}

// Backend is where the shared bot lives: the cloud (Cloud) or this runtime (Local).
type Backend interface {
	State(ctx context.Context) (SharedState, error)
	Link(ctx context.Context) (SharedState, error)
	Unlink(ctx context.Context) (SharedState, error)
	SetInstruction(ctx context.Context, text string) (SharedState, error)
	Send(ctx context.Context, parts []Part, silent bool) (SharedSent, error)
}

// Own bots by token, as RUNTIME_NOTIFY_OWN_BOTS says: on always, off never, auto (the default) only while there is no
// shared bot.
const (
	OwnBotsAuto = "auto"
	OwnBotsOn   = "on"
	OwnBotsOff  = "off"
)

// OwnBotsAllowed says whether the person may link and use bots of their own.
func OwnBotsAllowed(mode string, sharedEnabled bool) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case OwnBotsOn:
		return true
	case OwnBotsOff:
		return false
	}
	return !sharedEnabled
}

// Shared is the shared bot of this runtime: its backend and the cache of what it last said.
type Shared struct {
	// Backend is nil when there is no shared bot to reach at all.
	Backend Backend
	// CachePath is SharedCacheFile in the runtime's v2 data folder.
	CachePath string

	mu        sync.Mutex
	refreshed time.Time
}

// RefreshSoon asks the backend in the background, at most once per every, so that the cache follows a change made
// on another machine or in the bot (/stop).
func (s *Shared) RefreshSoon(every time.Duration) {
	if s.Backend == nil {
		return
	}
	s.mu.Lock()
	if !s.refreshed.IsZero() && time.Since(s.refreshed) < every {
		s.mu.Unlock()
		return
	}
	s.refreshed = time.Now()
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, _ = s.State(ctx)
	}()
}

// SharedCachePath is the cache's place in a runtime's data folder.
func SharedCachePath(dataDir string) string { return filepath.Join(dataDir, "v2", SharedCacheFile) }

// errNoCloud: the cloud backend has no machine agent to go through.
var errNoCloud = errors.New("notifybots: no machine agent")

// clean is a state as it may be shown and put into a prompt: the chat's title one quoted-safe line.
func clean(st SharedState) SharedState {
	if !st.Enabled {
		return SharedState{}
	}
	st.Bot = strings.TrimPrefix(Label(st.Bot), "@")
	if st.Chat != nil {
		st.Chat = &SharedChat{Title: Label(st.Chat.Title)}
	}
	if st.Binding != nil && !strings.HasPrefix(st.Binding.Link, "https://t.me/") {
		st.Binding = nil
	}
	return st
}

func (s *Shared) remember(st SharedState) {
	if s.CachePath == "" {
		return
	}
	st.Binding = nil
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	have, err := os.ReadFile(s.CachePath)
	if err == nil && string(have) == string(raw) {
		return
	}
	// Nothing enabled and nothing cached: the missing file already reads as that.
	if err != nil && !st.Enabled {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.CachePath), 0o700); err != nil {
		return
	}
	tmp := s.CachePath + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, s.CachePath)
	}
}

// ReadSharedCache is the state last seen, or nothing enabled.
func ReadSharedCache(path string) SharedState {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SharedState{}
	}
	var st SharedState
	if json.Unmarshal(raw, &st) != nil {
		return SharedState{}
	}
	return clean(st)
}

// Cached is the state last seen.
func (s *Shared) Cached() SharedState { return ReadSharedCache(s.CachePath) }

func (s *Shared) after(st SharedState, err error) (SharedState, error) {
	if errors.Is(err, errNoCloud) {
		s.remember(SharedState{})
		return SharedState{}, nil
	}
	if err != nil {
		return SharedState{}, err
	}
	st = clean(st)
	s.remember(st)
	return st, nil
}

// State asks the backend; when it cannot be asked the cached state comes back with the error.
func (s *Shared) State(ctx context.Context) (SharedState, error) {
	if s.Backend == nil {
		return SharedState{}, nil
	}
	st, err := s.after(s.Backend.State(ctx))
	if err != nil {
		return s.Cached(), err
	}
	return st, nil
}

func (s *Shared) backend() (Backend, error) {
	if s.Backend == nil {
		return nil, refuse(ReasonNoShared)
	}
	return s.Backend, nil
}

// Link makes a new one-time code: the state carries the link that opens the bot with it.
func (s *Shared) Link(ctx context.Context) (SharedState, error) {
	b, err := s.backend()
	if err != nil {
		return SharedState{}, err
	}
	st, err := s.after(b.Link(ctx))
	if err == nil && !st.Enabled {
		return st, refuse(ReasonNoShared)
	}
	return st, err
}

// Unlink forgets the person's chat; the instruction stays for the next one.
func (s *Shared) Unlink(ctx context.Context) (SharedState, error) {
	b, err := s.backend()
	if err != nil {
		return SharedState{}, err
	}
	return s.after(b.Unlink(ctx))
}

// SetInstruction keeps the person's instruction of when agents write.
func (s *Shared) SetInstruction(ctx context.Context, text string) (SharedState, error) {
	b, err := s.backend()
	if err != nil {
		return SharedState{}, err
	}
	if text, err = checkInstruction(text); err != nil {
		return SharedState{}, err
	}
	return s.after(b.SetInstruction(ctx, text))
}

// Parts is the agent's markdown-ish text as the messages to send.
func Parts(text string) ([]Part, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, refuse(ReasonEmpty)
	}
	if utf8.RuneCountInString(text) > MaxText {
		return nil, refuse(ReasonTooLong)
	}
	split := Split(text, MessageLimit)
	parts := make([]Part, len(split))
	for i, p := range split {
		parts[i] = Part{HTML: ToHTML(p), Text: p}
	}
	return parts, nil
}

// Send writes the text to the person's chat through the shared bot.
func (s *Shared) Send(ctx context.Context, text string, silent bool) (SharedSent, error) {
	b, err := s.backend()
	if err != nil {
		return SharedSent{}, err
	}
	parts, err := Parts(text)
	if err != nil {
		return SharedSent{}, err
	}
	sent, err := b.Send(ctx, parts, silent)
	if errors.Is(err, errNoCloud) {
		return SharedSent{}, refuse(ReasonNoShared)
	}
	if err != nil && (RefusalOf(err).Reason == ReasonNotLinked || RefusalOf(err).Reason == ReasonChatGone) {
		// The chat is gone (or was never there): the prompt stops offering it.
		st := s.Cached()
		st.Chat = nil
		s.remember(st)
	}
	sent.Chat = Label(sent.Chat)
	return sent, err
}

// SharedPromptBlock is the part of an agent's system prompt about the person's Telegram; "" until it is connected.
func SharedPromptBlock(st SharedState) string {
	st = clean(st)
	if !st.Enabled || st.Chat == nil {
		return ""
	}
	instruction := strings.Join(strings.Fields(st.Instruction), " ")
	if instruction == "" {
		instruction = "no instruction: write only when the person asks you to"
	}
	return "## Telegram notifications\n\n" +
		"The person connected their Telegram (chat " + strconv.Quote(st.Chat.Title) + ") to reach them when they are away from " +
		"the screen. Their instruction of when to write: " + instruction + "\n\n" +
		"Send with `harnessly notify send \"<text>\"` (add `--silent` for a message without a sound; `-` reads the text from " +
		"stdin). Write as to a person: short, what happened and what you need from them; **bold**, `code` and links work. " +
		"Write only as the instruction says, never for routine progress, and not twice for the same thing. " +
		"`harnessly notify list` shows the connection as it is now.\n"
}

// Label is a name from outside (a Telegram chat's title) as it may be shown and put into a prompt: one line of
// printable characters, at most 64 of them.
func Label(s string) string { return label(s) }
