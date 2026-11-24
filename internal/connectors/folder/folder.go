// Package folder is the folder connector (docs/connectors.md, «Folder»): a folder of the machine inside the person's
// home, an Obsidian vault among them, watched for files created, changed and removed. Changes become file.changed
// envelopes (the relative path, what changed, the size: never the text), debounced and joined into batches; an
// Obsidian vault also gives note.created for a new note and note.task for a new open task with a date. The agent
// reads the changes of a period, a file and a search of the folder, all marked as data, not instructions. There is no
// action outward: writing into the folder is the agent's ordinary work with files.
package folder

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// ID names the connector.
const ID = "folder"

// Event types.
const (
	EventChanged     = "file.changed"
	EventNoteCreated = "note.created"
	EventNoteTask    = "note.task"
)

// Presets.
const (
	PresetPlain    = "plain"
	PresetObsidian = "obsidian"
)

const (
	// maxFiles is the most files a folder may hold: a bigger one is refused at connect and stops its watch.
	maxFiles = 20000
	// maxRead is the largest file the agent reads or a task is looked for in.
	maxRead = 512 << 10
	// hashMax: files up to this size are hashed, so a touch without a change gives no event.
	hashMax = 2 << 20
)

// Home is where a watched folder must be; the runtime may set it to the person's home of a profile.
var Home = os.UserHomeDir

// ignoredDirs are never watched nor listed, at any depth.
var ignoredDirs = map[string]bool{".git": true, "node_modules": true, ".hg": true, ".svn": true, "__pycache__": true, ".venv": true}

// obsidianIgnored are the vault's own folders.
var obsidianIgnored = map[string]bool{".obsidian": true, ".trash": true}

type folderConn struct{}

func init() { connectors.Register(folderConn{}) }

func (folderConn) Spec() connectors.Spec {
	return connectors.Spec{
		ID:    ID,
		Kind:  connectors.Files,
		Inbox: "folder", // HRN-1727: the inbox takes a folder's changes (harnessly-inbox 0.10.12)
		Title: connectors.T("Folder", "Папка"),
		Summary: connectors.T("A folder of this machine, an Obsidian vault among them: new and changed files, new tasks",
			"Папка этой машины, в том числе хранилище Obsidian: новые и изменённые файлы, новые задачи"),
		Fields: []connectors.Field{
			{ID: "path", Type: connectors.FieldPath, Label: connectors.T("Folder", "Папка"), Placeholder: "~/Notes",
				Hint: connectors.T("Inside your home folder", "Внутри домашней папки")},
			{ID: "preset", Type: connectors.FieldChoice, Default: PresetPlain, Label: connectors.T("Kind", "Вид"),
				Choices: []connectors.Choice{
					{Value: PresetPlain, Label: connectors.T("Any folder", "Любая папка")},
					{Value: PresetObsidian, Label: connectors.T("Obsidian vault", "Хранилище Obsidian")},
				}},
			{ID: "mask", Type: connectors.FieldText, Optional: true, Placeholder: "*.md, *.txt",
				Label: connectors.T("Files", "Файлы"),
				Hint:  connectors.T("Masks by name, comma-separated; all files when empty", "Маски имён через запятую; пусто — все файлы")},
		},
		Events: []string{EventChanged, EventNoteCreated, EventNoteTask},
		Tools: []connectors.Tool{
			{Name: "changes", Summary: "the changes of a period: when, what, path, size", Args: []connectors.Arg{
				{Name: "since", Summary: "a time (2026-11-15T10:00:00Z) or how long ago (24h), 24h by default"},
				{Name: "until", Summary: "a time, now by default"},
				{Name: "limit", Summary: "how many, 100 by default"}}},
			{Name: "file", Summary: "one file's text", Args: []connectors.Arg{{Name: "path", Summary: "relative to the folder", Required: true}}},
			{Name: "search", Summary: "lines of the folder's files with the words", Args: []connectors.Arg{
				{Name: "query", Summary: "the words, case does not matter", Required: true},
				{Name: "limit", Summary: "how many lines, 50 by default"}}},
		},
	}
}

// root is the folder, its symlinks resolved, checked to be a folder inside the home.
func root(in connectors.Instance) (string, error) {
	p := strings.TrimSpace(in.Config["path"])
	home, err := Home()
	if err != nil || home == "" {
		return "", connectors.Sayf("The home folder of this machine is not known")
	}
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", connectors.Sayf("The home folder of this machine is not known")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(home, p)
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(p))
	if err != nil {
		return "", connectors.Sayf("There is no such folder")
	}
	if !inside(realHome, real) {
		return "", connectors.Sayf("The folder must be inside your home folder")
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return "", connectors.Sayf("This is not a folder")
	}
	return real, nil
}

// inside says p is base or under it (both clean, symlinks resolved).
func inside(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../") && !filepath.IsAbs(rel)
}

func obsidian(in connectors.Instance) bool { return in.Config["preset"] == PresetObsidian }

