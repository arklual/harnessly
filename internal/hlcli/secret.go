package hlcli

import (
	"encoding/json"
	"errors"
	"github.com/ly-solutions/harnessly/internal/secretmask"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The safe of this machine (internal/secrets): the person puts secrets in outside the chat, an agent uses them by name.
// No subcommand prints a value: run hands it to a command's environment (and masks it in the command's output), file
// hands a path.

// secretReasons are the refusals in words an agent can act on.
var secretReasons = map[string]string{
	"bad-origin":   "a site must be an HTTPS origin, or HTTP on localhost, 127.0.0.1 or [::1]",
	"bad-project":  "project identifiers must be nonempty and have no surrounding spaces",
	"bad-name":     "a name is letters, digits, '.', '_' and '-' (64 at most), e.g. github-token; a reference is local:NAME or vault:PATH#KEY",
	"not-found":    "the safe has no secret by that name: ask the person for it with harnessly secret ask NAME --why \"...\"",
	"key-lost":     "the safe's key is gone, its values cannot be read: ask for the secret again with --replace",
	"rate-limited": "too many asks from this conversation in the last 10 minutes, wait a little",
	"too-many":     "too many asks wait for the person already",
	"empty":        "the value is empty",
	"too-large":    "the value is too large",
	"failed":       "the runtime could not read its safe",
	"denied":       "the safe's server refused this machine's login or this path: tell the person, do not retry",
	"unavailable":  "the safe's server does not answer or is not set up right: tell the person, try again later",
	"read-only":    "a vault: reference is read only: it is changed on the server, not through the safe",
	"bad-code":     "a code is 4 to 8 digits (--digits)",
	"no-chat":      "a code is asked only from a chat of this machine: the person types it in that chat's panel",
}

const (
	// secretAskWait is how long ask waits for the person by default; one request waits at most secretAskStep.
	secretAskWait = 100 * time.Second
	secretAskStep = 50 * time.Second
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// exitCode is the exit of a command run passes on.
type exitCode int

func (e exitCode) Error() string { return "exit " + strconv.Itoa(int(e)) }

func (c *cli) secretRefused(status int, raw []byte) error {
	var r struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &r)
	reason := secretReasons[r.Error]
	if reason == "" {
		return apiError(status, raw)
	}
	return fail("refused: %s — %s", r.Error, reason)
}

func (c *cli) secret(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		return c.secretList()
	case "ask":
		return c.secretAsk(args)
	case "run":
		return c.secretRun(args)
	case "file":
		return c.secretFile(args)
	case "get", "show", "print", "cat", "reveal":
		return usageErr("the safe never prints a value: use harnessly secret run --env VAR=NAME -- COMMAND, or harnessly secret file NAME")
	}
	return usageErr("secret takes list, ask, run or file (harnessly secret --help)")
}

