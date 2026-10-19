// Package scripttest replays the scripted cases the previous runtime's modules ran (the golden files of the checkpoint
// store and of the routes over git): the same file and git actions in a temporary folder, and the same dumps of a
// working folder and a shadow repository, so a Go test compares like with like.
package scripttest

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Action is one step on the files: write (text, base64 or a repeated string), link, rm, mkdir, chmod, mtime, or a git
// command run in the folder (with a fixed author and, given a date, fixed dates).
type Action struct {
	Path    string  `json:"path"`
	PathB64 string  `json:"pathB64"`
	Write   *string `json:"write"`
	B64     *string `json:"b64"`
	Repeat  *string `json:"repeat"`
	Count   int     `json:"count"`
	Mode    *int    `json:"mode"`
	Link    *string `json:"link"`
	Rm      bool    `json:"rm"`
	Mkdir   bool    `json:"mkdir"`
	Chmod   *int    `json:"chmod"`
	Mtime   *int64  `json:"mtime"`
	Git     []string
	Date    string
}

// World is a script's temporary folder; HOME and XDG_CONFIG_HOME point into it, so git reads no person's config.
type World struct {
	t      testing.TB
	Root   string
	hashes map[string]string
}

// New is a fresh world; roots are the working folders a script names (their hashes are scrubbed too).
func New(t testing.TB, roots []string) *World {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"home", "data", "xdg"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o777)
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	w := &World{t: t, Root: root, hashes: map[string]string{}}
	for _, r := range roots {
		w.NoteRoot(r)
	}
	return w
}

// NoteRoot creates a working folder and has the hash of its real path scrubbed as #<cwd>#.
func (w *World) NoteRoot(cwd string) {
	p := filepath.Join(w.Root, cwd)
	_ = os.MkdirAll(p, 0o777)
	real, _ := filepath.EvalSymlinks(p)
	sum := sha1.Sum([]byte(real))
	w.hashes[hex.EncodeToString(sum[:])[:10]] = "#" + cwd + "#"
}

// Scrub writes the temporary folder as $ROOT and the noted hashes by name.
func (w *World) Scrub(s string) string {
	s = strings.ReplaceAll(s, w.Root, "$ROOT")
	for h, name := range w.hashes {
		s = strings.ReplaceAll(s, h, name)
	}
	return s
}

// GitEnv is the environment without GIT_ variables, plus extra.
func GitEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return append(env, extra...)
}

// Git runs git in dir and fails the test when it fails.
func (w *World) Git(dir string, input []byte, args []string, extra ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = GitEnv(extra...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		w.t.Fatalf("git %v: %v %s", args, err, errb.String())
	}
	return string(out)
}

// Apply runs file and git actions; a file action that fails is skipped from where it failed, as gen.mjs skipped it.
func (w *World) Apply(actions []Action) {
	for _, a := range actions {
		if a.Git != nil {
			extra := []string{"GIT_AUTHOR_NAME=Ann Example", "GIT_AUTHOR_EMAIL=ann@example.com", "GIT_COMMITTER_NAME=Ann Example", "GIT_COMMITTER_EMAIL=ann@example.com"}
			if a.Date != "" {
				extra = append(extra, "GIT_AUTHOR_DATE="+a.Date+" +0000", "GIT_COMMITTER_DATE="+a.Date+" +0000")
			}
			w.Git(filepath.Join(w.Root, a.Path), nil, a.Git, extra...)
			continue
		}
		_ = w.fileAction(a)
	}
}

func (w *World) fileAction(a Action) error {
	p := filepath.Join(w.Root, a.Path)
	if a.PathB64 != "" {
		raw, _ := base64.StdEncoding.DecodeString(a.PathB64)
		p = w.Root + "/" + string(raw)
	}
	switch {
	case a.Write != nil:
		if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
			return err
		}
		data := []byte(*a.Write)
		if a.B64 != nil {
			data, _ = base64.StdEncoding.DecodeString(*a.B64)
		} else if a.Repeat != nil {
			data = []byte(strings.Repeat(*a.Repeat, a.Count))
		}
		if err := os.RemoveAll(p); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o666); err != nil {
			return err
		}
		if a.Mode != nil {
			return os.Chmod(p, os.FileMode(*a.Mode))
		}
	case a.Link != nil:
		if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
			return err
		}
		if err := os.RemoveAll(p); err != nil {
			return err
		}
		return os.Symlink(*a.Link, p)
	case a.Rm:
		return os.RemoveAll(p)
	case a.Mkdir:
		return os.MkdirAll(p, 0o777)
	case a.Chmod != nil:
		return os.Chmod(p, os.FileMode(*a.Chmod))
	case a.Mtime != nil:
		tm := time.UnixMilli(*a.Mtime)
		return os.Chtimes(p, tm, tm)
	default:
		w.t.Fatalf("unknown action %+v", a)
	}
	return nil
}

func pathEntry(name []byte) map[string]any {
	if utf8.Valid(name) {
		return map[string]any{"path": string(name)}
	}
	return map[string]any{"pathB64": base64.StdEncoding.EncodeToString(name)}
}

// DumpTree is every entry of a working folder but .git folders, by raw name: folders, files by mode and content hash,
// links by target.
func (w *World) DumpTree(dir string) []any {
	out := []any{}
	var walk func(abs, rel string)
	walk = func(abs, rel string) {
		entries, _ := os.ReadDir(abs)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			if name == ".git" {
				continue
			}
			relName := name
			if rel != "" {
				relName = rel + "/" + name
			}
			full := abs + "/" + name
			st, _ := os.Lstat(full)
			e := pathEntry([]byte(relName))
			switch {
			case st.Mode()&os.ModeSymlink != 0:
				target, _ := os.Readlink(full)
				e["link"] = base64.StdEncoding.EncodeToString([]byte(target))
			case st.IsDir():
				e["dir"] = true
				out = append(out, e)
				walk(full, relName)
				continue
			default:
				raw, _ := os.ReadFile(full)
				sum := sha1.Sum(raw)
				e["mode"] = int(st.Mode().Perm())
				e["sha"] = hex.EncodeToString(sum[:])
			}
			out = append(out, e)
		}
	}
	walk(dir, "")
	return out
}

// DumpRepo is a shadow repository: its refs (the namespace written as NS; commits by tree and subject, other objects by
// id), its own files and leftover temporary files.
func (w *World) DumpRepo(dir, ns string) any {
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	refs := []any{}
	out := w.Git(w.Root, nil, []string{"for-each-ref", "--format=%(refname)%00%(objecttype)%00%(tree)%00%(contents:subject)%00%(parent)%00%(objectname)"}, "GIT_DIR="+dir)
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		name := strings.Replace(f[0], "refs/"+ns+"/", "refs/NS/", 1)
		if f[1] == "commit" {
			parents := 0
			if f[4] != "" {
				parents = len(strings.Split(f[4], " "))
			}
			refs = append(refs, map[string]any{"ref": name, "tree": f[2], "subject": f[3], "parents": parents})
		} else {
			refs = append(refs, map[string]any{"ref": name, "type": f[1], "object": f[5]})
		}
	}
	files := map[string]any{}
	for _, f := range []string{"HEAD", "config", "root", "info/exclude", "info/attributes", "rewinds.json", "description"} {
		if raw, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			files[f] = w.Scrub(string(raw))
		} else {
			files[f] = nil
		}
	}
	leftovers := []any{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			leftovers = append(leftovers, e.Name())
		}
	}
	return map[string]any{"refs": refs, "files": files, "leftovers": leftovers}
}