// masks are the name patterns of the files watched; nil is every file. Obsidian without a mask watches notes.
func masks(in connectors.Instance) []string {
	raw := strings.TrimSpace(in.Config["mask"])
	if raw == "" && obsidian(in) {
		raw = "*.md"
	}
	var out []string
	for _, m := range strings.Split(raw, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

func (folderConn) Check(_ context.Context, in connectors.Instance) (connectors.Account, error) {
	for _, m := range masks(in) {
		if _, err := filepath.Match(m, "x"); err != nil {
			return connectors.Account{}, connectors.Sayf("The mask %q is not valid", m)
		}
	}
	r, err := root(in)
	if err != nil {
		return connectors.Account{}, err
	}
	if _, err := scan(r, in); err != nil {
		return connectors.Account{}, err
	}
	label := r
	if home, err := Home(); err == nil {
		if real, err := filepath.EvalSymlinks(home); err == nil && inside(real, r) {
			rel, _ := filepath.Rel(real, r)
			label = "~/" + filepath.ToSlash(rel)
			if rel == "." {
				label = "~"
			}
		}
	}
	return connectors.Account{Label: label}, nil
}

// resolve is a relative path of the folder as a real path inside it, or ErrNotFound.
func resolve(r, rel string) (string, error) {
	rel = filepath.FromSlash(strings.TrimSpace(rel))
	if rel == "" || filepath.IsAbs(rel) {
		return "", connectors.ErrBadArgs
	}
	real, err := filepath.EvalSymlinks(filepath.Join(r, rel))
	if err != nil || !inside(r, real) {
		return "", connectors.ErrNotFound
	}
	return real, nil
}

func (folderConn) Read(_ context.Context, in connectors.Instance, tool string, args map[string]string) (connectors.Result, error) {
	r, err := root(in)
	if err != nil {
		return connectors.Result{}, err
	}
	from := "folder " + in.Name
	switch tool {
	case "changes":
		now := time.Now()
		since, until := now.Add(-24*time.Hour), now
		if v := args["since"]; v != "" {
			if since, err = when(v, now); err != nil {
				return connectors.Result{}, err
			}
		}
		if v := args["until"]; v != "" {
			if until, err = when(v, now); err != nil {
				return connectors.Result{}, err
			}
		}
		limit := atoi(args["limit"], 100, 1000)
		list := journal{dir: in.Dir}.between(since, until, limit)
		if len(list) == 0 {
			return connectors.Result{Text: "no changes in the period"}, nil
		}
		var b strings.Builder
		for _, c := range list {
			fmt.Fprintf(&b, "%s  %s  %s", c.At.UTC().Format(time.RFC3339), c.Change, c.Path)
			if c.Change != changeDeleted {
				fmt.Fprintf(&b, "  %d bytes", c.Size)
			}
			b.WriteString("\n")
		}
		return connectors.Result{Text: fmt.Sprintf("%d changes, newest first", len(list)), Data: b.String(), From: from}, nil
	case "file":
		p, err := resolve(r, args["path"])
		if err != nil {
			return connectors.Result{}, err
		}
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			return connectors.Result{}, connectors.ErrNotFound
		}
		f, err := os.Open(p)
		if err != nil {
			return connectors.Result{}, connectors.ErrNotFound
		}
		defer f.Close()
		buf := make([]byte, maxRead)
		n, _ := f.Read(buf)
		text := string(buf[:n])
		// A cut at maxRead may split the last letter.
		for i := 0; i < 3 && n == maxRead && !utf8.ValidString(text); i++ {
			text = text[:len(text)-1]
		}
		if !utf8.ValidString(text) {
			return connectors.Result{Text: fmt.Sprintf("%s is not text (%d bytes)", args["path"], fi.Size())}, nil
		}
		note := fmt.Sprintf("%s, %d bytes", args["path"], fi.Size())
		if fi.Size() > maxRead {
			note += fmt.Sprintf(", the first %d shown", maxRead)
		}
		return connectors.Result{Text: note, Data: text, From: from + ": " + args["path"]}, nil
	case "search":
		q := strings.ToLower(strings.TrimSpace(args["query"]))
		if q == "" {
			return connectors.Result{}, connectors.ErrBadArgs
		}
		limit := atoi(args["limit"], 50, 500)
		hits, err := search(r, in, q, limit)
		if err != nil {
			return connectors.Result{}, err
		}
		if len(hits) == 0 {
			return connectors.Result{Text: "nothing found"}, nil
		}
		return connectors.Result{Text: fmt.Sprintf("%d lines", len(hits)), Data: strings.Join(hits, "\n"), From: from}, nil
	}
	return connectors.Result{}, connectors.ErrUnknownTool
}

func atoi(v string, def, most int) int {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return min(n, most)
}

// when is a time or how long ago.
func when(v string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t, nil
	}
	if d, err := time.ParseDuration(v); err == nil {
		return now.Add(-d), nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(v, "d")); err == nil && strings.HasSuffix(v, "d") {
		return now.AddDate(0, 0, -n), nil
	}
	return time.Time{}, fmt.Errorf("%w: since and until are a time or a duration like 24h", connectors.ErrBadArgs)
}

// search finds the lines with q in the watched files.
func search(r string, in connectors.Instance, q string, limit int) ([]string, error) {
	snap, err := scan(r, in)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rel := range sortedPaths(snap) {
		if snap[rel].Size > maxRead {
			continue
		}
		f, err := os.Open(filepath.Join(r, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), maxRead)
		n := 0
		for sc.Scan() {
			n++
			line := sc.Text()
			if strings.Contains(strings.ToLower(line), q) {
				if len(line) > 300 {
					line = line[:300] + "…"
				}
				out = append(out, fmt.Sprintf("%s:%d: %s", rel, n, line))
				if len(out) == limit {
					f.Close()
					return out, nil
				}
			}
		}
		f.Close()
	}
	return out, nil
}
