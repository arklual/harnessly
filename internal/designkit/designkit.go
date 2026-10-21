// Package designkit keeps UI kits of the design library by version: a kit (stylesheets, scripts, fonts, docs) is
// installed once as a folder <kits>/<id>/<version>/ and every project loads it from /kit/<id>/<version>/... instead of
// carrying its own copy. A version never changes after it is installed, so the files are served for good.
//
// The package knows nothing about the library, the runtime or the command line; all three use it.
package designkit

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxArchiveBytes is the largest tarball read, MaxUnpackedBytes and MaxFiles what it may unpack to.
	MaxArchiveBytes  = 128 << 20
	MaxUnpackedBytes = 256 << 20
	MaxFiles         = 5000

	// ManifestFile is looked at, when the kit has it, to make sure the tarball is the kit and version asked for.
	ManifestFile = "design-system.json"
	// DirName is the kits folder inside the runtime's data folder when none is configured.
	DirName = "design-kits"
)

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	versionRe = regexp.MustCompile(`^\d{1,9}\.\d{1,9}\.\d{1,9}(?:-[0-9A-Za-z][0-9A-Za-z.-]{0,40})?$`)

	// ErrExists: the version is installed with other content; versions are immutable, Force replaces it.
	ErrExists = errors.New("this kit version is already installed with different content")
)

// ValidID is whether v can name a kit.
func ValidID(v string) bool { return idRe.MatchString(v) }

// ValidVersion is whether v can name a version: major.minor.patch with an optional pre-release tag.
func ValidVersion(v string) bool { return versionRe.MatchString(v) }

// DefaultDir is the kits folder when none is configured.
func DefaultDir(dataDir string) string { return filepath.Join(dataDir, DirName) }

// Version is one installed version of a kit.
type Version struct {
	Version     string `json:"version"`
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	InstalledAt int64  `json:"installedAt"`
}

// Kit is an installed kit with its versions, the newest first.
type Kit struct {
	ID       string    `json:"id"`
	Versions []Version `json:"versions"`
}

func versionDir(dir, id, version string) string { return filepath.Join(dir, id, version) }
func metaFile(dir, id, version string) string   { return filepath.Join(dir, id, "."+version+".json") }

// List is every installed kit, by id; an empty or missing folder lists nothing.
func List(dir string) []Kit {
	kits := []Kit{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return kits
	}
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		if versions := listVersions(dir, e.Name()); len(versions) > 0 {
			kits = append(kits, Kit{ID: e.Name(), Versions: versions})
		}
	}
	return kits
}

func listVersions(dir, id string) []Version {
	entries, err := os.ReadDir(filepath.Join(dir, id))
	if err != nil {
		return nil
	}
	var out []Version
	for _, e := range entries {
		if !e.IsDir() || !ValidVersion(e.Name()) {
			continue
		}
		v := Version{Version: e.Name()}
		if raw, err := os.ReadFile(metaFile(dir, id, e.Name())); err == nil {
			_ = json.Unmarshal(raw, &v)
			v.Version = e.Name()
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i].Version, out[j].Version) > 0 })
	return out
}

