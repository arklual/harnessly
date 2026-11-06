package hlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// harnessly app: write, check and install apps (internal/apps), and turn them on in the chat the command runs in.
// new and validate work on the folder alone; install, list, remove, enable and disable go through the runtime.

func (c *cli) app(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "docs":
		return c.appDocs(args)
	case "new":
		return c.appNew(args)
	case "validate", "check":
		return c.appValidate(args)
	case "install":
		if sharedInstall(args) {
			return c.appInstallShared(args)
		}
		return c.appInstall(args, false)
	case "publish":
		return c.appPublish(args)
	case "promote":
		return c.appPromote(args)
	case "rollback":
		return c.appRollback(args)
	case "channels":
		return c.appChannels(args)
	case "update":
		return c.appUpdate(args)
	case "share":
		return c.appShare(args)
	case "shares":
		return c.appShares(args)
	case "unshare":
		return c.appUnshare(args)
	case "team":
		return c.appTeam(args)
	case "public":
		return c.appPublic(args)
	case "eval":
		return c.appEval(args)
	case "mock-mcp":
		return c.appMockMCP(args)
	case "dev":
		return c.appInstall(args, true)
	case "list", "ls":
		return c.appList()
	case "remove", "rm", "uninstall":
		return c.appRemove(args)
	case "connect":
		return c.appConnect(args)
	case "enable", "on":
		return c.appEnable(args, true)
	case "disable", "off":
		return c.appEnable(args, false)
	case "explain", "why":
		return c.appExplain(args)
	}
	return usageErr("app takes new, validate, install, dev, list, remove, enable, disable, explain, publish, promote, rollback, channels, update, share, team, public or eval (harnessly app --help)")
}

// appDir is a folder argument made absolute; none is the working folder.
func (c *cli) appDir(p string) string {
	if p == "" {
		p = "."
	}
	return c.absPath(p)
}

func (c *cli) appNew(args []string) error {
	id, dir, name, template := "", "", "", ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--template" || a == "--name" || a == "--dir":
			if i+1 >= len(args) {
				return usageErr("%s needs a value", a)
			}
			i++
			switch a {
			case "--template":
				template = args[i]
			case "--name":
				name = args[i]
			default:
				dir = args[i]
			}
		case strings.HasPrefix(a, "-"):
			return usageErr("unknown flag %s", a)
		case id == "":
			id = a
		default:
			return usageErr("app new takes one id")
		}
	}
	if id == "" {
		return usageErr("app new needs an id: harnessly app new acme.cashback")
	}
	if dir == "" {
		_, short, _ := strings.Cut(id, ".")
		dir = short
	}
	dir = c.appDir(dir)
	if err := apps.Scaffold(dir, id, name, template); err != nil {
		return fail("%v", err)
	}
	c.out("created %s in %s\nnext: harnessly app validate %s, then harnessly app dev %s\n", id, dir, dir, dir)
	return nil
}

func (c *cli) appValidate(args []string) error {
	dir, strict := "", false
	for _, a := range args {
		switch {
		case a == "--strict":
			strict = true
		case strings.HasPrefix(a, "-"):
			return usageErr("unknown flag %s", a)
		case dir == "":
			dir = a
		default:
			return usageErr("app validate takes one folder")
		}
	}
	dir = c.appDir(dir)
	pkg, problems := apps.LoadPackage(dir)
	if pkg != nil {
		problems = append(problems, apps.DesignProblems(dir)...)
	}
	if strict && pkg != nil {
		problems = append(problems, c.weighPackage(pkg)...)
	}
	if c.json {
		raw, _ := json.Marshal(map[string]any{"ok": pkg != nil && failing(problems, strict) == 0, "problems": orEmpty(problems)})
		c.printRaw(raw)
	} else {
		for _, p := range problems {
			c.out("%s\n", p)
		}
	}
	if n := failing(problems, strict); pkg == nil || n > 0 {
		if !c.json {
			c.out("%d problem(s): the package is not ready\n", max(n, 1))
		}
		return exitCode(exitError)
	}
	if !c.json {
		c.out("ok: %s %s\n", pkg.Manifest.ID, pkg.Manifest.Version)
	}
	return nil
}

// failing counts the problems that fail a check: the errors, and the warnings too when strict.
func failing(ps apps.Problems, strict bool) int {
	if strict {
		return len(ps)
	}
	return len(ps.Errors())
}

func orEmpty(ps apps.Problems) apps.Problems {
	if ps == nil {
		return apps.Problems{}
	}
	return ps
}

