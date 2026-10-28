package design

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The board engine. Boards of the design tool's format load an engine as ./support.js. The engine is not ours and is
// not shipped with the runtime: each machine fetches it with its owner's own account (`<cli> design engine fetch`,
// enginefetch.go) into a folder of the data dir laid out as a releases mirror (releases.go), so a fetched engine is a
// release like any other. A deployment may instead name a mirror (RUNTIME_DESIGN_RELEASES_DIR) or one engine file
// (RUNTIME_DESIGN_ENGINE_FILE). Until one of them is there, the library lists and serves what it has, and a new project
// is refused with the command that fixes it.

// EngineDirname is the engine's folder in the runtime's data dir.
const EngineDirname = "design-engine"

// ErrNoEngine: no engine on this machine, so a new project cannot be made from the shipped template.
var ErrNoEngine = errors.New("no board engine")

// DefaultEngineDir is where the engine of a runtime with this data dir is kept ("" without a data dir).
func DefaultEngineDir(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, EngineDirname)
}

// EngineStatus is what the panel needs to tell the person whether the design tool works here and what is missing.
type EngineStatus struct {
	// Ready: a new project gets an engine (a configured file, or a stable release).
	Ready bool `json:"ready"`
	// Release is the release new projects start on, null without releases.
	Release *string `json:"release"`
	// Chrome: a headless browser for checks, pictures and deck exports; Ffmpeg: video exports and recordings.
	Chrome bool `json:"chrome"`
	Ffmpeg bool `json:"ffmpeg"`
	// Fetch is the command that fetches the engine with the owner's account.
	Fetch string `json:"fetch"`
}

// engineBytes is the engine a new project gets: the configured file, else the newest stable release's engine
// (the release new projects start on); ErrNoEngine when there is neither.
func (l *Library) engineBytes() ([]byte, error) {
	if l.cfg.EngineFile != "" {
		if b, err := readFileLimited(l.cfg.EngineFile, 16<<20); err == nil {
			return b, nil
		}
		l.warn("design: the configured board engine cannot be read", nil)
	}
	if file := l.newProjectsEngine(); file != "" {
		if b, err := readFileLimited(file, 16<<20); err == nil {
			return b, nil
		}
	}
	if testEngine != nil {
		return testEngine, nil
	}
	return nil, ErrNoEngine
}

// testEngine is the engine of the package's tests, which make projects without fetching one; nil in the runtime.
var testEngine []byte

// newProjectsEngine is the verified engine file of the release new projects start on, "" when there is none.
func (l *Library) newProjectsEngine() string {
	list := l.Releases()
	if i, ok := findRelease(list, l.NewProjectsRelease()); ok {
		return list[i].engine
	}
	return ""
}

// EngineFetchCommand is the command the person runs to fetch the engine.
func (l *Library) EngineFetchCommand() string {
	cli := l.cfg.Texts.CLI
	if cli == "" {
		cli = "harnessly"
	}
	return cli + " design engine fetch"
}

// Engine says what this machine has for the design tool.
func (l *Library) Engine() EngineStatus {
	s := EngineStatus{Chrome: l.findChrome() != "", Ffmpeg: DeckFFmpeg() != "", Fetch: l.EngineFetchCommand()}
	if r := l.NewProjectsRelease(); r != "" && l.newProjectsEngine() != "" {
		s.Release, s.Ready = &r, true
	}
	if !s.Ready && l.cfg.EngineFile != "" {
		if st, err := os.Stat(l.cfg.EngineFile); err == nil && st.Mode().IsRegular() {
			s.Ready = true
		}
	}
	return s
}

// noEngineErr is the refusal of a new project on a machine without an engine, worded for the person.
func (l *Library) noEngineErr() error {
	cmd := l.EngineFetchCommand()
	return createErr(locale.Pick("The board engine is not on this machine yet: run `"+cmd+"` in a terminal (it uses your own Claude account) and try again",
		"Движка досок на этой машине пока нет: выполните `"+cmd+"` в терминале (он берёт движок с вашего аккаунта Claude) и повторите"))
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
