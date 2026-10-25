package sessions

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// Recent is one agent session found on the machine without asking for a folder: what "continue a session from the
// terminal" lists.
type Recent struct {
	SessionID string
	// Title is the agent's own name for the session, else the first thing the person asked.
	Title string
	Cwd   string
	// Path is the session's transcript.
	Path      string
	UpdatedAt time.Time
	Turns     int
}

// RecentDays is how far back Codex's day folders are looked into.
const RecentDays = 30

// Only sessions with a UUID for an id are offered: the CLIs resume nothing else (Claude's side chains, named by an agent
// id, are not sessions a person continues).

// Recent is the newest sessions in every folder, most recently active first, at most limit, leaving out the ones skip
// names and the ones nobody has said anything in.
func (l *ClaudeLister) Recent(skip func(id string) bool, limit int) []Recent {
	dirs, err := os.ReadDir(l.ProjectsDir)
	if err != nil {
		return []Recent{}
	}
	type file struct {
		path  string
		mtime time.Time
	}
	var files []file
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(l.ProjectsDir, d.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			id := jscompat.Basename(name, ".jsonl")
			if !strings.HasSuffix(name, ".jsonl") || !agents.IsUUID(id) || (skip != nil && skip(id)) {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			files = append(files, file{filepath.Join(l.ProjectsDir, d.Name(), name), info.ModTime()})
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].mtime.After(files[j].mtime) })
	out := []Recent{}
	for _, f := range files {
		if len(out) >= limit {
			break
		}
		m := l.meta(f.path)
		if m == nil || m.TurnCount == 0 || m.cwd == "" {
			continue
		}
		out = append(out, Recent{SessionID: m.SessionID, Title: titleOf(m.title, m.FirstMessage, m.cwd), Cwd: m.cwd, Path: f.path, UpdatedAt: f.mtime, Turns: m.TurnCount})
	}
	return out
}

// Recent is the newest Codex sessions of the last RecentDays days in every folder, most recently active first, at most
// limit, leaving out the ones skip names.
func (l *CodexLister) Recent(skip func(id string) bool, limit int) []Recent {
	type file struct {
		path  string
		mtime time.Time
	}
	var files []file
	for _, dir := range CodexDays(l.Root, l.Now(), RecentDays) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			files = append(files, file{filepath.Join(dir, e.Name()), info.ModTime()})
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].mtime.After(files[j].mtime) })
	out := []Recent{}
	for _, f := range files {
		if len(out) >= limit {
			break
		}
		e := l.entry(f.path)
		if e == nil || e.TurnCount == 0 {
			continue
		}
		id, _ := e.SessionID.(string)
		cwd, _ := e.Cwd.(string)
		if !agents.IsUUID(id) || cwd == "" || (skip != nil && skip(id)) {
			continue
		}
		first := ""
		if e.FirstUserMessage != nil {
			first = *e.FirstUserMessage
		}
		out = append(out, Recent{SessionID: id, Title: titleOf("", first, cwd), Cwd: cwd, Path: f.path, UpdatedAt: f.mtime, Turns: e.TurnCount})
	}
	return out
}

// Find is the Claude session with this id wherever it ran, false when no transcript of it is kept. id must be a plain
// session id (no path in it).
func (l *ClaudeLister) Find(id string) (Recent, bool) {
	if !agents.IsUUID(id) {
		return Recent{}, false
	}
	dirs, err := os.ReadDir(l.ProjectsDir)
	if err != nil {
		return Recent{}, false
	}
	for _, d := range dirs {
		path := filepath.Join(l.ProjectsDir, d.Name(), id+".jsonl")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		m := l.meta(path)
		if m == nil || m.cwd == "" {
			return Recent{}, false
		}
		return Recent{SessionID: m.SessionID, Title: titleOf(m.title, m.FirstMessage, m.cwd), Cwd: m.cwd, Path: path, UpdatedAt: info.ModTime(), Turns: m.TurnCount}, true
	}
	return Recent{}, false
}

// Find is the Codex session with this id among those of the last RecentDays days.
func (l *CodexLister) Find(id string) (Recent, bool) {
	if !agents.IsUUID(id) {
		return Recent{}, false
	}
	for _, dir := range CodexDays(l.Root, l.Now(), RecentDays) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), "-"+id+".jsonl") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			ent := l.entry(path)
			info, err := os.Stat(path)
			if ent == nil || err != nil {
				return Recent{}, false
			}
			if got, _ := ent.SessionID.(string); got != id {
				return Recent{}, false
			}
			cwd, _ := ent.Cwd.(string)
			first := ""
			if ent.FirstUserMessage != nil {
				first = *ent.FirstUserMessage
			}
			return Recent{SessionID: id, Title: titleOf("", first, cwd), Cwd: cwd, Path: path, UpdatedAt: info.ModTime(), Turns: ent.TurnCount}, true
		}
	}
	return Recent{}, false
}

// titleOf is what a list line says of a session: the agent's name for it, else the first thing asked, else its folder.
func titleOf(name, first, cwd string) string {
	switch {
	case name != "":
		return name
	case strings.TrimSpace(first) != "":
		return first
	}
	return filepath.Base(cwd)
}