type cliApp struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Trust       string   `json:"trust"`
	Targets     []string `json:"targets"`
	Permissions []string `json:"permissions"`
	Dev         bool     `json:"dev"`
	Dir         string   `json:"dir"`
}

func (c *cli) appInstall(args []string, dev bool) error {
	dir := ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-"):
			return usageErr("unknown flag %s", a)
		case dir == "":
			dir = a
		default:
			return usageErr("install takes one folder")
		}
	}
	dir = c.appDir(dir)
	status, raw, err := c.call("POST", "/api/cli/apps/install", nil, map[string]any{"dir": dir, "dev": dev})
	if err != nil {
		return err
	}
	var out struct {
		Error    string        `json:"error"`
		Message  string        `json:"message"`
		Problems apps.Problems `json:"problems"`
		App      cliApp        `json:"app"`
	}
	_ = json.Unmarshal(raw, &out)
	if c.json {
		c.printRaw(raw)
	} else {
		for _, p := range out.Problems {
			c.out("%s\n", p)
		}
	}
	switch {
	case status == 422:
		return fail("not installed: the package has errors (harnessly app validate %s)", dir)
	case status == 409:
		return fail("not installed: %s", out.Message)
	case status >= 300:
		return apiError(status, raw)
	}
	if !c.json {
		how := "installed"
		if out.App.Dev {
			how = "installed for development (the folder itself: a chat started after an edit gets it)"
		}
		c.out("%s %s %s\nturn it on in this chat: harnessly app enable %s\n", how, out.App.ID, out.App.Version, out.App.ID)
	}
	return nil
}

func (c *cli) appList() error {
	raw, err := c.request("GET", "/api/cli/apps", nil, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Apps []cliApp `json:"apps"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, a := range out.Apps {
		dev := ""
		if a.Dev {
			dev = "\tdev " + a.Dir
		}
		c.out("%s\t%s\t%s\t%s%s\n", a.ID, a.Version, a.Name, strings.Join(a.Targets, ","), dev)
	}
	if len(out.Apps) == 0 {
		c.out("no apps installed: harnessly app new acme.demo makes one\n")
	}
	return nil
}

func (c *cli) appRemove(args []string) error {
	if len(args) != 1 {
		return usageErr("app remove takes the app's id")
	}
	raw, err := c.request("POST", "/api/cli/apps/remove", nil, map[string]string{"id": args[0]})
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	c.out("removed %s\n", args[0])
	return nil
}

func (c *cli) appEnable(args []string, on bool) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return usageErr("takes the app's id")
	}
	id := args[0]
	session := c.session()
	if session == "" {
		return fail("not in a chat's tab: run it in the chat the app is for")
	}
	status, raw, err := c.call("POST", "/api/cli/apps/enable", nil, map[string]any{"session": session, "id": id, "on": on})
	if err != nil {
		return err
	}
	if status == 404 {
		var r struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &r)
		if r.Error == "no-chat" {
			return fail("no chat runs in this tab")
		}
		return fail("%s is not installed, or does not work in a chat (harnessly app list)", id)
	}
	if status >= 300 {
		return apiError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	if on {
		var r struct {
			Warnings []string `json:"warnings"`
		}
		_ = json.Unmarshal(raw, &r)
		for _, w := range r.Warnings {
			c.out("warning: %s\n", w)
		}
		c.out("%s is on in this chat; the agent gets it when it starts next\n", id)
	} else {
		c.out("%s is off in this chat\n", id)
	}
	return nil
}

// appExplain says why an app is on or off, layer by layer: the machine, the team, its setup, how it turns on, the chat of
// this tab (when run in one) and the wall. The line marked ✗ is the one that keeps it off.
func (c *cli) appExplain(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return usageErr("takes the app's id")
	}
	q := url.Values{"id": {args[0]}}
	if session := c.session(); session != "" {
		q.Set("session", session)
	}
	status, raw, err := c.call("GET", "/api/cli/apps/explain", q, nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return apiError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var r struct {
		Lines []struct {
			Layer string `json:"layer"`
			Says  string `json:"says"`
			Off   bool   `json:"off"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return fail("the runtime's answer is not readable: %v", err)
	}
	for _, l := range r.Lines {
		mark := "·"
		if l.Off {
			mark = "✗"
		}
		c.out("%s %-10s %s\n", mark, l.Layer, l.Says)
	}
	return nil
}

