package connect

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
)

// winPath is an absolute Windows path in a program's output: C:\Users\m\proj\a.py, C:/Users/m.
var winPath = regexp.MustCompile(`(^|[\s"'(=\[,:])([A-Za-z]):[\\/]((?:[^\s"'<>|:*?\\/]+[\\/]?)*)`)

// RewriteWinPaths gives a Windows program's output with its paths in the form the agent sees (/c/Users/m/proj/a.py).
func RewriteWinPaths(b []byte) []byte {
	return winPath.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := winPath.FindSubmatch(m)
		rest := strings.ReplaceAll(string(sub[3]), `\`, "/")
		return append(append([]byte(nil), sub[1]...), "/"+strings.ToLower(string(sub[2]))+"/"+rest...)
	})
}

// lineRewriter passes whole lines through RewriteWinPaths, so a path split between two writes is still seen whole;
// a line longer than 64 KiB goes as it is.
type lineRewriter struct {
	mu   sync.Mutex
	out  func([]byte)
	part []byte
}

func (w *lineRewriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.part = append(w.part, p...)
	if i := bytes.LastIndexByte(w.part, '\n'); i >= 0 {
		w.out(RewriteWinPaths(w.part[:i+1]))
		w.part = append(w.part[:0:0], w.part[i+1:]...)
	}
	if len(w.part) > 64<<10 {
		w.out(w.part)
		w.part = nil
	}
	return len(p), nil
}

// flush gives what is left once the program has ended.
func (w *lineRewriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.part) > 0 {
		w.out(RewriteWinPaths(w.part))
		w.part = nil
	}
}