func (c *cli) secretList() error {
	raw, err := c.request("GET", "/api/cli/secrets", nil, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Secrets []struct {
			Name       string `json:"name"`
			UpdatedAt  int64  `json:"updatedAt"`
			LastUsedAt int64  `json:"lastUsedAt"`
			Where      string `json:"where"`
		} `json:"secrets"`
		Store *struct {
			Mode  string `json:"mode"`
			Addr  string `json:"addr"`
			Refs  bool   `json:"refs"`
			Error string `json:"error"`
		} `json:"store"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	stamp := func(ms int64) string {
		if ms == 0 {
			return "never"
		}
		return time.UnixMilli(ms).In(c.zone()).Format("2006-01-02 15:04")
	}
	if st := out.Store; st != nil {
		switch {
		case st.Error != "":
			c.out("the safe is on a server that cannot be used: %s\n", st.Error)
		case st.Refs:
			c.out("the safe is on %s; vault:PATH#KEY references may be used\n", st.Addr)
		default:
			c.out("the safe is on %s\n", st.Addr)
		}
	}
	for _, s := range out.Secrets {
		where := ""
		if s.Where != "" {
			where = "\t" + s.Where
		}
		c.out("%s\tput %s\tused %s%s\n", s.Name, stamp(s.UpdatedAt), stamp(s.LastUsedAt), where)
	}
	if len(out.Secrets) == 0 {
		c.out("the safe is empty: ask the person for a secret with harnessly secret ask NAME --why \"...\"\n")
	}
	return nil
}

func (c *cli) zone() *time.Location {
	if c.env.Now != nil {
		return c.env.Now().Location()
	}
	return time.Local
}

func (c *cli) secretAsk(args []string) error {
	name, why, origin, replace, wait := "", "", "", false, secretAskWait
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--why":
			if i+1 >= len(args) {
				return usageErr("--why needs the reason")
			}
			i++
			why = args[i]
		case strings.HasPrefix(a, "--why="):
			why = strings.TrimPrefix(a, "--why=")
		case a == "--origin":
			if i+1 >= len(args) {
				return usageErr("--origin needs a site")
			}
			i++
			origin = args[i]
		case strings.HasPrefix(a, "--origin="):
			origin = strings.TrimPrefix(a, "--origin=")
		case a == "--replace":
			replace = true
		case a == "--wait" || strings.HasPrefix(a, "--wait="):
			v := strings.TrimPrefix(a, "--wait=")
			if a == "--wait" {
				if i+1 >= len(args) {
					return usageErr("--wait needs seconds")
				}
				i++
				v = args[i]
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 1800 {
				return usageErr("--wait takes seconds, 0 to 1800")
			}
			wait = time.Duration(n) * time.Second
		case strings.HasPrefix(a, "-"):
			return usageErr("secret ask: unknown option %s", a)
		case name == "":
			name = a
		default:
			return usageErr("secret ask takes one name: harnessly secret ask NAME --why \"...\"")
		}
	}
	if name == "" || strings.TrimSpace(why) == "" {
		return usageErr("secret ask needs the name and why you need it: harnessly secret ask NAME --why \"...\"")
	}
	deadline := time.Now().Add(wait)
	for {
		step := min(time.Until(deadline), secretAskStep)
		status, raw, err := c.call("POST", "/api/cli/secrets/ask", nil,
			map[string]any{"session": c.session(), "name": name, "why": why, "replace": replace, "origin": origin, "wait": max(step, 0).Seconds()})
		if err != nil {
			return err
		}
		if status < 200 || status > 299 {
			return c.secretRefused(status, raw)
		}
		var r struct {
			Status   string `json:"status"`
			PageOpen bool   `json:"pageOpen"`
		}
		_ = json.Unmarshal(raw, &r)
		switch r.Status {
		case "ready":
			if c.json {
				c.printRaw(raw)
			} else {
				c.out("ready: %s (use it with harnessly secret run --env VAR=%s -- COMMAND)\n", name, name)
			}
			return nil
		case "declined":
			return fail("declined: the person did not put %s in the safe. Do not ask for it in the chat; go on without it or ask them what to do", name)
		}
		// Replace was asked once: asking again only waits on the same request.
		if time.Until(deadline) <= 0 {
			if c.json {
				c.printRaw(raw)
				return nil
			}
			where := "the person sees a card asking for it"
			if !r.PageOpen {
				where = "the person's page is not open now: the card waits there for 30 minutes"
			}
			c.out("waiting: %s — %s. Run the same command again to keep waiting (in the background with --wait 1800, its end wakes you).\n", name, where)
			return nil
		}
	}
}

// pair is VAR=NAME, or NAME alone when it is also a variable's name.
func pair(v string) (env, name string, ok bool) {
	env, name, found := strings.Cut(v, "=")
	if !found {
		name = env
	}
	return env, name, envNameRe.MatchString(env) && name != ""
}

// codeAsk is a one-time code run waits for before it starts the command: the person types it in the chat's panel.
type codeAsk struct {
	env, why string
	digits   int
	wait     time.Duration
}

// flagValue is the value of --flag VALUE or --flag=VALUE at args[*i].
func flagValue(args []string, i *int, flag string) (string, bool, error) {
	a := args[*i]
	if v, ok := strings.CutPrefix(a, flag+"="); ok {
		return v, true, nil
	}
	if a != flag {
		return "", false, nil
	}
	if *i+1 >= len(args) {
		return "", true, usageErr("%s needs a value", flag)
	}
	*i++
	return args[*i], true, nil
}

func (c *cli) secretRun(args []string) error {
	envs, files := map[string]string{}, map[string]string{}
	code := codeAsk{digits: 6, wait: 10 * time.Minute}
	var cmd []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			cmd = args[i+1:]
			break
		}
		if v, ok, err := flagValue(args, &i, "--code"); ok {
			if err != nil {
				return err
			}
			if !envNameRe.MatchString(v) || code.env != "" {
				return usageErr("secret run: --code takes one variable name, e.g. --code OTP")
			}
			code.env = v
			continue
		}
		if v, ok, err := flagValue(args, &i, "--why"); ok {
			if err != nil {
				return err
			}
			code.why = v
			continue
		}
		if v, ok, err := flagValue(args, &i, "--digits"); ok {
			if err != nil {
				return err
			}
			n, e := strconv.Atoi(v)
			if e != nil || n < 4 || n > 8 {
				return usageErr("--digits takes 4 to 8")
			}
			code.digits = n
			continue
		}
		if v, ok, err := flagValue(args, &i, "--wait"); ok {
			if err != nil {
				return err
			}
			d, e := time.ParseDuration(v)
			if e != nil || d < time.Minute || d > 30*time.Minute {
				return usageErr("--wait takes a duration from 1m to 30m (10m)")
			}
			code.wait = d
			continue
		}
		var target map[string]string
		v := ""
		switch {
		case a == "--env" || a == "-e" || a == "--file":
			if i+1 >= len(args) {
				return usageErr("%s needs VAR=NAME", a)
			}
			i++
			v = args[i]
			target = envs
			if a == "--file" {
				target = files
			}
		case strings.HasPrefix(a, "--env="):
			v, target = strings.TrimPrefix(a, "--env="), envs
		case strings.HasPrefix(a, "--file="):
			v, target = strings.TrimPrefix(a, "--file="), files
		default:
			return usageErr("secret run: the command goes after --: harnessly secret run --env VAR=NAME -- COMMAND ARGS...")
		}
		env, name, ok := pair(v)
		if !ok {
			return usageErr("secret run: %q is not VAR=NAME", v)
		}
		target[env] = name
	}
	if len(cmd) == 0 || len(envs)+len(files) == 0 && code.env == "" {
		return usageErr("secret run needs a secret and a command: harnessly secret run --env VAR=NAME -- COMMAND ARGS...")
	}
	if code.env != "" && strings.TrimSpace(code.why) == "" {
		return usageErr("secret run --code needs --why: what the code is for, as the person sees it (\"npm publish vizloom\")")
	}
	if code.env == "" && code.why != "" {
		return usageErr("secret run: --why goes with --code")
	}
	values := map[string]string{}
	if len(envs)+len(files) > 0 {
		v, err := c.secretValues(envs, files)
		if err != nil {
			return err
		}
		values = v
	}
	environ := os.Environ()
	if c.env.Environ != nil {
		environ = c.env.Environ()
	}
	masked := make([]string, 0, len(values)+1)
	for _, v := range values {
		masked = append(masked, v)
	}
	if code.env != "" {
		value, err := c.waitCode(code)
		if err != nil {
			return err
		}
		environ = append(environ, code.env+"="+value)
		masked = append(masked, value)
	}
	for v, n := range envs {
		environ = append(environ, v+"="+values[n])
	}
	if len(files) > 0 {
		dir, err := os.MkdirTemp("", "harnessly-secret-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		for v, n := range files {
			p := dir + string(os.PathSeparator) + v
			if err := os.WriteFile(p, []byte(values[n]), 0o600); err != nil {
				return err
			}
			environ = append(environ, v+"="+p)
		}
	}
	stdout, stderr := newMasker(c.env.Stdout, masked), newMasker(c.env.Stderr, masked)
	child := exec.Command(cmd[0], cmd[1:]...)
	child.Env = environ
	child.Stdin = c.env.Stdin
	child.Stdout, child.Stderr = stdout, stderr
	err := child.Run()
	stdout.Flush()
	stderr.Flush()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exit):
		return exitCode(max(exit.ExitCode(), 1))
	}
	return fail("secret run: %v", err)
}

// secretValues reads the named secrets of run from the safe.
func (c *cli) secretValues(envs, files map[string]string) (map[string]string, error) {
	seen := map[string]bool{}
	var names []string
	for _, m := range []map[string]string{envs, files} {
		for _, n := range m {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)
	status, raw, err := c.call("POST", "/api/cli/secrets/use", nil, map[string]any{"session": c.session(), "names": names})
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, c.secretRefused(status, raw)
	}
	var r struct {
		Values map[string]string `json:"values"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fail("the runtime's answer is not readable")
	}
	return r.Values, nil
}

