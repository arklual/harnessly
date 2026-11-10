package apps

import (
	"bufio"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// What an app sees of the workspace besides one file: a folder's entries and a search, both inside the masks
// of files.read and by the rules of Resolve. A folder is shown when some mask can reach into it, a file when a mask
// names it; a secret, the repository's own folder and the host's folders are never shown, whatever the masks say.

// FileEntry is a file or a folder of the workspace an app may see.
type FileEntry struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir,omitempty"`
	Size int64  `json:"size,omitempty"`
	// Modified is the time of the last change, in milliseconds.
	Modified int64 `json:"modified,omitempty"`
}

// SearchHit is a line of a file that holds what was looked for; Line 0 is a file found by its name alone.
type SearchHit struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Text string `json:"text,omitempty"`
}

// SearchOptions narrow a search.
type SearchOptions struct {
	// Glob is a mask the files must match too (src/**/*.go); "" is every file the app may read.
	Glob string
	// Case: the text is matched as written; otherwise case is ignored.
	Case  bool
	Limit int
}

const (
	// MaxListed is how many entries of one folder a listing gives.
	MaxListed = 1000
	// MaxSearchHits is how many lines a search gives at most.
	MaxSearchHits = 500
	// maxSearchFiles and maxSearchBytes bound the work of one search: the files looked into, the size of each.
	maxSearchFiles = 20000
	maxSearchBytes = 1 << 20
	maxHitText     = 300
)

// skippedDirs are folders a search does not walk into unless a mask names them: they are big and nobody's text.
var skippedDirs = []string{"node_modules", ".venv", "venv", "__pycache__", "dist", "build", "target", ".next", ".cache"}

// reachable says whether a mask can match a path under the folder dir ("" is the workspace).
func reachable(masks []string, dir string) bool {
	if dir == "" || dir == "." {
		return len(masks) > 0
	}
	ds := strings.Split(dir, "/")
	for _, m := range masks {
		if reachSegments(strings.Split(m, "/"), ds) {
			return true
		}
	}
	return false
}

func reachSegments(mask, dir []string) bool {
	for i, d := range dir {
		if i >= len(mask) {
			return false
		}
		if mask[i] == "**" {
			return true
		}
		if ok, _ := path.Match(mask[i], d); !ok {
			return false
		}
	}
	// The folder itself is a prefix of the mask: something under it may match.
	return len(mask) > len(dir)
}

// named says whether a mask names the folder (or above it) outright, so a search walks it although it is a big one.
func named(masks []string, dir string) bool {
	for _, m := range masks {
		if strings.HasPrefix(m, dir+"/") {
			return true
		}
	}
	return false
}

// root is the workspace's real folder.
func (s FileScope) root() (string, error) {
	if s.Workspace == "" || !filepath.IsAbs(s.Workspace) {
		return "", refuseFile("there is no workspace to reach files in")
	}
	root, err := filepath.EvalSymlinks(s.Workspace)
	if err != nil {
		return "", refuseFile("the workspace cannot be reached")
	}
	return root, nil
}

// hostOwn says whether a folder of the workspace is, or holds, one of the host's own.
func (s FileScope) hostOwn(written string) bool {
	real, err := filepath.EvalSymlinks(written)
	if err != nil {
		real = written
	}
	for _, f := range s.Forbidden {
		if f == "" {
			continue
		}
		fr, err := realPath(f)
		if err != nil {
			fr = f
		}
		if within(fr, real) || within(real, fr) || within(f, written) || within(written, f) {
			return true
		}
	}
	return false
}

