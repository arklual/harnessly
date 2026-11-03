package design

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
)

// The board engine. Boards of the design tool's format load an engine as ./support.js. A new project gets, in order: the
// file a deployment names (RUNTIME_DESIGN_ENGINE_FILE), the newest release this machine keeps (the vendor's engine,
// fetched with the owner's own account by `<cli> design engine fetch`, enginefetch.go, or a mirror named by
// RUNTIME_DESIGN_RELEASES_DIR), else our own engine (ownengine/support.js), so designs work on any machine without an
// account. Projects keep the engine they were made with.

//go:embed ownengine/support.js
var ownEngine []byte

// Where a new project's engine comes from.
const (
	EngineSourceOwn    = "own"
	EngineSourceVendor = "vendor"
	// EngineSourceFile: the file a deployment names.
	EngineSourceFile = "file"
)

// EngineDirname is the engine's folder in the runtime's data dir.
const EngineDirname = "design-engine"

// DefaultEngineDir is where the engine of a runtime with this data dir is kept ("" without a data dir).
func DefaultEngineDir(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, EngineDirname)
}

// EngineStatus is what the panel needs to tell the person which engine new designs get and what else the machine has.
type EngineStatus struct {
	// Ready: a new project gets an engine. Always true now; older panels read it.
	Ready bool `json:"ready"`
	// Source is where a new project's engine comes from: EngineSourceOwn or EngineSourceVendor.
	Source string `json:"source"`
	// Release is the vendor's release new projects start on, null on our own engine.
	Release *string `json:"release"`
	// Chrome: a headless browser for checks, pictures and deck exports; Ffmpeg: video exports and recordings.
	Chrome bool `json:"chrome"`
	Ffmpeg bool `json:"ffmpeg"`
	// Fetch is the command that fetches the vendor's engine with the owner's account (optional), "" where it cannot work.
	Fetch string `json:"fetch"`
}

// engineBytes is the engine a new project gets: the configured file, else the newest stable release's engine
// (the release new projects start on), else our own.
func (l *Library) engineBytes() []byte {
	if l.cfg.EngineFile != "" {
		if b, err := readFileLimited(l.cfg.EngineFile, 16<<20); err == nil {
			return b
		}
		l.warn("design: the configured board engine cannot be read", nil)
	}
	if file := l.newProjectsEngine(); file != "" {
		if b, err := readFileLimited(file, 16<<20); err == nil {
			return b
		}
		l.warn("design: the release's board engine cannot be read", nil)
	}
	return ownEngine
}

// newProjectsEngine is the verified engine file of the release new projects start on, "" when there is none.
func (l *Library) newProjectsEngine() string {
	list := l.Releases()
	if i, ok := findRelease(list, l.NewProjectsRelease()); ok {
		return list[i].engine
	}
	return ""
}

// EngineFetchCommand is the command that fetches the vendor's engine.
func (l *Library) EngineFetchCommand() string {
	cli := l.cfg.Texts.CLI
	if cli == "" {
		cli = "harnessly"
	}
	return cli + " design engine fetch"
}

// Engine says what this machine has for the design tool.
func (l *Library) Engine() EngineStatus {
	s := EngineStatus{Ready: true, Source: EngineSourceOwn, Chrome: l.findChrome() != "", Ffmpeg: DeckFFmpeg() != "", Fetch: l.EngineFetchCommand()}
	if readCanvas(l.ReleasesDir()) == "" && artifactRestricted() {
		s.Fetch = ""
	}
	if r := l.NewProjectsRelease(); r != "" && l.newProjectsEngine() != "" {
		s.Release, s.Source = &r, EngineSourceVendor
	}
	if l.cfg.EngineFile != "" {
		s.Source, s.Release = EngineSourceFile, nil
	}
	return s
}

// managedSettingsFiles are where Claude Code reads the settings a machine's owner enforces.
var managedSettingsFiles = []string{"/etc/claude-code/managed-settings.json", "/Library/Application Support/ClaudeCode/managed-settings.json"}

// artifactRestricted says a machine's enforced settings hook the Artifact tool. A fetch that has no canvas yet must make
// one with that tool, and such a hook is there to refuse it, so the command is not offered. The hook is not run.
func artifactRestricted() bool {
	for _, file := range managedSettingsFiles {
		raw, err := readFileLimited(file, 1<<20)
		if err != nil {
			continue
		}
		var doc struct {
			Hooks struct {
				PreToolUse []struct {
					Matcher string `json:"matcher"`
				} `json:"PreToolUse"`
			} `json:"hooks"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		for _, h := range doc.Hooks.PreToolUse {
			if re, err := regexp.Compile("^(" + h.Matcher + ")$"); h.Matcher == "*" || h.Matcher == "" || (err == nil && re.MatchString("Artifact")) {
				return true
			}
		}
	}
	return false
}

// lookPathOr is exec.LookPath that answers "" when the name is not found.
func lookPathOr(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// EngineStatus is GET what this machine has for the design tool: the engine, the browser, ffmpeg, and the command that
// fetches the engine.
func (a *API) EngineStatus(r *http.Request) Answer {
	if _, no := a.Guard(r, http.MethodGet); no != nil {
		return *no
	}
	return JSON(r, 200, a.L.Engine(), noStore())
}
