package hlcli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/design"
)

// `design video record` (HRN-627): plays a script of actions in a headless browser, frame by frame on the page's own clock,
// and keeps the clip and the log in the project's records/ folder. It works on the library's folder itself, so it needs
// the browser, ffmpeg and the project on this machine, not the runtime.

const videoRecordHelp = `harnessly design video record <project> <script.yaml|script.json|-> [--name NAME] [--crf 8] [--preset fast] [--library DIR]

Plays a script of actions in a headless browser and records the interface: a clip at twice the pixels of the final video
(3840x2160 for 1080p) and a log of the cursor and every action, in <project>/records/NAME.mp4 and NAME.json. A scene lays a
cursor, a window and a camera over it afterwards (tk.screen, harnessly design video guide record): the time and the camera are
edited in the scene; the interface is shot again only when the actions change.

A script (YAML or JSON): name, url, viewport {width, height}, scale (2), fps (30), theme (light|dark), steps:
  open url · click on · hover on · type into + text|secret · key Enter · scroll by|to + ms · wait ms · wait-for on + timeout
  on/into/to is a CSS selector or text=Words. A password is typed with secret: ENV_NAME, never as text; run the command as
  harnessly secret run --env ENV_NAME=<name in the safe> -- harnessly design video record ...
Needs chrome-headless-shell (RUNTIME_DESIGN_HEADLESS_SHELL, or ~/.cache/chrome-headless-shell) and ffmpeg.
`

func (c *cli) videoRecord(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		c.out("%s", videoRecordHelp)
		return nil
	}
	f, err := parseFlags(args, map[string]string{"name": "name", "crf": "crf", "preset": "preset", "library": "library"})
	if err != nil {
		return err
	}
	if len(f.pos) != 2 {
		return usageErr("design video record <project> <script.yaml|-> [--name NAME] [--crf 8] [--library DIR]")
	}
	crf := 8
	if v := f.values["crf"]; v != "" {
		if crf, err = strconv.Atoi(v); err != nil || crf < 0 || crf > 30 {
			return usageErr("--crf is a number from 0 (lossless) to 30")
		}
	}
	dir, err := c.libraryDir(f.values["library"])
	if err != nil {
		return err
	}
	l := design.New(design.Config{LibraryDir: dir, Texts: design.Texts{CLI: design.GuardCLI()}})
	project := l.ProjectDirOf(f.pos[0])
	if st, err := os.Stat(project); project == "" || err != nil || !st.IsDir() {
		return fail("no project %q in %s", f.pos[0], dir)
	}
	var raw []byte
	if f.pos[1] == "-" {
		in := c.env.Stdin
		if in == nil {
			in = os.Stdin
		}
		raw, err = io.ReadAll(io.LimitReader(in, 1<<20))
	} else {
		script := f.pos[1]
		if !filepath.IsAbs(script) && c.env.Cwd != "" {
			script = filepath.Join(c.env.Cwd, script)
		}
		raw, err = os.ReadFile(script)
	}
	if err != nil {
		return err
	}
	sc, err := design.ParseRecordScenario(raw)
	if err != nil {
		return fail("%v", err)
	}
	name := f.values["name"]
	if name == "" && sc.Name == "" && f.pos[1] != "-" {
		name = strings.TrimSuffix(filepath.Base(f.pos[1]), filepath.Ext(f.pos[1]))
	}
	res, err := design.Record(context.Background(), sc, design.RecordOptions{Dir: project, Name: name, CRF: crf, Preset: f.values["preset"],
		Getenv: c.env.Getenv, Log: func(s string) {
			if c.env.Stderr != nil {
				_, _ = io.WriteString(c.env.Stderr, s+"\n")
			}
		}})
	if err != nil {
		return fail("%v", err)
	}
	rel := func(p string) string { r, _ := filepath.Rel(project, p); return filepath.ToSlash(r) }
	if c.json {
		b, _ := json.MarshalIndent(map[string]any{"video": rel(res.Video), "log": rel(res.LogFile), "frames": res.Frames, "dropped": res.Dropped,
			"durMs": res.DurMs, "wallMs": res.Wall.Milliseconds()}, "", "  ")
		c.out("%s\n", b)
		return nil
	}
	c.out("recorded %d frames (%.1f s), %d without a picture, in %s\n", res.Frames, float64(res.DurMs)/1000, res.Dropped, res.Wall.Round(1e9))
	c.out("clip: %s\nlog:  %s\n", rel(res.Video), rel(res.LogFile))
	c.out("in a scene: tk.screen(el, {src: %q, log: %q})   (harnessly design video guide record)\n", rel(res.Video), rel(res.LogFile))
	return nil
}
