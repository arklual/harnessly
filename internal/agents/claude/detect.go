package claude

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
	"harnessly/internal/proc"
	"harnessly/internal/sessions"
)

// ErrBadRecord: a pid file of a live Claude names no working folder, so its transcript cannot be found. The previous
// runtime failed the whole request on it.
var ErrBadRecord = errors.New("agent session record without a working folder")

// Detector looks for Claude Code under a pane's shell.
type Detector struct {
	// Dir is Claude Code's folder (~/.claude): sessions/<pid>.json while it runs, projects/<folder>/<id>.jsonl.
	Dir   string
	Procs proc.Procs
	Match agents.Match
	// Installed is asked only when Dir is missing, to tell not-initialized from not-installed; nil counts as not
	// installed.
	Installed func() bool
	// RemoveStale deletes a pid file whose process is gone or is not Claude any more.
	RemoveStale bool
}

// NewDetector looks at the machine's processes and <home>/.claude, recognising Claude by its program name and
// removing stale pid files.
func NewDetector(home string) *Detector {
	return &Detector{Dir: filepath.Join(home, ".claude"), Procs: proc.System(), Match: agents.MatchArgv, RemoveStale: true}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// IsRunning: one of the pane shell's own children is Claude.
func (d *Detector) IsRunning(children []int) bool {
	for _, pid := range children {
		if d.Match.Is(d.Procs, pid, Binary) {
			return true
		}
	}
	return false
}

// ProjectsDir holds the transcripts, one folder per working folder.
func (d *Detector) ProjectsDir() string { return filepath.Join(d.Dir, "projects") }

// TranscriptIn is <projects>/<folder of cwd>/<id>.jsonl when it exists, else "".
func (d *Detector) TranscriptIn(cwd, id string) string {
	p := filepath.Join(d.ProjectsDir(), sessions.ProjectName(cwd), id+".jsonl")
	if exists(p) {
		return p
	}
	return ""
}

type pidFile struct {
	path      string
	pid       int
	sessionID any
	cwd       any
	startedAt any
}

func readPidFile(path string) (pidFile, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pidFile{}, false
	}
	var rec map[string]any
	if json.Unmarshal(raw, &rec) != nil || rec == nil {
		return pidFile{}, false
	}
	if !jscompat.Truthy(rec["pid"]) || !jscompat.Truthy(rec["sessionId"]) {
		return pidFile{}, false
	}
	pf := pidFile{path: path, pid: -1, sessionID: rec["sessionId"], cwd: rec["cwd"], startedAt: rec["startedAt"]}
	if n, ok := rec["pid"].(float64); ok && n == float64(int(n)) {
		pf.pid = int(n)
	}
	return pf, true
}

var digitsJSON = regexp.MustCompile(`^\d+\.json$`)

// pidFiles are the records of the given processes: looked up by name first (Claude names its file after its pid),
// every other file of the folder only when none is.
func (d *Detector) pidFiles(pids []int) []pidFile {
	dir := filepath.Join(d.Dir, "sessions")
	wanted := map[int]bool{}
	for _, p := range pids {
		wanted[p] = true
	}
	var found []pidFile
	for _, p := range pids {
		if rec, ok := readPidFile(filepath.Join(dir, strconv.Itoa(p)+".json")); ok && wanted[rec.pid] {
			found = append(found, rec)
		}
	}
	if len(found) > 0 {
		return found
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || digitsJSON.MatchString(e.Name()) {
			continue
		}
		if rec, ok := readPidFile(filepath.Join(dir, e.Name())); ok && wanted[rec.pid] {
			found = append(found, rec)
		}
	}
	return found
}

var (
	resumeArg       = regexp.MustCompile(`--resume` + jscompat.SpaceClass + `+([0-9a-f-]{36})`)
	claudeResumeArg = regexp.MustCompile(`claude` + jscompat.SpaceClass + `+--resume` + jscompat.SpaceClass + `+([0-9a-f-]{36})`)
)

