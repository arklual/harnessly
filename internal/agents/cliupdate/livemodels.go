package cliupdate

import (
	"bufio"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// The subscription voice route takes the «gpt-live-…-codex» model the ChatGPT backend serves to Codex; the name is in
// Codex's own program and nowhere else on the machine, so the program is read for it once per version.

var liveRe = regexp.MustCompile(`gpt-live-[0-9][0-9.]*-codex`)

var liveCache struct {
	sync.Mutex
	key    string
	models []string
	busy   bool
}

// ScanLiveModels finds the «gpt-live-…-codex» names in r, each once.
func ScanLiveModels(r io.Reader) []string {
	var out []string
	br := bufio.NewReaderSize(r, 1<<20)
	buf := make([]byte, 0, 2<<20)
	chunk := make([]byte, 1<<20)
	for {
		n, err := br.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for _, m := range liveRe.FindAll(buf, -1) {
			if s := string(m); !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		// Keep a tail so a name split between two reads is found whole.
		if len(buf) > 64 {
			buf = append(buf[:0], buf[len(buf)-64:]...)
		}
		if err != nil {
			return out
		}
	}
}

// codexProgram is Codex's native program for what PATH starts: itself when it is the program, else the biggest file
// called codex under the npm package that the script belongs to.
func codexProgram(real string) string {
	st, err := os.Stat(real)
	if err != nil {
		return ""
	}
	if st.Size() > 4<<20 {
		return real
	}
	// …/node_modules/@openai/codex/bin/codex.js: the vendored program is under the package or beside it.
	pkg := filepath.Dir(filepath.Dir(real))
	best, size := "", int64(0)
	for _, dir := range []string{pkg, filepath.Dir(pkg)} {
		_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasPrefix(e.Name(), "codex") || strings.HasSuffix(e.Name(), ".js") {
				return nil
			}
			if info, err := e.Info(); err == nil && info.Size() > size {
				best, size = p, info.Size()
			}
			return nil
		})
		if best != "" {
			break
		}
	}
	return best
}

// LiveModels are the «gpt-live» models of the Codex at real (the path PATH's codex resolves to). The first call for a
// version starts the read in the background and answers nil; the next ones answer what it found.
func LiveModels(real string) []string {
	if real == "" {
		return nil
	}
	key := real
	if st, err := os.Stat(real); err == nil {
		key += "|" + st.ModTime().Format(time.RFC3339Nano)
	}
	liveCache.Lock()
	defer liveCache.Unlock()
	if liveCache.key == key {
		return liveCache.models
	}
	if !liveCache.busy {
		liveCache.busy = true
		go func() {
			var found []string
			if prog := codexProgram(real); prog != "" {
				if f, err := os.Open(prog); err == nil {
					found = ScanLiveModels(f)
					f.Close()
				}
			}
			liveCache.Lock()
			liveCache.key, liveCache.models, liveCache.busy = key, found, false
			liveCache.Unlock()
		}()
	}
	return liveCache.models
}
