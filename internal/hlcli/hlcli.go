// Package hlcli is the harnessly command: what an agent running in a harnessly tab calls to show things to the person
// in the right pane, to look at the workspace's tabs, and to hand work to another agent. It is a thin client of the
// runtime's /api/cli routes: the runtime's address and its CLI token come from the files the runtime writes into its
// data folder (or from the environment), so a pane needs no setup.
package hlcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Env is what the command depends on outside its arguments; tests replace the parts.
type Env struct {
	Getenv func(string) string
	// Home is the home folder ($HOME).
	Home string
	// Session names the tmux session of the pane the command runs in; "" when it runs outside one.
	Session func() string
	// Cwd is the working folder, for relative file targets.
	Cwd    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Client *http.Client
}

// OSEnv is the process's own environment.
func OSEnv() Env {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	return Env{Getenv: os.Getenv, Home: home, Session: tmuxSession, Cwd: cwd, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
}

// tmuxSession asks tmux for the session of the pane this process runs in. $TMUX names the server's socket, so the
// answer is the runtime's own session and not one of the person's.
func tmuxSession() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Version is set by the build (-X); "dev" otherwise.
var Version = "dev"

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// userError is a failure whose message is the whole story.
type userError struct{ msg string }

func (e userError) Error() string { return e.msg }

func fail(format string, a ...any) error { return userError{fmt.Sprintf(format, a...)} }

// Run executes one command line (without the program name) and returns the exit code.
func Run(args []string, env Env) int {
	if env.Getenv == nil {
		env.Getenv = os.Getenv
	}
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	if env.Session == nil {
		env.Session = func() string { return "" }
	}
	c := &cli{env: env}
	err := c.run(args)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errHelp):
		return exitOK
	case errors.Is(err, errUsage):
		fmt.Fprintln(env.Stderr, "harnessly: "+strings.TrimPrefix(err.Error(), errUsage.Error()+": "))
		fmt.Fprintln(env.Stderr, "Run `harnessly --help` for the commands.")
		return exitUsage
	}
	fmt.Fprintln(env.Stderr, "harnessly: "+err.Error())
	return exitError
}

var (
	errHelp  = errors.New("help")
	errUsage = errors.New("usage")
)

func usageErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, a...))
}

type cli struct {
	env Env
	// json, workspace are the global flags.
	json      bool
	workspace string
}

func (c *cli) out(format string, a ...any) { fmt.Fprintf(c.env.Stdout, format, a...) }

func (c *cli) run(args []string) error {
	// Global flags may stand anywhere; take them out first.
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			c.json = true
		case a == "-w" || a == "--workspace":
			if i+1 >= len(args) {
				return usageErr("%s needs a workspace id", a)
			}
			c.workspace = args[i+1]
			i++
		case strings.HasPrefix(a, "--workspace="):
			c.workspace = strings.TrimPrefix(a, "--workspace=")
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 {
		fmt.Fprint(c.env.Stdout, helpText)
		return nil
	}
	cmd, rest := rest[0], rest[1:]
	helpAsked := len(rest) > 0 && (rest[0] == "--help" || rest[0] == "-h" || rest[0] == "help")
	switch cmd {
	case "-h", "--help", "help":
		if len(rest) > 0 {
			if t, ok := commandHelp[rest[0]]; ok {
				fmt.Fprint(c.env.Stdout, t)
				return errHelp
			}
		}
		fmt.Fprint(c.env.Stdout, helpText)
		return errHelp
	case "version", "--version":
		c.out("harnessly %s\n", Version)
		return nil
	}
	if helpAsked {
		if t, ok := commandHelp[cmd]; ok {
			fmt.Fprint(c.env.Stdout, t)
			return errHelp
		}
	}
	switch cmd {
	case "workspaces":
		return c.workspaces()
	case "tab", "tabs":
		return c.tab(rest)
	case "open":
		return c.open(rest)
	case "panel":
		return c.panel(rest)
	case "status":
		return c.status()
	case "api-guide":
		return c.apiGuide()
	}
	return usageErr("unknown command %q", cmd)
}

// ---- finding the runtime -------------------------------------------------------------------------------------------

// dataDir is the runtime's data folder: HARNESSLY_DATA_DIR (the runtime puts it in every pane), AGENT_DATA_DIR, else
// ~/.harnessly.
func (c *cli) dataDir() string {
	for _, k := range []string{"HARNESSLY_DATA_DIR", "AGENT_DATA_DIR"} {
		if v := strings.TrimSpace(c.env.Getenv(k)); v != "" {
			return v
		}
	}
	return filepath.Join(c.env.Home, ".harnessly")
}