// Detect finds the conversation of the Claude under a pane, given the shell's children: from the pid file of a
// process among the children and grandchildren (Claude may run under a launcher or re-spawn itself), else from a
// `claude --resume <id>` command line.
func (d *Detector) Detect(children []int) (agents.SessionInfo, error) {
	if !exists(d.Dir) {
		if d.Installed != nil && d.Installed() {
			return agents.Info(agents.NotInitialized), nil
		}
		return agents.Info(agents.NotInstalled), nil
	}
	if len(children) == 0 {
		return agents.Info(agents.NotRunning), nil
	}
	all := d.Procs.WithGrandchildren(children)
	for _, rec := range d.pidFiles(all) {
		args, ok := d.Procs.ArgsOf(rec.pid)
		if !ok || !d.Match.Is(d.Procs, rec.pid, Binary) {
			if d.RemoveStale {
				_ = os.Remove(rec.path)
			}
			continue
		}
		cwd, isStr := rec.cwd.(string)
		if !isStr {
			return agents.SessionInfo{}, ErrBadRecord
		}
		info := agents.SessionInfo{
			Status:       agents.Running,
			SessionID:    jscompat.String(rec.sessionID),
			RawSessionID: rec.sessionID,
			PID:          rec.pid,
			Cwd:          cwd,
		}
		if n, ok := rec.startedAt.(float64); ok {
			info.StartedAt = &n
		}
		info.JSONLPath = d.TranscriptIn(cwd, info.SessionID)
		if info.JSONLPath == "" {
			// A resumed Claude writes into the resumed conversation until its own transcript exists.
			if m := resumeArg.FindStringSubmatch(args); m != nil {
				if p := d.TranscriptIn(cwd, m[1]); p != "" {
					info.JSONLPath, info.SessionID, info.RawSessionID = p, m[1], m[1]
				}
			}
		}
		return info, nil
	}
	for _, pid := range all {
		args, ok := d.Procs.ArgsOf(pid)
		if !ok || args == "" {
			continue
		}
		m := claudeResumeArg.FindStringSubmatch(args)
		if m == nil {
			continue
		}
		cwd := d.Procs.CwdOf(pid)
		if cwd == "" {
			break
		}
		return agents.SessionInfo{Status: agents.Running, SessionID: m[1], RawSessionID: m[1], JSONLPath: d.TranscriptIn(cwd, m[1]), PID: pid, Cwd: cwd}, nil
	}
	return agents.Info(agents.NotRunning), nil
}

var settingsArg = regexp.MustCompile(`--settings(?:=|` + jscompat.SpaceClass + `)+([^` + jscompat.SpaceClass[1:] + `+)`)

// RunsWithHooks tells whether a Claude among the pane's children and grandchildren was given the runtime's hooks: a
// `--settings` file that is hooksPath itself, or whose text names the status hook script (hookScriptName; a managed
// launcher writes settings of its own, and older data folders had another name). found is false when no Claude runs
// there.
func (d *Detector) RunsWithHooks(children []int, hooksPath, hookScriptName string) (hooked, found bool) {
	for _, pid := range d.Procs.WithGrandchildren(children) {
		args, ok := d.Procs.ArgsOf(pid)
		if !ok || args == "" || !d.Match.Is(d.Procs, pid, Binary) {
			continue
		}
		found = true
		for _, m := range settingsArg.FindAllStringSubmatch(args, -1) {
			if m[1] == hooksPath {
				return true, true
			}
			if raw, err := os.ReadFile(m[1]); err == nil && strings.Contains(string(raw), hookScriptName) {
				return true, true
			}
		}
	}
	return false, found
}

// WithEnv is the Claude among the pane's children and grandchildren whose environment has one of the exact KEY=value
// entries (the prompt editor's VISUAL), or 0.
func (d *Detector) WithEnv(children []int, entries ...string) int {
	for _, pid := range d.Procs.WithGrandchildren(children) {
		if !d.Match.Is(d.Procs, pid, Binary) {
			continue
		}
		if env, ok := d.Procs.EnvironOf(pid); ok && proc.HasEnvEntry(env, entries...) {
			return pid
		}
	}
	return 0
}

// FindTranscript is a session's transcript in whichever project folder holds it, "" when none does: a tab's folder
// may have changed since the session started.
func FindTranscript(projectsDir, sessionID string) string {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		p := filepath.Join(projectsDir, e.Name(), sessionID+".jsonl")
		if exists(p) {
			return p
		}
	}
	return ""
}
