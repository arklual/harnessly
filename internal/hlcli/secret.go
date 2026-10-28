package hlcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The safe of this machine (internal/secrets): the person puts secrets in outside the chat, an agent uses them by name.
// No subcommand prints a value: run hands it to a command's environment (and masks it in the command's output), file
// hands a path.

// secretReasons are the refusals in words an agent can act on.
var secretReasons = map[string]string{
	"bad-name":     "a name is letters, digits, '.', '_' and '-' (64 at most), e.g. github-token",
	"not-found":    "the safe has no secret by that name: ask the person for it with harnessly secret ask NAME --why \"...\"",
	"key-lost":     "the safe's key is gone, its values cannot be read: ask for the secret again with --replace",
	"rate-limited": "too many asks from this conversation in the last 10 minutes, wait a little",
	"too-many":     "too many asks wait for the person already",
	"empty":        "the value is empty",
	"too-large":    "the value is too large",
	"failed":       "the runtime could not read its safe",
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
		} `json:"secrets"`
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
	for _, s := range out.Secrets {
		c.out("%s\tput %s\tused %s\n", s.Name, stamp(s.UpdatedAt), stamp(s.LastUsedAt))
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
	name, why, replace, wait := "", "", false, secretAskWait
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
			if err != nil || n < 0 || n > 600 {
				return usageErr("--wait takes seconds, 0 to 600")
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
			map[string]any{"session": c.session(), "name": name, "why": why, "replace": replace, "wait": max(step, 0).Seconds()})
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
			c.out("waiting: %s — %s. Run the same command again to keep waiting.\n", name, where)
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

func (c *cli) secretRun(args []string) error {
	envs, files := map[string]string{}, map[string]string{}
	var cmd []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			cmd = args[i+1:]
			break
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
	if len(cmd) == 0 || len(envs)+len(files) == 0 {
		return usageErr("secret run needs a secret and a command: harnessly secret run --env VAR=NAME -- COMMAND ARGS...")
	}
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
		return err
	}
	if status < 200 || status > 299 {
		return c.secretRefused(status, raw)
	}
	var r struct {
		Values map[string]string `json:"values"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return fail("the runtime's answer is not readable")
	}
	environ := os.Environ()
	if c.env.Environ != nil {
		environ = c.env.Environ()
	}
	for v, n := range envs {
		environ = append(environ, v+"="+r.Values[n])
	}
	if len(files) > 0 {
		dir, err := os.MkdirTemp("", "harnessly-secret-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		for v, n := range files {
			p := dir + string(os.PathSeparator) + v
			if err := os.WriteFile(p, []byte(r.Values[n]), 0o600); err != nil {
				return err
			}
			environ = append(environ, v+"="+p)
		}
	}
	values := make([]string, 0, len(r.Values))
	for _, v := range r.Values {
		values = append(values, v)
	}
	stdout, stderr := newMasker(c.env.Stdout, values), newMasker(c.env.Stderr, values)
	child := exec.Command(cmd[0], cmd[1:]...)
	child.Env = environ
	child.Stdin = c.env.Stdin
	child.Stdout, child.Stderr = stdout, stderr
	err = child.Run()
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

// masker writes through to w with every value replaced by a mark. It holds back only the end of what it was given that
// could be the start of a value, so a value split between two writes is masked too, and output is not delayed otherwise.
type masker struct {
	w      io.Writer
	values [][]byte
	mu     sync.Mutex
	held   []byte
}

var maskMark = []byte("••••••")

// minMasked: shorter values are not masked (they would mangle ordinary output).
const minMasked = 4

func newMasker(w io.Writer, values []string) *masker {
	m := &masker{w: w}
	for _, v := range values {
		if len(v) >= minMasked {
			m.values = append(m.values, []byte(v))
		}
	}
	// The longer first: a value that contains another is masked whole.
	sort.Slice(m.values, func(i, j int) bool { return len(m.values[i]) > len(m.values[j]) })
	return m
}

func (m *masker) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := append(m.held, p...)
	for _, v := range m.values {
		buf = bytes.ReplaceAll(buf, v, maskMark)
	}
	keep := 0
	for _, v := range m.values {
		for n := min(len(v)-1, len(buf)); n > keep; n-- {
			if bytes.HasSuffix(buf, v[:n]) {
				keep = n
				break
			}
		}
	}
	m.held = append([]byte(nil), buf[len(buf)-keep:]...)
	if _, err := m.w.Write(buf[:len(buf)-keep]); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush writes what was held back: the output ended, it was not a value.
func (m *masker) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.held) > 0 {
		_, _ = m.w.Write(m.held)
		m.held = nil
	}
}
