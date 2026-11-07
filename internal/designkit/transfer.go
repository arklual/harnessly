package designkit

// Moving a kit between machines: Export packs one installed version, with everything inside it, into one tar.gz that
// starts with a manifest (kit-export.json: every file with its size and sha256, what is license-restricted, what
// other kits it needs). Import checks that manifest against the bytes before anything is installed, never overwrites
// a version that holds other content, and tells what differs instead.

import (
	"archive/tar"
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
	"sort"
	"strings"
	"time"
)

const (
	// ExportManifest is the manifest's name, the first entry of an export.
	ExportManifest = "kit-export.json"
	// KitFile is the kit's own record (layers, themes, fonts with licenses, other kits it needs).
	KitFile = "kit.json"
	// SourcesDir holds the heavy originals; an export takes it only when asked.
	SourcesDir = "sources"

	exportSchema = 1
)

// ErrRestricted: the kit has license-restricted files and the export was not confirmed.
var ErrRestricted = errors.New("the kit has files with a restricted license")

// ExportFile is one file of an export.
type ExportFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Restricted is a file whose license limits who may have it.
type Restricted struct {
	Path   string `json:"path"`
	Font   string `json:"font,omitempty"`
	Holder string `json:"holder,omitempty"`
	Reason string `json:"reason"`
}

// Require is another kit this one needs; import wants it installed.
type Require struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// Manifest is kit-export.json.
type Manifest struct {
	Schema      int          `json:"schema"`
	ID          string       `json:"id"`
	Version     string       `json:"version"`
	ExportedAt  string       `json:"exportedAt"`
	Upstream    []string     `json:"upstreamTested,omitempty"`
	Requires    []Require    `json:"requires,omitempty"`
	Restricted  []Restricted `json:"restricted,omitempty"`
	WithSources bool         `json:"withSources"`
	Files       []ExportFile `json:"files"`
	// SHA256 is the hash of the sorted "path size sha256" lines: one number for the whole content.
	SHA256 string `json:"sha256"`
}

// ExportOptions of BuildManifest.
type ExportOptions struct {
	WithSources bool
	// AcceptRestricted confirms exporting files with a restricted license.
	AcceptRestricted bool
	Now              func() time.Time
}

// kitRecord is the part of kit.json an export reads.
type kitRecord struct {
	Upstream struct {
		Tested []string `json:"tested"`
	} `json:"upstream"`
	Requires []Require `json:"requires"`
	Fonts    []struct {
		ID      string `json:"id"`
		Family  string `json:"family"`
		Prefix  string `json:"prefix"`
		License struct {
			Kind   string `json:"kind"`
			Holder string `json:"holder"`
		} `json:"license"`
	} `json:"fonts"`
}

func treeSum(files []ExportFile) string {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s %d %s\n", f.Path, f.Size, f.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashFile(p string) (int64, string, error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return n, hex.EncodeToString(h.Sum(nil)), err
}

