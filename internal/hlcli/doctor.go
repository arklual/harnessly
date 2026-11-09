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
harnessly doctor preview [--json]

Checks that this machine can draw designs: renders a real picture (PNG) and a 2-second MP4 with the built-in engine's
browser pipeline, then reads the video back with ffprobe (codec, length, size). Prints each step with its result; a
failed step has a code (RND-xxx) and a hint, and the command exits 1. Nothing is sent anywhere and nothing is kept.

The same check decides the design engine's "ready" in the API when RUNTIME_DESIGN_READY_PROBE=1 (docs/render-kit.md).
Uses RUNTIME_DESIGN_CHROME, RUNTIME_DESIGN_CHROME_ARGS, RUNTIME_DESIGN_FFMPEG and RUNTIME_DESIGN_FFPROBE like the runtime.

doctor share checks what guest links of designs rely on, on the design library's own filesystem: a lookup stays beneath
its folder («..», relative, absolute and directory symlinks out of it are refused) and an open file keeps reading the
same file after its path is swapped. A picture and a video that render say nothing about this. While a check fails the
runtime serves no guest and makes no link (codes SHR-xxx, docs/design-sharing.md); the command exits 1.

doctor preview asks the running runtime for its preview slots' public origins (RUNTIME_PREVIEW_PUBLIC_ORIGINS, or the
ones it names under RUNTIME_PREVIEW_DOMAIN) and resolves each name: a name that does not resolve (NXDOMAIN) leaves the
panel without a view over https although the slots listen here, and the command exits 1. A name of another address is
said, not failed: a reverse proxy or a balancer may stand there (docs/deploy.md, "Preview domains").
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
	if args[0] == "preview" {
		return c.doctorPreview(args[1:])
	}
	if args[0] != "render" {
		return usageErr("unknown doctor check %q (there are: render, share, preview)", args[0])
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

// previewReport is the runtime's GET /api/cli/preview/check.
type previewReport struct {
	Mode    string `json:"mode"`
	Source  string `json:"source"`
	Domain  string `json:"domain"`
	Origins []struct {
		Origin    string   `json:"origin"`
		Host      string   `json:"host"`
		State     string   `json:"state"`
		Addresses []string `json:"addresses"`
		Error     string   `json:"error"`
	} `json:"origins"`
	Hint string `json:"hint"`
}

// harnessly doctor preview: do the preview slots' names lead anywhere.
func (c *cli) doctorPreview(args []string) error {
	for _, a := range args {
		if a != "--json" {
			return usageErr("doctor preview takes only --json, got %q", a)
		}
	}
	raw, err := c.request("GET", "/api/cli/preview/check", nil, nil)
	if err != nil {
		return err
	}
	var rep previewReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return fail("the runtime answered something else: %v", err)
	}
	failed := false
	for _, o := range rep.Origins {
		if o.State == "not-found" || o.State == "error" {
			failed = true
		}
	}
	if c.json {
		c.printRaw(raw)
	} else {
		switch {
		case rep.Mode == "off":
			c.out("preview: off on this runtime (RUNTIME_PREVIEW_LISTEN=off, or it runs behind another runtime)\n")
		case len(rep.Origins) == 0:
			c.out("preview: no public origins (mode %s): the slots go by port or by path, there is no name to resolve.\n"+
				"       For https slots set RUNTIME_PREVIEW_DOMAIN or RUNTIME_PREVIEW_PUBLIC_ORIGINS (docs/deploy.md)\n", rep.Mode)
		default:
			from := "RUNTIME_PREVIEW_PUBLIC_ORIGINS"
			if rep.Source == "domain" {
				from = "RUNTIME_PREVIEW_DOMAIN=" + rep.Domain
			}
			c.out("preview origins from %s:\n", from)
			for _, o := range rep.Origins {
				mark := map[string]string{"ok": "ok  ", "literal": "ok  ", "elsewhere": "warn"}[o.State]
				if mark == "" {
					mark = "FAIL"
				}
				c.out("[%s] %s", mark, o.Origin)
				switch {
				case o.Error != "":
					c.out("  %s", o.Error)
				case len(o.Addresses) > 0:
					c.out("  -> %s", strings.Join(o.Addresses, ", "))
				}
				if o.State == "elsewhere" {
					c.out("  (not this machine)")
				}
				c.out("\n")
			}
			if rep.Hint != "" {
				c.out("       -> %s\n", rep.Hint)
			}
		}
	}
	if failed {
		return exitCode(1)
	}
	return nil
}
