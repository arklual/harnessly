package designkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A kit's draft is its one working copy: a folder people and agents edit (the library keeps it as a project of its
// own), started from a published version. Projects switched to the draft see its files as they change; publishing
// the draft copies it into a new version, which never changes after that. The draft's record says which folder it is
// and which version it started from.

// MaxChangesListed is how many changed files a version's record names; the count is kept in full.
const MaxChangesListed = 200

// ErrNoVersion: the kit has no published version to start from or to compare with.
var ErrNoVersion = errors.New("this kit has no installed version")

// Draft is the record of a kit's draft.
type Draft struct {
	// Project is the library project holding the draft's files (its project folder is the kit's root).
	Project string `json:"project"`
	// Base is the version the draft started from or was last published as.
	Base      string `json:"base"`
	CreatedAt int64  `json:"createdAt"`
	// Skip are top-level names of the draft's folder that are the project's, not the kit's.
	Skip []string `json:"skip,omitempty"`
	// Dir is the project's folder: the kit screen reads the draft as the version DraftVersion from there.
	Dir string `json:"dir,omitempty"`
}

// DraftVersion names the kit's draft where a version goes: the kit screen and /kit/<id>/draft/... read the draft's
// folder as if it were an installed version. It is never installed, listed or published under this name.
const DraftVersion = "draft"

// validRef is a version or the draft.
func validRef(v string) bool { return v == DraftVersion || ValidVersion(v) }

// Change is one file a draft has changed against its base.
type Change struct {
	Path string `json:"path"`
	// Kind is "added", "changed" or "removed".
	Kind string `json:"kind"`
}

func draftFile(dir, id string) string { return filepath.Join(dir, id, ".draft.json") }

// ReadDraft is the kit's draft, when it has one.
func ReadDraft(dir, id string) (Draft, bool) {
	if !ValidID(id) {
		return Draft{}, false
	}
	raw, err := os.ReadFile(draftFile(dir, id))
	if err != nil {
		return Draft{}, false
	}
	var d Draft
	if json.Unmarshal(raw, &d) != nil || d.Project == "" || !ValidVersion(d.Base) {
		return Draft{}, false
	}
	return d, true
}

// WriteDraft records the kit's draft.
func WriteDraft(dir, id string, d Draft) error {
	if !ValidID(id) {
		return fmt.Errorf("%q is not a kit id", id)
	}
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(d, "", "  ")
	tmp := draftFile(dir, id) + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, draftFile(dir, id))
}

// ClearDraft forgets the kit's draft; its folder is the caller's to put away.
func ClearDraft(dir, id string) error {
	if !ValidID(id) {
		return nil
	}
	err := os.Remove(draftFile(dir, id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Latest is the newest installed version of a kit.
func Latest(dir, id string) (Version, bool) {
	if !ValidID(id) {
		return Version{}, false
	}
	versions := listVersions(dir, id)
	if len(versions) == 0 {
		return Version{}, false
	}
	return versions[0], true
}

// Versions are the installed versions of a kit, the newest first.
func Versions(dir, id string) []Version {
	if !ValidID(id) {
		return nil
	}
	return listVersions(dir, id)
}

// Root is the folder of an installed version, or "" when it is not installed.
func Root(dir, id, version string) string {
	if !ValidID(id) || !ValidVersion(version) {
		return ""
	}
	p := versionDir(dir, id, version)
	if st, err := os.Lstat(p); err != nil || !st.IsDir() {
		return ""
	}
	return p
}

// Compare orders two versions: below zero when a is older than b.
func Compare(a, b string) int { return compareVersions(a, b) }

// Bump is the version after v: "patch", "minor" (the default) or "major"; a pre-release tag is dropped.
func Bump(v, part string) (string, error) {
	if !ValidVersion(v) {
		return "", fmt.Errorf("%q is not a version", v)
	}
	main, _, _ := strings.Cut(v, "-")
	parts := strings.SplitN(main, ".", 3)
	n := [3]int{}
	for i, p := range parts {
		n[i], _ = strconv.Atoi(p)
	}
	switch part {
	case "patch":
		n[2]++
	case "major":
		n = [3]int{n[0] + 1, 0, 0}
	case "", "minor":
		n = [3]int{n[0], n[1] + 1, 0}
	default:
		return "", fmt.Errorf("%q is not patch, minor or major", part)
	}
	return fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2]), nil
}

// MountOf is the folder of a kit's root a project sees as ds/<mount>/: a theme (themes/<mount>/) when the kit has
// themes, else the whole kit when the mount is the kit's own id. "" when the kit has no such folder.
func MountOf(root, kitID, mount string) string {
	if root == "" || !ValidID(mount) {
		return ""
	}
	theme := filepath.Join(root, "themes", mount)
	if st, err := os.Stat(theme); err == nil && st.IsDir() {
		return theme
	}
	if mount == kitID {
		return root
	}
	return ""
}

// Mounts are the names a kit's root offers as ds/<name>/: its themes, or its own id.
func Mounts(root, kitID string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "themes"))
	if err != nil {
		return []string{kitID}
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && ValidID(e.Name()) {
			out = append(out, e.Name())
		}
	}
	if len(out) == 0 {
		return []string{kitID}
	}
	return out
}

// skipped: a name of the tree a kit never carries (the library's own folders, the draft project's own files).
func skipped(rel string, skip map[string]bool) bool {
	first, _, _ := strings.Cut(rel, "/")
	if skip[first] {
		return true
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".git" || part == "node_modules" || part == ".thumbs" || part == ".claims" || part == ".check" || part == ".trash" {
			return true
		}
	}
	return false
}

