package chatwork

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Hidden are paths that are never work: the system's and the agents' scratch folders.
var hidden = regexp.MustCompile(`^/(proc|sys|dev)(/|$)|^/tmp/claude-[^/]+/`)

// words splits a shell command into words and the separators between commands (";", "&&", "||", "|", "&", newline,
// "(", ")"), honouring single and double quotes. It is no shell: it only needs to find the paths a command names.
func words(cmd string) []string {
	var out []string
	var cur strings.Builder
	have := false
	flush := func() {
		if have {
			out = append(out, cur.String())
			cur.Reset()
			have = false
		}
	}
	var quote rune
	for _, r := range cmd {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, have = r, true
		case r == ' ' || r == '\t':
			flush()
		case r == ';' || r == '&' || r == '|' || r == '\n' || r == '(' || r == ')':
			flush()
			out = append(out, ";")
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	flush()
	return out
}

// resolve makes p absolute against dir, expanding ~/; "" for what cannot be a path.
func resolve(p, dir, home string) string {
	switch {
	case p == "" || strings.Contains(p, "://") || strings.ContainsAny(p, "$`*?"):
		return ""
	case p == "~" || strings.HasPrefix(p, "~/"):
		if home == "" {
			return ""
		}
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(p, "~")))
	case filepath.IsAbs(p):
		return filepath.Clean(p)
	case dir == "":
		return ""
	}
	return filepath.Clean(filepath.Join(dir, p))
}

// pathLike: a word that names a file or a folder rather than an argument (an absolute, a home or a dotted relative path).
func pathLike(w string) bool {
	return strings.HasPrefix(w, "/") || strings.HasPrefix(w, "~/") || strings.HasPrefix(w, "./") ||
		strings.HasPrefix(w, "../") || (strings.HasPrefix(w, ".") && strings.Contains(w, "/"))
}

// commandPaths are the folders and files a shell command names: the targets of cd and of -C, and the absolute, home
// and dotted relative paths, each relative one taken from the folder the command is in at that word.
func commandPaths(cmd, cwd, home string) []string {
	ws := words(cmd)
	dir := cwd  // where the shell is: cd moves it for the rest of the line
	local := "" // a -C of the current command
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] && !hidden.MatchString(p) {
			seen[p] = true
			out = append(out, p)
		}
	}
	here := func() string {
		if local != "" {
			return local
		}
		return dir
	}
	for i := 0; i < len(ws); i++ {
		w := ws[i]
		switch {
		case w == ";":
			local = ""
		case w == "cd" && i+1 < len(ws) && ws[i+1] != ";":
			if p := resolve(ws[i+1], dir, home); p != "" {
				dir = p
				add(p)
			}
			i++
		case w == "-C" && i+1 < len(ws) && ws[i+1] != ";":
			if p := resolve(ws[i+1], here(), home); p != "" {
				local = p
				add(p)
			}
			i++
		case strings.HasPrefix(w, "--work-tree=") || strings.HasPrefix(w, "--git-dir="):
			_, v, _ := strings.Cut(w, "=")
			add(resolve(v, here(), home))
		default:
			if eq := strings.IndexByte(w, '='); eq > 0 && !strings.HasPrefix(w, "-") && pathLike(w[eq+1:]) {
				w = w[eq+1:] // VAR=/path
			}
			if pathLike(w) {
				add(resolve(strings.TrimRight(w, ",:"), here(), home))
			}
		}
	}
	return out
}

// gitChange matches a git command that makes or moves work: a commit, a new worktree or branch, a merge, a rebase, a
// pick, a patch applied, a push.
var gitChange = regexp.MustCompile(`\bgit\b(?:\s+-C\s+\S+|\s+-c\s+\S+|\s+--[\w-]+(?:=\S+)?)*\s+(commit|worktree\s+add|checkout\s+-[bB]|switch\s+-[cC]|branch\s+\S|merge|rebase|cherry-pick|am|apply|revert|push|reset|stash\s+pop)\b`)

// changesGit reports whether a command changes a repository.
func changesGit(cmd string) bool { return gitChange.MatchString(cmd) }

// summaryPath is the file a write tool's summary names: "Update /p (+1, -2)", "Create /p (…)", "Write /p".
func summaryPath(summary string) string {
	for _, verb := range []string{"Update ", "Create ", "Write ", "Edit ", "MultiEdit ", "NotebookEdit "} {
		if rest, ok := strings.CutPrefix(summary, verb); ok {
			if i := strings.LastIndex(rest, " (+"); i > 0 {
				rest = rest[:i]
			}
			rest = strings.TrimSpace(rest)
			if filepath.IsAbs(rest) {
				return filepath.Clean(rest)
			}
		}
	}
	return ""
}

// absPaths are the absolute paths a text names (an apply_patch's files).
var absPath = regexp.MustCompile(`(?:^|[\s'"=(])(/(?:[\w.@+-]+/)*[\w.@+-]+)`)

func absPaths(text string) []string {
	var out []string
	for _, m := range absPath.FindAllStringSubmatch(text, -1) {
		if !hidden.MatchString(m[1]) {
			out = append(out, filepath.Clean(m[1]))
		}
	}
	return out
}
