package hlcli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/design"
)

// `design video overlay ls|add|set|rm`: a scene's layer of titles, callouts, highlights, key caps, counters and
// badges, drawn over the board as a function of time. Positions are fractions of the frame (0..1 from the top left),
// times are typed as 1.5s, 1500ms or 0:06 and counted from the scene's start. An overlay a person edited is not changed
// unless --force is given.

const designVideoOverlayPath = "/api/cli/design/video/overlay"

// overlayFlags are the options add and set take, by name.
var overlayFlags = map[string]string{
	"kind": "kind", "text": "text", "at": "at", "out": "out", "x": "x", "y": "y", "w": "w", "h": "h", "tx": "tx", "ty": "ty",
	"anchor": "anchor", "enter": "enter", "leave": "leave", "theme": "theme", "value": "value", "from": "from",
	"decimals": "decimals", "portrait": "portrait", "id": "id",
}

func (c *cli) videoOverlay(args []string) error {
	if len(args) == 0 {
		return usageErr("design video overlay needs a subcommand: ls, add, set, rm")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ls", "list":
		f, err := parseFlags(rest, nil)
		if err != nil {
			return err
		}
		if len(f.pos) != 2 {
			return usageErr("design video overlay ls <project> <scene>")
		}
		return c.overlayCall(map[string]any{"project": f.pos[0], "page": f.pos[1], "action": "ls"}, "")
	case "add", "new":
		rest, _ = switches(rest, "force")
		f, err := parseFlags(rest, overlayFlags)
		if err != nil {
			return err
		}
		if len(f.pos) != 2 || f.values["kind"] == "" {
			return usageErr("design video overlay add <project> <scene> --kind %s --text TEXT [--at 1.5s] [--out 4s] [--x 0.1 --y 0.4 --w 0.8 --h 0.16] [--enter %s] ...", overlayKindList(), overlayEnterList())
		}
		body, err := overlayBody(f)
		if err != nil {
			return err
		}
		body["project"], body["page"], body["action"] = f.pos[0], f.pos[1], "add"
		return c.overlayCall(body, "Overlay added")
	case "set", "edit":
		rest, on := switches(rest, "force")
		f, err := parseFlags(rest, overlayFlags)
		if err != nil {
			return err
		}
		if len(f.pos) != 3 {
			return usageErr("design video overlay set <project> <scene> <id> [--text T] [--at 1.5s] [--out 4s|end] [--x ...] [--force]")
		}
		body, err := overlayBody(f)
		if err != nil {
			return err
		}
		if len(body) == 0 {
			return usageErr("design video overlay set needs something to change")
		}
		body["project"], body["page"], body["id"], body["action"], body["force"] = f.pos[0], f.pos[1], f.pos[2], "set", on["force"]
		return c.overlayCall(body, "Overlay changed")
	case "rm", "remove", "delete":
		rest, on := switches(rest, "force")
		f, err := parseFlags(rest, nil)
		if err != nil {
			return err
		}
		if len(f.pos) != 3 {
			return usageErr("design video overlay rm <project> <scene> <id> [--force]")
		}
		return c.overlayCall(map[string]any{"project": f.pos[0], "page": f.pos[1], "id": f.pos[2], "action": "rm", "force": on["force"]}, "Overlay removed")
	}
	return usageErr("unknown design video overlay command %q", sub)
}

func overlayKindList() string {
	out := make([]string, len(design.VideoOverlayKinds))
	for i, k := range design.VideoOverlayKinds {
		out[i] = string(k)
	}
	return strings.Join(out, "|")
}

func overlayEnterList() string {
	out := make([]string, len(design.VideoOverlayEnters))
	for i, k := range design.VideoOverlayEnters {
		out[i] = string(k)
	}
	return strings.Join(out, "|")
}

