package bgmodel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The background features a provider can be picked for.
const (
	// FeatureNotifySummary: the text of a finished turn's notification.
	FeatureNotifySummary = "notify-summary"
	// FeatureChatNow: «what is going on now» in a chat.
	FeatureChatNow = "chat-now"
	// FeatureNextStep: the hints of the next step.
	FeatureNextStep = "next-step"
	// FeatureMemory: the background memory.
	FeatureMemory = "memory"
	// FeatureSearchAnswer: the answer the search gives to a question in words.
	FeatureSearchAnswer = "search-answer"
	// FeatureUIAgent: the agent of the window, which answers a request in words and takes the person where they asked.
	FeatureUIAgent = "ui-agent"
	// FeatureWidget: the short line a smart widget of the chat's right column asks for.
	FeatureWidget = "widget"
)

// Feature is one background feature as Settings lists it.
type Feature struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Live: the feature already goes through the picked provider. One that is not live keeps the choice for the day
	// it does.
	Live bool `json:"live"`
}

// Features are the background features, in the order Settings lists them.
func Features() []Feature {
	return []Feature{
		{ID: FeatureNotifySummary, Live: true,
			Title:       locale.Pick("Notification summaries", "Сводки для уведомлений"),
			Description: locale.Pick("One short line of what the agent did, in the notification of a finished turn.", "Одна короткая строка о том, что сделал агент, в уведомлении о законченном ходе.")},
		{ID: FeatureChatNow,
			Title:       locale.Pick("What is going on now", "Что сейчас происходит"),
			Description: locale.Pick("The line above a working chat that says what the agent is busy with.", "Строка над работающим чатом: чем сейчас занят агент.")},
		{ID: FeatureNextStep,
			Title:       locale.Pick("Next-step hints", "Подсказки следующего шага"),
			Description: locale.Pick("What to ask the agent next, offered under its answer.", "Что попросить у агента дальше: варианты под его ответом.")},
		{ID: FeatureMemory,
			Title:       locale.Pick("Background memory", "Фоновая память"),
			Description: locale.Pick("What is worth remembering from a chat, picked out after it.", "Что стоит запомнить из чата: выбирается после разговора.")},
		{ID: FeatureSearchAnswer, Live: true,
			Title:       locale.Pick("Answers in the search", "Ответы в поиске"),
			Description: locale.Pick("A question typed into the search is answered from what the search found, with links to where the answer comes from.", "На вопрос, набранный в поиске, отвечает модель по найденному, со ссылками на то, откуда ответ.")},
		{ID: FeatureUIAgent, Live: true,
			Title:       locale.Pick("Window assistant", "Помощник окна"),
			Description: locale.Pick("The field of Settings: opens what you name and offers to change a setting, at once and without a chat. A quick, cheap model is enough.", "Поле в Настройках: открывает названное и предлагает поменять настройку — сразу и без чата. Хватает быстрой дешёвой модели.")},
		{ID: FeatureWidget, Live: true,
			Title:       locale.Pick("Smart widgets", "Умные виджеты"),
			Description: locale.Pick("A widget under the chat's right column that asks a model now and then and shows its short answer. Without a pick it runs on the machine's Claude Haiku.", "Виджет под правой колонкой чата, который время от времени спрашивает модель и показывает её короткий вывод. Без выбора — Claude Haiku машины по подписке.")},
	}
}

// ValidFeature says whether id is a background feature.
func ValidFeature(id string) bool {
	return slices.ContainsFunc(Features(), func(f Feature) bool { return f.ID == id })
}

// Choice is the provider and the model picked for a feature.
type Choice struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Effort is the reasoning effort the model is asked for (extproviders.ModelEfforts); "" is the model's own.
	Effort string `json:"effort,omitempty"`
}

// String is the choice as one value: "<provider>/<model>".
func (c Choice) String() string { return c.Provider + "/" + c.Model }