func readTrim(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// endpoint is the runtime's base URL and the CLI token.
func (c *cli) endpoint() (base, token string, err error) {
	dir := c.dataDir()
	base = strings.TrimRight(strings.TrimSpace(c.env.Getenv("HARNESSLY_URL")), "/")
	if base == "" {
		port := readTrim(filepath.Join(dir, "port"))
		if port == "" {
			return "", "", fail("not inside a harnessly workspace: the runtime's port file %s is missing. "+
				"Run this in a tab of harnessly on this machine, or set HARNESSLY_URL and HARNESSLY_CLI_TOKEN",
				filepath.Join(dir, "port"))
		}
		base = "http://127.0.0.1:" + port
	}
	token = strings.TrimSpace(c.env.Getenv("HARNESSLY_CLI_TOKEN"))
	if token == "" {
		token = readTrim(filepath.Join(dir, "cli-token"))
	}
	if token == "" {
		return "", "", fail("no CLI token: %s is missing and HARNESSLY_CLI_TOKEN is not set", filepath.Join(dir, "cli-token"))
	}
	return base, token, nil
}

func (c *cli) header() string {
	if h := strings.TrimSpace(c.env.Getenv("HARNESSLY_CLI_HEADER")); h != "" {
		return h
	}
	return "x-cli-token"
}

// call sends one request to the runtime and returns the status and body.
func (c *cli) call(method, path string, query url.Values, body any) (int, []byte, error) {
	base, token, err := c.endpoint()
	if err != nil {
		return 0, nil, err
	}
	target := base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, target, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set(c.header(), token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.env.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) || strings.Contains(err.Error(), "refused") {
			return 0, nil, fail("the runtime does not answer at %s (%v). Is harnessly running on this machine?", base, shortErr(err))
		}
		return 0, nil, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return 0, nil, err
	}
	return res.StatusCode, raw, nil
}

func shortErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// apiError is the text of a refusal: the runtime's own "error" when it sent one.
func apiError(status int, raw []byte) error {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	text := e.Error
	if text == "" {
		text = e.Message
	}
	switch {
	case status == http.StatusForbidden && (text == "" || text == "Forbidden"):
		return fail("the runtime rejected the CLI token (403). Run this inside a tab of the harnessly that started you; " +
			"the token changes when the runtime's data folder is recreated")
	case status == http.StatusUnauthorized:
		return fail("the runtime wants a signed-in page session (401): this command must go to the CLI routes with the CLI token")
	case status == http.StatusServiceUnavailable && text == "":
		return fail("the runtime is starting or the feature is off (503), try again in a moment")
	}
	if text == "" {
		text = http.StatusText(status)
	}
	return fail("%s (HTTP %d)", text, status)
}

// getJSON, sendJSON run a request that must succeed and decode its answer into out.
func (c *cli) request(method, path string, query url.Values, body any) ([]byte, error) {
	status, raw, err := c.call(method, path, query, body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, apiError(status, raw)
	}
	return raw, nil
}

// printJSON writes raw JSON as it came, indented, for --json.
func (c *cli) printRaw(raw []byte) {
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") == nil {
		buf.WriteByte('\n')
		_, _ = c.env.Stdout.Write(buf.Bytes())
		return
	}
	_, _ = c.env.Stdout.Write(raw)
	if !bytes.HasSuffix(raw, []byte("\n")) {
		c.out("\n")
	}
}

// ---- who am I ------------------------------------------------------------------------------------------------------

type tabInfo struct {
	TabID           string `json:"tabId"`
	WorkspaceID     string `json:"workspaceId"`
	PaneID          string `json:"paneId"`
	Name            string `json:"name"`
	SessionName     string `json:"sessionName"`
	PanelType       string `json:"panelType"`
	AgentProviderID string `json:"agentProviderId"`
	AgentSessionID  string `json:"agentSessionId"`
	Launched        bool   `json:"launched"`
}

func (c *cli) session() string {
	if s := strings.TrimSpace(c.env.Getenv("HARNESSLY_SESSION")); s != "" {
		return s
	}
	return c.env.Session()
}

var errNoPane = fail("not inside a harnessly pane: this command acts on the tab you run in, and there is no tmux session here " +
	"(TMUX is not set). Run it in a tab of harnessly, or set HARNESSLY_SESSION to the tab's session name")

