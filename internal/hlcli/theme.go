package hlcli

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// The person's own look of harnessly (docs/looks.md): a theme on everything, on the folder of this chat, or on
// this chat alone. The look is data (internal/looks): colours by hue, a background (a gradient, a picture, an animated
// scene), grain, dim, roundness, a font and a few safe variants of the chat's parts. The person's page puts it on and
// shows «Applied · Undo»; the command waits for that.

const themeHelp = `harnessly theme get|presets|preset|set|reset

The person's own look of harnessly: colours, the background of the chats (a gradient, a picture, an animated scene
such as matrix, stars, aurora, rain, synthwave), grain, dim, roundness, font and the look of the chat's parts. Use it
when the person asks for a theme, a background, colours or a style ("make it look like night Tokyo", "put the matrix
on this folder"). The person's page applies the change at once and shows "Applied · Undo".

  harnessly theme get                         the looks now (everything, the folders, the chats) and the presets
  harnessly theme presets                     the ready looks: id, name
  harnessly theme preset ID [--scope S] --why "…"
  harnessly theme set '<look JSON>' [--scope S] --why "…"    (or --file look.json)
  harnessly theme reset [--scope S] --why "…"                takes the scope's own look away (it inherits again)

--scope: all (the default: every chat), folder (the folder of this chat), chat (this chat only). A chat wears its own
look, else its folder's, else the one for all.

A look (every field checked; no CSS, no code):
  {"v":1, "name":"Night Tokyo", "mode":"auto|light|dark",
   "hues":[{"h":0-360,"s":0-100}, …1 to 3],   the theme's colours; the first one is the accent unless "accent" is given
   "intensity":0-100,                          how much the hues tint the surfaces
   "accent":{"h":…,"s":…},                     optional
   "bg":{"kind":"none|color|gradient|image|scene", "scene":"matrix|stars|aurora|rain|synthwave|twinkle|paper|lava|snow|topo",
         "image":"<sha256 of a picture uploaded by the page>", "fit":"cover|tile", "speed":0-100},
   "grain":0-100, "dim":0-100, "blur":0-24,     noise over the background; darkening and blur under the text
   "radius":"sharp|default|round", "density":"compact|cozy", "font":"system|inter|serif|mono|rounded",
   "parts":{"bubble":"soft|solid|outline|glass", "turns":"plain|card|glass", "composer":"solid|glass|outline",
            "sidebar":"plain|tinted|glass"}}
The page keeps the text readable: a colour that would make text fail contrast is moved until it passes.
`

func (c *cli) theme(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		c.out("%s", themeHelp)
		return nil
	}
	switch args[0] {
	case "get", "presets":
		status, raw, err := c.call(http.MethodGet, "/api/cli/looks", nil, nil)
		if err != nil {
			return err
		}
		if status < 200 || status > 299 {
			return apiError(status, raw)
		}
		var out struct {
			Looks   json.RawMessage `json:"looks"`
			Presets []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"presets"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		if args[0] == "get" {
			if string(out.Looks) == "null" || len(out.Looks) == 0 {
				c.out("looks: not known yet (the person's page has not reported them; the default look is on)\n")
			} else {
				c.out("looks: %s\n", out.Looks)
			}
		}
		c.out("presets:\n")
		for _, p := range out.Presets {
			c.out("  %-10s %s\n", p.ID, p.Name)
		}
		return nil
	case "preset", "set", "reset":
		return c.themeSet(args[0], args[1:])
	}
	return usageErr("theme: unknown command %s (harnessly theme --help)", args[0])
}

func (c *cli) themeSet(verb string, args []string) error {
	body := map[string]any{"scope": "all"}
	var why, value string
	wait := 20 * time.Second
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", usageErr("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch {
		case a == "--scope":
			var v string
			if v, err = next(); err == nil {
				body["scope"] = v
			}
		case strings.HasPrefix(a, "--scope="):
			body["scope"] = strings.TrimPrefix(a, "--scope=")
		case a == "--why":
			why, err = next()
		case strings.HasPrefix(a, "--why="):
			why = strings.TrimPrefix(a, "--why=")
		case a == "--file":
			var p string
			if p, err = next(); err == nil {
				b, rerr := os.ReadFile(p)
				if rerr != nil {
					return fail("cannot read %s: %v", p, rerr)
				}
				value = string(b)
			}
		case a == "--wait":
			var v string
			if v, err = next(); err == nil {
				n, aerr := strconv.Atoi(v)
				if aerr != nil || n < 0 || n > 25 {
					return usageErr("--wait takes seconds, 0 to 25")
				}
				wait = time.Duration(n) * time.Second
			}
		case strings.HasPrefix(a, "--"):
			return usageErr("theme %s: unknown option %s", verb, a)
		case value == "":
			value = a
		default:
			return usageErr("theme %s takes one value (harnessly theme --help)", verb)
		}
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(why) == "" {
		return usageErr("theme %s needs --why \"what the person asked for\"", verb)
	}
	switch verb {
	case "preset":
		if value == "" {
			return usageErr("theme preset needs an id: harnessly theme presets lists them")
		}
		body["preset"] = value
	case "set":
		if !json.Valid([]byte(value)) {
			return usageErr("theme set needs a look as JSON (harnessly theme --help shows its fields)")
		}
		body["look"] = json.RawMessage(value)
	case "reset":
		if value != "" {
			return usageErr("theme reset takes no value, only --scope")
		}
		body["reset"] = true
	}
	body["why"], body["wait"], body["session"] = why, wait.Seconds(), c.session()
	status, raw, err := c.call(http.MethodPost, "/api/cli/looks/set", nil, body)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		var r struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &r) == nil && r.Message != "" {
			return fail("refused: %s", r.Message)
		}
		return apiError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var r struct {
		Status  string `json:"status"`
		Request struct {
			Scope string `json:"scope"`
			Note  string `json:"note"`
		} `json:"request"`
	}
	_ = json.Unmarshal(raw, &r)
	switch r.Status {
	case "applied":
		c.out("applied on %s: the person sees it now, with Undo\n", r.Request.Scope)
		return nil
	case "failed":
		return fail("failed: the page did not apply it — %s", r.Request.Note)
	}
	c.out("waiting: no page of the person has it on yet; it applies when they open harnessly (the request keeps 10 minutes)\n")
	return nil
}
