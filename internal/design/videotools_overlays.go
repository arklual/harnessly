package design

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

// The agent's tools over a scene's overlay layer. They are the CLI's twins: the same store, the same rule that
// a person's edit wins unless the agent says force.

func videoEnum(desc string, values ...string) *jsvalue.Object {
	list := make([]any, len(values))
	for i, v := range values {
		list[i] = v
	}
	return jsvalue.NewObject("type", "string", "description", desc, "enum", list)
}

func enumStrings[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

// videoOverlayProps is the input schema of add (kind is chosen) and set (id names the overlay).
func videoOverlayProps(scene *jsvalue.Object, add bool) *jsvalue.Object {
	p := jsvalue.NewObject("scene", scene)
	if add {
		p.Set("kind", videoEnum("What it is.", enumStrings(VideoOverlayKinds)...))
		p.Set("id", videoStr("A name for it (lower case, dashes); omit to get kind-1, kind-2..."))
	} else {
		p.Set("id", videoStr("The overlay's id (from sinopia_overlay_list)."))
		p.Set("kind", videoEnum("Change the kind.", enumStrings(VideoOverlayKinds)...))
	}
	p.Set("text", videoStr("The words (up to 200 characters; a key cap: \"Ctrl + K\"; a counter: label, \"{n} ₽\" puts the number in place)."))
	p.Set("at", videoNum("Seconds from the scene's start when it comes in."))
	p.Set("out", videoNum("Seconds from the scene's start when it goes; omit or -1 for the scene's end."))
	p.Set("x", videoNum("Left edge, fraction of the frame width."))
	p.Set("y", videoNum("Top edge, fraction of the frame height."))
	p.Set("w", videoNum("Width, fraction of the frame width."))
	p.Set("h", videoNum("Height, fraction of the frame height; sets the size of the text."))
	p.Set("tx", videoNum("Callout: where the arrow ends, fraction of the width."))
	p.Set("ty", videoNum("Callout: where the arrow ends, fraction of the height."))
	p.Set("anchor", videoEnum("Text alignment in the box.", enumStrings(VideoOverlayAnchors)...))
	p.Set("enter", videoEnum("How it comes in.", enumStrings(VideoOverlayEnters)...))
	p.Set("leave", videoEnum("How it goes out.", enumStrings(VideoOverlayLeaves)...))
	p.Set("theme", videoEnum("Look, from the deck's colors.", enumStrings(VideoOverlayThemes)...))
	p.Set("value", videoNum("Counter: the number it counts to."))
	p.Set("from", videoNum("Counter: the number it starts at (0)."))
	p.Set("decimals", videoNum("Counter: digits after the point, 0..3."))
	p.Set("portrait", jsvalue.NewObject("type", "object", "description", "A place of its own in 9:16: {x, y, w, h, tx?, ty?}. Without it the 16:9 place is moved into the safe zone."))
	if !add {
		p.Set("force", videoBool("Change it even if a person edited it. Only when the person asked for this change."))
	}
	return p
}

// overlayPatchOf turns a tool's arguments into a patch of the store: times in seconds become ms.
func overlayPatchOf(args *jsvalue.Object) (map[string]json.RawMessage, string) {
	var all map[string]json.RawMessage
	if args == nil {
		return map[string]json.RawMessage{}, ""
	}
	if err := json.Unmarshal(jsvalue.Stringify(args, ""), &all); err != nil {
		return nil, "the arguments are not valid"
	}
	patch := map[string]json.RawMessage{}
	for _, k := range videoOverlayFieldNames {
		raw, ok := all[k]
		if !ok {
			continue
		}
		if k == "at" || k == "out" {
			var s *float64
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, k + " is seconds"
			}
			if s == nil || *s < 0 {
				raw = json.RawMessage("null")
				if k == "at" {
					return nil, "at is seconds, 0 or more"
				}
			} else {
				ms, _ := json.Marshal(int64(math.Round(*s * 1000)))
				raw = ms
			}
		}
		patch[k] = raw
	}
	return patch, ""
}

func (t VideoTools) overlay(name string, args *jsvalue.Object) *jsvalue.Object {
	scene := argStr(args, "scene")
	if scene == "" {
		return mcpserver.TextResult("scene is expected", true)
	}
	if name == "sinopia_overlay_list" {
		list, err := t.L.VideoOverlaysOf(t.Project, scene)
		if err != nil {
			return mcpserver.TextResult(err.Error(), true)
		}
		if list == nil {
			list = []VideoOverlay{}
		}
		return videoJSONResult(list)
	}
	patch, msg := overlayPatchOf(args)
	if msg != "" {
		return mcpserver.TextResult(msg, true)
	}
	action := map[string]string{"sinopia_overlay_add": "add", "sinopia_overlay_set": "set"}[name]
	if action == "" {
		action = "rm"
	}
	out, err := t.L.videoOverlayDo(t.Project, scene, action, argStr(args, "id"), patch, argBool(args, "force"), VideoAuthorAgent)
	if err != nil {
		return mcpserver.TextResult(err.Error(), true)
	}
	if out.Overlay == nil {
		return mcpserver.TextResult("Removed. "+overlayCount(out.List), false)
	}
	if len(out.Overlaps) == 0 {
		return videoJSONResult(out.Overlay)
	}
	return videoJSONResult(struct {
		*VideoOverlay
		Warning  string   `json:"warning"`
		Overlaps []string `json:"overlaps"`
	}{out.Overlay, "This overlay stands on " + strings.Join(out.Overlaps, ", ") + " at the same time: move one or change its times (sinopia_overlay_set).", out.Overlaps})
}

func overlayCount(list []VideoOverlay) string {
	return fmt.Sprintf("The scene has %d overlays.", len(list))
}