func skipSet(names []string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// walkFiles lists the regular files of a tree by slash path, sorted; links and other kinds are left out.
func walkFiles(root string, skip map[string]bool) (map[string]fs.FileInfo, error) {
	out := map[string]fs.FileInfo{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel := filepath.ToSlash(p[len(root)+1:])
		if skipped(rel, skip) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && rel != SealFile {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if len(out) >= MaxFiles {
				return fmt.Errorf("more than %d files", MaxFiles)
			}
			out[rel] = info
		}
		return nil
	})
	return out, err
}

func sameFile(a, b string, ai, bi fs.FileInfo) bool {
	if ai.Size() != bi.Size() {
		return false
	}
	// A file the draft copied and never wrote has the time of its source: the same, without reading both.
	if ai.ModTime().Equal(bi.ModTime()) {
		return true
	}
	x, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	y, err := os.ReadFile(b)
	return err == nil && bytes.Equal(x, y)
}

// Changes are the files of draft that differ from base, by path; skip names top-level entries of draft to leave out.
func Changes(base, draft string, skip []string) ([]Change, error) {
	set := skipSet(skip)
	was, err := walkFiles(base, nil)
	if err != nil {
		return nil, err
	}
	now, err := walkFiles(draft, set)
	if err != nil {
		return nil, err
	}
	var out []Change
	for rel, info := range now {
		prev, ok := was[rel]
		switch {
		case !ok:
			out = append(out, Change{rel, "added"})
		case !sameFile(filepath.Join(base, filepath.FromSlash(rel)), filepath.Join(draft, filepath.FromSlash(rel)), prev, info):
			out = append(out, Change{rel, "changed"})
		}
	}
	for rel := range was {
		if _, ok := now[rel]; !ok {
			out = append(out, Change{rel, "removed"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// CopyTree copies the regular files and folders of src into dst (made), leaving out skip's top-level names; links
// are not followed. It reports the files and bytes copied.
func CopyTree(src, dst string, skip []string) (int, int64, error) {
	set := skipSet(skip)
	files, err := walkFiles(src, set)
	if err != nil {
		return 0, 0, err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, 0, err
	}
	var total int64
	for rel, info := range files {
		total += info.Size()
		if total > MaxUnpackedBytes {
			return 0, 0, fmt.Errorf("more than %d bytes", int64(MaxUnpackedBytes))
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, 0, err
		}
		if err := copyFile(filepath.Join(src, filepath.FromSlash(rel)), target); err != nil {
			return 0, 0, err
		}
	}
	return len(files), total, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// The copy keeps the source's time: Changes tells an untouched file by its size and time without reading it.
	if st, err := in.Stat(); err == nil {
		_ = os.Chtimes(dst, st.ModTime(), st.ModTime())
	}
	return nil
}

// PublishOptions are what a published version says about itself.
type PublishOptions struct {
	// Notes is the short "what changed" of the version.
	Notes string
	// Changes are the files changed against the version the draft started from.
	Changes []Change
	// From is the version the draft started from.
	From string
	Now  func() time.Time
}

// Published is a version's record with what the publishing said.
type Published struct {
	Version
	Notes        string   `json:"notes,omitempty"`
	From         string   `json:"from,omitempty"`
	Changed      []Change `json:"changed,omitempty"`
	ChangedCount int      `json:"changedCount,omitempty"`
}

// Publish copies a draft's folder into the new version <dir>/<id>/<version>/, all or nothing, and records what
// changed. A version that is already installed is never replaced.
func Publish(dir, id, version, src string, skip []string, opt PublishOptions) (Published, error) {
	if !ValidID(id) {
		return Published{}, fmt.Errorf("%q is not a kit id", id)
	}
	if !ValidVersion(version) {
		return Published{}, fmt.Errorf("%q is not a version", version)
	}
	if Root(dir, id, version) != "" {
		return Published{}, ErrExists
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	kitDir := filepath.Join(dir, id)
	if err := os.MkdirAll(kitDir, 0o755); err != nil {
		return Published{}, err
	}
	staging, err := os.MkdirTemp(kitDir, ".publish-")
	if err != nil {
		return Published{}, err
	}
	defer os.RemoveAll(staging)
	root := filepath.Join(staging, "v")
	files, size, err := CopyTree(src, root, skip)
	if err != nil {
		return Published{}, err
	}
	if err := rewriteIdentity(root, id, version); err != nil {
		return Published{}, err
	}
	hashed, err := hashTree(root, nil)
	if err != nil {
		return Published{}, err
	}
	sum := treeSum(hashed)
	res, err := commit(dir, id, version, root, Result{Version: Version{Version: version, Files: files, Bytes: size, SHA256: sum, InstalledAt: now().Unix()}}, false)
	if err != nil {
		return Published{}, err
	}
	p := Published{Version: res.Version, Notes: opt.Notes, From: opt.From, ChangedCount: len(opt.Changes)}
	p.Changed = opt.Changes
	if len(p.Changed) > MaxChangesListed {
		p.Changed = p.Changed[:MaxChangesListed]
	}
	raw, _ := json.Marshal(p)
	if err := os.WriteFile(metaFile(dir, id, version), append(raw, '\n'), 0o644); err != nil {
		return Published{}, err
	}
	return p, nil
}

// ReadPublished is a version's record with its notes; a version installed from an archive has none.
func ReadPublished(dir, id, version string) (Published, bool) {
	if Root(dir, id, version) == "" {
		return Published{}, false
	}
	var p Published
	if raw, err := os.ReadFile(metaFile(dir, id, version)); err == nil {
		_ = json.Unmarshal(raw, &p)
	}
	p.Version.Version = version
	return p, true
}

// StampVersion writes the kit's id and version into kit.json of a folder (a draft just published as that version), the
// way Publish writes them into the version: the draft and its version then hold the same bytes.
func StampVersion(root, id, version string) error { return rewriteIdentity(root, id, version) }
