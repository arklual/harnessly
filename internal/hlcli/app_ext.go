package hlcli

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// The extension side of harnessly app: switch an app off for the whole machine, settings, secrets, the
// compatibility check and running a command with the app's secrets. Values of secrets go in through stdin and out
// only into the environment of the command, masked in its output.

// appEverywhere switches an app off or on for the whole machine (`app disable ID --everywhere`).
func (c *cli) appEverywhere(id string, off bool) error {
	raw, err := c.request("POST", "/api/cli/apps/disable", nil, map[string]any{"id": id, "off": off})
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	if off {
		c.out("%s is disabled: its settings, secrets and data are kept\n", id)
	} else {
		c.out("%s is enabled\n", id)
	}
	return nil
}

// appToggle is `app enable|disable ID [--everywhere]`.
func (c *cli) appToggle(args []string, on bool) error {
	every, rest := false, []string{}
	for _, a := range args {
		if a == "--everywhere" || a == "--all" {
			every = true
		} else {
			rest = append(rest, a)
		}
	}
	if every {
		if len(rest) != 1 || strings.HasPrefix(rest[0], "-") {
			return usageErr("takes the app's id")
		}
		return c.appEverywhere(rest[0], !on)
	}
	return c.appEnable(rest, on)
}

func (c *cli) appSettings(args []string) error {
	if len(args) == 0 {
		return usageErr("app settings get ID, or app settings set ID KEY=VALUE…")
	}
	switch args[0] {
	case "get":
		if len(args) != 2 {
			return usageErr("app settings get takes the app's id")
		}
		raw, err := c.request("GET", "/api/cli/apps/settings", url.Values{"id": {args[1]}}, nil)
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var r struct {
			Extension struct {
				Values map[string]any `json:"values"`
			} `json:"extension"`
		}
		_ = json.Unmarshal(raw, &r)
		keys := make([]string, 0, len(r.Extension.Values))
		for k := range r.Extension.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v, _ := json.Marshal(r.Extension.Values[k])
			c.out("%s=%s\n", k, v)
		}
		return nil
	case "set":
		if len(args) < 3 {
			return usageErr("app settings set ID KEY=VALUE… (a value is JSON when it parses as one, else text)")
		}
		values := map[string]any{}
		for _, kv := range args[2:] {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				return usageErr("%q is not KEY=VALUE", kv)
			}
			var j any
			if json.Unmarshal([]byte(v), &j) == nil {
				values[k] = j
			} else {
				values[k] = v
			}
		}
		// Set replaces the form as a whole: start from what is saved so one key can be changed.
		raw, err := c.request("GET", "/api/cli/apps/settings", url.Values{"id": {args[1]}}, nil)
		if err != nil {
			return err
		}
		var cur struct {
			Extension struct {
				Values map[string]any `json:"values"`
			} `json:"extension"`
		}
		_ = json.Unmarshal(raw, &cur)
		merged := cur.Extension.Values
		if merged == nil {
			merged = map[string]any{}
		}
		for k, v := range values {
			merged[k] = v
		}
		raw, err = c.request("POST", "/api/cli/apps/settings", nil, map[string]any{"id": args[1], "values": merged})
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		c.out("settings of %s saved\n", args[1])
		return nil
	}
	return usageErr("app settings takes get or set")
}

func (c *cli) appSecret(args []string) error {
	if len(args) < 3 || (args[0] != "set" && args[0] != "rm") {
		return usageErr("app secret set ID NAME (the value is read from stdin), or app secret rm ID NAME")
	}
	body := map[string]any{"id": args[1], "name": args[2]}
	if args[0] == "rm" {
		body["remove"] = true
	} else {
		if c.env.Stdin == nil {
			return usageErr("the value of the secret comes through stdin")
		}
		v, err := io.ReadAll(io.LimitReader(c.env.Stdin, 64*1024+1))
		if err != nil {
			return err
		}
		if len(v) > 64*1024 {
			return fail("the secret is longer than 64 KB")
		}
		val := strings.TrimRight(string(v), "\r\n")
		if val == "" {
			return fail("the secret is empty: nothing was read from stdin")
		}
		body["value"] = val
	}
	raw, err := c.request("POST", "/api/cli/apps/secret", nil, body)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	if args[0] == "rm" {
		c.out("secret %s of %s removed\n", args[2], args[1])
	} else {
		c.out("secret %s of %s saved\n", args[2], args[1])
	}
	return nil
}

// appCompat prints the compatibility of the installed apps with a harnessly version; it exits 1 when one does not fit,
// so `harnessly upgrade` and a script can stop before the core is replaced.
func (c *cli) appCompat(args []string) error {
	host := ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--host":
			if i+1 >= len(args) {
				return usageErr("--host needs a version")
			}
			i++
			host = args[i]
		case strings.HasPrefix(a, "--host="):
			host = strings.TrimPrefix(a, "--host=")
		default:
			return usageErr("app compat takes --host VERSION")
		}
	}
	q := url.Values{}
	if host != "" {
		q.Set("host", host)
	}
	raw, err := c.request("GET", "/api/cli/apps/compat", q, nil)
	if err != nil {
		return err
	}
	var rep apps.CompatReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return fail("the runtime's answer is not readable")
	}
	if c.json {
		c.printRaw(raw)
	} else {
		c.out("harnessly %s\n", rep.Host)
		for _, a := range rep.Apps {
			state := "ok"
			if !a.OK() {
				state = "does not fit"
			}
			c.out("%-32s %-8s %-10s %s\n", a.ID, a.Kind, a.Version, state)
			for _, i := range a.Issues {
				tag := "error"
				if i.Warning {
					tag = "warning"
				}
				c.out("    %s: %s\n", tag, i.Message)
			}
		}
	}
	if !rep.OK {
		return exitCode(1)
	}
	return nil
}

// appRun is `app run ID --secret NAME[=VAR] … -- COMMAND ARGS…`: the command gets the secrets the app's manifest
// declares and has the right to, in its environment; what it prints is masked.
func (c *cli) appRun(args []string) error {
	if len(args) == 0 {
		return usageErr("app run ID --secret NAME[=VAR] -- COMMAND ARGS…")
	}
	id, envs := args[0], map[string]string{}
	var cmd []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			cmd = args[i+1:]
			break
		}
		v := ""
		switch {
		case a == "--secret" && i+1 < len(args):
			i++
			v = args[i]
		case strings.HasPrefix(a, "--secret="):
			v = strings.TrimPrefix(a, "--secret=")
		default:
			return usageErr("app run: the command goes after --")
		}
		name, env, found := strings.Cut(v, "=")
		if !found {
			env = strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(name))
		}
		if name == "" || !envNameRe.MatchString(env) {
			return usageErr("app run: %q is not NAME or NAME=VAR", v)
		}
		envs[env] = name
	}
	if len(cmd) == 0 || len(envs) == 0 {
		return usageErr("app run needs a secret and a command: harnessly app run ID --secret NAME -- COMMAND ARGS…")
	}
	seen, names := map[string]bool{}, []string{}
	for _, n := range envs {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	status, raw, err := c.call("POST", "/api/cli/apps/secrets/use", nil, map[string]any{"id": id, "names": names})
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
	values := make([]string, 0, len(r.Values))
	for v, n := range envs {
		environ = append(environ, v+"="+r.Values[n])
	}
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
	return fail("app run: %v", err)
}