// List gives the entries of a folder of the workspace the app may see, folders first, by name.
func (s FileScope) List(dir string) ([]FileEntry, error) {
	if len(s.ReadMasks) == 0 {
		return nil, refuseFile("the app has no right to read files (files.read:<mask>)")
	}
	clean := ""
	if dir != "" && dir != "." && dir != "/" {
		c, err := lexical(dir)
		if err != nil {
			return nil, err
		}
		clean = c
	}
	if clean != "" && (!reachable(s.ReadMasks, clean) || secret(clean)) {
		return nil, refuseFile("%q is not under a mask the app may read", clean)
	}
	root, err := s.root()
	if err != nil {
		return nil, err
	}
	at := filepath.Join(root, filepath.FromSlash(clean))
	real, err := filepath.EvalSymlinks(at)
	if err != nil {
		return nil, fileErr(err)
	}
	if !within(root, real) || s.hostOwn(at) {
		return nil, refuseFile("%q is out of reach", clean)
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return nil, fileErr(err)
	}
	out := []FileEntry{}
	for _, e := range entries {
		rel := path.Join(clean, e.Name())
		if secret(rel) {
			continue
		}
		if e.IsDir() {
			if reachable(s.ReadMasks, rel) && !s.hostOwn(filepath.Join(root, filepath.FromSlash(rel))) {
				out = append(out, FileEntry{Path: rel, Dir: true})
			}
		} else if anyMask(s.ReadMasks, rel) {
			// A file, or a link: where it leads is judged as a read of it would be.
			if _, err := s.Resolve(rel, false); err != nil {
				continue
			}
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil || info.IsDir() {
				continue
			}
			out = append(out, FileEntry{Path: rel, Size: info.Size(), Modified: info.ModTime().UnixMilli()})
		}
		if len(out) >= MaxListed {
			break
		}
	}
	slices.SortStableFunc(out, func(a, b FileEntry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out, nil
}

// Search looks for text in the files the app may read; an empty text finds the files by Glob alone. It gives the
// hits and whether it stopped before the end (too many hits or files).
func (s FileScope) Search(text string, opt SearchOptions) ([]SearchHit, bool, error) {
	if len(s.ReadMasks) == 0 {
		return nil, false, refuseFile("the app has no right to read files (files.read:<mask>)")
	}
	if opt.Glob != "" {
		if why := CheckMask(opt.Glob); why != "" {
			return nil, false, &BridgeError{Code: CodeInvalidParams, Message: "glob " + why}
		}
	}
	if text == "" && opt.Glob == "" {
		return nil, false, &BridgeError{Code: CodeInvalidParams, Message: "say what to look for: a text, a glob or both"}
	}
	limit := opt.Limit
	if limit <= 0 || limit > MaxSearchHits {
		limit = MaxSearchHits
	}
	root, err := s.root()
	if err != nil {
		return nil, false, err
	}
	want := text
	if !opt.Case {
		want = strings.ToLower(text)
	}
	hits := []SearchHit{}
	files, cut := 0, false
	stop := fs.SkipAll
	err = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if e != nil && e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			if secret(rel) || !reachable(s.ReadMasks, rel) || s.hostOwn(p) ||
				(slices.Contains(skippedDirs, e.Name()) && !named(s.ReadMasks, rel)) {
				return fs.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() || !anyMask(s.ReadMasks, rel) || secret(rel) || (opt.Glob != "" && !MatchMask(opt.Glob, rel)) {
			return nil
		}
		if files++; files > maxSearchFiles {
			cut = true
			return stop
		}
		if _, err := s.Resolve(rel, false); err != nil {
			return nil
		}
		if text == "" {
			hits = append(hits, SearchHit{Path: rel})
		} else {
			hits = searchFile(p, rel, want, opt.Case, hits, limit)
		}
		if len(hits) >= limit {
			cut = true
			return stop
		}
		return nil
	})
	if err != nil {
		return nil, false, fileErr(err)
	}
	return hits, cut, nil
}

// searchFile adds the lines of one file that hold the text; a file that is not text, or too big, is passed over.
func searchFile(p, rel, want string, matchCase bool, hits []SearchHit, limit int) []SearchHit {
	f, err := os.Open(p)
	if err != nil {
		return hits
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.Size() > maxSearchBytes {
		return hits
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxSearchBytes))
	if err != nil || !utf8.Valid(raw) {
		return hits
	}
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 64<<10), maxSearchBytes)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		hay := line
		if !matchCase {
			hay = strings.ToLower(line)
		}
		if !strings.Contains(hay, want) {
			continue
		}
		shown := strings.TrimSpace(line)
		if len(shown) > maxHitText {
			shown = strings.ToValidUTF8(shown[:maxHitText], "") + "…"
		}
		hits = append(hits, SearchHit{Path: rel, Line: n, Text: shown})
		if len(hits) >= limit {
			break
		}
	}
	return hits
}
