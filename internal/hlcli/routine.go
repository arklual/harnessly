package hlcli

import (
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The person's routines: recurring agent tasks the hub runs on a schedule (docs/hub/routines.md).

type routineView struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	PromptTemplate string         `json:"promptTemplate"`
	Model          string         `json:"model"`
	Enabled        bool           `json:"enabled"`
	PausedReason   string         `json:"pausedReason"`
	State          string         `json:"state"`
	Trigger        map[string]any `json:"trigger"`
	Routine        struct {
		Words    string `json:"words"`
		Autonomy string `json:"autonomy"`
		Once     bool   `json:"once"`
		Until    string `json:"until"`
		Delivery struct {
			When     string   `json:"when"`
			Channels []string `json:"channels"`
			File     string   `json:"file"`
		} `json:"delivery"`
		Checks []struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
			Label string `json:"label"`
		} `json:"checks"`
	} `json:"routine"`
	Next []string `json:"next"`
	Last []struct {
		At      string `json:"at"`
		Status  string `json:"status"`
		Outcome string `json:"outcome"`
	} `json:"last"`
}

func (r routineView) cron() string {
	if s, ok := r.Trigger["schedule"].(map[string]any); ok {
		c, _ := s["cron"].(string)
		return c
	}
	return ""
}

