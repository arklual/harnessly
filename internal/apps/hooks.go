package apps

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The agent's hooks of an app (hooks.agent, HRN-852): a view of the app hears each tool call of the chat's agent before
// it runs and may stop it or add context; it cannot change the call. The guard (internal/hlcli appGuard) asks the
// runtime, the runtime asks the live views of the chat's apps that hear, and their words are put together here: any
// stop wins, contexts add up. A view that does not answer in HookTimeout lets the call through and the journal says so;
// an app in strict mode (hooks.strict) stops it instead. The person turns an app's hook off with one click in the feed.

// HookTimeout is how long the views have to answer a call.
const HookTimeout = 5 * time.Second

// HookReasonMax bounds what a view says about a stop, and HookContextMax what it adds for the agent.
const (
	HookReasonMax  = 1000
	HookContextMax = 4000
)

// HookWord is what a view answers about the agent's call.
type HookWord struct {
	// Decision is "deny" to stop the call; anything else lets it through.
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Context  string `json:"context,omitempty"`
}

// HookAsk is one app asked about a call: its word, or none (Word nil) when it did not answer in time or had no view.
type HookAsk struct {
	App, Name string
	Strict    bool
	// Live: a view of the app heard the call (it may still have said nothing in time).
	Live bool
	Word *HookWord
}

// HookVerdict is the apps' words put together.
type HookVerdict struct {
	Deny bool `json:"deny,omitempty"`
	// App and Name are the app that stopped the call; Reason is why, in its words.
	App    string `json:"app,omitempty"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Context is what the apps add for the agent, each under its name.
	Context string `json:"context,omitempty"`
	// Silent are the apps whose view heard the call and said nothing in time: the journal gets a line for each.
	Silent []string `json:"-"`
}

// MergeHooks puts the apps' words together: the first stop (in the order asked) wins, contexts add up, and a strict app
// that did not answer stops the call.
func MergeHooks(asks []HookAsk) HookVerdict {
	var v HookVerdict
	var contexts []string
	for _, a := range asks {
		switch {
		case a.Word == nil && a.Strict:
			if !v.Deny {
				v = HookVerdict{Deny: true, App: a.App, Name: a.Name, Silent: v.Silent,
					Reason: "The app " + a.Name + " checks every tool call in strict mode and did not answer in time; ask the person to open its view or turn its hook off."}
			}
		case a.Word == nil:
			if a.Live {
				v.Silent = append(v.Silent, a.App)
			}
		case a.Word.Decision == "deny":
			if !v.Deny {
				reason := clipHookText(a.Word.Reason, HookReasonMax)
				if reason == "" {
					reason = "The app " + a.Name + " stopped this call."
				}
				v = HookVerdict{Deny: true, App: a.App, Name: a.Name, Reason: reason, Silent: v.Silent}
			}
		default:
			if c := strings.TrimSpace(clipHookText(a.Word.Context, HookContextMax)); c != "" {
				contexts = append(contexts, "["+a.Name+"] "+c)
			}
		}
	}
	if !v.Deny {
		v.Context = strings.Join(contexts, "\n")
	}
	return v
}

func clipHookText(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// ParseHookWord reads a view's answer; one it cannot read lets the call through, as a silence would.
func ParseHookWord(raw json.RawMessage) (*HookWord, error) {
	var w HookWord
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// hooksOffPath is the list of apps whose hook the person turned off.
func (r *Registry) hooksOffPath() string { return filepath.Join(r.Root, "hooks-off.json") }

// HooksOff says whether the person turned off the app's hook.
func (r *Registry) HooksOff(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.readHooksOff(), id)
}

func (r *Registry) readHooksOff() []string {
	raw, err := os.ReadFile(r.hooksOffPath())
	if err != nil {
		return nil
	}
	var list []string
	_ = json.Unmarshal(raw, &list)
	return list
}

// SetHooksOff turns the app's hook off (or back on) on this machine: its views stop hearing the agent's calls.
func (r *Registry) SetHooksOff(id string, off bool) error {
	if !idRe.MatchString(id) {
		return errors.New("apps: not an app id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	list := slices.DeleteFunc(r.readHooksOff(), func(s string) bool { return s == id })
	if off {
		list = append(list, id)
	}
	raw, _ := json.Marshal(list)
	if err := os.MkdirAll(r.Root, 0o700); err != nil {
		return err
	}
	tmp := r.hooksOffPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.hooksOffPath())
}