// overlayBody is the options as the route's body: numbers as numbers, times in ms.
func overlayBody(f *flags) (map[string]any, error) {
	body := map[string]any{}
	for _, k := range []string{"kind", "text", "anchor", "enter", "leave", "theme", "id"} {
		if v, ok := f.values[k]; ok {
			body[k] = v
		}
	}
	if v, ok := f.values["at"]; ok {
		ms, err := parseVideoTime(v)
		if err != nil {
			return nil, usageErr("--at: %v", err)
		}
		body["at"] = ms
	}
	if v, ok := f.values["out"]; ok {
		if v == "end" || v == "auto" {
			body["out"] = nil
		} else {
			ms, err := parseVideoTime(v)
			if err != nil {
				return nil, usageErr("--out: %v (or end)", err)
			}
			body["out"] = ms
		}
	}
	for _, k := range []string{"x", "y", "w", "h", "tx", "ty", "value", "from"} {
		if v, ok := f.values[k]; ok {
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return nil, usageErr("--%s: not a number: %q", k, v)
			}
			body[k] = n
		}
	}
	if v, ok := f.values["decimals"]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, usageErr("--decimals: not a whole number: %q", v)
		}
		body["decimals"] = n
	}
	if v, ok := f.values["portrait"]; ok {
		if v == "none" || v == "auto" {
			body["portrait"] = nil
		} else {
			place, err := parsePortrait(v)
			if err != nil {
				return nil, err
			}
			body["portrait"] = place
		}
	}
	return body, nil
}

// parsePortrait is --portrait x,y,w,h[,tx,ty]: the overlay's own place in 9:16.
func parsePortrait(s string) (map[string]any, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 && len(parts) != 6 {
		return nil, usageErr("--portrait: x,y,w,h or x,y,w,h,tx,ty (or none)")
	}
	names := []string{"x", "y", "w", "h", "tx", "ty"}
	out := map[string]any{}
	for i, p := range parts {
		n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, usageErr("--portrait: not a number: %q", p)
		}
		out[names[i]] = n
	}
	return out, nil
}

func (c *cli) overlayCall(body map[string]any, verb string) error {
	raw, err := c.request(http.MethodPost, designVideoOverlayPath, nil, body)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Overlay  *design.VideoOverlay  `json:"overlay"`
		Overlays []design.VideoOverlay `json:"overlays"`
		Overlaps []string              `json:"overlaps"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if verb != "" {
		if out.Overlay != nil {
			c.out("%s: %s\n", verb, out.Overlay.ID)
		} else {
			c.out("%s\n", verb)
		}
	}
	if len(out.Overlaps) > 0 && out.Overlay != nil {
		c.out("Warning: %s stands on %s at the same time. Move one or change its times.\n", out.Overlay.ID, strings.Join(out.Overlaps, ", "))
	}
	c.out(videoDataNote)
	if len(out.Overlays) == 0 {
		c.out("No overlays in the scene.\n")
		return nil
	}
	for _, o := range out.Overlays {
		c.out("%s\n", overlayLine(o))
	}
	return nil
}

// overlayLine is one overlay for a person or an agent: id, kind, times, place, style, the words and who wrote it last.
func overlayLine(o design.VideoOverlay) string {
	end := "end"
	if o.Out != nil {
		end = msText(*o.Out)
	}
	who := string(o.Author)
	if o.Author == design.VideoAuthorHuman {
		who = "human (a person edited it: --force to change)"
	}
	extra := ""
	if o.TX != nil && o.TY != nil {
		extra = fmt.Sprintf(" -> %.2f,%.2f", *o.TX, *o.TY)
	}
	if o.Value != nil {
		extra += fmt.Sprintf(" value %g from %g", *o.Value, o.From)
	}
	if o.Portrait != nil {
		extra += fmt.Sprintf(" portrait %.2f,%.2f,%.2f,%.2f", o.Portrait.X, o.Portrait.Y, o.Portrait.W, o.Portrait.H)
	}
	return fmt.Sprintf("%s\t%s\t%s-%s\tat %.2f,%.2f size %.2f x %.2f%s\t%s/%s/%s %s\t%q\t%s",
		o.ID, o.Kind, msText(o.At), end, o.X, o.Y, o.W, o.H, extra, o.Enter, o.Leave, o.Theme, o.Anchor, o.Text, who)
}
