package hlcli

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// An agent asks a person (internal/asks): the runtime sends the question through a Telegram bot with the person
// tagged, and this command waits for the answer. Exit codes: 0 answered (an approval: approved), 3 an approval not
// given (changes requested or only a comment), 4 no answer in time or called off, 1 refused or failed.

const (
	exitNotApproved = 3
	exitNoAnswer    = 4
)

// askExit is an outcome that is not an error but needs its own exit code.
type askExit struct {
	code int
	msg  string
}

func (e askExit) Error() string { return e.msg }

type askReply struct {
	From struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"from"`
	Text   string `json:"text"`
	Choice string `json:"choice"`
	Via    string `json:"via"`
}

type askView struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Question string `json:"question"`
	To       *struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"to"`
	Chat        string     `json:"chat"`
	Deadline    int64      `json:"deadline"`
	Decision    string     `json:"decision"`
	Answer      *askReply  `json:"answer"`
	Replies     []askReply `json:"replies"`
	SeenReplies int        `json:"seenReplies"`
	Error       string     `json:"error"`
}

func (a askView) who() string {
	if a.To == nil {
		return "the chat"
	}
	if a.To.Username != "" {
		return "@" + a.To.Username
	}
	return a.To.Name
}

func (r askReply) who() string {
	if r.From.Username != "" {
		return "@" + r.From.Username
	}
	return r.From.Name
}

var askReasons = map[string]string{
	"disabled":       "the person turned asks off (Settings → Notifications → Questions to people)",
	"no-bot":         "no Telegram bot with a chat may carry questions: the person links one in Settings → Notifications",
	"bot-not-found":  "no such bot among those allowed for questions (see: harnessly ask people)",
	"unknown-person": "only the person's contacts may be tagged (see: harnessly ask people)",
	"bad-person":     "--to takes a contact's name or a Telegram @username",
	"empty":          "the question is empty",
	"too-long":       "the question (1500), details (2000), an option (40, at most 6) or the link is too long",
	"rate-limited":   "too many questions from this conversation in the last hour: decide yourself or wait",
	"not-found":      "no question of that id",
	"closed":         "the question is closed already",
	"chat-gone":      "the bot cannot write to its chat any more: tell the person",
	"invalid-token":  "Telegram no longer takes the bot's token: the person replaces it in Settings → Notifications",
	"unreachable":    "Telegram did not answer, try again later",
	"rejected":       "Telegram refused the message",
	"failed":         "the runtime could not read or write its questions",
}

func askRefused(status int, raw []byte) error {
	var r struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(raw, &r)
	reason := askReasons[r.Error]
	if reason == "" {
		return apiError(status, raw)
	}
	if r.Detail != "" {
		reason += " (" + r.Detail + ")"
	}
	return fail("refused: %s — %s", r.Error, reason)
}

func (c *cli) ask(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "approve", "approval":
			return c.askNew("approval", args[1:])
		case "wait":
			return c.askWaitCmd(args[1:])
		case "status", "show":
			return c.askShow(args[1:])
		case "cancel":
			return c.askCancel(args[1:])
		case "people", "contacts":
			return c.askPeople()
		case "list", "ls":
			return c.askList()
		}
	}
	return c.askNew("question", args)
}

func durationArg(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return n * 60, nil // bare minutes
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, usageErr("--timeout takes minutes or a duration like 30m, 2h")
	}
	return int(d / time.Second), nil
}

