package memstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/files"
)

// FileStore is the memory as it is on disk: <parent>/<entry>/memory/<name>.md under Claude Code's projects and the
// folders of synced session copies; the freshest copy of a name wins. It reads as it always did. It writes only when
// WriteDir is set, so a person's memory is never changed by the runtime on its own.
type FileStore struct {
	Parents []string
	// WriteDir is where Put writes; empty means the store is read-only.
	WriteDir string
}

func versionOf(mtimeMs float64) string { return strconv.FormatInt(int64(mtimeMs*1e3), 10) }

func entryOf(f files.MemoryFile, raw string) Note {
	o := files.ParseMemoryNote(f.Name, raw)
	get := func(k string) string {
		for _, kv := range o {
			if kv.K == k {
				s, _ := kv.V.(string)
				return s
			}
		}
		return ""
	}
	return Note{
		Entry: Entry{Name: f.Name, Title: get("title"), Description: get("description"), ModifiedMs: f.MtimeMs, Version: versionOf(f.MtimeMs)},
		Body:  get("body"),
	}
}

func (s FileStore) read(f files.MemoryFile) (Note, error) {
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return Note{}, ErrNotFound
	}
	return entryOf(f, files.DecodeUTF8(raw)), nil
}

func (s FileStore) List(context.Context) ([]Entry, error) {
	var out []Entry
	for _, f := range files.ListMemoryFiles(s.Parents) {
		n, err := s.read(f)
		if err != nil {
			continue
		}
		out = append(out, n.Entry)
	}
	return out, nil
}

func (s FileStore) Get(_ context.Context, name string) (Note, error) {
	if !files.MemoryName(name) {
		return Note{}, ErrNotFound
	}
	for _, f := range files.ListMemoryFiles(s.Parents) {
		if f.Name == name {
			return s.read(f)
		}
	}
	return Note{}, ErrNotFound
}

func (s FileStore) Put(_ context.Context, name, text, ifVersion string) (Entry, error) {
	if !files.MemoryName(name) {
		return Entry{}, ErrBadName
	}
	if s.WriteDir == "" {
		return Entry{}, ErrUnsupported
	}
	if ifVersion != "" {
		if st, err := os.Stat(filepath.Join(s.WriteDir, name)); err == nil {
			t := st.ModTime()
			if versionOf(float64(t.Unix())*1e3+float64(t.Nanosecond())/1e6) != ifVersion {
				return Entry{}, ErrConflict
			}
		}
	}
	if err := os.MkdirAll(s.WriteDir, 0o755); err != nil {
		return Entry{}, fmt.Errorf("memstore: %w", err)
	}
	path := filepath.Join(s.WriteDir, name)
	tmp, err := os.CreateTemp(s.WriteDir, ".note-*")
	if err != nil {
		return Entry{}, fmt.Errorf("memstore: %w", err)
	}
	_, werr := tmp.WriteString(text)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return Entry{}, fmt.Errorf("memstore: write: %v %v", werr, cerr)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return Entry{}, fmt.Errorf("memstore: %w", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		return Entry{}, fmt.Errorf("memstore: %w", err)
	}
	t := st.ModTime()
	ms := float64(t.Unix())*1e3 + float64(t.Nanosecond())/1e6
	return entryOf(files.MemoryFile{Name: name, Path: path, MtimeMs: ms}, text).Entry, nil
}

func (s FileStore) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	return searchIn(ctx, s, query, limit)
}

// searchIn is the text search over a store that has none of its own: it reads every note.
func searchIn(ctx context.Context, s Store, query string, limit int) ([]Hit, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Hit
	for _, e := range list {
		n, err := s.Get(ctx, e.Name)
		if err != nil {
			continue
		}
		snip, ok := snippetOf(n, q)
		if !ok {
			continue
		}
		out = append(out, Hit{Entry: n.Entry, Snippet: snip})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func snippetOf(n Note, q string) (string, bool) {
	if strings.Contains(strings.ToLower(n.Title), q) {
		return n.Title, true
	}
	for _, line := range strings.Split(n.Body, "\n") {
		if strings.Contains(strings.ToLower(line), q) {
			line = strings.TrimSpace(line)
			if r := []rune(line); len(r) > 200 {
				line = string(r[:200])
			}
			return line, true
		}
	}
	return "", false
}