// appGuard is the PreToolUse hook of a chat with apps (internal/apps policy.go): it decides the call by the snapshot of
// the apps' rules the chat's launch wrote, alone, so a guard never waits on the runtime. A call it cannot check is
// refused: the apps of the chat asked for their rules to hold.
func (c *cli) appGuard(policyPath string, codexInput bool) error {
	in := c.env.Stdin
	if in == nil {
		in = os.Stdin
	}
	if codexInput {
		return c.appGuardCodex(in, policyPath)
	}
	answer := func(decision, reason string) error {
		out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": decision, "permissionDecisionReason": reason,
		}})
		_, _ = c.env.Stdout.Write(append(out, '\n'))
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(in, 4<<20))
	var call struct {
		Session string         `json:"session_id"`
		Tool    string         `json:"tool_name"`
		Input   map[string]any `json:"tool_input"`
		Cwd     string         `json:"cwd"`
	}
	if err != nil || json.Unmarshal(raw, &call) != nil || call.Tool == "" {
		return answer("deny", "The app guard could not read the tool call, so the apps of this chat cannot let it through.")
	}
	var snap apps.Snapshot
	rawSnap, err := os.ReadFile(policyPath)
	if err != nil || !apps.SnapshotIntact(policyPath, rawSnap) || json.Unmarshal(rawSnap, &snap) != nil {
		return answer("deny", "The app guard could not read the rules of this chat's apps, so it cannot let this call through. "+
			"Ask the person to turn the apps off and on again.")
	}
	v, reason := snap.Decide(apps.Call{Tool: call.Tool, Input: call.Input, Cwd: call.Cwd,
		Root: os.Getenv("CLAUDE_PROJECT_DIR")})
	switch v.Decision {
	case apps.DecideDeny:
		reason = guardMark(snap, v) + reason
		c.journalDecision(call.Session, call.Tool, v, "deny", reason)
		return answer("deny", reason)
	case apps.DecideConfirm:
		c.journalDecision(call.Session, call.Tool, v, "ask", reason)
		return answer("ask", reason)
	}
	// No rule of the apps, or one that allows: the chat's own permissions decide.
	return nil
}

// guardMark is the head of a refusal's reason that the feed knows it by (web: app-views/guard-denial.ts): the app's
// name and the rule, each as a JSON string.
func guardMark(snap apps.Snapshot, v apps.Verdict) string {
	name := v.App
	for _, a := range snap.Apps {
		if a.ID == v.App {
			name = a.Name
		}
	}
	n, _ := json.Marshal(name)
	r, _ := json.Marshal(v.Rule)
	return "[app-guard " + string(n) + " " + string(r) + "] "
}

// journalDecision tells the runtime about a refusal or a question to the person, for the journal the app's page shows
// (internal/apps decisions.go). It is a courtesy: the guard has answered by the snapshot alone, so a runtime that is
// slow or away costs the journal a line and the call nothing.
func (c *cli) journalDecision(agentSession, tool string, v apps.Verdict, decision, reason string) {
	session := c.session()
	if session == "" {
		return
	}
	cc := *c
	if cc.env.Client == nil {
		cc.env.Client = &http.Client{Timeout: 2 * time.Second}
	}
	_, _, _ = cc.call("POST", "/api/cli/apps/decision", nil, map[string]any{
		"session": session, "agentSession": agentSession, "app": v.App, "tool": tool, "rule": v.Rule,
		"decision": decision, "reason": reason,
	})
}

// appGuardCodex is the guard for Codex's PreToolUse (apps.CodexCalls reads the call, Snapshot.CodexAnswer decides it):
// the same snapshot and the same rules, with Codex's own tool names and its answer, which has no "ask" and says nothing
// for a call that goes on. Anything it cannot read or check is a refusal.
func (c *cli) appGuardCodex(in io.Reader, policyPath string) error {
	raw, err := io.ReadAll(io.LimitReader(in, 4<<20))
	var snap apps.Snapshot
	rawSnap, snapErr := os.ReadFile(policyPath)
	var out []byte
	switch {
	case err != nil:
		out = apps.CodexDeny("The app guard could not read the tool call, so the apps of this chat cannot let it through.")
	case snapErr != nil || !apps.SnapshotIntact(policyPath, rawSnap) || json.Unmarshal(rawSnap, &snap) != nil:
		out = apps.CodexDeny("The app guard could not read the rules of this chat's apps, so it cannot let this call through. " +
			"Ask the person to turn the apps off and on again.")
	default:
		out = snap.CodexAnswer(raw)
	}
	if len(out) > 0 {
		_, _ = c.env.Stdout.Write(out)
	}
	return nil
}