// listTabs is every tab (of one workspace when given).
func (c *cli) listTabs(workspace string) ([]tabInfo, error) {
	q := url.Values{}
	if workspace != "" {
		q.Set("workspaceId", workspace)
	}
	raw, err := c.request("GET", "/api/cli/tabs", q, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Tabs []tabInfo `json:"tabs"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fail("the runtime answered something else than a tab list")
	}
	return out.Tabs, nil
}

// ownWorkspace is -w, else the workspace of the tab this command runs in, else the only workspace there is.
func (c *cli) ownWorkspace() (string, error) {
	if c.workspace != "" {
		return c.workspace, nil
	}
	if s := c.session(); s != "" {
		tabs, err := c.listTabs("")
		if err != nil {
			return "", err
		}
		for _, t := range tabs {
			if t.SessionName == s {
				return t.WorkspaceID, nil
			}
		}
	}
	raw, err := c.request("GET", "/api/cli/workspaces", nil, nil)
	if err != nil {
		return "", err
	}
	var out struct {
		Workspaces []struct {
			ID string `json:"id"`
		} `json:"workspaces"`
	}
	if json.Unmarshal(raw, &out) == nil && len(out.Workspaces) == 1 {
		return out.Workspaces[0].ID, nil
	}
	return "", fail("which workspace? Pass -w <id>; `harnessly workspaces` lists them")
}

// ---- commands ------------------------------------------------------------------------------------------------------

func (c *cli) workspaces() error {
	raw, err := c.request("GET", "/api/cli/workspaces", nil, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Workspaces []struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Directories []string `json:"directories"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fail("the runtime answered something else than a workspace list")
	}
	if len(out.Workspaces) == 0 {
		c.out("No workspaces.\n")
	}
	for _, w := range out.Workspaces {
		c.out("%s\t%s\t%s\n", w.ID, w.Name, strings.Join(w.Directories, ", "))
	}
	return nil
}

// flags is a tiny parser: --name value, --name=value, -n value, switches, and the rest as positionals.
type flags struct {
	values map[string]string
	pos    []string
}

