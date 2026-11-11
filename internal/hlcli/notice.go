package hlcli

import (
	"encoding/json"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// `harnessly notice`: the agent asks the person in the smart notice — the same description as the
// notice_ask MCP tool and the SDK's hl.notice. Never passwords, keys or sign-ins: the machine refuses them.

func (c *cli) notice(args []string) error {
	if len(args) == 0 {
		return fail("say what: harnessly notice ask|show|update|close|wait (harnessly help notice)")
	}
	switch args[0] {
	case "ask":
		return c.noticeOpen(args[1:], true)
	case "show":
		return c.noticeOpen(args[1:], false)
	case "update":
		return c.noticeUpdate(args[1:])
	case "close":
		if len(args) != 2 {
			return fail("harnessly notice close ID")
		}
		return c.noticePost(args[1], "close", map[string]any{})
	case "wait":
		if len(args) != 2 {
			return fail("harnessly notice wait ID")
		}
		return c.noticeAwait(args[1], false)
	}
	return fail("unknown: harnessly notice %s (ask, show, update, close, wait)", args[0])
}

func (c *cli) noticeRefused(status int, raw []byte) error {
	var r struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &r)
	if r.Message != "" {
		return fail("%s", r.Message)
	}
	return apiError(status, raw)
}

func (c *cli) noticeOpen(args []string, waits bool) error {
	spec := map[string]any{}
	var options []string
	noWait := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fail("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var v string
		var err error
		switch a {
		case "--kind", "--title", "--text", "--icon", "--yes", "--no", "--placeholder", "--option", "-o":
			if v, err = next(); err != nil {
				return err
			}
			switch a {
			case "--option", "-o":
				options = append(options, v)
			default:
				spec[strings.TrimPrefix(a, "--")] = v
			}
		case "--own":
			spec["own"] = true
		case "--danger":
			spec["danger"] = true
		case "--no-wait":
			noWait = true
		case "--progress":
			if v, err = next(); err != nil {
				return err
			}
			p, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fail("--progress is a share, 0 to 1")
			}
			spec["progress"] = p
		case "--timeout":
			if v, err = next(); err != nil {
				return err
			}
			s, err := durationArg(v)
			if err != nil {
				return err
			}
			spec["timeoutSec"] = s
		default:
			if strings.HasPrefix(a, "--pass") || strings.HasPrefix(a, "--secret") || strings.HasPrefix(a, "--token") {
				return fail("the notice has no password or key field: for a secret run `harnessly secret ask NAME --why …`")
			}
			return fail("unknown option %s (harnessly help notice)", a)
		}
	}
	if len(options) > 0 {
		spec["options"] = options
	}
	if spec["kind"] == nil {
		if waits {
			spec["kind"] = "confirm"
			if len(options) > 0 {
				spec["kind"] = "choice"
			}
		} else {
			spec["kind"] = "status"
		}
	}
	kind, _ := spec["kind"].(string)
	if kind == "code" {
		return fail("an agent does not get one-time codes through the notice: run the command that needs it with " +
			"`harnessly secret run --code VAR --why \"…\" -- COMMAND` (the person types it in the chat's panel, it goes straight into the command)")
	}
	asking := kind != "status" && kind != "progress"
	if asking != waits || kind == "custom" {
		if waits {
			return fail("notice ask shows choice, confirm, field or code; a status or progress goes through notice show")
		}
		return fail("notice show shows status or progress; a question goes through notice ask")
	}
	status, raw, err := c.call("POST", "/api/cli/notices", nil, map[string]any{"session": c.session(), "spec": spec})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return c.noticeRefused(status, raw)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if !waits || noWait {
		if c.json {
			c.out("{\"id\":%q}\n", out.ID)
		} else {
			c.out("%s\n", out.ID)
		}
		return nil
	}
	return c.noticeAwait(out.ID, true)
}

// noticeAwait long-polls until the notice ends and prints its result as JSON; not answered exits 4. closeOnStop: a
// stop of this process takes the plate away.
func (c *cli) noticeAwait(id string, closeOnStop bool) error {
	if closeOnStop {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		defer signal.Stop(stop)
		go func() {
			if _, ok := <-stop; ok {
				_ = c.noticePost(id, "close", map[string]any{})
				os.Exit(exitNoAnswer)
			}
		}()
	}
	failures := 0
	for {
		q := url.Values{"timeout": {"50"}, "session": {c.session()}}
		status, raw, err := c.call("GET", "/api/cli/notices/"+url.PathEscape(id)+"/wait", q, nil)
		if err != nil || status >= 500 {
			failures++
			if failures > 30 {
				if err != nil {
					return err
				}
				return apiError(status, raw)
			}
			time.Sleep(5 * time.Second)
			continue
		}
		failures = 0
		if status < 200 || status >= 300 {
			return c.noticeRefused(status, raw)
		}
		var out struct {
			Done   bool            `json:"done"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		if !out.Done {
			continue
		}
		c.out("%s\n", out.Result)
		var r struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(out.Result, &r)
		if r.Status != "answered" {
			return askExit{exitNoAnswer, ""}
		}
		return nil
	}
}

func (c *cli) noticeUpdate(args []string) error {
	if len(args) == 0 {
		return fail("harnessly notice update ID [--progress 0.5] [--title …] [--text …]")
	}
	body := map[string]any{}
	for i := 1; i < len(args); i++ {
		if i+1 >= len(args) {
			return fail("%s needs a value", args[i])
		}
		switch args[i] {
		case "--progress":
			p, err := strconv.ParseFloat(args[i+1], 64)
			if err != nil {
				return fail("--progress is a share, 0 to 1")
			}
			body["progress"] = p
		case "--title", "--text":
			body[strings.TrimPrefix(args[i], "--")] = args[i+1]
		default:
			return fail("unknown option %s", args[i])
		}
		i++
	}
	return c.noticePost(args[0], "update", body)
}

func (c *cli) noticePost(id, action string, body map[string]any) error {
	body["session"] = c.session()
	status, raw, err := c.call("POST", "/api/cli/notices/"+url.PathEscape(id)+"/"+action, nil, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return c.noticeRefused(status, raw)
	}
	return nil
}
