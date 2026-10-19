package coreapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"harnessly/internal/agents"
	"harnessly/internal/status"
)

// ErrResumeLine: the resume line could not be built (a stored id that is no session id, a launcher not written yet);
// trying again does not help, unlike a tmux call that failed.
var ErrResumeLine = errors.New("resume line not built")

const (
	resumeNoSession = "У разговора нет сессии, которую можно поднять"
	resumeBusyShell = "Во вкладке занят терминал — сначала освободите его"
)

// sessionProcess: {tabId, action: kill} stops a conversation's agent and everything it started, keeping the tab and
// the transcript; {tabId, action: resume} resumes its session in its pane. The dispatcher's authorization is the only
// one (the previous handler also demanded a page session, refusing the CLI token after the dispatcher let it in).
func (c *Config) sessionProcess(r *http.Request, body any) (int, any, http.Header) {
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, nil, http.Header{"Allow": {"POST"}}
	}
	f := fields(body)
	tabID, _ := f["tabId"].(string)
	action := f["action"]
	if tabID == "" || action != "kill" && action != "resume" {
		return answer(r, http.StatusBadRequest, errorBody("Нужны tabId и action"), nil)
	}
	found, ok := c.Tabs.FindTab(tabID)
	if !ok {
		return answer(r, http.StatusNotFound, errorBody("Разговор не найден"), nil)
	}
	if action == "kill" {
		return answer(r, 200, map[string]int{"killed": c.Kill(found.Tab)}, nil)
	}
	result, err := c.Resume(found)
	if err != nil {
		c.log("error", "api handler failed", "path", r.URL.Path, "err", err.Error())
		return internalError()
	}
	switch result {
	case "no-session":
		return answer(r, http.StatusConflict, errorBody(resumeNoSession), nil)
	case "busy-shell":
		return answer(r, http.StatusConflict, errorBody(resumeBusyShell), nil)
	}
	return answer(r, 200, map[string]string{"result": result}, nil)
}

// Kill stops the agent of a tab and everything it started, keeping the pane's shell, then marks the agent gone (its
// launch, if any, ends). It returns how many processes it stopped; 0 without the tmux session or its pane.
func (c *Config) Kill(t status.Tab) int {
	if !c.Panes.HasSession(t.SessionName) {
		return 0
	}
	pane := c.Panes.PanePID(t.SessionName)
	if pane <= 0 {
		return 0
	}
	n := c.KillTree(pane)
	c.Keeper.MarkAgentGone(t.ID, true)
	return n
}

// Resume types the resume line of the tab's session into its pane: "resumed", "running" (the agent already runs),
// "no-session" (nothing to resume) or "busy-shell" (something else holds the terminal).
func (c *Config) Resume(found FoundTab) (string, error) {
	t := found.Tab
	providerID := agents.ProviderOfPanel(t.PanelType.Value())
	agent, ok := c.Agents[providerID]
	if !ok {
		return "no-session", nil
	}
	sessionID := ""
	if agent.ReadSessionID != nil {
		sessionID = agent.ReadSessionID(t.Agent)
	}
	if sessionID == "" && c.RecoverSession != nil {
		sessionID, _ = c.RecoverSession(t.SessionName, providerID)
	}
	if sessionID == "" {
		return "no-session", nil
	}
	timings := c.Timings.withDefaults()
	// A machine that rebooted has no tmux session for the tab until someone opens it.
	if !c.Panes.HasSession(t.SessionName) {
		cwd := found.Cwd
		if cwd == "" && len(found.Directories) > 0 {
			cwd = found.Directories[0]
		}
		if err := c.Panes.CreateSession(t.SessionName, 80, 24, cwd); err != nil {
			return "", err
		}
		time.Sleep(timings.ShellReady)
	}
	if pane := c.Panes.PanePID(t.SessionName); pane > 0 && agent.Running(pane) {
		return "running", nil
	}
	if safe, _ := c.Panes.CheckTerminalProcess(t.SessionName); !safe {
		return "busy-shell", nil
	}
	var launch *agents.Launch
	if c.LaunchOf != nil {
		launch = c.LaunchOf(t.SessionName)
	}
	line, err := agent.ResumeCommand(sessionID, found.WorkspaceID, launch)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrResumeLine, err)
	}
	if err := c.Panes.SendKeys(t.SessionName, line); err != nil {
		return "", err
	}
	c.Keeper.MarkAgentLaunch(t.ID, false)
	go c.backstopSessionStart(t.ID, t.SessionName, agent.Running, timings)
	return "resumed", nil
}

// backstopSessionStart: the composer holds the prompt until the SessionStart hook; a hook lost under load would leave it
// waiting, so once the resumed agent has run for a few seconds without one, the event is synthesized.
func (c *Config) backstopSessionStart(tabID, session string, running func(int) bool, t Timings) {
	seqOf := func() (int, string, bool) {
		for _, s := range c.Keeper.AgentSources() {
			if s.TabID == tabID {
				return s.EventSeq, s.CliState, true
			}
		}
		return 0, "", false
	}
	start, _, _ := seqOf()
	for waited := time.Duration(0); waited < t.BackstopWait; waited += t.BackstopPoll {
		time.Sleep(t.BackstopPoll)
		if pane := c.Panes.PanePID(session); pane > 0 && running(pane) {
			break
		}
	}
	time.Sleep(t.BackstopGrace)
	if seq, state, ok := seqOf(); ok && state == status.Inactive && seq == start {
		c.Keeper.SynthesizeEvent(session, status.EventSessionStart)
	}
}
