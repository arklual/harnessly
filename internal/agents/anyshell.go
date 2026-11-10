package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// The line that starts an agent is typed into whatever shell the pane runs: bash, zsh, fish, sh. Each reads its own
// syntax, and fish reads none of sh's `$'…'`, `set -a`, `${VAR:-x}` or `VAR=1 cmd` (before 3.1). So the platform writes
// the line for sh and types it as `sh -c '…'`, quoted so every one of those shells hands sh the same bytes.

// anyShellPrefix starts every line ForAnyShell gives, so it is never wrapped twice.
const anyShellPrefix = "sh -c "

// AnyShellQuote quotes s as one word that sh, bash, zsh and fish all read back as s: single-quoted runs, with each `'`
// and `\` outside the quotes as `\'` and `\\` (fish reads `\'` and `\\` inside single quotes as escapes, sh does not).
// s must have no control characters: a newline would end the typed line.
func AnyShellQuote(s string) string {
	var b strings.Builder
	open := false
	for _, r := range s {
		if r == '\'' || r == '\\' {
			if open {
				b.WriteByte('\'')
				open = false
			}
			b.WriteByte('\\')
			b.WriteRune(r)
			continue
		}
		if !open {
			b.WriteByte('\'')
			open = true
		}
		b.WriteRune(r)
	}
	if open {
		b.WriteByte('\'')
	}
	if b.Len() == 0 {
		return "''"
	}
	return b.String()
}

// envSourceEnd ends the prefix EnvSourcePrefix writes.
const envSourceEnd = " set +a; "

// ForAnyShell is a launch line written for sh (an optional EnvSourcePrefix, then `VAR=value… command args`) as a line
// any interactive shell runs the same way: `sh -c '…; exec env VAR=value… command args'`. The command is exec'd, so the
// pane's foreground process is the agent's CLI (what tells a running agent, #{pane_current_command}) and not sh, and
// the person's shell is back when the agent exits.
func ForAnyShell(line string) string {
	if line == "" || strings.HasPrefix(line, anyShellPrefix) {
		return line
	}
	prefix, command := "", line
	if strings.HasPrefix(line, "set -a; ") {
		if i := strings.Index(line, envSourceEnd); i >= 0 {
			prefix, command = line[:i+len(envSourceEnd)], line[i+len(envSourceEnd):]
		}
	}
	body := prefix + "exec env " + command
	if typed := AnyShellQuote(body); len(typed) <= longLine {
		return anyShellPrefix + typed
	}
	if short, ok := lineInFile(body); ok {
		return short
	}
	return anyShellPrefix + AnyShellQuote(body)
}

// longLine is the most bytes of a line that is typed as it is. A shell takes typed keys one by one and redraws the line
// for each: 7 KB (the wall of a chat with apps, HRN-621) took zsh 13 s and more, a launch counts as failed after 15 s, and
// typed before the shell's line editor is up the terminal's own 4 KB limit loses the line and its Enter.
const longLine = 1 << 10

// A line kept this long was never run: its shell went away first.
const lineFileAge = time.Hour

var lineDir atomic.Pointer[string]

// SetLineDir names the folder long launch lines wait in. It must be one the wall of a chat with apps hides (the data
// folder's own, off the seal's list, internal/apps/seal.go): an agent that could write a waiting line would run outside
// its wall. Without it long lines are typed whole.
func SetLineDir(dir string) { lineDir.Store(&dir) }

// lineInFile keeps body in a file readable only by its owner and gives the short line that runs it; the file removes
// itself first. Run by `.`, in the `sh -c` itself, so its exec makes the pane's foreground process the agent's CLI.
func lineInFile(body string) (string, bool) {
	dir := lineDir.Load()
	if dir == nil || *dir == "" || os.MkdirAll(*dir, 0o700) != nil {
		return "", false
	}
	sweepLines(*dir)
	f, err := os.CreateTemp(*dir, "line-*")
	if err != nil {
		return "", false
	}
	name := f.Name()
	_, err = f.WriteString("rm -f -- " + shellWord(name) + "\n" + body + "\n")
	if cerr := f.Close(); err != nil || cerr != nil {
		_ = os.Remove(name)
		return "", false
	}
	return anyShellPrefix + AnyShellQuote(". "+shellWord(name)), true
}

func sweepLines(dir string) {
	old, _ := filepath.Glob(filepath.Join(dir, "line-*"))
	for _, f := range old {
		if info, err := os.Stat(f); err == nil && time.Since(info.ModTime()) > lineFileAge {
			_ = os.Remove(f)
		}
	}
}

// shellWord is s as one word of sh.
func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// PrintfQuote is s as one sh word that keeps every character, newlines and control characters too, on one typed line:
// `"$(printf %b '…')"` with the escapes printf's %b reads. ($'…' would do in bash and zsh, but not in dash or fish.)
// $(…) drops trailing newlines, so s is a trimmed prompt.
func PrintfQuote(s string) string {
	var b strings.Builder
	b.WriteString(`"$(printf %b '`)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\'':
			b.WriteString(`'\''`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\0%03o`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString(`')"`)
	return b.String()
}

// AppendArgs puts args (sh words) at the end of the command of a launch line, wrapped by ForAnyShell or not.
func AppendArgs(line, args string) string {
	if args == "" {
		return line
	}
	inner, ok := UnwrapAnyShell(line)
	if !ok {
		return line + args
	}
	return anyShellPrefix + AnyShellQuote(inner+args)
}

// UnwrapAnyShell is the sh line ForAnyShell wrapped; false when line is not one it wrote.
func UnwrapAnyShell(line string) (string, bool) {
	if !strings.HasPrefix(line, anyShellPrefix) {
		return "", false
	}
	return unquoteAnyShell(strings.TrimPrefix(line, anyShellPrefix))
}

// unquoteAnyShell reads back a word AnyShellQuote wrote; false when s is not one.
func unquoteAnyShell(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch s[i] {
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return "", false
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 2
		case '\\':
			if i+1 >= len(s) {
				return "", false
			}
			b.WriteByte(s[i+1])
			i += 2
		default:
			return "", false
		}
	}
	return b.String(), true
}