// ParseChoice reads "<provider>/<model>" (a model's name may have slashes of its own).
func ParseChoice(s string) (Choice, bool) {
	provider, model, ok := strings.Cut(s, "/")
	if !ok || provider == "" || model == "" {
		return Choice{}, false
	}
	return Choice{Provider: provider, Model: model}, true
}

// Reasons a choice is refused.
var (
	ErrUnknownFeature  = errors.New("bgmodel: no such feature")
	ErrUnknownProvider = errors.New("bgmodel: no such provider")
	ErrBadModel        = errors.New("bgmodel: no model named")
	ErrBadEffort       = errors.New("bgmodel: no such effort")
)

// Store keeps the choices in a file: {"features": {"<id>": {"provider", "model"}}}. A feature that is not there is
// «as now». A file that is missing or does not read is no choices.
type Store struct {
	Path string

	mu sync.Mutex
}

type choicesFile struct {
	Features map[string]Choice `json:"features"`
}

func (s *Store) load() map[string]Choice {
	out := map[string]Choice{}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return out
	}
	var f choicesFile
	if json.Unmarshal(raw, &f) != nil {
		return out
	}
	for id, c := range f.Features {
		if ValidFeature(id) && c.Provider != "" && c.Model != "" {
			out[id] = c
		}
	}
	return out
}

// All is every choice made, by feature.
func (s *Store) All() map[string]Choice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Get is the choice of a feature; ok is false for «as now».
func (s *Store) Get(feature string) (Choice, bool) {
	c, ok := s.All()[feature]
	return c, ok
}