// compareVersions orders major.minor.patch numerically and a release above its own pre-releases.
func compareVersions(a, b string) int {
	core := func(v string) ([3]int, string) {
		main, pre, _ := strings.Cut(v, "-")
		var n [3]int
		for i, part := range strings.SplitN(main, ".", 3) {
			n[i], _ = strconv.Atoi(part)
		}
		return n, pre
	}
	an, ap := core(a)
	bn, bp := core(b)
	for i := range an {
		if an[i] != bn[i] {
			if an[i] < bn[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case ap == bp:
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return strings.Compare(ap, bp)
}

// Files lists an installed version's files as slash paths, or nil when it is not installed.
func Files(dir, id, version string) []string {
	root := Resolve(dir, id, version, nil)
	if root == "" {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// Resolve is a requested file of an installed version as a real path confined to the version's folder, or "" when the
// kit, the version or the file is not there, the path escapes (.., a link) or is not a regular file. With no segments
// it is the version's folder itself.
func Resolve(dir, id, version string, segments []string) string {
	if dir == "" || !ValidID(id) || !ValidVersion(version) {
		return ""
	}
	for _, s := range segments {
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "\\\x00") {
			return ""
		}
	}
	root, err := filepath.EvalSymlinks(versionDir(dir, id, version))
	if err != nil {
		return ""
	}
	if len(segments) == 0 {
		return root
	}
	real, err := filepath.EvalSymlinks(filepath.Join(append([]string{root}, segments...)...))
	if err != nil || !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return ""
	}
	if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return real
}

// Options of Install.
type Options struct {
	// Force replaces a version installed with other content.
	Force bool
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// Result of Install.
type Result struct {
	Version
	// Unchanged: the same tarball was already installed and nothing was written.
	Unchanged bool `json:"unchanged"`
	// Replaced: Force replaced a different install.
	Replaced bool `json:"replaced"`
}

// Install unpacks a .tar or .tar.gz into <dir>/<id>/<version>/. It is all or nothing: the tarball is unpacked into a
// hidden folder next to the target and renamed into place, so a half-installed version is never served. Links,
// devices and paths outside the kit are refused. When the kit has a design-system.json with an id and a version, they
// must be the ones asked for.
func Install(dir, id, version string, src io.Reader, opt Options) (Result, error) {
	if dir == "" {
		return Result{}, errors.New("no kits folder is set")
	}
	if !ValidID(id) {
		return Result{}, fmt.Errorf("%q is not a kit id (lower case letters, digits and dashes)", id)
	}
	if !ValidVersion(version) {
		return Result{}, fmt.Errorf("%q is not a version (major.minor.patch, an optional -tag)", version)
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	kitDir := filepath.Join(dir, id)
	if err := os.MkdirAll(kitDir, 0o755); err != nil {
		return Result{}, err
	}
	staging, err := os.MkdirTemp(kitDir, ".unpack-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(staging)

	hash := sha256.New()
	files, bytes, err := unpack(io.TeeReader(&limitReader{r: src, n: MaxArchiveBytes}, hash), staging)
	if err != nil {
		return Result{}, err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	root, err := contentRoot(staging)
	if err != nil {
		return Result{}, err
	}
	if err := checkManifest(root, id, version); err != nil {
		return Result{}, err
	}
	res := Result{Version: Version{Version: version, Files: files, Bytes: bytes, SHA256: sum, InstalledAt: now().Unix()}}

	target := versionDir(dir, id, version)
	if _, err := os.Lstat(target); err == nil {
		if prev := readMeta(dir, id, version); prev.SHA256 == sum {
			res.Version = prev
			res.Version.Version = version
			res.Unchanged = true
			return res, nil
		}
		if !opt.Force {
			return Result{}, ErrExists
		}
		old, err := os.MkdirTemp(kitDir, ".replaced-")
		if err != nil {
			return Result{}, err
		}
		defer os.RemoveAll(old)
		if err := os.Rename(target, filepath.Join(old, "v")); err != nil {
			return Result{}, err
		}
		res.Replaced = true
	}
	if err := os.Rename(root, target); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return Result{}, err
	}
	meta, _ := json.Marshal(res.Version)
	if err := os.WriteFile(metaFile(dir, id, version), append(meta, '\n'), 0o644); err != nil {
		return Result{}, err
	}
	return res, nil
}

func readMeta(dir, id, version string) Version {
	var v Version
	if raw, err := os.ReadFile(metaFile(dir, id, version)); err == nil {
		_ = json.Unmarshal(raw, &v)
	}
	return v
}

// unpack extracts a tar (gzip or not) into dst and returns how many files and bytes it wrote.
func unpack(src io.Reader, dst string) (files int, total int64, err error) {
	br := bufio.NewReader(src)
	var r io.Reader = br
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return 0, 0, fmt.Errorf("not a gzip archive: %w", err)
		}
		defer zr.Close()
		r = zr
	}
	tr := tar.NewReader(r)
	for entries := 0; ; entries++ {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, 0, fmt.Errorf("not a tar archive: %w", err)
		}
		if entries >= MaxFiles*2 {
			return 0, 0, fmt.Errorf("the archive has more than %d entries", MaxFiles*2)
		}
		name, err := safeName(h.Name)
		if err != nil {
			return 0, 0, err
		}
		if name == "" {
			continue
		}
		out := filepath.Join(dst, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(out, 0o755); err != nil {
				return 0, 0, err
			}
		case tar.TypeReg:
			files++
			if files > MaxFiles {
				return 0, 0, fmt.Errorf("the archive has more than %d files", MaxFiles)
			}
			if total += h.Size; total > MaxUnpackedBytes {
				return 0, 0, fmt.Errorf("the archive unpacks to more than %d MB", MaxUnpackedBytes>>20)
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return 0, 0, err
			}
			f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				return 0, 0, fmt.Errorf("%s: %w", name, err)
			}
			n, err := io.Copy(f, io.LimitReader(tr, h.Size+1))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return 0, 0, err
			}
			if n != h.Size {
				return 0, 0, fmt.Errorf("%s: the archive is damaged", name)
			}
		case tar.TypeXGlobalHeader:
			continue
		default:
			return 0, 0, fmt.Errorf("%s: links and special files are not allowed in a kit", name)
		}
	}
	if files == 0 {
		return 0, 0, errors.New("the archive has no files")
	}
	return files, total, nil
}

// safeName is a tar entry's name as a clean relative slash path; "" for the archive's root itself.
func safeName(raw string) (string, error) {
	if strings.ContainsAny(raw, "\\\x00") {
		return "", fmt.Errorf("%q: not a valid path in a kit", raw)
	}
	name := path.Clean(strings.TrimPrefix(raw, "./"))
	switch {
	case name == "." || name == "":
		return "", nil
	case path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../"):
		return "", fmt.Errorf("%q: a kit's files stay inside the kit", raw)
	}
	return name, nil
}

// contentRoot is the folder holding the kit: the staging folder, or the one folder it holds when the tarball was
// made around a top-level directory (`tar czf kit.tgz dist-browser`).
func contentRoot(staging string) (string, error) {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(staging, entries[0].Name()), nil
	}
	return staging, nil
}

func checkManifest(root, id, version string) error {
	raw, err := os.ReadFile(filepath.Join(root, ManifestFile))
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return fmt.Errorf("%s is not valid JSON", ManifestFile)
	}
	// Any top-level member naming both an id and a version is the kit's own record ("kit", or the kit's brand).
	for _, member := range m {
		var own struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		}
		if json.Unmarshal(member, &own) != nil || own.ID == "" || own.Version == "" {
			continue
		}
		if own.ID != "" && own.ID != id {
			return fmt.Errorf("the archive is the kit %q, not %q", own.ID, id)
		}
		if own.Version != "" && own.Version != version {
			return fmt.Errorf("the archive is version %s, not %s", own.Version, version)
		}
	}
	return nil
}

// limitReader fails, instead of ending quietly, when more than n bytes are read.
type limitReader struct {
	r io.Reader
	n int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	if l.n -= int64(n); l.n < 0 {
		return n, fmt.Errorf("the archive is larger than %d MB", MaxArchiveBytes>>20)
	}
	return n, err
}
