package hlcli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
)

// harnessly design engine fetch|status|hook: the board engine of the design tool, which the runtime does not ship.
// fetch reads it with the person's own Claude Code and account into the engine folder of this machine; status says
// what the machine has; hook is the fetch session's PreToolUse hook (not for people).

const engineHelp = `harnessly design engine fetch [--canvas URL] [--claude PATH] [--model MODEL] [--dir DIR]
harnessly design engine status [--dir DIR]

The board engine and the format's rules belong to the design tool's vendor and are not shipped with harnessly. fetch
gets the current release with your own Claude Code, logged in to your own claude.ai account: a short session on a small
model whose only tool is Artifact, allowed to read (and, the first time, to make one private empty canvas of the
design type on your account, which it remembers). The files are checked against their checksums and kept as a release
in the engine folder; new projects start on the newest one, existing ones stay on theirs until you upgrade them.

  --canvas URL   read from this canvas of the design type instead of the remembered one (or a new one)
  --claude PATH  the Claude Code binary (default: claude on PATH)
  --model MODEL  the session's model (default: haiku)
  --dir DIR      the engine folder (default: $RUNTIME_DESIGN_ENGINE_DIR, else design-engine in the runtime's data folder)
`

func (c *cli) designEngine(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		c.out("%s", engineHelp)
		return nil
	}
	sub, args := args[0], args[1:]
	if sub == "hook" {
		return c.engineHook()
	}
	opt := design.FetchOptions{}
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if !strings.HasPrefix(args[i], "-") {
			return usageErr("design engine %s takes no arguments, got %q", sub, args[i])
		}
		if !hasVal {
			if i+1 >= len(args) {
				return usageErr("%s needs a value", args[i])
			}
			i++
			val = args[i]
		}
		switch name {
		case "canvas":
			opt.Canvas = val
		case "claude":
			opt.Claude = val
		case "model":
			opt.Model = val
		case "dir":
			opt.Dir = val
		default:
			return usageErr("unknown option %s", args[i])
		}
	}
	if opt.Dir == "" {
		opt.Dir = c.engineDir()
	}
	switch sub {
	case "fetch":
		return c.engineFetch(opt)
	case "status":
		return c.engineStatus(opt.Dir)
	}
	return usageErr("unknown design engine command %q", sub)
}

// engineDir is the engine folder the runtime reads by default.
func (c *cli) engineDir() string {
	if v := strings.TrimSpace(c.env.Getenv("RUNTIME_DESIGN_ENGINE_DIR")); v != "" {
		return v
	}
	return design.DefaultEngineDir(c.dataDir())
}

func (c *cli) engineFetch(opt design.FetchOptions) error {
	self, err := os.Executable()
	if err != nil {
		return fail("cannot find this program: %v", err)
	}
	opt.Hook = shellQuote(self) + " design engine hook"
	opt.Log = func(s string) { _, _ = io.WriteString(c.env.Stderr, s+"\n") }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res, err := design.FetchEngine(ctx, opt)
	if err != nil {
		return fail("%v", err)
	}
	if c.json {
		raw, _ := json.Marshal(res)
		c.out("%s\n", raw)
		return nil
	}
	if res.New {
		c.out("Fetched release %s of the board engine into %s\nNew projects start on it; reload the design page.\n", res.Release, opt.Dir)
	} else {
		c.out("Release %s is already here (%s), nothing to fetch\n", res.Release, opt.Dir)
	}
	return nil
}

func (c *cli) engineStatus(dir string) error {
	l := design.New(design.Config{ReleasesDir: dir, EngineFile: c.env.Getenv("RUNTIME_DESIGN_ENGINE_FILE"),
		Chrome: c.env.Getenv("RUNTIME_DESIGN_CHROME"), Texts: design.Texts{CLI: "harnessly"}})
	defer l.StopWatching()
	s := l.Engine()
	if c.json {
		raw, _ := json.Marshal(s)
		c.out("%s\n", raw)
		return nil
	}
	yes := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	release := "none"
	if s.Release != nil {
		release = *s.Release
	}
	c.out("engine   %s (release %s, %s)\nchrome   %s (checks, pictures, PDF and video exports)\nffmpeg   %s (video exports, recordings)\n",
		yes(s.Ready), release, dir, yes(s.Chrome), yes(s.Ffmpeg))
	if !s.Ready {
		c.out("\nRun `%s` to fetch the engine with your Claude account.\n", s.Fetch)
	}
	return nil
}

func (c *cli) engineHook() error {
	in := c.env.Stdin
	if in == nil {
		in = os.Stdin
	}
	raw, _ := io.ReadAll(io.LimitReader(in, 4<<20))
	_, _ = c.env.Stdout.Write(design.EngineHook(raw, c.env.Getenv(design.EngineHookCreateEnv) == "1"))
	return nil
}

// shellQuote quotes a path for the hook's command line.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