// Set keeps the choice of a feature; nil puts it back to «as now». It answers what the choice was before.
func (s *Store) Set(feature string, c *Choice) (before *Choice, err error) {
	if !ValidFeature(feature) {
		return nil, ErrUnknownFeature
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	if was, ok := all[feature]; ok {
		before = &was
	}
	if c == nil {
		delete(all, feature)
	} else {
		all[feature] = *c
	}
	raw, err := json.MarshalIndent(choicesFile{Features: all}, "", "  ")
	if err != nil {
		return before, err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return before, err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		_ = os.Remove(tmp)
		return before, err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		_ = os.Remove(tmp)
		return before, err
	}
	return before, nil
}

// Providers is the part of the providers' registry the features use.
type Providers interface {
	Get(id string) (extproviders.Provider, error)
	Key(p extproviders.Provider) (string, error)
}

// Outcome is how a feature's last call went.
type Outcome struct {
	At int64 `json:"at"`
	OK bool  `json:"ok"`
	// Reason and Message say why it failed (Message in words for the person); empty when it went well.
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// The agents of the machine a feature may also run on: a model of Claude Code or Codex through its CLI and
// the machine's own sign-in (a plan), with no key of a provider.
const (
	AgentClaude = "claude"
	AgentCodex  = "codex"
)

// IsAgent says whether a provider's name is an agent of the machine.
func IsAgent(provider string) bool { return provider == AgentClaude || provider == AgentCodex }

// onAgent says whether a choice runs on an agent of the machine: an agent's name that no connected provider has (a
// connected provider's id is the person's own word, and wins).
func (r *Runner) onAgent(c Choice) bool {
	if !IsAgent(c.Provider) {
		return false
	}
	if r.Providers == nil {
		return true
	}
	_, err := r.Providers.Get(c.Provider)
	return err != nil
}

// AgentCall asks a model of an agent of the machine: the call's system and prompt to the agent's CLI, its text handed
// to onDelta as it comes when onDelta is not nil. It answers the whole text.
type AgentCall func(ctx context.Context, agent, model string, call Call, onDelta func(string)) (string, error)

// Runner runs the features on the providers picked for them.
type Runner struct {
	Store     *Store
	Providers Providers
	Client    *Client
	// Agent asks the machine's agents; nil: a feature may be given a provider's model only.
	Agent AgentCall
	Now   func() time.Time
	// Log is told of a call that failed: the feature falls back to what it did before, and this is its record.
	Log func(feature string, c Choice, err error)

	mu   sync.Mutex
	last map[string]Outcome
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Picked says whether a provider is picked for the feature: false is «as now», and the feature does what it always
// did without calling Run.
func (r *Runner) Picked(feature string) bool {
	if r == nil || r.Store == nil {
		return false
	}
	_, ok := r.Store.Get(feature)
	return ok
}

// Lister is what a registry of providers may add to Providers: its accounts, for a feature that has a default.
type Lister interface {
	List() ([]extproviders.View, error)
}

// resolve is the choice a feature runs on: the one picked, or, for the search's answer and the window's agent only (a
// dead end otherwise; the agent takes a quick model first), the first provider of the registry that holds a key and names a model: an account marked default first, then the
// others in the registry's order; the first model it lists.
func (r *Runner) resolve(feature string) (Choice, bool) {
	if r == nil || r.Store == nil {
		return Choice{}, false
	}
	if c, ok := r.Store.Get(feature); ok {
		return c, true
	}
	// A smart widget runs on the machine's own Claude by its subscription when nothing is picked for it.
	if feature == FeatureWidget {
		if r.Agent == nil {
			return Choice{}, false
		}
		return Choice{Provider: AgentClaude, Model: WidgetDefaultModel}, true
	}
	if feature != FeatureSearchAnswer && feature != FeatureUIAgent {
		return Choice{}, false
	}
	l, ok := r.Providers.(Lister)
	if !ok {
		return Choice{}, false
	}
	views, err := l.List()
	if err != nil {
		return Choice{}, false
	}
	// The window's agent answers in a line: a quick model of an account is worth more than the first one it lists.
	if feature == FeatureUIAgent {
		if c, ok := quickChoice(views); ok {
			return c, true
		}
	}
	var first *Choice
	for _, v := range views {
		if !v.HasKey || len(v.Models) == 0 || v.Models[0].ID == "" {
			continue
		}
		c := Choice{Provider: v.ID, Model: v.Models[0].ID}
		if v.Default {
			return c, true
		}
		if first == nil {
			first = &c
		}
	}
	if first == nil {
		return Choice{}, false
	}
	return *first, true
}

// quickModel names the small fast models of the providers (Claude Haiku, Gemini Flash, GPT mini, GLM Air and the like).
var quickModel = regexp.MustCompile(`(?i)(haiku|flash|mini|lite|nano|air|small|turbo)`)

// quickChoice is the first quick model of an account that holds a key, the default account's first.
func quickChoice(views []extproviders.View) (Choice, bool) {
	var first *Choice
	for _, v := range views {
		if !v.HasKey {
			continue
		}
		k := slices.IndexFunc(v.Models, func(m extproviders.Model) bool { return m.ID != "" && quickModel.MatchString(m.ID) })
		if k < 0 {
			continue
		}
		c := Choice{Provider: v.ID, Model: v.Models[k].ID}
		if v.Default {
			return c, true
		}
		if first == nil {
			first = &c
		}
	}
	if first == nil {
		return Choice{}, false
	}
	return *first, true
}

// Usable says whether Run would call a model for the feature: one is picked, or the feature has a default to fall
// back on.
func (r *Runner) Usable(feature string) bool {
	_, ok := r.resolve(feature)
	return ok
}

// Last is how each feature's last call went, by feature; a feature that was never called is not there.
func (r *Runner) Last() map[string]Outcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]Outcome, len(r.last))
	for k, v := range r.last {
		out[k] = v
	}
	return out
}

