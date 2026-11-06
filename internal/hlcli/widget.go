package hlcli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// harnessly widget: what an agent shows in the feed of the chat it runs in (internal/apps/widgets.go).
//
//	harnessly widget show <app>/<view> [--data JSON|@file|-] [--id ID] [--title T]
//	harnessly widget render --title T --html @file [--css @file] [--js @file] [--data JSON|@file|-] [--id ID]
//	harnessly widget list
//	harnessly widget remove <id>
//
// The same --id again changes that widget where it stands. A widget's answer is the line «widget <id> shown»: the feed
// draws the widget under the command's step; shown again it answers «widget <id> changed» and draws nothing new.

const widgetUsage = `harnessly widget — a widget in the feed of this chat

  harnessly widget show <app>/<view> [--data JSON|@file|-] [--id ID] [--title T]
      an app's view for the chat (its card), drawn with the data; the app has to be on in this chat
  harnessly widget render --title T --html @file [--css @file] [--js @file] [--data JSON|@file|-] [--id ID]
      a widget you write: the body's markup, its style and a classic script; no network, window.echarts and
      window.widgetData are there, harnessly.connect() gives hl.chat.send(text)
  harnessly widget list
  harnessly widget remove <id>

  --id ID   the same id changes that widget where it stands (lower-case latin letters, digits, - and _, up to 40)
  @file     reads the value from a file; - reads it from stdin`

func (c *cli) widget(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		_, _ = fmt.Fprintln(c.env.Stdout, widgetUsage)
		return nil
	}
	session := c.session()
	if session == "" {
		return errNoPane
	}
	switch args[0] {
	case "show", "render":
		return c.widgetShow(args[0], args[1:], session)
	case "list", "ls":
		status, raw, err := c.call("GET", "/api/cli/apps/widgets", url.Values{"session": {session}}, nil)
		if err != nil {
			return err
		}
		if status != 200 {
			return widgetRefused(status, raw)
		}
		var out struct {
			Widgets []struct {
				ID, Kind, App, View, Title string
				Rev                        int
			} `json:"widgets"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		for _, w := range out.Widgets {
			what := w.App + "/" + w.View
			if w.Kind == "html" {
				what = "html"
			}
			_, _ = fmt.Fprintf(c.env.Stdout, "%s\t%s\trev %d\t%s\n", w.ID, what, w.Rev, w.Title)
		}
		return nil
	case "remove", "rm":
		if len(args) != 2 {
			return usageErr("harnessly widget remove <id>")
		}
		status, raw, err := c.call("POST", "/api/cli/apps/widgets/remove", nil, map[string]any{"session": session, "id": args[1]})
		if err != nil {
			return err
		}
		if status != 200 {
			return widgetRefused(status, raw)
		}
		_, _ = fmt.Fprintf(c.env.Stdout, "widget %s removed\n", args[1])
		return nil
	}
	return usageErr("unknown widget command %q (show, render, list, remove)", args[0])
}

// widgetValue is a flag's value: as written, @file read from the file, - read from stdin.
func (c *cli) widgetValue(v string) (string, error) {
	switch {
	case v == "-":
		raw, err := io.ReadAll(io.LimitReader(c.env.Stdin, 1<<20))
		return string(raw), err
	case strings.HasPrefix(v, "@"):
		path := v[1:]
		if !strings.HasPrefix(path, "/") && c.env.Cwd != "" {
			path = c.env.Cwd + "/" + path
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fail("cannot read %s: %v", v[1:], err)
		}
		return string(raw), nil
	}
	return v, nil
}

func (c *cli) widgetShow(cmd string, args []string, session string) error {
	body := map[string]any{"session": session}
	if cmd == "render" {
		body["kind"] = "html"
	} else {
		body["kind"] = "view"
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, inline, hasInline := strings.Cut(a, "=")
		if !strings.HasPrefix(a, "--") {
			if cmd != "show" || body["app"] != nil {
				return usageErr("unexpected %q (see harnessly widget --help)", a)
			}
			app, view, ok := strings.Cut(a, "/")
			if !ok || app == "" || view == "" {
				return usageErr("show takes <app>/<view>, the app's view for the chat")
			}
			body["app"], body["view"] = app, view
			continue
		}
		v := inline
		if !hasInline {
			if i+1 >= len(args) {
				return usageErr("%s needs a value", name)
			}
			i++
			v = args[i]
		}
		val, err := c.widgetValue(v)
		if err != nil {
			return err
		}
		switch name {
		case "--id":
			body["id"] = val
		case "--title":
			body["title"] = val
		case "--data":
			if !json.Valid([]byte(val)) {
				return usageErr("--data is JSON (or @file of JSON)")
			}
			body["data"] = json.RawMessage(val)
		case "--html":
			body["html"] = val
		case "--css":
			body["css"] = val
		case "--js", "--script":
			body["script"] = val
		default:
			return usageErr("unknown flag %s (see harnessly widget --help)", name)
		}
	}
	if cmd == "show" && body["app"] == nil {
		return usageErr("harnessly widget show <app>/<view> [--data JSON]")
	}
	if cmd == "render" && (body["html"] == nil || body["title"] == nil) {
		return usageErr("harnessly widget render --title T --html @file [--css @file] [--js @file]")
	}
	if h, _ := body["html"].(string); strings.Contains(strings.ToLower(h), "<script") {
		return usageErr("put the script in --js, not in the markup: the widget's page takes no inline script")
	}
	status, raw, err := c.call("POST", "/api/cli/apps/widgets", nil, body)
	if err != nil {
		return err
	}
	if status != 200 {
		return widgetRefused(status, raw)
	}
	var out struct {
		ID  string `json:"id"`
		Rev int    `json:"rev"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	// The words the host's tools answer with too (apps.WidgetAnswer): the feed draws a widget under the step that showed it.
	if out.Rev > 1 {
		_, _ = fmt.Fprintf(c.env.Stdout, "widget %s changed\n", out.ID)
	} else {
		_, _ = fmt.Fprintf(c.env.Stdout, "widget %s shown\n", out.ID)
	}
	return nil
}

func widgetRefused(status int, raw []byte) error {
	var e struct{ Error, Message string }
	_ = json.Unmarshal(raw, &e)
	switch e.Error {
	case "bad-widget":
		return fail("the widget is past its limits: id up to 40 of a-z 0-9 - _, title 120, data JSON 64 KB, html 128 KB, css 64 KB, js 256 KB")
	case "too-many-widgets":
		return fail("this chat keeps as many widgets as it may: change one with --id")
	case "another-widget":
		return fail("that id is another widget's: give a new --id")
	case "not-on":
		return fail("the app is not on in this chat")
	case "no-view":
		return fail("the app has no such view for the chat")
	case "no-chat":
		return fail("this tab runs no chat")
	case "no-widget":
		return fail("no such widget in this chat")
	}
	if e.Message != "" {
		return fail("%s", e.Message)
	}
	return fail("the widget was refused (%d %s)", status, e.Error)
}
