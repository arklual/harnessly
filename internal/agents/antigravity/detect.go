package antigravity

import (
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/proc"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var versionedBinary = regexp.MustCompile(`^agy[0-9]+\.[0-9]+\.[0-9]+$`)

func MatchesProcess(command string, args []string) bool {
	name := filepath.Base(command)
	if name == Binary || versionedBinary.MatchString(name) {
		return true
	}
	// Do not infer identity from arbitrary prompt arguments naming agy.
	return false
}

// IsBridge: the process is `harnessly antigravity-acp`, the ACP bridge a native chat runs as (the official CLI is its
// child). Its --conversation names the session it resumes.
func IsBridge(argv []string) bool {
	return len(argv) > 1 && argv[1] == "antigravity-acp" && strings.HasPrefix(filepath.Base(argv[0]), "harnessly")
}

func ParsePaneTitle(string) *string { return nil }

func Dir(home string) string { return filepath.Join(home, ".gemini", "antigravity-cli") }
func TranscriptPath(home, id string) string {
	if !agents.IsUUID(id) {
		return ""
	}
	return filepath.Join(Dir(home), "brain", id, ".system_generated", "logs", "transcript.jsonl")
}

// SessionIDFromPath reads the UUID ancestor, never the transcript's filename.
func SessionIDFromPath(path string) string {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ""
	}
	logs := filepath.Dir(path)
	generated := filepath.Dir(logs)
	session := filepath.Dir(generated)
	if (filepath.Base(path) != "transcript.jsonl" && filepath.Base(path) != "transcript_full.jsonl") || filepath.Base(logs) != "logs" || filepath.Base(generated) != ".system_generated" || filepath.Base(filepath.Dir(session)) != "brain" || filepath.Base(filepath.Dir(filepath.Dir(session))) != "antigravity-cli" || filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(session)))) != ".gemini" {
		return ""
	}
	id := filepath.Base(session)
	if !agents.IsUUID(id) {
		return ""
	}
	return id
}

// ValidateTranscriptPath checks both lexical binding and existing symlink resolution.
// A missing transcript is valid: hooks may precede its creation.
func ValidateTranscriptPath(home, path, id string) bool {
	expected := TranscriptPath(home, id)
	if expected == "" || SessionIDFromPath(path) != id {
		return false
	}
	expected = filepath.Join(filepath.Dir(expected), filepath.Base(path))
	if path != expected {
		return false
	}
	// Resolve every existing ancestor, including a symlinked home.
	canonical := func(p string) string {
		suffix := ""
		for {
			if real, err := filepath.EvalSymlinks(p); err == nil {
				return filepath.Join(real, suffix)
			}
			parent := filepath.Dir(p)
			if parent == p {
				return ""
			}
			suffix = filepath.Join(filepath.Base(p), suffix)
			p = parent
		}
	}
	realHome := canonical(home)
	return realHome != "" && canonical(path) == filepath.Join(filepath.Dir(TranscriptPath(realHome, id)), filepath.Base(path))
}

var conversationArg = regexp.MustCompile(`(?:^|\s)--conversation(?:=|\s+)["']?([0-9a-fA-F-]{36})["']?(?:\s|$)`)

func ResumeID(args string) string {
	m := conversationArg.FindStringSubmatch(args)
	if m != nil && agents.IsUUID(m[1]) {
		return m[1]
	}
	return ""
}

type SessionMeta struct {
	SessionID string
	JSONLPath string
}
type Detector struct {
	// Binary optionally identifies the deployment's exact absolute launcher path.
	// It does not broaden global process-name matching.
	Binary string
	Home   string
	Procs  proc.Procs
	// Installed probes the executable only; it must not be a login probe.
	Installed func() bool
}

func NewDetector(home string) *Detector  { return &Detector{Home: home, Procs: proc.System()} }
func (d *Detector) SessionsRoot() string { return filepath.Join(Dir(d.Home), "brain") }
func (d *Detector) FindSession(id string) (SessionMeta, bool) {
	base := TranscriptPath(d.Home, id)
	if base == "" {
		return SessionMeta{}, false
	}
	for _, name := range []string{"transcript.jsonl", "transcript_full.jsonl"} {
		path := filepath.Join(filepath.Dir(base), name)
		if !ValidateTranscriptPath(d.Home, path, id) {
			continue
		}
		st, err := os.Stat(path)
		if err == nil && st.Mode().IsRegular() {
			return SessionMeta{id, path}, true
		}
	}
	return SessionMeta{}, false
}
func (d *Detector) IsRunning(children []int) bool {
	return d.Detect(children, false).Status == agents.Running
}

// Detect deliberately ignores cwd fallback: a directory or globally newest transcript
// is not proof of a pane's conversation. Fresh sessions bind through validated hooks.
func (d *Detector) Detect(children []int, _ bool) agents.SessionInfo {
	for _, pid := range d.Procs.WithGrandchildren(children) {
		args, ok := d.Procs.ArgsOf(pid)
		if !ok {
			continue
		}
		words, argvOK := d.Procs.ArgvOf(pid)
		if !argvOK || len(words) == 0 {
			continue
		}
		configured := filepath.IsAbs(d.Binary) && words[0] == d.Binary
		if !configured && !MatchesProcess(words[0], words[1:]) && !IsBridge(words) {
			continue
		}
		info := agents.SessionInfo{Status: agents.Running, PID: pid, Cwd: d.Procs.CwdOf(pid), Dir: Dir(d.Home)}
		if id := ResumeID(args); id != "" {
			info.SessionID = id
			if m, ok := d.FindSession(id); ok {
				info.JSONLPath = m.JSONLPath
			}
		}
		return info
	}
	if d.Installed == nil || !d.Installed() {
		return agents.Info(agents.NotInstalled)
	}
	return agents.Info(agents.NotRunning)
}
