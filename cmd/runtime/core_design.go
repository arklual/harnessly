package main

import (
	"net/http"
	"os"

	"harnessly/internal/design"
	"harnessly/internal/files"
	"harnessly/internal/gate"
	"harnessly/internal/jscompat"
	"harnessly/internal/launch"
	"harnessly/internal/prompts"
	"harnessly/internal/push"
)

// designTerminal is a message to an agent through this core's layouts, tmux and prompt editor.
type designTerminal struct{ c *core }

func (t designTerminal) AgentTab(tabID string) (string, string, bool) {
	found, ok := t.c.FindTab(tabID)
	if !ok {
		return "", "", false
	}
	return found.Tab.SessionName, found.Tab.PanelType.Value(), true
}

func (t designTerminal) Front(session string) (bool, string) {
	return t.c.cfg.Tmux.CheckTerminalProcess(session)
}

func (t designTerminal) NeedsEditor(text string) bool { return prompts.NeedsEditor(text) }

func (t designTerminal) Deliver(session, text string, viaEditor bool) error {
	if viaEditor {
		if svc := t.c.promptService(); svc != nil {
			return svc.SubmitText(session, text)
		}
		return errCoreOff
	}
	return t.c.cfg.Tmux.SendLiteralThenEnter(session, text)
}

// designRoutes wire the library to the core: the tabs its claims and listing read, the starter of its dialogs, the
// terminal of its messages, the pusher of its boards; and the routes that need them, answered while the core runs.
func (c *core) designRoutes(f *gate.Gate, d deps, sender *push.Sender) []gate.Route {
	a := designAPI(d)
	running := func() bool { return c.manager() != nil }
	designKeeperRunning = running
	a.L.SetKeeper(func() ([]design.Chat, bool) {
		m := c.manager()
		if m == nil {
			return nil, false
		}
		views := m.ClientViews()
		out := make([]design.Chat, 0, len(views))
		for _, v := range views {
			out = append(out, design.Chat{TabID: v.TabID, PanelType: v.PanelType, State: v.CliState, AgentSummary: v.AgentSummary,
				TabName: v.TabName, WorkspaceID: v.WorkspaceID, Session: v.TmuxSession, Transcript: v.Transcript, LastEventAt: v.LastEventAt})
		}
		return out, true
	})
	a.Terminal = designTerminal{c}
	a.AppBase = env("RUNTIME_APP_BASE_PATH", "/agents")
	a.Launch = func(req design.LaunchRequest) design.LaunchResult {
		if c.launcher == nil || c.manager() == nil {
			return design.LaunchResult{Status: 503, Error: "the tab keeper is not running"}
		}
		prompt := req.Prompt
		res, err := c.launcher.Start(launch.Options{Provider: "claude", Cwd: req.Cwd, CwdRoots: req.CwdRoots, Prompt: &prompt, PromptFiles: req.PromptFiles})
		if err != nil {
			return design.LaunchResult{Status: 500, Error: "Internal Server Error"}
		}
		if !res.OK {
			msg := ""
			if res.Body != nil {
				v, _ := res.Body.Get("error")
				msg = jscompat.String(v)
			}
			return design.LaunchResult{Status: res.Status, Error: msg}
		}
		return design.LaunchResult{OK: true, TabID: res.ConversationID()}
	}
	if sender != nil {
		a.Push = func(req design.PushRequest) []int {
			var statuses []int
			msg := design.PushMessage(req, sender.TagPrefix())
			for _, sub := range design.PhoneTargets(sender.Store.Subscriptions(), req.DeviceID) {
				st, _ := sender.Send(sub, msg)
				if st == 404 || st == 410 {
					_ = sender.Store.Remove(sub.Endpoint)
				}
				statuses = append(statuses, st)
			}
			return statuses
		}
	}
	workspaceRoot := os.Getenv("RUNTIME_WORKSPACE_DIR")
	resolveCwd := func(requested string, roots []string) (string, error) {
		return launch.ResolveCwd(requested, workspaceRoot, roots)
	}
	at := func(path string, compute func(*http.Request) design.Answer) gate.JSONRoute {
		return gate.JSONRoute{Slice: "design", Match: func(r *http.Request) bool { return running() && files.RouteIs(r, path) },
			Compute: func(r *http.Request) (int, any, http.Header) { return compute(r).Split() }}
	}
	library := at(platformAPI+"/design/library", a.Library)
	library.Volatile = []string{"port"}
	return f.AsRoutes(library,
		at(platformAPI+"/design/library/message", a.Message),
		at(platformAPI+"/design/library/continue", func(r *http.Request) design.Answer { return a.Continue(r, resolveCwd) }),
		at(platformAPI+"/design/library/create", func(r *http.Request) design.Answer { return a.Create(r, resolveCwd) }),
		at("/api/cli/design", a.CLIDesign),
		at("/api/cli/design/claims", a.CLIClaims),
	)
}