// hashTree lists every regular file under root with its size and sha256, sorted by path. skip drops paths.
func hashTree(root string, skip func(rel string) bool) ([]ExportFile, error) {
	var out []ExportFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if skip != nil && skip(rel) {
			return nil
		}
		n, sum, err := hashFile(p)
		if err != nil {
			return err
		}
		out = append(out, ExportFile{Path: rel, Size: n, SHA256: sum})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func readKitRecord(root string) kitRecord {
	var k kitRecord
	if raw, err := os.ReadFile(filepath.Join(root, KitFile)); err == nil {
		_ = json.Unmarshal(raw, &k)
	}
	return k
}

// restrictedFiles marks the files of fonts whose kit.json license is restricted: files in a fonts folder whose name
// starts with the font's prefix (or family, or id).
func restrictedFiles(k kitRecord, files []ExportFile) []Restricted {
	var out []Restricted
	for _, f := range k.Fonts {
		if f.License.Kind != "restricted" {
			continue
		}
		prefix := strings.ToLower(firstNonEmpty(f.Prefix, f.Family, f.ID))
		for _, file := range files {
			dir, base := path.Split(file.Path)
			if (strings.HasPrefix(dir, "fonts/") || strings.Contains(dir, "/fonts/")) && strings.HasPrefix(strings.ToLower(base), prefix) {
				out = append(out, Restricted{Path: file.Path, Font: f.Family, Holder: f.License.Holder,
					Reason: "license of " + firstNonEmpty(f.Family, f.ID) + ": restricted" + holder(f.License.Holder)})
			}
		}
	}
	return out
}

func holder(h string) string {
	if h == "" {
		return ""
	}
	return " (" + h + ")"
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// BuildManifest reads an installed version and lists what its export holds. When the kit has restricted files and the
// export is not confirmed it returns the manifest together with ErrRestricted, so the caller can show what is limited.
func BuildManifest(dir, id, version string, opt ExportOptions) (*Manifest, error) {
	root := Resolve(dir, id, version, nil)
	if root == "" {
		return nil, fmt.Errorf("%s %s is not installed", id, version)
	}
	files, err := hashTree(root, func(rel string) bool {
		return rel == ExportManifest || (!opt.WithSources && (rel == SourcesDir || strings.HasPrefix(rel, SourcesDir+"/")))
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("the kit has no files")
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	rec := readKitRecord(root)
	m := &Manifest{Schema: exportSchema, ID: id, Version: version, ExportedAt: now().UTC().Format(time.RFC3339),
		Upstream: rec.Upstream.Tested, Requires: rec.Requires, Restricted: restrictedFiles(rec, files),
		WithSources: opt.WithSources, Files: files, SHA256: treeSum(files)}
	if len(m.Restricted) > 0 && !opt.AcceptRestricted {
		return m, ErrRestricted
	}
	return m, nil
}

// WriteArchive writes the manifest and then the files it lists as a tar.gz. Files are read again and checked against
// the manifest, so a kit that changed under the export is refused rather than packed half-and-half.
func WriteArchive(dir string, m *Manifest, w io.Writer) error {
	root := Resolve(dir, m.ID, m.Version, nil)
	if root == "" {
		return fmt.Errorf("%s %s is not installed", m.ID, m.Version)
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	mod := time.Unix(0, 0)
	if err := tw.WriteHeader(&tar.Header{Name: ExportManifest, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(raw)), ModTime: mod}); err != nil {
		return err
	}
	if _, err := tw.Write(raw); err != nil {
		return err
	}
	for _, f := range m.Files {
		src, err := os.Open(filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil {
			return err
		}
		err = writeEntry(tw, src, f, mod)
		_ = src.Close()
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeEntry(tw *tar.Writer, src io.Reader, f ExportFile, mod time.Time) error {
	if err := tw.WriteHeader(&tar.Header{Name: f.Path, Typeflag: tar.TypeReg, Mode: 0o644, Size: f.Size, ModTime: mod}); err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tw, h), io.LimitReader(src, f.Size+1))
	if err != nil {
		return err
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("%s changed while the kit was being exported", f.Path)
	}
	return nil
}

// Diff is how an incoming kit differs from the installed one with the same id and version.
type Diff struct {
	Added   []string `json:"added"`
	Changed []string `json:"changed"`
	Removed []string `json:"removed"`
}

// Empty is whether the two are the same.
func (d Diff) Empty() bool { return len(d.Added)+len(d.Changed)+len(d.Removed) == 0 }

// ConflictError: the kit version is installed with other content. Nothing was overwritten.
type ConflictError struct {
	ID, Version string
	Diff        Diff
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %s is already installed with different content (%d changed, %d added, %d removed)",
		e.ID, e.Version, len(e.Diff.Changed), len(e.Diff.Added), len(e.Diff.Removed))
}

// Is lets errors.Is(err, ErrExists) see a conflict.
func (e *ConflictError) Is(target error) bool { return target == ErrExists }

// ImportOptions of Import.
type ImportOptions struct {
	// AsID and AsVersion install the kit under another id or version instead of the one in the file.
	AsID, AsVersion string
	// DryRun checks everything and installs nothing.
	DryRun bool
	// Force replaces a version holding other content (repair only).
	Force bool
	// UpstreamRelease is the current release of Claude Design; a kit not tested on it gets a warning.
	UpstreamRelease string
	Now             func() time.Time
}

// ImportResult of Import.
type ImportResult struct {
	Manifest *Manifest `json:"manifest"`
	ID       string    `json:"id"`
	Version  string    `json:"version"`
	// Installed is false for a dry run and for a kit that was already there.
	Installed bool `json:"installed"`
	Unchanged bool `json:"unchanged"`
	Replaced  bool `json:"replaced"`
	// Conflict is set (and an error returned) for dry runs too, so the caller can show the difference.
	Conflict *Diff    `json:"conflict,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Import reads an export, checks it and installs the kit. It installs all or nothing, never overwrites a version with
// other content (a *ConflictError with the difference is returned), and refuses a file whose bytes do not match its
// manifest.
func Import(dir string, src io.Reader, opt ImportOptions) (ImportResult, error) {
	if dir == "" {
		return ImportResult{}, errors.New("no kits folder is set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ImportResult{}, err
	}
	staging, err := os.MkdirTemp(dir, ".import-")
	if err != nil {
		return ImportResult{}, err
	}
	defer os.RemoveAll(staging)
	if _, _, err := unpack(&limitReader{r: src, n: MaxArchiveBytes}, staging); err != nil {
		return ImportResult{}, err
	}
	m, err := readExport(staging)
	if err != nil {
		return ImportResult{}, err
	}
	id, version := m.ID, m.Version
	if opt.AsID != "" {
		id = opt.AsID
	}
	if opt.AsVersion != "" {
		version = opt.AsVersion
	}
	if !ValidID(id) {
		return ImportResult{}, fmt.Errorf("%q is not a kit id (lower case letters, digits and dashes)", id)
	}
	if !ValidVersion(version) {
		return ImportResult{}, fmt.Errorf("%q is not a version (major.minor.patch, an optional -tag)", version)
	}
	res := ImportResult{Manifest: m, ID: id, Version: version}
	renamed := id != m.ID || version != m.Version
	if !renamed {
		if err := checkManifest(staging, id, version); err != nil {
			return res, err
		}
	}
	var missing []string
	for _, r := range m.Requires {
		if Resolve(dir, r.ID, r.Version, nil) == "" {
			missing = append(missing, r.ID+" "+r.Version)
		}
	}
	if len(missing) > 0 {
		return res, fmt.Errorf("the kit needs other kits that are not installed: %s", strings.Join(missing, ", "))
	}
	if opt.UpstreamRelease != "" && !contains(m.Upstream, opt.UpstreamRelease) {
		res.Warnings = append(res.Warnings, fmt.Sprintf("the kit was not checked against the Claude Design release %s (checked: %s)", opt.UpstreamRelease, orNone(m.Upstream)))
	}

	if target := Resolve(dir, id, version, nil); target != "" {
		have, err := hashTree(target, func(rel string) bool { return rel == ExportManifest })
		if err != nil {
			return res, err
		}
		d := diffFiles(m.Files, have)
		if d.Empty() {
			res.Unchanged = true
			return res, nil
		}
		res.Conflict = &d
		if renamed || !opt.Force {
			return res, &ConflictError{ID: id, Version: version, Diff: d}
		}
	}
	if opt.DryRun {
		return res, nil
	}
	if err := os.Remove(filepath.Join(staging, ExportManifest)); err != nil {
		return res, err
	}
	if renamed {
		if err := rewriteIdentity(staging, id, version); err != nil {
			return res, err
		}
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	var bytes int64
	for _, f := range m.Files {
		bytes += f.Size
	}
	rv := Result{Version: Version{Version: version, Files: len(m.Files), Bytes: bytes, SHA256: m.SHA256, InstalledAt: now().Unix()}}
	out, err := commit(dir, id, version, staging, rv, opt.Force)
	if err != nil {
		return res, err
	}
	// commit renamed the staging folder away; the deferred RemoveAll finds nothing to do.
	res.Installed = !out.Unchanged
	res.Unchanged = out.Unchanged
	res.Replaced = out.Replaced
	return res, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func orNone(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}

// readExport parses the manifest in an unpacked export and checks every file against it, both ways.
func readExport(root string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(root, ExportManifest))
	if err != nil {
		return nil, fmt.Errorf("not a kit export: %s is missing", ExportManifest)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", ExportManifest, err)
	}
	if m.Schema != exportSchema {
		return nil, fmt.Errorf("%s has schema %d, this version reads %d", ExportManifest, m.Schema, exportSchema)
	}
	if !ValidID(m.ID) || !ValidVersion(m.Version) {
		return nil, fmt.Errorf("%s names an invalid kit %q %q", ExportManifest, m.ID, m.Version)
	}
	if len(m.Files) == 0 {
		return nil, fmt.Errorf("%s lists no files", ExportManifest)
	}
	listed := make(map[string]bool, len(m.Files))
	for _, f := range m.Files {
		clean, err := safeName(f.Path)
		if err != nil || clean != f.Path || clean == ExportManifest {
			return nil, fmt.Errorf("%s lists a path that is not allowed: %q", ExportManifest, f.Path)
		}
		if listed[f.Path] {
			return nil, fmt.Errorf("%s lists %s twice", ExportManifest, f.Path)
		}
		listed[f.Path] = true
		n, sum, err := hashFile(filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, fmt.Errorf("%s is listed in the manifest but missing from the file", f.Path)
		}
		if n != f.Size || sum != f.SHA256 {
			return nil, fmt.Errorf("%s does not match its hash in the manifest: the file is damaged or was changed", f.Path)
		}
	}
	if got := treeSum(m.Files); got != m.SHA256 {
		return nil, fmt.Errorf("the manifest's own hash does not match its file list")
	}
	var extra []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel = filepath.ToSlash(rel); rel != ExportManifest && !listed[rel] {
			extra = append(extra, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return nil, fmt.Errorf("the file holds files the manifest does not list: %s", strings.Join(firstN(extra, 5), ", "))
	}
	return &m, nil
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// diffFiles is how the incoming files differ from those installed.
func diffFiles(incoming, installed []ExportFile) Diff {
	have := make(map[string]string, len(installed))
	for _, f := range installed {
		have[f.Path] = f.SHA256
	}
	d := Diff{Added: []string{}, Changed: []string{}, Removed: []string{}}
	seen := map[string]bool{}
	for _, f := range incoming {
		seen[f.Path] = true
		sum, ok := have[f.Path]
		switch {
		case !ok:
			d.Added = append(d.Added, f.Path)
		case sum != f.SHA256:
			d.Changed = append(d.Changed, f.Path)
		}
	}
	for _, f := range installed {
		if !seen[f.Path] {
			d.Removed = append(d.Removed, f.Path)
		}
	}
	return d
}

// rewriteIdentity makes kit.json (when there is one) say the id and version the kit was installed under.
func rewriteIdentity(root, id, version string) error {
	p := filepath.Join(root, KitFile)
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	m["id"], _ = json.Marshal(id)
	m["version"], _ = json.Marshal(version)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(out, '\n'), 0o644)
}