// appContext is the SessionStart and UserPromptSubmit hook that hands the agent the context of the chat's apps
// (internal/apps context.go): each on-activation item of the snapshot is asked of the runtime, and its text goes out as
// additionalContext only when it differs from what this chat got last (a digest per chat, app and item kept beside the
// snapshot). A SessionStart that begins a fresh context (startup, clear, compact) sends it whatever the digest says. The
// hook never blocks: an item it cannot get is skipped, and the turn goes on without it.
func (c *cli) appContext(policyPath string) error {
	in := c.env.Stdin
	if in == nil {
		in = os.Stdin
	}
	raw, err := io.ReadAll(io.LimitReader(in, 4<<20))
	if err != nil {
		return nil
	}
	var ev struct {
		Event  string `json:"hook_event_name"`
		Source string `json:"source"`
	}
	if json.Unmarshal(raw, &ev) != nil || (ev.Event != "SessionStart" && ev.Event != "UserPromptSubmit") {
		return nil
	}
	var snap apps.Snapshot
	rawSnap, err := os.ReadFile(policyPath)
	if err != nil || !apps.SnapshotIntact(policyPath, rawSnap) || json.Unmarshal(rawSnap, &snap) != nil {
		return nil
	}
	session := c.session()
	if session == "" {
		return nil
	}
	fresh := ev.Event == "SessionStart" && ev.Source != "resume"
	var blocks []string
	for _, a := range snap.Apps {
		for _, item := range a.Context {
			status, body, err := c.call("POST", "/api/cli/apps/context", nil, map[string]any{"session": session, "app": a.ID, "id": item.ID})
			if err != nil || status != 200 {
				continue
			}
			var r struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(body, &r) != nil || strings.TrimSpace(r.Text) == "" {
				continue
			}
			digest := apps.ContextDigest(r.Text)
			state := policyPath + ".ctx-" + apps.ContextDigest(session + "\x00" + a.ID + "\x00" + item.ID)[:16]
			if prev, err := os.ReadFile(state); err == nil && string(prev) == digest && !fresh {
				continue
			}
			_ = os.WriteFile(state, []byte(digest), 0o600)
			blocks = append(blocks, fmt.Sprintf("## Context of the app %s (%s)\n\n%s", a.Name, item.ID, r.Text))
		}
	}
	if len(blocks) == 0 {
		return nil
	}
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": ev.Event, "additionalContext": strings.Join(blocks, "\n\n"),
	}})
	_, _ = c.env.Stdout.Write(append(out, '\n'))
	return nil
}