func (c *cli) askNew(kind string, args []string) error {
	body := map[string]any{"kind": kind, "session": c.session()}
	options := []string{}
	noWait := false
	var words []string
	value := func(i *int, name string) (string, error) {
		if *i+1 >= len(args) {
			return "", usageErr("%s needs a value", name)
		}
		*i++
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, inline, hasInline := strings.Cut(a, "=")
		get := func() (string, error) {
			if hasInline && strings.HasPrefix(a, "--") {
				return inline, nil
			}
			return value(&i, name)
		}
		switch name {
		case "--to", "-t", "--bot", "-b", "--details", "-d", "--link", "-l", "--option", "-o", "--timeout":
			v, err := get()
			if err != nil {
				return err
			}
			switch name {
			case "--to", "-t":
				body["to"] = v
			case "--bot", "-b":
				body["bot"] = v
			case "--details", "-d":
				body["details"] = v
			case "--link", "-l":
				body["link"] = v
			case "--option", "-o":
				options = append(options, v)
			case "--timeout":
				sec, err := durationArg(v)
				if err != nil {
					return err
				}
				body["timeoutSec"] = sec
			}
		case "--no-wait":
			noWait = true
		default:
			words = append(words, a)
		}
	}
	text := strings.Join(words, " ")
	if text == "-" {
		raw, err := io.ReadAll(io.LimitReader(c.env.Stdin, 1<<20))
		if err != nil {
			return err
		}
		text = string(raw)
	}
	if strings.TrimSpace(text) == "" {
		return usageErr("ask needs the question: harnessly ask --to NAME QUESTION... (see harnessly help ask)")
	}
	body["question"] = text
	body["options"] = options
	status, raw, err := c.call("POST", "/api/cli/asks", nil, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return askRefused(status, raw)
	}
	var out struct {
		Ask askView `json:"ask"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	a := out.Ask
	left := time.Until(time.UnixMilli(a.Deadline)).Round(time.Minute)
	// The first line names the ask: the runtime finds a background wait by it.
	c.out("asked %s → %s in «%s», waiting up to %s\n", a.ID, a.who(), a.Chat, left)
	if f, ok := c.env.Stdout.(*os.File); ok {
		_ = f.Sync()
	}
	if noWait || c.json && noWait {
		return nil
	}
	return c.askAwait(a.ID, true)
}

// askAwait long-polls the runtime until the ask closes and prints the outcome. cancelOnStop: a stop of this process
// (the person stopped the background task) calls the ask off.
func (c *cli) askAwait(id string, cancelOnStop bool) error {
	if cancelOnStop {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		defer signal.Stop(stop)
		go func() {
			if _, ok := <-stop; ok {
				_, _, _ = c.call("POST", "/api/cli/asks/"+url.PathEscape(id)+"/cancel", nil, map[string]any{})
				c.out("stopped: the question was called off\n")
				os.Exit(exitNoAnswer)
			}
		}()
	}
	failures := 0
	for {
		status, raw, err := c.call("GET", "/api/cli/asks/"+url.PathEscape(id)+"/wait", url.Values{"timeout": {"50"}}, nil)
		if err != nil || status >= 500 {
			// The runtime restarts or the network blinks: the ask is kept, ask again a little later.
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
			return askRefused(status, raw)
		}
		var out struct {
			Ask askView `json:"ask"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		if out.Ask.Status == "waiting" {
			continue
		}
		if c.json {
			c.printRaw(raw)
		}
		return c.askOutcome(out.Ask)
	}
}

func (c *cli) askOutcome(a askView) error {
	say := func(format string, v ...any) {
		if !c.json {
			c.out(format, v...)
		}
	}
	others := func() {
		for _, r := range a.Replies {
			if a.Answer != nil && r.Text == a.Answer.Text && r.who() == a.Answer.who() {
				continue
			}
			if r.Text != "" {
				say("  also %s: %s\n", r.who(), r.Text)
			}
		}
	}
	switch a.Status {
	case "answered":
		ans := a.Answer
		via := ""
		if ans != nil && ans.Via == "web" {
			via = " (in harnessly)"
		}
		switch a.Decision {
		case "approved":
			say("approved by %s%s\n", ans.who(), via)
			others()
			return nil
		case "changes":
			say("changes requested by %s%s\n", ans.who(), via)
			others()
			return askExit{exitNotApproved, ""}
		case "comment":
			say("not approved — %s commented%s: %s\n", ans.who(), via, ans.Text)
			others()
			return askExit{exitNotApproved, ""}
		}
		say("answered by %s%s: %s\n", ans.who(), via, ans.Text)
		others()
		return nil
	case "timeout":
		say("no answer in time from %s: decide yourself, or ask again later\n", a.who())
		return askExit{exitNoAnswer, ""}
	case "cancelled":
		say("the question was called off\n")
		return askExit{exitNoAnswer, ""}
	}
	return fail("the question could not be sent: %s", a.Error)
}

