// Package router turns what the person asked the voice for into actions on the whole harnessly. The voice
// itself (the realtime model, by subscription or by key) can only hand a task over; the router is one short run of a
// fast text model that reads the task with what is on the screen, the person's chats and the page's catalog (pages,
// settings, themes) and answers a plan: a few actions and the line the voice says. The page carries the plan out
// (it owns navigation, the sidebar, the theme, sending to chats); what only the runtime knows — the person's memory
// and past chats — is read here in a second round when the model asks for it (Recall).
//
// The plan is JSON in the model's answer, not tool calls: the subscription's Responses route and a warm Claude Code
// both answer text, and a plan is quick to check in evals (evals/voice-router).
package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Action kinds the page carries out. Each has its own fields in Action; the prompt (Prompt) lists them for the model.
const (
	DoAgent     = "agent"      // text → the current chat's agent (the work the voice cannot do itself)
	DoSend      = "send"       // chat, text → a message to another chat
	DoNewChat   = "new_chat"   // text, agent?, model?, effort?, orchestrate? → a new chat started with that prompt
	DoModel     = "model"      // chat?, model?, effort? → the chat's model or depth (the current chat unless named)
	DoOpenChat  = "open_chat"  // chat → show that chat
	DoAnswer    = "answer"     // chat, option? (1-based) | text? → the answer to a chat's pending question
	DoPermit    = "permit"     // chat, allow → a chat's pending permission
	DoOpen      = "open"       // page, section? → a page of harnessly (from the catalog)
	DoTheme     = "theme"      // value → a theme (from the catalog)
	DoSidebar   = "sidebar"    // open → the sidebar shown or hidden
	DoPane      = "pane"       // open, tab? → the right pane shown, hidden, or on a tab
	DoSetting   = "setting"    // key, value → a setting (from the catalog)
	DoShow      = "show"       // what (chats|text|chat), text?, chat? → a card on the voice's stage
	DoReadAloud = "read_aloud" // source (chat|answer|text|file), chat?, text?, path? → a long reading
	DoMood      = "mood"       // value → the mascot's mood
	DoRecall    = "recall"     // query → a second round with the memory and past chats that match (runtime-side)
	DoStop      = "stop"       // → the call ends
)

// Moods the mascot shows.
var Moods = []string{"neutral", "joy", "sad", "thinking", "surprised"}

