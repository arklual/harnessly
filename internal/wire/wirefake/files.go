package wirefake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeTree is the folder every fake conversation has: a small project to browse.
var fakeTree = map[string]string{
	"README.md":               "# Harnessly\n\nA place to work with agents.\n",
	"package.json":            "{\n  \"name\": \"harnessly\",\n  \"private\": true\n}\n",
	"src/main.go":             "package main\n\nfunc main() {}\n",
	"src/util.go":             "package main\n\nfunc add(a, b int) int { return a + b }\n",
	"src/components/card.tsx": "export const Card = () => <div className=\"card\" />;\n",
	"docs/plan.md":            "# Plan\n\n1. Browse the files.\n2. Open one.\n",
}

type fakeFiles struct{ s *Service }

// Files is the fake's folders: the same small project for every conversation. The path "forbidden" is refused as outside
// the folder, "binary" as not text, so a page can see each refusal.
func (s *Service) Files() wire.FilesSource { return fakeFiles{s} }

func (f fakeFiles) known(conv string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	_, err := f.s.get(conv)
	return err
}

func (f fakeFiles) Dir(_ context.Context, conv, dir string) (wire.DirListing, error) {
	if err := f.known(conv); err != nil {
		return wire.DirListing{}, err
	}
	dir = strings.Trim(path.Clean("/"+dir), "/")
	if strings.HasPrefix(dir, "forbidden") {
		return wire.DirListing{}, &wire.FileError{Code: wire.RefuseForbidden, Message: "The path is outside the project."}
	}
	seen := map[string]wire.FileEntry{}
	for p := range fakeTree {
		rest := p
		if dir != "" {
			var ok bool
			if rest, ok = strings.CutPrefix(p, dir+"/"); !ok {
				continue
			}
		}
		name, _, nested := strings.Cut(rest, "/")
		kind := wire.FileFile
		if nested {
			kind = wire.FileDirectory
		}
		seen[name] = wire.FileEntry{Name: name, Path: path.Join(dir, name), Kind: kind}
	}
	if len(seen) == 0 {
		return wire.DirListing{}, &wire.FileError{Code: wire.RefuseNotFound, Message: "The file or folder no longer exists."}
	}
	out := wire.DirListing{Path: dir, Entries: []wire.FileEntry{}}
	for _, e := range seen {
		out.Entries = append(out.Entries, e)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.Kind != b.Kind {
			return a.Kind == wire.FileDirectory
		}
		return a.Name < b.Name
	})
	return out, nil
}

func (f fakeFiles) File(_ context.Context, conv, file string) (wire.FileContent, error) {
	if err := f.known(conv); err != nil {
		return wire.FileContent{}, err
	}
	switch p := strings.Trim(path.Clean("/"+file), "/"); {
	case strings.HasPrefix(p, "forbidden"):
		return wire.FileContent{}, &wire.FileError{Code: wire.RefuseForbidden, Message: "The path is outside the project."}
	case strings.HasPrefix(p, "binary"):
		return wire.FileContent{}, &wire.FileError{Code: wire.RefuseUnsupported, Message: "This file is not a text file."}
	default:
		text, ok := f.text(conv, p)
		if !ok {
			return wire.FileContent{}, &wire.FileError{Code: wire.RefuseNotFound, Message: "The file or folder no longer exists."}
		}
		return fileContent(file, text), nil
	}
}

// Save keeps the text in memory for the conversation: the path "readonly" is refused as no access, a stale revision as a
// conflict.
func (f fakeFiles) Save(_ context.Context, conv, file, content, revision string) (wire.FileContent, error) {
	if err := f.known(conv); err != nil {
		return wire.FileContent{}, err
	}
	p := strings.Trim(path.Clean("/"+file), "/")
	if strings.HasPrefix(p, "forbidden") || strings.HasPrefix(p, "readonly") {
		return wire.FileContent{}, &wire.FileError{Code: wire.RefuseForbidden, Message: "No access to this file."}
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	current, ok := f.s.fileText(conv, p)
	if !ok {
		return wire.FileContent{}, &wire.FileError{Code: wire.RefuseNotFound, Message: "The file or folder no longer exists."}
	}
	if fileContent(file, current).Revision != revision {
		return wire.FileContent{}, &wire.FileError{Code: wire.RefuseConflict, Message: "The file changed after it was opened. Copy your edits and load the current version."}
	}
	if f.s.edits == nil {
		f.s.edits = map[string]string{}
	}
	f.s.edits[conv+"\x00"+p] = content
	return fileContent(file, content), nil
}

func (f fakeFiles) text(conv, p string) (string, bool) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.fileText(conv, p)
}

// fileText is a file as the conversation has it: saved, else as the small project has it. The caller holds mu.
func (s *Service) fileText(conv, p string) (string, bool) {
	if text, ok := s.edits[conv+"\x00"+p]; ok {
		return text, true
	}
	text, ok := fakeTree[p]
	return text, ok
}

func fileContent(file, text string) wire.FileContent {
	sum := sha256.Sum256([]byte(text))
	return wire.FileContent{Path: file, Content: text, Revision: hex.EncodeToString(sum[:]), Bytes: len(text)}
}
