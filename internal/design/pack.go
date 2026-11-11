package design

// The portable pack: Sinopia projects together with the UI kits they are pinned to, in one tar.gz that
// another harnessly (a closed network, no outside access) opens and edits. It starts with the manifest
// sinopia-pack.json: the format version, the projects, the kits with their versions (parents included), and every
// file with its size and sha256. A kit travels as its own export (internal/designkit: the same file `design kit export`
// makes, with its own manifest), so the kit checks, the refusal to overwrite a version with other content and the
// license confirmation are the ones kits already have. A project travels as its files: meta/ (pages, notes, the pin to
// a kit version), the boards and everything they load, as «Весь проект» takes them (deckzip.go): service folders, files
// that look like credentials and the agent's instructions stay out, on the way in as well.

import (
	"archive/tar"
	"compress/gzip"
	"context"
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
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

const (
	// PackManifestName is the manifest, the first entry of a pack.
	PackManifestName = "sinopia-pack.json"
	// PackFormat names the format; PackSchema is the version of it this harnessly reads and writes.
	PackFormat = "sinopia-pack"
	PackSchema = 1

	packProjectsDir = "projects"
	packKitsDir     = "kits"
	packOrigins     = ".pack-imports.json"

	packMaxBytes   = 16 << 30
	packMaxFiles   = 200000
	packMaxFile    = 4 << 30
	packManifestMx = 64 << 20
)

// PackKitRef names a kit version.
type PackKitRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// PackProject is a project of a pack: its files are the entries projects/<id>/….
type PackProject struct {
	ID    string      `json:"id"`
	Title string      `json:"title"`
	Kit   *PackKitRef `json:"kit,omitempty"`
	Files int         `json:"files"`
	Bytes int64       `json:"bytes"`
	// SHA256 is the hash of the project's own file list (path, size, hash): one number for its content.
	SHA256 string `json:"sha256"`
}

// PackKit is a kit of a pack: the entry kits/<id>/<version>.kit.tar.gz is the kit's export.
type PackKit struct {
	ID         string                 `json:"id"`
	Version    string                 `json:"version"`
	File       string                 `json:"file"`
	Size       int64                  `json:"size"`
	SHA256     string                 `json:"sha256"`
	Requires   []designkit.Require    `json:"requires,omitempty"`
	Restricted []designkit.Restricted `json:"restricted,omitempty"`
}

// PackManifest is sinopia-pack.json.
type PackManifest struct {
	Format     string                 `json:"format"`
	Schema     int                    `json:"schema"`
	ExportedAt string                 `json:"exportedAt"`
	Projects   []PackProject          `json:"projects"`
	Kits       []PackKit              `json:"kits"`
	Warnings   []string               `json:"warnings,omitempty"`
	Files      []designkit.ExportFile `json:"files"`
	// SHA256 is the hash of the sorted "path size sha256" lines of Files.
	SHA256 string `json:"sha256"`
}

// PackRestrictedError: kits of the pack have license-restricted files and the export was not confirmed.
type PackRestrictedError struct{ Restricted []designkit.Restricted }

func (e *PackRestrictedError) Error() string {
	return fmt.Sprintf("the kits have %d files with a restricted license: confirm to pack them", len(e.Restricted))
}

func packTreeSum(files []designkit.ExportFile) string {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s %d %s\n", f.Path, f.Size, f.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// packFile is a project file to pack: its place in the project (rel), where it is read (real, relative to the project's
// root) and its size and hash.
type packFile struct {
	rel, real string
	size      int64
	sum       string
}

// projectFiles lists what a pack takes of a project, with sizes and hashes, sorted by path: the files «Весь проект»
// takes (deckZipList), less the agent's instruction files. The folder is opened as a root: a link cannot lead out of it.
func (l *Library) projectFiles(ctx context.Context, id string) (root string, out []packFile, err error) {
	root, err = filepath.EvalSymlinks(l.ArtifactDir(id))
	if err != nil {
		return "", nil, fmt.Errorf("the project %q is not in the library", id)
	}
	list, _, err := deckZipList(ctx, root, root)
	if err != nil {
		return "", nil, fmt.Errorf("project %s: %w", id, err)
	}
	inside, err := os.OpenRoot(root)
	if err != nil {
		return "", nil, err
	}
	defer inside.Close()
	for _, e := range list {
		rel, _ := filepath.Rel(root, e.name)
		rel = filepath.ToSlash(rel)
		if packDropped(rel) {
			continue
		}
		n, sum, err := hashInRoot(inside, e.real)
		if err != nil {
			continue
		}
		out = append(out, packFile{rel: rel, real: e.real, size: n, sum: sum})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	if len(out) == 0 {
		return "", nil, fmt.Errorf("project %s has no files to pack", id)
	}
	return root, out, nil
}

func hashInRoot(inside *os.Root, real string) (int64, string, error) {
	f, err := inside.OpenFile(real, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return 0, "", errors.New("not a regular file")
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	return n, hex.EncodeToString(h.Sum(nil)), err
}

// packDropped says a project file stays out of a pack, or is not taken from one: a service name (a part starting with
// a dot), a file that looks like a credential, an instruction file of an agent.
func packDropped(rel string) bool {
	if deckServicePath(rel) || deckSecrets.Secret(rel) {
		return true
	}
	return agentFileName.MatchString(path.Base(rel))
}

// LibraryProjectIDs are the projects of the library: folders with a project folder or a model.
func (l *Library) LibraryProjectIDs() []string {
	var out []string
	entries, _ := os.ReadDir(l.Root())
	for _, e := range entries {
		if !e.IsDir() || !IsLibraryID(e.Name()) {
			continue
		}
		if exists(l.ProjectDir(e.Name())) || l.HasMeta(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// PackExportOptions say what goes into a pack.
type PackExportOptions struct {
	// Projects are project ids; All takes every project of the library.
	Projects []string
	All      bool
	// Kits are kits to pack besides the ones the projects are pinned to: "<id>" (the newest version) or "<id>@<version>".
	Kits []string
	// AcceptRestricted confirms packing kit files with a restricted license (a corporate font).
	AcceptRestricted bool
	Now              func() time.Time
}

func (l *Library) newestKit(id string) string {
	for _, k := range designkit.List(l.KitsDir()) {
		if k.ID == id && len(k.Versions) > 0 {
			return k.Versions[0].Version
		}
	}
	return ""
}

// PackExport writes a pack of the projects and the kits they need, and returns its manifest.
func (l *Library) PackExport(ctx context.Context, opt PackExportOptions, w io.Writer) (*PackManifest, error) {
	ids := append([]string(nil), opt.Projects...)
	if opt.All {
		ids = append(ids, l.LibraryProjectIDs()...)
	}
	ids = uniqueStrings(ids)
	if len(ids) == 0 && len(opt.Kits) == 0 {
		return nil, errors.New("name what to pack: projects, --all, or kits")
	}
	m := &PackManifest{Format: PackFormat, Schema: PackSchema, Projects: []PackProject{}, Kits: []PackKit{}}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	m.ExportedAt = now().UTC().Format(time.RFC3339)

	type projectData struct {
		root  string
		files []packFile
	}
	data := map[string]projectData{}
	wanted := map[PackKitRef]bool{}
	var order []PackKitRef
	var refErr error
	want := func(r PackKitRef) {
		if !designkit.ValidID(r.ID) || !designkit.ValidVersion(r.Version) {
			if refErr == nil {
				refErr = fmt.Errorf("%q %q is not a kit version (id: lower case letters, digits, dashes; version: major.minor.patch)", r.ID, r.Version)
			}
			return
		}
		if !wanted[r] {
			wanted[r] = true
			order = append(order, r)
		}
	}
	for _, id := range ids {
		if !IsLibraryID(id) {
			return nil, fmt.Errorf("%q is not a project id", id)
		}
		root, files, err := l.projectFiles(ctx, id)
		if err != nil {
			return nil, err
		}
		data[id] = projectData{root, files}
		pr := PackProject{ID: id, Title: id, Files: len(files)}
		if model := l.ReadModel(id); model != nil && model.Meta.Title != "" {
			pr.Title = model.Meta.Title
		}
		list := make([]designkit.ExportFile, len(files))
		for i, f := range files {
			pr.Bytes += f.size
			list[i] = designkit.ExportFile{Path: f.rel, Size: f.size, SHA256: f.sum}
			m.Files = append(m.Files, designkit.ExportFile{Path: packProjectsDir + "/" + id + "/" + f.rel, Size: f.size, SHA256: f.sum})
		}
		pr.SHA256 = packTreeSum(list)
		if pin, ok := l.KitPinOf(id); ok {
			pr.Kit = &PackKitRef{ID: pin.ID, Version: pin.Version}
			want(*pr.Kit)
			if pin.Draft {
				m.Warnings = append(m.Warnings, fmt.Sprintf("project %s is shown on the draft of kit %s: the pack carries the version %s it goes back to", id, pin.ID, pin.Version))
			}
		}
		m.Projects = append(m.Projects, pr)
	}
	if refErr != nil {
		return nil, refErr
	}
	for _, k := range opt.Kits {
		id, version, _ := strings.Cut(k, "@")
		if version == "" {
			version = l.newestKit(id)
		}
		if version == "" {
			return nil, fmt.Errorf("the kit %q is not installed here", id)
		}
		want(PackKitRef{id, version})
	}
	if refErr != nil {
		return nil, refErr
	}

	// The kits, each as its own export, with the kits they require.
	tmp, err := os.MkdirTemp("", "sinopia-pack-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	var restricted []designkit.Restricted
	archives := map[string]string{} // entry name → temp file
	for i := 0; i < len(order); i++ {
		ref := order[i]
		km, err := designkit.BuildManifest(l.KitsDir(), ref.ID, ref.Version, designkit.ExportOptions{AcceptRestricted: true})
		if err != nil {
			return nil, fmt.Errorf("the kit %s %s: %w (it is installed here with `harnessly design kit install`)", ref.ID, ref.Version, err)
		}
		for _, r := range km.Restricted {
			r.Path = ref.ID + "@" + ref.Version + "/" + r.Path
			restricted = append(restricted, r)
		}
		for _, r := range km.Requires {
			want(PackKitRef{r.ID, r.Version})
		}
		if refErr != nil {
			return nil, fmt.Errorf("the kit %s %s requires %w", ref.ID, ref.Version, refErr)
		}
		file, err := os.CreateTemp(tmp, "kit-*")
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		if err := designkit.WriteArchive(l.KitsDir(), km, io.MultiWriter(file, h)); err != nil {
			file.Close()
			return nil, err
		}
		st, _ := file.Stat()
		file.Close()
		entry := packKitsDir + "/" + ref.ID + "/" + ref.Version + ".kit.tar.gz"
		archives[entry] = file.Name()
		sum := hex.EncodeToString(h.Sum(nil))
		m.Kits = append(m.Kits, PackKit{ID: ref.ID, Version: ref.Version, File: entry, Size: st.Size(), SHA256: sum, Requires: km.Requires, Restricted: km.Restricted})
		m.Files = append(m.Files, designkit.ExportFile{Path: entry, Size: st.Size(), SHA256: sum})
	}
	if len(restricted) > 0 && !opt.AcceptRestricted {
		return nil, &PackRestrictedError{Restricted: restricted}
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	m.SHA256 = packTreeSum(m.Files)

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	raw, _ := json.MarshalIndent(m, "", "  ")
	if err := packEntry(tw, PackManifestName, int64(len(raw)), strings.NewReader(string(raw)), time.Time{}); err != nil {
		return nil, err
	}
	for _, f := range m.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if src, ok := archives[f.Path]; ok {
			if err := packFromFile(tw, f, src); err != nil {
				return nil, err
			}
			continue
		}
		rest := strings.TrimPrefix(f.Path, packProjectsDir+"/")
		id, rel, _ := strings.Cut(rest, "/")
		pd := data[id]
		if err := packFromProject(tw, f, pd.root, rel, pd.files); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return m, gz.Close()
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func packEntry(tw *tar.Writer, name string, size int64, src io.Reader, mod time.Time) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: size, ModTime: mod}); err != nil {
		return err
	}
	n, err := io.Copy(tw, src)
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("%s changed while it was packed", name)
	}
	return nil
}

// packCheck reads src into the entry and fails when what it read is not what the manifest lists: a file that changed
// between the listing and the writing must not reach the pack under a hash that is no longer true.
func packCheck(tw *tar.Writer, f designkit.ExportFile, src io.Reader) error {
	h := sha256.New()
	if err := tw.WriteHeader(&tar.Header{Name: f.Path, Typeflag: tar.TypeReg, Mode: 0o644, Size: f.Size}); err != nil {
		return err
	}
	n, err := io.Copy(io.MultiWriter(tw, h), io.LimitReader(src, f.Size+1))
	if err != nil {
		return err
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("%s changed while the pack was written: try again", f.Path)
	}
	return nil
}

func packFromFile(tw *tar.Writer, f designkit.ExportFile, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return packCheck(tw, f, in)
}

func packFromProject(tw *tar.Writer, f designkit.ExportFile, root, rel string, files []packFile) error {
	var real string
	for _, pf := range files {
		if pf.rel == rel {
			real = pf.real
		}
	}
	inside, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer inside.Close()
	in, err := inside.OpenFile(real, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Path, err)
	}
	defer in.Close()
	return packCheck(tw, f, in)
}

// --- import ---

// PackProjectResult is what an import does with one project.
type PackProjectResult struct {
	ID string `json:"id"`
	// As is the id it has in this library; it differs from ID when ID was taken by another project.
	As     string `json:"as"`
	Status string `json:"status"` // added | renamed | unchanged
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
	// Kit is the kit version the project is pinned to.
	Kit *PackKitRef `json:"kit,omitempty"`
}

// PackKitResult is what an import does with one kit version.
type PackKitResult struct {
	ID      string          `json:"id"`
	Version string          `json:"version"`
	Status  string          `json:"status"` // added | unchanged | conflict
	Diff    *designkit.Diff `json:"diff,omitempty"`
}

// PackImportResult is the plan (dry run) or the outcome of an import.
type PackImportResult struct {
	Manifest *PackManifest       `json:"manifest"`
	Projects []PackProjectResult `json:"projects"`
	Kits     []PackKitResult     `json:"kits"`
	// Dropped are files the pack held that are not taken: service names, credentials, an agent's instructions.
	Dropped  []string `json:"dropped,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	DryRun   bool     `json:"dryRun"`
	// Changed is whether the library or the kits folder changed (or would).
	Changed bool `json:"changed"`
}

// PackError is a refusal worded for the person; Conflict lists kit versions that are installed with other content.
type PackError struct {
	Msg      string
	Conflict []PackKitResult
}

func (e *PackError) Error() string { return e.Msg }

func packErr(format string, a ...any) error { return &PackError{Msg: fmt.Sprintf(format, a...)} }

// PackImportOptions of PackImport.
type PackImportOptions struct {
	// DryRun checks and reports; nothing is written.
	DryRun bool
	// Person is who imports (a page session's address; "" is the runtime's own): an imported project is theirs, as one
	// they made, since the pack carries no .owner.json.
	Person string
}

type limitedReader struct {
	r io.Reader
	n int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, fmt.Errorf("the pack is larger than %d GB", packMaxBytes>>30)
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

// unpackPack writes the tar.gz into dir: regular files only, names that stay inside it, all or an error.
func unpackPack(src io.Reader, dir string) error {
	gz, err := gzip.NewReader(&limitedReader{r: src, n: packMaxBytes})
	if err != nil {
		return packErr("this is not a Sinopia pack: it is not a .tar.gz file")
	}
	tr := tar.NewReader(gz)
	var files int
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return packErr("the pack is damaged: %v", err)
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return packErr("the pack holds %s, which is not a file: refused", h.Name)
		}
		name := path.Clean(h.Name)
		if name == "." || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00") || name != h.Name {
			return packErr("the pack names a path that is not allowed: %q", h.Name)
		}
		if h.Size < 0 || h.Size > packMaxFile {
			return packErr("%s is too large for a pack", name)
		}
		if files++; files > packMaxFiles {
			return packErr("the pack has more than %d files", packMaxFiles)
		}
		if total += h.Size; total > packMaxBytes {
			return packErr("the pack is larger than %d GB", packMaxBytes>>30)
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o777); err != nil {
			return err
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return packErr("the pack lists %s twice or clashes with another entry", name)
		}
		_, cerr := io.Copy(f, io.LimitReader(tr, h.Size))
		if err := f.Close(); cerr == nil {
			cerr = err
		}
		if cerr != nil {
			return packErr("the pack is damaged at %s: %v", name, cerr)
		}
	}
}

// readPack reads the manifest from the unpacked folder and checks it against the bytes: every listed file is there
// with its size and hash, and nothing else is.
func readPack(dir string) (*PackManifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, PackManifestName))
	if err != nil {
		return nil, packErr("this is not a Sinopia pack: %s is missing", PackManifestName)
	}
	if len(raw) > packManifestMx {
		return nil, packErr("%s is too large", PackManifestName)
	}
	var head struct {
		Format string `json:"format"`
		Schema int    `json:"schema"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, packErr("%s is not valid JSON: %v", PackManifestName, err)
	}
	if head.Format != PackFormat {
		return nil, packErr("this is not a Sinopia pack: the format is %q", head.Format)
	}
	if head.Schema > PackSchema {
		return nil, packErr("this pack is in format %d, made by a newer harnessly; this one reads up to format %d: update harnessly, or export the pack again from an older one", head.Schema, PackSchema)
	}
	if head.Schema < 1 {
		return nil, packErr("%s has no valid format version", PackManifestName)
	}
	var m PackManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, packErr("%s is not valid JSON: %v", PackManifestName, err)
	}
	listed := map[string]bool{}
	for _, f := range m.Files {
		clean := path.Clean(f.Path)
		if clean != f.Path || !(strings.HasPrefix(clean, packProjectsDir+"/") || strings.HasPrefix(clean, packKitsDir+"/")) || strings.Contains(clean, "..") {
			return nil, packErr("%s lists a path that is not allowed: %q", PackManifestName, f.Path)
		}
		if listed[f.Path] {
			return nil, packErr("%s lists %s twice", PackManifestName, f.Path)
		}
		listed[f.Path] = true
		n, sum, err := hashFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, packErr("%s is listed in the manifest but missing from the pack", f.Path)
		}
		if n != f.Size || sum != f.SHA256 {
			return nil, packErr("%s does not match its hash in the manifest: the pack is damaged or was changed", f.Path)
		}
	}
	if packTreeSum(m.Files) != m.SHA256 {
		return nil, packErr("the manifest's own hash does not match its file list")
	}
	var extra []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if rel = filepath.ToSlash(rel); rel != PackManifestName && !listed[rel] {
			extra = append(extra, rel)
		}
		return nil
	})
	if len(extra) > 0 {
		sort.Strings(extra)
		if len(extra) > 5 {
			extra = extra[:5]
		}
		return nil, packErr("the pack holds files the manifest does not list: %s", strings.Join(extra, ", "))
	}
	byProject := map[string][]designkit.ExportFile{}
	for _, f := range m.Files {
		if rest, ok := strings.CutPrefix(f.Path, packProjectsDir+"/"); ok {
			id, rel, _ := strings.Cut(rest, "/")
			byProject[id] = append(byProject[id], designkit.ExportFile{Path: rel, Size: f.Size, SHA256: f.SHA256})
		}
	}
	for _, p := range m.Projects {
		if !IsLibraryID(p.ID) {
			return nil, packErr("the pack names a project with an invalid id %q", p.ID)
		}
		files := byProject[p.ID]
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		if len(files) == 0 || packTreeSum(files) != p.SHA256 {
			return nil, packErr("the project %s does not match its hash in the manifest: the pack was changed", p.ID)
		}
	}
	for _, k := range m.Kits {
		if !designkit.ValidID(k.ID) || !designkit.ValidVersion(k.Version) || !listed[k.File] || k.File != packKitsDir+"/"+k.ID+"/"+k.Version+".kit.tar.gz" {
			return nil, packErr("the pack names a kit %q %q that is not valid or not in the pack", k.ID, k.Version)
		}
	}
	return &m, nil
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

// kitOrder puts the kits of a pack so that a kit comes after the kits it requires; a requirement outside the pack
// is left to the kits folder (import tells when it is not installed).
func kitOrder(kits []PackKit) ([]PackKit, error) {
	by := map[PackKitRef]PackKit{}
	for _, k := range kits {
		by[PackKitRef{k.ID, k.Version}] = k
	}
	var out []PackKit
	state := map[PackKitRef]int{}
	var visit func(k PackKit) error
	visit = func(k PackKit) error {
		ref := PackKitRef{k.ID, k.Version}
		switch state[ref] {
		case 2:
			return nil
		case 1:
			return packErr("the kits %s %s require each other in a loop", k.ID, k.Version)
		}
		state[ref] = 1
		for _, r := range k.Requires {
			if dep, ok := by[PackKitRef{r.ID, r.Version}]; ok {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		state[ref] = 2
		out = append(out, k)
		return nil
	}
	sorted := append([]PackKit(nil), kits...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID+"@"+sorted[i].Version < sorted[j].ID+"@"+sorted[j].Version
	})
	for _, k := range sorted {
		if err := visit(k); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// packOriginsFile remembers which project of this library a pack's project became, by the project's content hash: the
// same pack imported again finds its projects even after they were edited here, and changes nothing.
func (l *Library) packOrigins() map[string]string {
	out := map[string]string{}
	if raw, err := os.ReadFile(filepath.Join(l.Root(), packOrigins)); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (l *Library) savePackOrigins(m map[string]string) error {
	raw, _ := json.MarshalIndent(m, "", "  ")
	tmp := filepath.Join(l.Root(), packOrigins+".tmp")
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(l.Root(), packOrigins))
}

// sameProject is whether the library's project id holds exactly the files of the pack's project.
func (l *Library) sameProject(id string, pack []designkit.ExportFile) bool {
	_, files, err := l.projectFiles(context.Background(), id)
	if err != nil || len(files) != len(pack) {
		return false
	}
	for i, f := range files {
		if f.rel != pack[i].Path || f.size != pack[i].Size || f.sum != pack[i].SHA256 {
			return false
		}
	}
	return true
}

// PackImport reads a pack, checks it, and installs its kits and projects. It plans first and writes only when the whole
// pack is acceptable: a kit version installed with other content refuses the import and nothing is written.
func (l *Library) PackImport(src io.Reader, opt PackImportOptions) (PackImportResult, error) {
	defer l.listing.invalidateAll()
	res := PackImportResult{DryRun: opt.DryRun, Projects: []PackProjectResult{}, Kits: []PackKitResult{}}
	root := l.Root()
	if err := os.MkdirAll(root, 0o777); err != nil {
		return res, err
	}
	staging, err := os.MkdirTemp(root, ".pack-import-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(staging)
	if err := unpackPack(src, staging); err != nil {
		return res, err
	}
	m, err := readPack(staging)
	if err != nil {
		return res, err
	}
	res.Manifest = m
	res.Warnings = append(res.Warnings, m.Warnings...)

	// Kits: dry run each, parents first.
	kits, err := kitOrder(m.Kits)
	if err != nil {
		return res, err
	}
	assume := make([]designkit.Require, 0, len(kits))
	for _, k := range kits {
		assume = append(assume, designkit.Require{ID: k.ID, Version: k.Version})
	}
	var conflicts []PackKitResult
	for _, k := range kits {
		kr := PackKitResult{ID: k.ID, Version: k.Version, Status: "added"}
		f, err := os.Open(filepath.Join(staging, filepath.FromSlash(k.File)))
		if err != nil {
			return res, err
		}
		r, ierr := designkit.Import(l.KitsDir(), f, designkit.ImportOptions{DryRun: true, Assume: assume})
		f.Close()
		var ce *designkit.ConflictError
		switch {
		case errors.As(ierr, &ce):
			kr.Status, kr.Diff = "conflict", &ce.Diff
			conflicts = append(conflicts, kr)
		case ierr != nil:
			return res, packErr("the kit %s %s: %v", k.ID, k.Version, ierr)
		case r.Unchanged:
			kr.Status = "unchanged"
		}
		res.Kits = append(res.Kits, kr)
	}
	if len(conflicts) > 0 {
		var names []string
		for _, c := range conflicts {
			names = append(names, c.ID+" "+c.Version)
		}
		return res, &PackError{Msg: "these kit versions are installed here with other content, and a version never changes: " +
			strings.Join(names, ", ") + ". Nothing was imported. Export the project against a new kit version, or install the pack's kit under another id with `design kit import --as-id`", Conflict: conflicts}
	}

	// Projects.
	byProject := map[string][]designkit.ExportFile{}
	for _, f := range m.Files {
		if rest, ok := strings.CutPrefix(f.Path, packProjectsDir+"/"); ok {
			id, rel, _ := strings.Cut(rest, "/")
			byProject[id] = append(byProject[id], designkit.ExportFile{Path: rel, Size: f.Size, SHA256: f.SHA256})
		}
	}
	inPack := map[PackKitRef]bool{}
	for _, k := range m.Kits {
		inPack[PackKitRef{k.ID, k.Version}] = true
	}
	origins := l.packOrigins()
	taken := l.TakenArtifactIDs()
	type placement struct {
		pr    PackProject
		as    string
		skip  bool
		files []designkit.ExportFile
	}
	var plan []placement
	for _, pr := range m.Projects {
		files := byProject[pr.ID]
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		kept := files[:0:0]
		for _, f := range files {
			if packDropped(f.Path) {
				res.Dropped = append(res.Dropped, pr.ID+"/"+f.Path)
				continue
			}
			kept = append(kept, f)
		}
		pl := placement{pr: pr, files: kept}
		status := "added"
		switch local := origins[pr.SHA256]; {
		case local != "" && exists(l.ArtifactDir(local)):
			pl.as, pl.skip, status = local, true, "unchanged"
		case taken[pr.ID] && len(res.Dropped) == 0 && l.sameProject(pr.ID, kept):
			pl.as, pl.skip, status = pr.ID, true, "unchanged"
		case taken[pr.ID]:
			pl.as, status = SlugifyArtifactID(pr.ID, taken), "renamed"
			taken[pl.as] = true
		default:
			pl.as = pr.ID
			taken[pl.as] = true
		}
		var bytes int64
		for _, f := range kept {
			bytes += f.Size
		}
		res.Projects = append(res.Projects, PackProjectResult{ID: pr.ID, As: pl.as, Status: status, Files: len(kept), Bytes: bytes, Kit: pr.Kit})
		if pr.Kit != nil && !inPack[*pr.Kit] && kitInstalled(l.KitsDir(), *pr.Kit) == "" {
			res.Warnings = append(res.Warnings, fmt.Sprintf("project %s is pinned to the kit %s %s, which is neither in the pack nor installed here: it will not draw until the kit is installed", pr.ID, pr.Kit.ID, pr.Kit.Version))
		}
		plan = append(plan, pl)
	}
	for _, k := range res.Kits {
		if k.Status == "added" {
			res.Changed = true
		}
	}
	for _, p := range res.Projects {
		if p.Status != "unchanged" {
			res.Changed = true
		}
	}
	if opt.DryRun {
		return res, nil
	}

	// Write: the kits first (they never change once installed), then each project as one rename.
	for i, k := range kits {
		if res.Kits[i].Status != "added" {
			continue
		}
		f, err := os.Open(filepath.Join(staging, filepath.FromSlash(k.File)))
		if err != nil {
			return res, err
		}
		_, ierr := designkit.Import(l.KitsDir(), f, designkit.ImportOptions{})
		f.Close()
		if ierr != nil {
			return res, packErr("the kit %s %s: %v", k.ID, k.Version, ierr)
		}
	}
	for i, pl := range plan {
		if pl.skip {
			continue
		}
		stage := filepath.Join(root, ".importing-"+pl.as+"-"+randomHex(4))
		defer os.RemoveAll(stage)
		for _, f := range pl.files {
			from := filepath.Join(staging, packProjectsDir, filepath.FromSlash(pl.pr.ID), filepath.FromSlash(f.Path))
			to := filepath.Join(stage, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(to), 0o777); err != nil {
				return res, err
			}
			if err := os.Rename(from, to); err != nil {
				return res, err
			}
		}
		if err := os.Rename(stage, l.ArtifactDir(pl.as)); err != nil {
			return res, packErr("the project %s could not be placed as %s: %v", pl.pr.ID, pl.as, err)
		}
		origins[pl.pr.SHA256] = pl.as
		l.AnnounceNewBy(pl.as, opt.Person)
		res.Projects[i].As = pl.as
	}
	if err := l.savePackOrigins(origins); err != nil {
		res.Warnings = append(res.Warnings, "the record of imported projects was not saved, so importing this pack again makes copies: "+err.Error())
	}
	return res, nil
}

// kitInstalled is the folder of an installed kit version, "" when it is not installed.
func kitInstalled(dir string, r PackKitRef) string {
	return designkit.Resolve(dir, r.ID, r.Version, nil)
}