// waitCode asks the person for a one-time code and waits for it in one run: the chat's panel shows the ask, the
// person types the code, and it comes back here once. An interrupt takes the ask off the panel.
func (c *cli) waitCode(code codeAsk) (string, error) {
	status, raw, err := c.call("POST", "/api/cli/secrets/code", nil,
		map[string]any{"session": c.session(), "why": code.why, "digits": code.digits, "wait": code.wait.Seconds()})
	if err != nil {
		return "", err
	}
	if status < 200 || status > 299 {
		return "", c.secretRefused(status, raw)
	}
	var opened struct {
		ID    string `json:"id"`
		Until int64  `json:"until"`
	}
	if json.Unmarshal(raw, &opened) != nil || opened.ID == "" {
		return "", fail("the runtime's answer is not readable")
	}
	_, _ = io.WriteString(c.env.Stderr, "waiting for a code: the person types it in this chat's panel («Ждут код»), up to "+
		code.wait.Round(time.Minute).String()+"; the command starts as soon as it comes\n")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(stop)
	go func() {
		if _, ok := <-stop; ok {
			_, _, _ = c.call("POST", "/api/cli/secrets/code/cancel", nil, map[string]any{"session": c.session(), "id": opened.ID})
			os.Exit(130)
		}
	}()
	deadline := time.UnixMilli(opened.Until).Add(5 * time.Second)
	for {
		step := max(min(time.Until(deadline), secretAskStep), time.Second)
		status, raw, err := c.call("POST", "/api/cli/secrets/code/wait", nil,
			map[string]any{"session": c.session(), "id": opened.ID, "wait": step.Seconds()})
		if err != nil {
			return "", err
		}
		if status < 200 || status > 299 {
			return "", c.secretRefused(status, raw)
		}
		var r struct {
			Status string `json:"status"`
			Value  string `json:"value"`
		}
		_ = json.Unmarshal(raw, &r)
		switch r.Status {
		case "ready":
			return r.Value, nil
		case "waiting":
			if time.Now().Before(deadline) {
				continue
			}
			return "", fail("expired: no code came in %s; the command did not run", code.wait.Round(time.Minute))
		case "cancelled":
			return "", fail("cancelled: the person cancelled the code; the command did not run. Ask them before asking again")
		case "expired":
			return "", fail("expired: no code came in %s; the command did not run", code.wait.Round(time.Minute))
		}
		return "", fail("the code ask is gone (the runtime restarted); the command did not run: run it again")
	}
}

func (c *cli) secretFile(args []string) error {
	name, ttl := "", 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--ttl" || strings.HasPrefix(a, "--ttl="):
			v := strings.TrimPrefix(a, "--ttl=")
			if a == "--ttl" {
				if i+1 >= len(args) {
					return usageErr("--ttl needs a duration (10m)")
				}
				i++
				v = args[i]
			}
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 || d > 2*time.Hour {
				return usageErr("--ttl takes a duration up to 2h (10m, 1h)")
			}
			ttl = int(d.Seconds())
		case strings.HasPrefix(a, "-"):
			return usageErr("secret file: unknown option %s", a)
		case name == "":
			name = a
		default:
			return usageErr("secret file takes one name")
		}
	}
	if name == "" {
		return usageErr("secret file needs the name: harnessly secret file NAME [--ttl 15m]")
	}
	status, raw, err := c.call("POST", "/api/cli/secrets/file", nil, map[string]any{"session": c.session(), "name": name, "ttl": ttl})
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return c.secretRefused(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var r struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &r)
	c.out("%s\n", r.Path)
	return nil
}

// newMasker masks the values in what is written to w (internal/secretmask).
func newMasker(w io.Writer, values []string) *secretmask.Masker { return secretmask.New(w, values) }