func failureMessage(reason string) string {
	switch reason {
	case ReasonUnauthorized:
		return locale.Pick("The provider did not accept the key.", "Провайдер не принял ключ.")
	case ReasonUnreachable:
		return locale.Pick("The provider's address did not answer.", "Адрес провайдера не ответил.")
	case ReasonTimeout:
		return locale.Pick("The provider took too long to answer.", "Провайдер отвечал слишком долго.")
	case ReasonRateLimited:
		return locale.Pick("The provider says there are too many requests.", "Провайдер говорит, что запросов слишком много.")
	case ReasonNoProvider:
		return locale.Pick("The provider picked for this is gone.", "Выбранного провайдера больше нет.")
	case ReasonEmpty:
		return locale.Pick("The model answered with nothing.", "Модель ответила пустотой.")
	}
	return locale.Pick("The provider answered with an error.", "Провайдер ответил ошибкой.")
}

func (r *Runner) record(feature string, c Choice, err error) {
	out := Outcome{At: r.now().UnixMilli(), OK: err == nil}
	if err != nil {
		out.Reason = ReasonOf(err)
		out.Message = failureMessage(out.Reason)
		if r.Log != nil {
			r.Log(feature, c, err)
		}
	}
	r.mu.Lock()
	if r.last == nil {
		r.last = map[string]Outcome{}
	}
	r.last[feature] = out
	r.mu.Unlock()
}

// Call is what a feature asks its model.
type Call struct {
	System    string
	Prompt    string
	MaxTokens int
	Timeout   time.Duration
	// Quick: an answer of a line, wanted at once (Request.Quick).
	Quick bool
}

// Run asks the model picked for the feature. used is false when none is picked («as now»): the caller does what it
// did before, and nothing was called. An error means the same for the caller, and is recorded (Last, Log).
func (r *Runner) Run(ctx context.Context, feature string, call Call) (text string, used bool, err error) {
	choice, ok := r.resolve(feature)
	if !ok {
		return "", false, nil
	}
	if r.onAgent(choice) {
		text, err = r.runAgent(ctx, choice, call, nil)
	} else {
		text, err = r.run(ctx, choice, call)
	}
	r.record(feature, choice, err)
	return text, true, err
}

// runAgent asks the agent of the choice; an agent the runner cannot ask is a provider that is gone.
func (r *Runner) runAgent(ctx context.Context, choice Choice, call Call, onDelta func(string)) (string, error) {
	if r.Agent == nil {
		return "", &Error{Reason: ReasonNoProvider}
	}
	if call.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, agentTimeout(call.Timeout))
		defer cancel()
	}
	text, err := r.Agent(ctx, choice.Provider, choice.Model, call, onDelta)
	switch {
	case err != nil && ctx.Err() != nil:
		return "", &Error{Reason: ReasonTimeout}
	case err != nil:
		return "", err
	case strings.TrimSpace(text) == "":
		return "", &Error{Reason: ReasonEmpty}
	}
	return text, nil
}

// agentTimeout is a call's time on an agent's CLI: it starts a program and signs in before it answers, so a few
// seconds more than a provider's request.
func agentTimeout(d time.Duration) time.Duration { return d + 15*time.Second }

// RunStream is Run with the text handed to onDelta as it comes.
func (r *Runner) RunStream(ctx context.Context, feature string, call Call, onDelta func(string)) (text string, used bool, err error) {
	choice, ok := r.resolve(feature)
	if !ok {
		return "", false, nil
	}
	if r.onAgent(choice) {
		text, err = r.runAgent(ctx, choice, call, onDelta)
	} else {
		text, err = r.requestOf(choice, call, func(req Request) (string, error) { return r.Client.Stream(ctx, req, onDelta) })
	}
	r.record(feature, choice, err)
	return text, true, err
}

func (r *Runner) run(ctx context.Context, choice Choice, call Call) (string, error) {
	return r.requestOf(choice, call, func(req Request) (string, error) { return r.Client.Complete(ctx, req) })
}

