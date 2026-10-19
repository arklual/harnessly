package statusline

import (
	"os"
	"path/filepath"
	"strings"
)

// tokenStop ends a word of a shell command.
const tokenStop = " \t\r\n;|&()<>`="

// refersToScript says whether a command names the platform's own statusline script, by any path that reaches it (a
// link to the data folder, ~ or $HOME): run, it would post the same JSON here again, and again.
func (s *Sink) refersToScript(command string) bool {
	if s.Script == "" {
		return false
	}
	base := filepath.Base(s.Script)
	script, statErr := os.Stat(s.Script)
	realScript, _ := filepath.EvalSymlinks(s.Script)
	for from := 0; ; {
		i := strings.Index(command[from:], base)
		if i < 0 {
			return false
		}
		end := from + i + len(base)
		start := strings.LastIndexAny(command[:end], tokenStop) + 1
		from = end
		word := strings.NewReplacer(`"`, "", `'`, "").Replace(command[start:end])
		path := s.expand(word)
		if filepath.Clean(path) == filepath.Clean(s.Script) {
			return true
		}
		if statErr == nil {
			if st, err := os.Stat(path); err == nil && os.SameFile(st, script) {
				return true
			}
		}
		if real, err := filepath.EvalSymlinks(path); err == nil && realScript != "" && real == realScript {
			return true
		}
	}
}

// expand resolves a path as the shell would for the forms that name a home: ~/, $HOME/, ${HOME}/; a relative path is
// the runtime's own folder's, where the command runs. The path is not cleaned: ".." after a link is the kernel's.
func (s *Sink) expand(word string) string {
	for _, prefix := range []string{"~/", "$HOME/", "${HOME}/"} {
		if rest, ok := strings.CutPrefix(word, prefix); ok && s.Home != "" {
			return s.Home + "/" + rest
		}
	}
	if !filepath.IsAbs(word) {
		if wd, err := os.Getwd(); err == nil {
			return wd + "/" + word
		}
	}
	return word
}