func (c *cli) routine(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	path := "/api/cli/routines"
	one := func() (string, error) {
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return "", usageErr("name the routine: its id or its name (harnessly routine list)")
		}
		id := args[0]
		args = args[1:]
		return path + "/" + url.PathEscape(id), nil
	}
	switch sub {
	case "list", "ls":
		raw, err := c.request("GET", path, nil, nil)
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var out struct {
			Routines     []routineView `json:"routines"`
			DefaultModel string        `json:"defaultModel"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		if len(out.Routines) == 0 {
			c.out("no routines yet. Make one: harnessly routine create --name … --cron … --prompt …\n")
			return nil
		}
		for _, r := range out.Routines {
			c.printRoutineLine(r)
		}
		return nil
	case "show", "get":
		p, err := one()
		if err != nil {
			return err
		}
		raw, err := c.request("GET", p, nil, nil)
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var out struct {
			Routine routineView `json:"routine"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		c.printRoutine(out.Routine)
		return nil
	case "create", "new", "add":
		body, err := routineBody(args, c.env.Stdin, true)
		if err != nil {
			return err
		}
		raw, err := c.request("POST", path, nil, body)
		if err != nil {
			return err
		}
		return c.printSaved(raw, "made")
	case "set", "update", "edit":
		p, err := one()
		if err != nil {
			return err
		}
		body, err := routineBody(args, c.env.Stdin, false)
		if err != nil {
			return err
		}
		raw, err := c.request("PATCH", p, nil, body)
		if err != nil {
			return err
		}
		return c.printSaved(raw, "saved")
	case "pause", "resume", "forget":
		p, err := one()
		if err != nil {
			return err
		}
		body := map[string]any{"enabled": sub == "resume"}
		if sub == "forget" {
			body = map[string]any{"state": ""}
		}
		raw, err := c.request("PATCH", p, nil, body)
		if err != nil {
			return err
		}
		return c.printSaved(raw, map[string]string{"pause": "paused", "resume": "resumed", "forget": "memory cleared"}[sub])
	case "run":
		p, err := one()
		if err != nil {
			return err
		}
		raw, err := c.request("POST", p+"/run", nil, nil)
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		c.out("queued: the hub starts it on one of your machines within a minute\n")
		return nil
	case "runs", "history":
		p, err := one()
		if err != nil {
			return err
		}
		raw, err := c.request("GET", p+"/runs", url.Values{"limit": {"20"}}, nil)
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var out struct {
			Runs []struct {
				CreatedAt string          `json:"createdAt"`
				Status    string          `json:"status"`
				Outcome   string          `json:"outcome"`
				Summary   string          `json:"summary"`
				Error     string          `json:"error"`
				Result    json.RawMessage `json:"result"`
			} `json:"runs"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		if len(out.Runs) == 0 {
			c.out("no runs yet\n")
		}
		for _, r := range out.Runs {
			what := firstText(r.Outcome, r.Status)
			text := firstText(r.Summary, r.Error)
			c.out("%s  %-7s %s\n", shortTime(r.CreatedAt), what, oneLine(text, 120))
		}
		return nil
	case "delete", "rm":
		p, err := one()
		if err != nil {
			return err
		}
		if _, err := c.request("DELETE", p, nil, nil); err != nil {
			return err
		}
		c.out("deleted\n")
		return nil
	}
	return usageErr("harnessly routine list|show|create|set|pause|resume|run|runs|forget|delete (harnessly help routine)")
}

// routineBody reads the flags of create and set into the body the hub takes.
func routineBody(args []string, stdin io.Reader, create bool) (map[string]any, error) {
	body := map[string]any{}
	rt := map[string]any{}
	delivery := map[string]any{}
	var checks []map[string]any
	sched := map[string]any{}
	need := func(i int, flag string) (string, error) {
		if i+1 >= len(args) {
			return "", usageErr("%s needs a value", flag)
		}
		return args[i+1], nil
	}
	for i := 0; i < len(args); i++ {
		flag := args[i]
		switch flag {
		case "--off":
			body["enabled"] = false
			continue
		case "--on":
			body["enabled"] = true
			continue
		case "--once":
			rt["once"] = true
			continue
		}
		v, err := need(i, flag)
		if err != nil {
			return nil, err
		}
		i++
		switch flag {
		case "--name":
			body["name"] = v
		case "--prompt":
			if v == "-" {
				raw, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
				if err != nil {
					return nil, err
				}
				v = string(raw)
			}
			body["promptTemplate"] = v
		case "--cron":
			sched["cron"] = v
		case "--tz":
			sched["tz"] = v
		case "--words":
			rt["words"] = v
		case "--model":
			body["model"] = v
		case "--machine":
			body["machineId"] = v
		case "--when":
			delivery["when"] = v
		case "--to":
			var chans []string
			for _, ch := range strings.Split(v, ",") {
				if ch = strings.TrimSpace(ch); ch != "" {
					chans = append(chans, ch)
				}
			}
			delivery["channels"] = chans
		case "--file":
			delivery["file"] = v
		case "--check":
			kind, value, _ := strings.Cut(v, ":")
			checks = append(checks, map[string]any{"kind": kind, "value": value})
		case "--autonomy":
			rt["autonomy"] = v
		case "--until":
			rt["until"] = v
		case "--budget":
			f, err := strconv.ParseFloat(strings.TrimSuffix(v, "$"), 64)
			if err != nil {
				return nil, usageErr("--budget is dollars per run, like 1.5")
			}
			rt["budgetUsd"] = f
		case "--chat":
			rt["chatId"] = v
		case "--json-body":
			if err := json.Unmarshal([]byte(v), &body); err != nil {
				return nil, usageErr("--json-body is the routine as JSON: %v", err)
			}
		default:
			return nil, usageErr("unknown flag %s (harnessly help routine)", flag)
		}
	}
	if len(sched) > 0 {
		if _, ok := sched["cron"]; !ok {
			return nil, usageErr("--tz goes with --cron")
		}
		body["trigger"] = map[string]any{"schedule": sched}
	}
	if len(delivery) > 0 {
		rt["delivery"] = delivery
	}
	if len(checks) > 0 {
		rt["checks"] = checks
	}
	if len(rt) > 0 || create {
		if _, ok := body["routine"]; !ok {
			body["routine"] = rt
		}
	}
	if create {
		for _, k := range []string{"name", "promptTemplate", "trigger"} {
			if _, ok := body[k]; !ok {
				return nil, usageErr("create needs --name, --prompt and --cron")
			}
		}
	}
	return body, nil
}

func (c *cli) printSaved(raw []byte, what string) error {
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Routine routineView `json:"routine"`
		Next    []string    `json:"next"`
		URL     string      `json:"url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	c.out("%s: %s (%s)\n", what, out.Routine.Name, out.Routine.ID)
	if len(out.Next) > 0 {
		c.out("next: %s\n", shortTime(out.Next[0]))
	}
	if out.URL != "" {
		c.out("page: %s\n", out.URL)
	}
	return nil
}

func (c *cli) printRoutineLine(r routineView) {
	state := "on"
	if !r.Enabled {
		state = "paused"
	}
	dots := ""
	for i := len(r.Last) - 1; i >= 0; i-- {
		switch r.Last[i].Outcome {
		case "ok":
			dots += "+"
		case "quiet":
			dots += "."
		case "failed":
			dots += "x"
		default:
			dots += "~"
		}
	}
	next := "—"
	if len(r.Next) > 0 {
		next = shortTime(r.Next[0])
	}
	when := firstText(r.Routine.Words, r.cron())
	c.out("%s  %-6s %-32s %-28s next %s  %s\n", r.ID, state, oneLine(r.Name, 32), oneLine(when, 28), next, dots)
}

func (c *cli) printRoutine(r routineView) {
	c.printRoutineLine(r)
	if r.PausedReason != "" {
		c.out("paused: %s\n", r.PausedReason)
	}
	c.out("schedule: %s  %s\n", r.cron(), r.Routine.Words)
	c.out("model: %s  autonomy: %s  result: %s → %s\n", firstText(r.Model, "default"), r.Routine.Autonomy, r.Routine.Delivery.When,
		strings.Join(append([]string{"chat"}, r.Routine.Delivery.Channels...), ", "))
	for _, ch := range r.Routine.Checks {
		c.out("check: %s %s\n", ch.Kind, firstText(ch.Label, ch.Value))
	}
	c.out("\n%s\n", r.PromptTemplate)
	if strings.TrimSpace(r.State) != "" {
		c.out("\nmemory:\n%s\n", r.State)
	}
}

func firstText(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func shortTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("Mon 02 Jan 15:04")
}