// requestOf makes the request of a call on the choice's provider and sends it with send.
func (r *Runner) requestOf(choice Choice, call Call, send func(Request) (string, error)) (string, error) {
	p, err := r.Providers.Get(choice.Provider)
	if err != nil {
		return "", &Error{Reason: ReasonNoProvider}
	}
	key, err := r.Providers.Key(p)
	if err != nil {
		return "", &Error{Reason: ReasonUnauthorized}
	}
	return send(Request{BaseURL: p.BaseURL, API: p.API, Key: key, Model: choice.Model,
		System: call.System, Prompt: call.Prompt, MaxTokens: call.MaxTokens, Timeout: call.Timeout, Quick: call.Quick, Effort: choice.Effort})
}

// Pick keeps the choice of a feature after checking it: the provider is one of the registry and a model is named;
// nil puts the feature back to «as now». It answers what the choice was before.
func (r *Runner) Pick(feature string, c *Choice) (before *Choice, err error) {
	if !ValidFeature(feature) {
		return nil, ErrUnknownFeature
	}
	if c != nil {
		c.Model = strings.TrimSpace(c.Model)
		if c.Model == "" || len(c.Model) > extproviders.MaxModelID || strings.ContainsAny(c.Model, " \t\r\n") {
			return nil, ErrBadModel
		}
		switch {
		case !r.onAgent(*c):
			if _, err := r.Providers.Get(c.Provider); err != nil {
				return nil, ErrUnknownProvider
			}
		case r.Agent == nil:
			return nil, ErrUnknownProvider
		default:
			// An agent's model runs at its own effort.
			c.Effort = ""
		}
		if c.Effort != "" && !slices.Contains(extproviders.ModelEfforts, c.Effort) {
			return nil, ErrBadEffort
		}
	}
	return r.Store.Set(feature, c)
}

// Forget puts every feature that runs on a provider back to «as now»: the provider was removed. It answers the
// features it changed.
func (r *Runner) Forget(provider string) []string {
	var changed []string
	for feature, c := range r.Store.All() {
		if c.Provider == provider {
			if _, err := r.Store.Set(feature, nil); err == nil {
				changed = append(changed, feature)
			}
		}
	}
	slices.Sort(changed)
	return changed
}

// Move puts every feature that runs on one account onto another of the same provider, where that one has the
// feature's model. It answers the features it moved; the rest stay for Forget.
func (r *Runner) Move(from, to string, hasModel func(model string) bool) []string {
	var moved []string
	for feature, c := range r.Store.All() {
		if c.Provider != from || !hasModel(c.Model) {
			continue
		}
		if _, err := r.Store.Set(feature, &Choice{Provider: to, Model: c.Model}); err == nil {
			moved = append(moved, feature)
		}
	}
	slices.Sort(moved)
	return moved
}

// NotifySummary is the summary of a finished turn for its notification: one short line of what the agent did, from
// the text of its answer. used is false for «as now».
func (r *Runner) NotifySummary(ctx context.Context, answer string) (summary string, used bool, err error) {
	system := locale.Pick(
		"You write the text of a push notification about an AI agent's finished turn. Say in one short sentence (at most 140 characters) what was done or what the agent needs from the person. Answer in the language of the text. No markdown, no quotes, no preamble.",
		"Ты пишешь текст пуш-уведомления о законченном ходе ИИ-агента. Одним коротким предложением (не длиннее 140 знаков) скажи, что сделано или что агенту нужно от человека. Отвечай на языке текста. Без разметки, без кавычек, без вступлений.")
	text, used, err := r.Run(ctx, FeatureNotifySummary, Call{System: system, Prompt: answer, MaxTokens: 120, Timeout: 8 * time.Second})
	if !used || err != nil {
		return "", used, err
	}
	// One line, whatever the model made of it.
	return strings.Join(strings.Fields(text), " "), true, nil
}

// TurnSummary is the few lines on a finished turn that stand on the chat as its recap, from a prompt that carries the
// turn and says what to write. used is false for «as now».
func (r *Runner) TurnSummary(ctx context.Context, prompt string) (text string, used bool, err error) {
	return r.Run(ctx, FeatureNotifySummary, Call{Prompt: prompt, MaxTokens: 300, Timeout: 20 * time.Second})
}