// parseFlags reads args against the names the command takes: value flags (long names, with one-letter aliases).
func parseFlags(args []string, valued map[string]string) (*flags, error) {
	f := &flags{values: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			f.pos = append(f.pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			f.pos = append(f.pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		long, ok := valued[name]
		if !ok {
			return nil, usageErr("unknown option %s", a)
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, usageErr("%s needs a value", a)
			}
			val = args[i+1]
			i++
		}
		f.values[long] = val
	}
	return f, nil
}

func (c *cli) tab(args []string) error {
	if len(args) == 0 {
		return usageErr("tab needs a subcommand: list, create, send, status, result, close")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "ls":
		return c.tabList()
	case "create", "new":
		return c.tabCreate(rest)
	case "send":
		return c.tabSend(rest)
	case "status":
		return c.tabGet(rest, "status")
	case "result":
		return c.tabGet(rest, "result")
	case "close", "rm", "delete":
		return c.tabClose(rest)
	}
	return usageErr("unknown tab command %q", sub)
}

func (c *cli) tabList() error {
	ws := c.workspace
	if ws == "" {
		var err error
		if ws, err = c.ownWorkspace(); err != nil {
			return err
		}
	}
	tabs, err := c.listTabs(ws)
	if err != nil {
		return err
	}
	if c.json {
		b, _ := json.MarshalIndent(map[string]any{"tabs": tabs}, "", "  ")
		c.out("%s\n", b)
		return nil
	}
	if len(tabs) == 0 {
		c.out("No tabs in this workspace.\n")
	}
	me := c.session()
	for _, t := range tabs {
		mark := ""
		if me != "" && t.SessionName == me {
			mark = "\t(this tab)"
		}
		c.out("%s\t%s\t%s\t%s%s\n", t.TabID, orDash(t.PanelType), orDash(t.AgentProviderID), orDash(t.Name), mark)
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

var panelTypes = []string{"terminal", "claude-code", "codex-cli", "agent-sessions", "web-browser", "diff"}

func (c *cli) tabCreate(args []string) error {
	f, err := parseFlags(args, map[string]string{"n": "name", "name": "name", "t": "type", "type": "type",
		"prompt": "prompt", "model": "model", "effort": "effort"})
	if err != nil {
		return err
	}
	if len(f.pos) > 0 {
		return usageErr("tab create takes options only, got %q", f.pos[0])
	}
	ws, err := c.ownWorkspace()
	if err != nil {
		return err
	}
	body := map[string]any{"workspaceId": ws}
	if v := f.values["name"]; v != "" {
		body["name"] = v
	}
	if v := f.values["type"]; v != "" {
		body["panelType"] = canonicalPanelType(v)
	}
	for _, k := range []string{"prompt", "model", "effort"} {
		if _, ok := f.values[k]; ok {
			body[k] = f.values[k]
		}
	}
	status, raw, err := c.call("POST", "/api/cli/tabs", nil, body)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "Invalid panelType" {
			return fail("unknown tab type %q; use one of: %s", f.values["type"], strings.Join(panelTypes, ", "))
		}
		return apiError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var t tabInfo
	_ = json.Unmarshal(raw, &t)
	c.out("%s\n", t.TabID)
	if t.Launched {
		c.out("Started %s in tab %s. Read it with `harnessly tab status %s` and `harnessly tab result %s`.\n",
			t.AgentProviderID, t.TabID, t.TabID, t.TabID)
	}
	return nil
}

func canonicalPanelType(s string) string {
	switch strings.ToLower(s) {
	case "claude", "claude-code":
		return "claude-code"
	case "codex", "codex-cli":
		return "codex-cli"
	case "browser":
		return "web-browser"
	case "shell":
		return "terminal"
	}
	return s
}

func (c *cli) tabSend(args []string) error {
	// Everything after the tab id is the text, dashes and all.
	if len(args) < 2 {
		return usageErr("tab send needs a tab id and the text: harnessly tab send TAB_ID TEXT... (or - to read stdin)")
	}
	id, text := args[0], strings.Join(args[1:], " ")
	if text == "-" {
		raw, err := io.ReadAll(io.LimitReader(c.env.Stdin, 1<<20))
		if err != nil {
			return err
		}
		text = string(raw)
	}
	ws, err := c.ownWorkspace()
	if err != nil {
		return err
	}
	raw, err := c.request("POST", "/api/cli/tabs/"+url.PathEscape(id)+"/send", url.Values{"workspaceId": {ws}}, map[string]any{"content": text})
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	c.out("sent\n")
	return nil
}

// tabGet is status and result: both read a tab.
func (c *cli) tabGet(args []string, what string) error {
	f, err := parseFlags(args, nil)
	if err != nil {
		return err
	}
	if len(f.pos) != 1 {
		return usageErr("tab %s needs a tab id", what)
	}
	ws, err := c.ownWorkspace()
	if err != nil {
		return err
	}
	raw, err := c.request("GET", "/api/cli/tabs/"+url.PathEscape(f.pos[0])+"/"+what, url.Values{"workspaceId": {ws}}, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	if what == "result" {
		var out struct {
			Content *string `json:"content"`
		}
		_ = json.Unmarshal(raw, &out)
		if out.Content != nil {
			c.out("%s\n", strings.TrimRight(*out.Content, "\n"))
		}
		return nil
	}
	var st struct {
		Alive    bool    `json:"alive"`
		Command  *string `json:"command"`
		CLIState *string `json:"cliState"`
		Provider string  `json:"agentProviderId"`
		Hooks    bool    `json:"hooksMissing"`
	}
	_ = json.Unmarshal(raw, &st)
	c.out("alive: %v\n", st.Alive)
	if st.Command != nil {
		c.out("command: %s\n", *st.Command)
	}
	if st.CLIState != nil {
		c.out("state: %s\n", *st.CLIState)
	}
	if st.Provider != "" {
		c.out("agent: %s\n", st.Provider)
	}
	return nil
}

func (c *cli) tabClose(args []string) error {
	f, err := parseFlags(args, nil)
	if err != nil {
		return err
	}
	if len(f.pos) != 1 {
		return usageErr("tab close needs a tab id")
	}
	ws, err := c.ownWorkspace()
	if err != nil {
		return err
	}
	raw, err := c.request("DELETE", "/api/cli/tabs/"+url.PathEscape(f.pos[0]), url.Values{"workspaceId": {ws}}, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	c.out("closed\n")
	return nil
}

// ---- the right pane ------------------------------------------------------------------------------------------------

// PaneKinds are the tabs of the right pane an agent may open by name.
var PaneKinds = []string{"overview", "tools", "browser", "terminal", "files", "disk", "changes", "artifacts", "gitlab",
	"subagents", "economics", "plan", "peers", "design", "side-chat", "gallery", "task"}

func isPaneKind(s string) bool {
	for _, k := range PaneKinds {
		if k == s {
			return true
		}
	}
	return false
}

var (
	taskKeyRe  = regexp.MustCompile(`^[A-Z][A-Z0-9]*-\d+$`)
	portRe     = regexp.MustCompile(`(?i)^(?:(?:https?://)?(?:localhost|127\.0\.0\.1|0\.0\.0\.0)?:)?\d{1,5}(?:/.*)?$`)
	portalRe   = regexp.MustCompile(`^/[ab]/[\w-]+/?$`)
	gitlabPath = regexp.MustCompile(`/-/(merge_requests/\d+|commit/[0-9a-f]{7,40})`)
)

// resolveOpen turns what the agent typed into the pane's tab kind and target.
func (c *cli) resolveOpen(args []string) (tab, target string, err error) {
	if len(args) == 0 {
		return "", "", usageErr("open needs a target: a link, a file, a port or a tab kind")
	}
	if isPaneKind(args[0]) && args[0] != "task" {
		if len(args) > 2 {
			return "", "", usageErr("open %s takes at most one target", args[0])
		}
		tab = args[0]
		if len(args) == 2 {
			target = args[1]
			if tab == "files" || tab == "changes" || tab == "terminal" {
				target = c.absPath(target)
			}
		}
		return tab, target, nil
	}
	if len(args) != 1 {
		return "", "", usageErr("open takes one target (quote it if it has spaces)")
	}
	t := args[0]
	switch {
	case taskKeyRe.MatchString(t):
		return "task", t, nil
	case portRe.MatchString(t):
		return "browser", t, nil
	case strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://"):
		u, perr := url.Parse(t)
		if perr != nil || u.Host == "" {
			return "", "", usageErr("%q is not a link", t)
		}
		switch {
		case portalRe.MatchString(u.Path):
			return "artifacts", t, nil
		case strings.Contains(u.Path, "/docs/") && !strings.Contains(u.Path, "/-/"):
			return "artifacts", t, nil
		case gitlabPath.MatchString(u.Path):
			return "gitlab", t, nil
		}
		return "browser", t, nil
	}
	return "files", c.absPath(t), nil
}

func (c *cli) absPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(c.env.Home, p[2:])
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(c.env.Cwd, p)
}

func (c *cli) pane(command map[string]any) ([]byte, error) {
	session := c.session()
	if session == "" {
		return nil, errNoPane
	}
	return c.request("POST", "/api/cli/panel", nil, map[string]any{"session": session, "command": command})
}

func (c *cli) printPane(raw []byte) {
	if c.json {
		c.printRaw(raw)
		return
	}
	var out struct {
		Message string   `json:"message"`
		Intents []string `json:"intents"`
		State   *struct {
			Visible *bool    `json:"visible"`
			Open    []string `json:"open"`
			Active  *string  `json:"active"`
		} `json:"state"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Message != "" {
		c.out("%s\n", out.Message)
	}
	if out.State != nil {
		if out.State.Visible != nil {
			c.out("pane visible: %v\n", *out.State.Visible)
		}
		if len(out.State.Open) > 0 {
			c.out("open tabs: %s\n", strings.Join(out.State.Open, ", "))
		}
		if out.State.Active != nil {
			c.out("in front: %s\n", *out.State.Active)
		}
	}
	for _, l := range out.Intents {
		c.out("%s\n", l)
	}
}

func (c *cli) open(args []string) error {
	tab, target, err := c.resolveOpen(args)
	if err != nil {
		return err
	}
	cmd := map[string]any{"kind": "open", "tab": tab}
	if target != "" {
		cmd["target"] = target
	}
	raw, err := c.pane(cmd)
	if err != nil {
		return err
	}
	c.printPane(raw)
	return nil
}

func (c *cli) panel(args []string) error {
	if len(args) != 1 || (args[0] != "show" && args[0] != "hide") {
		return usageErr("panel needs show or hide")
	}
	raw, err := c.pane(map[string]any{"kind": args[0]})
	if err != nil {
		return err
	}
	c.printPane(raw)
	return nil
}

func (c *cli) status() error {
	raw, err := c.pane(map[string]any{"kind": "status"})
	if err != nil {
		return err
	}
	c.printPane(raw)
	return nil
}

func (c *cli) apiGuide() error {
	raw, err := c.request("GET", "/api/cli/api-guide", nil, nil)
	if err != nil {
		return err
	}
	_, _ = c.env.Stdout.Write(raw)
	if !bytes.HasSuffix(raw, []byte("\n")) {
		c.out("\n")
	}
	return nil
}