// Action is one step of a plan. Only the fields of its kind are set.
type Action struct {
	Do          string `json:"do"`
	Chat        string `json:"chat,omitempty"`
	Text        string `json:"text,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Model       string `json:"model,omitempty"`
	Effort      string `json:"effort,omitempty"`
	Orchestrate bool   `json:"orchestrate,omitempty"`
	Option      int    `json:"option,omitempty"`
	Allow       *bool  `json:"allow,omitempty"`
	Page        string `json:"page,omitempty"`
	Section     string `json:"section,omitempty"`
	Value       string `json:"value,omitempty"`
	Open        *bool  `json:"open,omitempty"`
	Tab         string `json:"tab,omitempty"`
	Key         string `json:"key,omitempty"`
	What        string `json:"what,omitempty"`
	Source      string `json:"source,omitempty"`
	Path        string `json:"path,omitempty"`
	Query       string `json:"query,omitempty"`
}

// Plan is the router's answer: what to do and the short line the voice says about it.
type Plan struct {
	Say     string   `json:"say"`
	Mood    string   `json:"mood,omitempty"`
	Actions []Action `json:"actions"`
	// Recalled is what the second round was given (the page shows it on the stage when asked); empty without one.
	Recalled []string `json:"recalled,omitempty"`
	Model    string   `json:"model,omitempty"`
	Ms       int64    `json:"ms"`
}

// Chat is one of the person's chats as the page sees it.
type Chat struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Current bool   `json:"current,omitempty"`
	State   string `json:"state,omitempty"` // working | idle | asks | needs_permission | done | failed
	Agent   string `json:"agent,omitempty"`
	Updated string `json:"updated,omitempty"` // when it last moved, as the page says it («вчера 18:40»)
	Last    string `json:"last,omitempty"`    // its recap or last answer, short
	// Ask is a pending question: its words and options in order (the answer's option is 1-based).
	Ask *Ask `json:"ask,omitempty"`
	// Permission is a pending permission: what the agent wants to run.
	Permission string `json:"permission,omitempty"`
}

// Ask is a chat's pending question.
type Ask struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// Choice is one value of the catalog with the words it is called by.
type Choice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Setting is a setting the voice may change, with its values.
type Setting struct {
	Key    string   `json:"key"`
	Label  string   `json:"label"`
	Values []string `json:"values,omitempty"`
	Now    string   `json:"now,omitempty"`
}

// Catalog is what the page can do, with the names the person may use.
type Catalog struct {
	Pages    []Choice  `json:"pages,omitempty"`
	Sections []Choice  `json:"sections,omitempty"` // settings sections: page "settings", section id
	Themes   []Choice  `json:"themes,omitempty"`
	Settings []Setting `json:"settings,omitempty"`
	Agents   []Choice  `json:"agents,omitempty"` // agents and models a new chat may start on (id: "claude:opus")
	Efforts  []string  `json:"efforts,omitempty"`
	PaneTabs []string  `json:"pane_tabs,omitempty"`
}

// Request is one task the voice handed over.
type Request struct {
	// Task is the voice's words for the task; Said is what the person said, when known.
	Task string `json:"task"`
	Said string `json:"said,omitempty"`
	// Screen is what is open now (route, theme, sidebar, pane), one line each.
	Screen  string  `json:"screen,omitempty"`
	Chats   []Chat  `json:"chats,omitempty"`
	Catalog Catalog `json:"catalog"`
	// History is the call's last turns, oldest first («person: …», «voice: …»).
	History []string `json:"history,omitempty"`
}

// Asker is one way to ask a model: the instructions and the prompt in, its words out.
type Asker func(ctx context.Context, instructions, prompt string) (string, error)

// Recaller finds what the person's memory and past chats hold about a query: short lines, best first.
type Recaller func(ctx context.Context, query string) []string

// Router plans the tasks.
type Router struct {
	Ask    Asker
	Recall Recaller
	// Timeout bounds one plan; 25 s when zero.
	Timeout time.Duration
	Now     func() time.Time
}

// ErrNoPlan is a model answer that is not a plan.
var ErrNoPlan = errors.New("no plan in the model's answer")

// Route plans one task: a first round, and a second when the model asked to recall something.
func (r *Router) Route(ctx context.Context, req Request) (Plan, error) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	start := now()
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 25 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	prompt := Prompt(req, nil)
	raw, err := r.Ask(ctx, Instructions, prompt)
	if err != nil {
		return Plan{}, err
	}
	plan, err := Parse(raw)
	if err != nil {
		return Plan{}, err
	}
	if q := recallOf(plan); q != "" && r.Recall != nil {
		found := r.Recall(ctx, q)
		if len(found) == 0 {
			found = []string{"(nothing found for «" + q + "»)"}
		}
		raw, err = r.Ask(ctx, Instructions, Prompt(req, found))
		if err != nil {
			return Plan{}, err
		}
		if plan, err = Parse(raw); err != nil {
			return Plan{}, err
		}
		plan.Recalled = found
		plan.Actions = dropRecall(plan.Actions)
	}
	plan.Actions = Clean(plan.Actions, req)
	plan.Ms = now().Sub(start).Milliseconds()
	return plan, nil
}

func recallOf(p Plan) string {
	for _, a := range p.Actions {
		if a.Do == DoRecall && strings.TrimSpace(a.Query) != "" {
			return strings.TrimSpace(a.Query)
		}
	}
	return ""
}

func dropRecall(in []Action) []Action {
	out := in[:0]
	for _, a := range in {
		if a.Do != DoRecall {
			out = append(out, a)
		}
	}
	return out
}

// Parse reads the plan out of a model's answer: the first JSON object in it, fences and words around allowed.
func Parse(raw string) (Plan, error) {
	s := strings.TrimSpace(raw)
	i := strings.Index(s, "{")
	if i < 0 {
		return Plan{}, ErrNoPlan
	}
	var p Plan
	// The first object only: an answer written twice, or words after it, are not part of the plan.
	if err := json.NewDecoder(strings.NewReader(s[i:])).Decode(&p); err != nil {
		return Plan{}, fmt.Errorf("%w: %v", ErrNoPlan, err)
	}
	if p.Say == "" && len(p.Actions) == 0 {
		return Plan{}, ErrNoPlan
	}
	return p, nil
}

// Clean drops what the page could not carry out: unknown kinds, chats that are not in the list, catalog values that
// are not in the catalog, a mood that is not one. A chat named by its title is turned into its id.
func Clean(in []Action, req Request) []Action {
	out := make([]Action, 0, len(in))
	for _, a := range in {
		a.Do = strings.TrimSpace(a.Do)
		switch a.Do {
		case DoSend, DoOpenChat, DoAnswer, DoPermit:
			id, ok := chatID(a.Chat, req.Chats)
			if !ok {
				continue
			}
			a.Chat = id
			if a.Do == DoSend && strings.TrimSpace(a.Text) == "" {
				continue
			}
			if a.Do == DoAnswer && a.Option == 0 && strings.TrimSpace(a.Text) == "" {
				continue
			}
			if a.Do == DoPermit && a.Allow == nil {
				continue
			}
		case DoModel:
			if a.Chat != "" {
				id, ok := chatID(a.Chat, req.Chats)
				if !ok {
					continue
				}
				a.Chat = id
			} else if !hasCurrent(req.Chats) {
				continue
			}
			if strings.TrimSpace(a.Model) == "" && strings.TrimSpace(a.Effort) == "" {
				continue
			}
		case DoAgent, DoNewChat:
			if strings.TrimSpace(a.Text) == "" {
				continue
			}
		case DoOpen:
			if a.Page == "chat" {
				continue
			}
			if !hasChoice(req.Catalog.Pages, a.Page) {
				continue
			}
			if a.Section != "" && !hasChoice(req.Catalog.Sections, a.Section) {
				a.Section = ""
			}
		case DoTheme:
			v, ok := choiceID(req.Catalog.Themes, a.Value)
			if !ok {
				continue
			}
			a.Value = v
		case DoSidebar, DoPane:
			if a.Open == nil {
				t := true
				a.Open = &t
			}
		case DoSetting:
			if !hasSetting(req.Catalog.Settings, a.Key, a.Value) {
				continue
			}
		case DoMood:
			if !contains(Moods, a.Value) {
				continue
			}
		case DoShow, DoReadAloud, DoStop:
		default:
			continue
		}
		out = append(out, a)
	}
	return out
}

func chatID(ref string, chats []Chat) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	for _, c := range chats {
		if c.ID == ref {
			return c.ID, true
		}
	}
	low := strings.ToLower(ref)
	for _, c := range chats {
		if strings.ToLower(strings.TrimSpace(c.Title)) == low {
			return c.ID, true
		}
	}
	return "", false
}

func hasCurrent(chats []Chat) bool {
	for _, c := range chats {
		if c.Current {
			return true
		}
	}
	return false
}

func hasChoice(list []Choice, id string) bool {
	_, ok := choiceID(list, id)
	return ok
}

func choiceID(list []Choice, v string) (string, bool) {
	v = strings.TrimSpace(v)
	for _, c := range list {
		if c.ID == v {
			return c.ID, true
		}
	}
	low := strings.ToLower(v)
	for _, c := range list {
		if strings.ToLower(c.Label) == low {
			return c.ID, true
		}
	}
	return "", false
}

func hasSetting(list []Setting, key, value string) bool {
	for _, s := range list {
		if s.Key != key {
			continue
		}
		return len(s.Values) == 0 || contains(s.Values, value)
	}
	return false
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