func (c *cli) askID(args []string, what string) (string, error) {
	if len(args) == 0 || !strings.HasPrefix(args[0], "ask_") {
		return "", usageErr("%s needs the question's id (ask_…)", what)
	}
	return args[0], nil
}

func (c *cli) askWaitCmd(args []string) error {
	id, err := c.askID(args, "ask wait")
	if err != nil {
		return err
	}
	return c.askAwait(id, false)
}

func (c *cli) askShow(args []string) error {
	id, err := c.askID(args, "ask status")
	if err != nil {
		return err
	}
	status, raw, err := c.call("GET", "/api/cli/asks/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return askRefused(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Ask askView `json:"ask"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	a := out.Ask
	c.out("%s\t%s\t%s → %s in «%s»\n\t%s\n", a.ID, a.Status, a.Kind, a.who(), a.Chat, a.Question)
	for i, r := range a.Replies {
		mark := ""
		if i >= a.SeenReplies {
			mark = " (new)"
		}
		text := r.Text
		if text == "" {
			text = "[" + r.Choice + "]"
		}
		c.out("\t%s%s: %s\n", r.who(), mark, text)
	}
	return nil
}

func (c *cli) askCancel(args []string) error {
	id, err := c.askID(args, "ask cancel")
	if err != nil {
		return err
	}
	status, raw, err := c.call("POST", "/api/cli/asks/"+url.PathEscape(id)+"/cancel", nil, map[string]any{})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return askRefused(status, raw)
	}
	c.out("called off\n")
	return nil
}

func (c *cli) askList() error {
	raw, err := c.request("GET", "/api/cli/asks", nil, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Asks []askView `json:"asks"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for i, a := range out.Asks {
		if i >= 20 {
			break
		}
		c.out("%s\t%s\t%s → %s\t%s\n", a.ID, a.Status, a.Kind, a.who(), oneLine(a.Question, 80))
	}
	if len(out.Asks) == 0 {
		c.out("no questions yet\n")
	}
	return nil
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func (c *cli) askPeople() error {
	raw, err := c.request("GET", "/api/cli/asks/people", nil, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Enabled           bool `json:"enabled"`
		OnlyContacts      bool `json:"onlyContacts"`
		DefaultTimeoutMin int  `json:"defaultTimeoutMin"`
		MaxTimeoutMin     int  `json:"maxTimeoutMin"`
		Contacts          []struct {
			Name     string `json:"name"`
			Username string `json:"username"`
			Note     string `json:"note"`
			Approver bool   `json:"approver"`
		} `json:"contacts"`
		Bots []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Chat  string `json:"chat"`
			Bound bool   `json:"bound"`
		} `json:"bots"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if !out.Enabled {
		c.out("questions to people are off on this machine\n")
		return nil
	}
	c.out("PEOPLE\n")
	for _, p := range out.Contacts {
		user := ""
		if p.Username != "" {
			user = " @" + p.Username
		}
		role := ""
		if p.Approver {
			role = " (may approve)"
		}
		c.out("  %s%s%s\n", p.Name, user, role)
		if p.Note != "" {
			c.out("\t%s\n", p.Note)
		}
	}
	if len(out.Contacts) == 0 {
		c.out("  (no contacts)\n")
	}
	if !out.OnlyContacts {
		c.out("  any @username in the chat may be tagged too\n")
	}
	c.out("CHATS\n")
	for _, b := range out.Bots {
		if b.Bound {
			c.out("  %s\t→ %s\n", b.Name, b.Chat)
		}
	}
	c.out("waits %d min by default, %d min at most\n", out.DefaultTimeoutMin, out.MaxTimeoutMin)
	return nil
}
