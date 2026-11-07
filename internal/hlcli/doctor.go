package hlcli

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
)

const doctorHelp = `harnessly doctor render [--json]
harnessly doctor share [--library DIR] [--json]

Checks that this machine can draw designs: renders a real picture (PNG) and a 2-second MP4 with the built-in engine's
browser pipeline, then reads the video back with ffprobe (codec, length, size). Prints each step with its result; a
failed step has a code (RND-xxx) and a hint, and the command exits 1. Nothing is sent anywhere and nothing is kept.

The same check decides the design engine's "ready" in the API when RUNTIME_DESIGN_READY_PROBE=1 (docs/render-kit.md).
Uses RUNTIME_DESIGN_CHROME, RUNTIME_DESIGN_CHROME_ARGS, RUNTIME_DESIGN_FFMPEG and RUNTIME_DESIGN_FFPROBE like the runtime.

doctor share checks what guest links of designs rely on, on the design library's own filesystem: a lookup stays beneath
its folder («..», relative, absolute and directory symlinks out of it are refused) and an open file keeps reading the
same file after its path is swapped. A picture and a video that render say nothing about this. While a check fails the
runtime serves no guest and makes no link (codes SHR-xxx, docs/design-sharing.md); the command exits 1.
`

// harnessly doctor render: the render kit's real check.
func (c *cli) doctor(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		c.out("%s", doctorHelp)
		return nil
	}
	if args[0] == "share" {
		return c.doctorShare(args[1:])
	}
	if args[0] != "render" {
		return usageErr("unknown doctor check %q (there are: render, share)", args[0])
	}
	for _, a := range args[1:] {
		if a != "--json" {
			return usageErr("doctor render takes only --json, got %q", a)
		}
	}
	l := design.New(design.Config{Chrome: c.env.Getenv("RUNTIME_DESIGN_CHROME"), Texts: design.Texts{CLI: "harnessly"}})
	defer l.StopWatching()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	rep := l.RenderDoctor(ctx)
	if c.json {
		raw, _ := json.Marshal(rep)
		c.out("%s\n", raw)
	} else {
		for _, s := range rep.Steps {
			mark := "ok  "
			switch {
			case s.Skipped:
				mark = "skip"
			case !s.OK:
				mark = "FAIL"
			case s.Warn:
				mark = "warn"
			}
			c.out("[%s] %s", mark, s.Name)
			if s.Code != "" {
				c.out("  (%s)", s.Code)
			}
			if !s.Skipped {
				c.out("  %d ms", s.Ms)
			}
			c.out("\n")
			if s.Detail != "" {
				c.out("       %s\n", s.Detail)
			}
			if s.Hint != "" && (!s.OK || s.Warn) {
				c.out("       -> %s\n", s.Hint)
			}
		}
		if rep.OK {
			c.out("render: ready (%s)\n", (time.Duration(rep.Ms) * time.Millisecond).Round(10*time.Millisecond))
		} else {
			c.out("render: NOT ready (%s)\n", strings.TrimSpace(rep.FailedStep().Code))
		}
	}
	if !rep.OK {
		return exitCode(1)
	}
	return nil
}

// harnessly doctor share: the share filesystem probe the runtime runs before it serves a guest.
func (c *cli) doctorShare(args []string) error {
	var library string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
		case a == "--library" && i+1 < len(args):
			i++
			library = args[i]
		case strings.HasPrefix(a, "--library="):
			library = strings.TrimPrefix(a, "--library=")
		default:
			return usageErr("doctor share takes --library DIR and --json, got %q", a)
		}
	}
	dir, err := c.libraryDir(library)
	if err != nil {
		return err
	}
	rep := design.ProbeShareFS(dir)
	if c.json {
		raw, _ := json.Marshal(rep)
		c.out("%s\n", raw)
	} else {
		for _, s := range rep.Steps {
			mark := "ok  "
			if !s.OK {
				mark = "FAIL"
			}
			c.out("[%s] %s", mark, s.ID)
			if s.Code != "" {
				c.out("  (%s)", s.Code)
			}
			c.out("\n")
			if s.Detail != "" {
				c.out("       %s\n", s.Detail)
			}
		}
		if rep.OK {
			c.out("share: guest links can be served (%s)\n", dir)
		} else {
			c.out("share: guest links are OFF on this runtime (%s)\n       -> %s\n", rep.Failed().Code, rep.Hint)
		}
	}
	if !rep.OK {
		return exitCode(1)
	}
	return nil
}