// appValidateHook is the PostToolUse and Stop hook that runs the validators of the chat's apps (internal/apps validate.go):
// a script that exits 2 fails the check, and its remark goes to the agent as {"decision":"block","reason":…}. Failures
// in a row are counted per chat and validator in a file beside the snapshot; past maxRetries the hook lets go (a Stop
// ends, a PostToolUse stops blocking) and says so on stderr, so the agent does not spin. A pass resets the count. A
// script that does not work blocks nothing.
func (c *cli) appValidateHook(policyPath, event string) error {
	if event != apps.ValidatorPostToolUse && event != apps.ValidatorStop {
		return usageErr("hook app-validate --policy <file> --event PostToolUse|Stop")
	}
	in := c.env.Stdin
	if in == nil {
		in = os.Stdin
	}
	raw, err := io.ReadAll(io.LimitReader(in, 4<<20))
	if err != nil {
		return nil
	}
	var ev struct {
		Tool      string         `json:"tool_name"`
		Input     map[string]any `json:"tool_input"`
		SessionID string         `json:"session_id"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return nil
	}
	var snap apps.Snapshot
	rawSnap, err := os.ReadFile(policyPath)
	if err != nil || !apps.SnapshotIntact(policyPath, rawSnap) || json.Unmarshal(rawSnap, &snap) != nil {
		return nil
	}
	session := ev.SessionID
	if session == "" {
		session = c.hookSession()
	}
	var reasons []string
	for _, a := range snap.Apps {
		for _, v := range a.Validators {
			if !v.Applies(event, a.ID, ev.Tool, ev.Input) {
				continue
			}
			limit := v.MaxRetries
			if limit <= 0 {
				limit = apps.ValidatorDefaultRetries
			}
			state := policyPath + ".val-" + apps.ContextDigest(session + "\x00" + a.ID + "\x00" + v.ID)[:16]
			fails := 0
			if b, err := os.ReadFile(state); err == nil {
				fails, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			}
			res := apps.RunValidator(context.Background(), a.Dir, v, raw)
			switch {
			case res.Err != nil:
				fmt.Fprintf(c.errOut(), "validator %s of %s did not work, nothing is blocked: %v\n", v.ID, a.ID, res.Err)
			case !res.Failed:
				if fails != 0 {
					_ = os.Remove(state)
				}
			case fails >= limit:
				// (A Stop with stop_hook_active set is a retry; once the retries are out it is let go too.)
				// Out of retries: let go. A Stop ends the turn, so the next one starts afresh; a PostToolUse stays released until a pass.
				fmt.Fprintf(c.errOut(), "validator %s of %s failed %d times in a row, no more retries: %s\n", v.ID, a.ID, fails, res.Remark)
				if event == apps.ValidatorStop {
					_ = os.Remove(state)
				} else {
					_ = os.WriteFile(state, []byte(strconv.Itoa(limit)), 0o600)
				}
			default:
				_ = os.WriteFile(state, []byte(strconv.Itoa(fails+1)), 0o600)
				reasons = append(reasons, fmt.Sprintf("%s (%s, attempt %d of %d): %s", a.Name, v.ID, fails+1, limit, res.Remark))
			}
		}
	}
	if len(reasons) > 0 {
		_, _ = c.env.Stdout.Write(apps.ValidatorBlockAnswer(strings.Join(reasons, "\n\n")))
	}
	return nil
}

// errOut is where a hook writes what is for the log, not for the agent.
func (c *cli) errOut() io.Writer {
	if c.env.Stderr != nil {
		return c.env.Stderr
	}
	return io.Discard
}

// weighPackage is the strict check of what the app costs the agent's context on every turn: its instructions, the
// descriptions of the tools of the remote servers it turns on (agent.mcp) and those servers' own `instructions`, in
// tokens (bytes / 4). The line is printed; past apps.AppContextWarnBytes it is a warning, which strict fails on. A
// server that does not answer is a warning too: the weight is unknown, not zero.
func (c *cli) weighPackage(pkg *apps.Package) apps.Problems {
	var out apps.Problems
	warn := func(path, format string, a ...any) {
		out = append(out, apps.Problem{Path: path, Message: fmt.Sprintf(format, a...), Warning: true})
	}
	own := 0
	if ag := pkg.Manifest.Agent; ag != nil && ag.Instructions != "" {
		if raw, err := pkg.File(ag.Instructions); err == nil {
			own = len(raw)
		}
	}
	total, tools, servers := own, 0, 0
	if ag := pkg.Manifest.Agent; ag != nil {
		for _, name := range ag.MCP {
			srv, ok := pkg.MCP[name]
			if !ok || srv.URL == "" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			instr, toolBytes, err := apps.MCPWeight(ctx, srv)
			cancel()
			if err != nil {
				warn(".mcp.json:"+name, "could not read the tools and instructions of the server, so their weight is not counted: %v", err)
			}
			servers += instr
			tools += toolBytes
			total += instr + toolBytes
		}
	}
	if !c.json {
		c.out("context weight: ~%d tokens per turn (instructions ~%d, tool descriptions ~%d, server instructions ~%d)\n",
			apps.EstimateTokens(total), apps.EstimateTokens(own), apps.EstimateTokens(tools), apps.EstimateTokens(servers))
	}
	if total > apps.AppContextWarnBytes {
		warn("agent", "the app puts ~%d tokens into every turn of a chat, more than the ~%d it should: cut the instructions and the tool descriptions",
			apps.EstimateTokens(total), apps.EstimateTokens(apps.AppContextWarnBytes))
	}
	return out
}

// appConnect is the person's agreement that an app uses their connection from Settings → Connections:
// harnessly app connect ID CONNECTION [--off].
func (c *cli) appConnect(args []string) error {
	on, words := true, []string{}
	for _, a := range args {
		if a == "--off" {
			on = false
		} else if strings.HasPrefix(a, "-") {
			return usageErr("unknown flag %s", a)
		} else {
			words = append(words, a)
		}
	}
	if len(words) != 2 {
		return usageErr("takes the app's id and the connection's id: harnessly app connect acme.tracker linear [--off]")
	}
	status, raw, err := c.call("POST", "/api/cli/apps/connect", nil, map[string]any{"id": words[0], "connection": words[1], "on": on})
	if err != nil {
		return err
	}
	if status == 404 {
		return fail("%s is not installed, or its package names no connection %s", words[0], words[1])
	}
	if status >= 300 {
		return apiError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	if on {
		c.out("%s may use your %s connection; the agent gets it when it starts next\n", words[0], words[1])
	} else {
		c.out("%s may no longer use your %s connection\n", words[0], words[1])
	}
	return nil
}
