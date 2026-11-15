package realtime

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// What the voice knows of the person and of the screen when a call starts.
//
// The instructions stay short and the same for every call; what differs between calls goes in as the call's first
// items, a developer message: who the person is and how they want to be answered (what they wrote in Settings → Voice
// and their agents' notes about them), and what is on the screen now. On the subscription route the instructions can
// not change once the call is made, so this is the one place for it; on the key route it follows the instructions.
//
// All of it is the person's own and goes only into their own call. The journal gets its sizes, never its words.

// Primer is what a call starts knowing.
type Primer struct {
	// About is what the person wrote about themselves and how to answer them (Settings → Voice).
	About string
	// Notes are their agents' notes about them: one line each (the description of a memory of type user or feedback).
	Notes []string
	// Screen is what the page says is in front of the person: the open board or file, the selection, the chat's last
	// lines and the agent's last answer.
	Screen string
}

// Limits of what a call starts with, in bytes: the voice answers fast while it is small.
const (
	maxAbout  = 2000
	maxNotes  = 2500
	maxScreen = 4000
)

// Empty says there is nothing to start with.
func (p Primer) Empty() bool {
	return strings.TrimSpace(p.About) == "" && len(p.Notes) == 0 && strings.TrimSpace(p.Screen) == ""
}

// Text is the developer message a call starts with.
func (p Primer) Text() string {
	var b strings.Builder
	if a := clip(strings.TrimSpace(p.About), maxAbout); a != "" {
		b.WriteString("About the person, in their own words (follow it; never read it out):\n")
		b.WriteString(a)
		b.WriteString("\n\n")
	}
	if len(p.Notes) > 0 {
		b.WriteString("What their agents noted about how they work and want answers (follow it quietly):\n")
		n := 0
		for _, line := range p.Notes {
			line = strings.TrimSpace(line)
			if line == "" || n+len(line) > maxNotes {
				continue
			}
			n += len(line)
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if s := clip(strings.TrimSpace(p.Screen), maxScreen); s != "" {
		b.WriteString("On the screen when the call began (it may change; the agent sees the live screen):\n")
		b.WriteString(s)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// Items are the call's first items on the subscription route.
func (p Primer) Items() []map[string]any {
	if p.Empty() {
		return nil
	}
	return []map[string]any{{
		"type": "message", "role": "developer",
		"content": []map[string]string{{"type": "input_text", "text": p.Text()}},
	}}
}

// Sizes are what the journal gets: how much of each part went in, never the words.
func (p Primer) Sizes() map[string]int {
	return map[string]int{
		"about":  utf8.RuneCountInString(strings.TrimSpace(p.About)),
		"notes":  len(p.Notes),
		"screen": utf8.RuneCountInString(strings.TrimSpace(p.Screen)),
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// noteKinds are the memories that say something about the person: who they are and how they want things done. The
// project's own facts (type project or reference) stay with the agent.
var noteKinds = map[string]bool{"user": true, "feedback": true}

// MemoryNotes reads the descriptions of the person's memories in an agent's memory folder (Claude Code's
// <project>/memory, one file per memory with a name, a description and a type in its front matter): only those of type
// user or feedback, one line each, and never a memory's body. A folder that is not there gives nothing.
func MemoryNotes(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || strings.EqualFold(name, "MEMORY.md") {
			continue
		}
		if kind, desc := frontMatter(filepath.Join(dir, name)); noteKinds[kind] && desc != "" {
			out = append(out, desc)
		}
	}
	sort.Strings(out)
	return out
}

// frontMatter is a memory's type and description, read from its front matter only.
func frontMatter(path string) (kind, desc string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	for i := 0; sc.Scan() && i < 40; i++ {
		line := sc.Text()
		if i == 0 {
			if strings.TrimSpace(line) != "---" {
				return "", ""
			}
			continue
		}
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch key {
		case "type":
			kind = val
		case "description":
			desc = val
		}
	}
	return kind, desc
}
