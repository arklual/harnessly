package design

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// emptyLibrary is a second harnessly: an empty library and an empty kits folder.
func emptyLibrary(t *testing.T) *Library {
	t.Helper()
	tmp := t.TempDir()
	l := New(Config{DataDir: filepath.Join(tmp, "data"), LibraryDir: filepath.Join(tmp, "lib"), KitsDir: filepath.Join(tmp, "kits"),
		Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(func() {
		l.StopWatching()
		l.StopBackground()
	})
	return l
}

// packSource is a library with two projects, one pinned to the kit acme-ds 1.0.0, one on no kit.
func packSource(t *testing.T) (*Library, []string) {
	t.Helper()
	a, _ := kitLibrary(t)
	var ids []string
	for _, title := range []string{"Pack on kit", "Pack plain"} {
		p, err := a.CreateProject(title)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.CreatePage(p.ID, PageSpec{Title: "Second " + title}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	if err := a.SetKitPin(ids[0], &KitPin{ID: "acme-ds", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(a.ArtifactDir(ids[0]), "project", "notes.md"), "notes of the first")
	return a, ids
}

func exportPack(t *testing.T, l *Library, opt PackExportOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := l.PackExport(context.Background(), opt, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// contentOf is the hash of every file a pack would take of the project, by path.
func contentOf(t *testing.T, l *Library, id string) map[string]string {
	t.Helper()
	_, files, err := l.projectFiles(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range files {
		out[f.rel] = f.sum
	}
	return out
}

func kitContent(t *testing.T, l *Library, id, version string) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := designkit.Resolve(l.KitsDir(), id, version, nil)
	if root == "" {
		t.Fatalf("kit %s %s is not installed", id, version)
	}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, p)
			_, sum, _ := hashFile(p)
			out[filepath.ToSlash(rel)] = sum
		}
		return nil
	})
	return out
}

func sameMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func pageIDs(t *testing.T, l *Library, id string) []string {
	t.Helper()
	m := l.ReadModel(id)
	if m == nil {
		t.Fatalf("%s has no model", id)
	}
	var out []string
	for _, p := range m.Pages {
		out = append(out, p.ID)
	}
	sort.Strings(out)
	return out
}

func TestPackRoundTripKeepsBytesPagesAndPinAndRepeatsAsNothing(t *testing.T) {
	a, ids := packSource(t)
	pack := exportPack(t, a, PackExportOptions{Projects: ids})
	b := emptyLibrary(t)

	dry, err := b.PackImport(bytes.NewReader(pack), PackImportOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !dry.Changed || len(dry.Kits) != 1 || dry.Kits[0].Status != "added" || len(dry.Projects) != 2 || dry.Projects[0].Status != "added" {
		t.Fatalf("dry run: %+v", dry)
	}
	if len(b.LibraryProjectIDs()) != 0 || designkit.Resolve(b.KitsDir(), "acme-ds", "1.0.0", nil) != "" {
		t.Fatal("a dry run wrote something")
	}

	res, err := b.PackImport(bytes.NewReader(pack), PackImportOptions{})
	if err != nil || !res.Changed {
		t.Fatalf("%+v %v", res, err)
	}
	for _, id := range ids {
		if !sameMap(contentOf(t, a, id), contentOf(t, b, id)) {
			t.Errorf("%s: the files differ after the round trip", id)
		}
		if got, want := pageIDs(t, b, id), pageIDs(t, a, id); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: page ids %v, want %v", id, got, want)
		}
	}
	if pin, ok := b.KitPinOf(ids[0]); !ok || pin.ID != "acme-ds" || pin.Version != "1.0.0" {
		t.Fatalf("the pin: %+v %v", pin, ok)
	}
	if _, ok := b.KitPinOf(ids[1]); ok {
		t.Fatal("a project without a kit got one")
	}
	if !sameMap(kitContent(t, a, "acme-ds", "1.0.0"), kitContent(t, b, "acme-ds", "1.0.0")) {
		t.Fatal("the kit differs after the round trip")
	}

	before := map[string]map[string]string{}
	for _, id := range ids {
		before[id] = contentOf(t, b, id)
	}
	again, err := b.PackImport(bytes.NewReader(pack), PackImportOptions{})
	if err != nil || again.Changed {
		t.Fatalf("the same pack again: %+v %v", again, err)
	}
	for _, p := range again.Projects {
		if p.Status != "unchanged" || p.As != p.ID {
			t.Errorf("%+v", p)
		}
	}
	if again.Kits[0].Status != "unchanged" || len(b.LibraryProjectIDs()) != 2 {
		t.Fatalf("%+v %v", again.Kits, b.LibraryProjectIDs())
	}
	for _, id := range ids {
		if !sameMap(before[id], contentOf(t, b, id)) {
			t.Errorf("%s changed on the second import", id)
		}
	}
}

func TestPackKitVersionWithOtherContentRefusesEverything(t *testing.T) {
	a, ids := packSource(t)
	pack := exportPack(t, a, PackExportOptions{Projects: ids})
	b := emptyLibrary(t)
	writeFile(t, filepath.Join(b.KitsDir(), "acme-ds", "1.0.0", "themes", "plain", "button.css"), ".button{color:red}")
	writeFile(t, filepath.Join(b.KitsDir(), "acme-ds", "1.0.0", "kit.json"), `{"id":"acme-ds","version":"1.0.0"}`)
	res, err := b.PackImport(bytes.NewReader(pack), PackImportOptions{})
	var pe *PackError
	if !errors.As(err, &pe) || len(pe.Conflict) != 1 || pe.Conflict[0].Diff == nil || res.Kits[0].Status != "conflict" {
		t.Fatalf("%+v %v", res, err)
	}
	if len(b.LibraryProjectIDs()) != 0 {
		t.Fatal("a project was written although a kit conflicts")
	}
	if got := kitContent(t, b, "acme-ds", "1.0.0")["themes/plain/button.css"]; got == kitContent(t, a, "acme-ds", "1.0.0")["themes/plain/button.css"] {
		t.Fatal("the installed kit version was overwritten")
	}
}

func TestPackProjectIDTakenGetsANewIDAndAgainChangesNothing(t *testing.T) {
	a, ids := packSource(t)
	pack := exportPack(t, a, PackExportOptions{Projects: ids[1:]})
	b := emptyLibrary(t)
	other, err := b.CreateProject("Pack plain")
	if err != nil {
		t.Fatal(err)
	}
	if other.ID != ids[1] {
		t.Fatalf("the test needs the same id: %s %s", other.ID, ids[1])
	}
	own := contentOf(t, b, other.ID)
	res, err := b.PackImport(bytes.NewReader(pack), PackImportOptions{})
	if err != nil || len(res.Projects) != 1 || res.Projects[0].Status != "renamed" || res.Projects[0].As == ids[1] {
		t.Fatalf("%+v %v", res, err)
	}
	as := res.Projects[0].As
	if !sameMap(contentOf(t, a, ids[1]), contentOf(t, b, as)) || !sameMap(own, contentOf(t, b, other.ID)) {
		t.Fatal("the new id does not hold the pack's project, or the existing project was touched")
	}
	again, err := b.PackImport(bytes.NewReader(pack), PackImportOptions{})
	if err != nil || again.Changed || again.Projects[0].As != as || again.Projects[0].Status != "unchanged" || len(b.LibraryProjectIDs()) != 2 {
		t.Fatalf("%+v %v %v", again, err, b.LibraryProjectIDs())
	}
}

// craftPack is a pack with the given manifest text and entries, the manifest first.
func craftPack(t *testing.T, manifest string, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	put := func(name, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(tw, body)
	}
	put(PackManifestName, manifest)
	for n, b := range entries {
		put(n, b)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestPackFromANewerFormatIsRefusedWithAMessage(t *testing.T) {
	b := emptyLibrary(t)
	_, err := b.PackImport(bytes.NewReader(craftPack(t, `{"format":"sinopia-pack","schema":2,"files":[]}`, nil)), PackImportOptions{})
	if err == nil || !strings.Contains(err.Error(), "newer harnessly") || !strings.Contains(err.Error(), "update harnessly") {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"not a pack": craftPack(t, `{"format":"other","schema":1}`, nil),
		"not gzip":   []byte("hello"),
		"no json":    craftPack(t, `{`, nil),
	} {
		if _, err := b.PackImport(bytes.NewReader(data), PackImportOptions{}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// repack rewrites a pack's entries with change: it returns the new content of a file, or nil to keep it.
func repack(t *testing.T, pack []byte, change func(name string, body []byte) []byte) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(pack))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(tr)
		if c := change(h.Name, body); c != nil {
			body = c
		}
		h.Size = int64(len(body))
		_ = tw.WriteHeader(h)
		_, _ = tw.Write(body)
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func TestPackWithAChangedByteOrAnExtraFileIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	a, ids := packSource(t)
	pack := exportPack(t, a, PackExportOptions{Projects: ids[:1]})
	b := emptyLibrary(t)
	changed := repack(t, pack, func(name string, body []byte) []byte {
		if strings.HasSuffix(name, "/notes.md") {
			return []byte("notes of the second")
		}
		return nil
	})
	if _, err := b.PackImport(bytes.NewReader(changed), PackImportOptions{}); err == nil || !strings.Contains(err.Error(), "does not match its hash") {
		t.Fatalf("changed byte: %v", err)
	}
	extra := repack(t, pack, func(name string, body []byte) []byte { return nil })
	// append an unlisted entry
	extra = append(extra[:0:0], craftExtra(t, extra)...)
	if _, err := b.PackImport(bytes.NewReader(extra), PackImportOptions{}); err == nil || !strings.Contains(err.Error(), "does not list") {
		t.Fatalf("extra file: %v", err)
	}
	if len(b.LibraryProjectIDs()) != 0 || designkit.Resolve(b.KitsDir(), "acme-ds", "1.0.0", nil) != "" {
		t.Fatal("something was written")
	}
}

// craftExtra adds an entry the manifest does not list.
func craftExtra(t *testing.T, pack []byte) []byte {
	t.Helper()
	gz, _ := gzip.NewReader(bytes.NewReader(pack))
	tr := tar.NewReader(gz)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		body, _ := io.ReadAll(tr)
		_ = tw.WriteHeader(h)
		_, _ = tw.Write(body)
	}
	_ = tw.WriteHeader(&tar.Header{Name: "projects/x/extra.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func TestPackLeavesOutServiceFilesCredentialsAndAgentInstructions(t *testing.T) {
	a, ids := packSource(t)
	proj := a.ArtifactDir(ids[1])
	writeFile(t, filepath.Join(proj, ".recordings", "take.webm"), "video")
	writeFile(t, filepath.Join(proj, "project", ".env"), "TOKEN=1")
	writeFile(t, filepath.Join(proj, "project", "id_rsa"), "key")
	writeFile(t, filepath.Join(proj, "project", "CLAUDE.md"), "do things")
	writeFile(t, filepath.Join(proj, "project", "picture.txt"), "keep")
	var buf bytes.Buffer
	m, err := a.PackExport(context.Background(), PackExportOptions{Projects: ids[1:]}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range m.Files {
		names = append(names, f.Path)
	}
	all := strings.Join(names, "\n")
	for _, bad := range []string{".recordings", ".env", "id_rsa", "CLAUDE.md"} {
		if strings.Contains(all, bad) {
			t.Errorf("%s went into the pack", bad)
		}
	}
	if !strings.Contains(all, "picture.txt") {
		t.Error("an ordinary file is missing")
	}
	// A pack made elsewhere that lists such files: they are not taken.
	files := map[string]string{"projects/foreign/project/index.dc.html": "<html></html>", "projects/foreign/AGENTS.md": "obey", "projects/foreign/.secret/a": "s"}
	var list []designkit.ExportFile
	for n, b := range files {
		sum := sha256hex(b)
		list = append(list, designkit.ExportFile{Path: n, Size: int64(len(b)), SHA256: sum})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	var own []designkit.ExportFile
	for _, f := range list {
		own = append(own, designkit.ExportFile{Path: strings.TrimPrefix(f.Path, "projects/foreign/"), Size: f.Size, SHA256: f.SHA256})
	}
	manifest := `{"format":"sinopia-pack","schema":1,"projects":[{"id":"foreign","title":"Foreign","files":1,"bytes":1,"sha256":"` + packTreeSum(own) + `"}],"kits":[],"files":[`
	for i, f := range list {
		if i > 0 {
			manifest += ","
		}
		manifest += `{"path":"` + f.Path + `","size":` + itoa(f.Size) + `,"sha256":"` + f.SHA256 + `"}`
	}
	manifest += `],"sha256":"` + packTreeSum(list) + `"}`
	b := emptyLibrary(t)
	res, err := b.PackImport(bytes.NewReader(craftPack(t, manifest, files)), PackImportOptions{})
	if err != nil || len(res.Dropped) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	got := contentOf(t, b, "foreign")
	if len(got) != 1 || got["project/index.dc.html"] == "" {
		t.Fatalf("%v", got)
	}
}

func TestPackBringsTheParentKitsAndInstallsThemInOrder(t *testing.T) {
	a, ids := packSource(t)
	writeFile(t, filepath.Join(a.KitsDir(), "base-ds", "2.0.0", "kit.json"), `{"id":"base-ds","version":"2.0.0"}`)
	writeFile(t, filepath.Join(a.KitsDir(), "base-ds", "2.0.0", "tokens.css"), ":root{--a:1}")
	writeFile(t, filepath.Join(a.KitsDir(), "acme-ds", "1.0.0", "kit.json"), `{"id":"acme-ds","version":"1.0.0","requires":[{"id":"base-ds","version":"2.0.0"}]}`)
	pack := exportPack(t, a, PackExportOptions{Projects: ids[:1]})
	b := emptyLibrary(t)
	res, err := b.PackImport(bytes.NewReader(pack), PackImportOptions{})
	if err != nil || len(res.Kits) != 2 || res.Kits[0].ID != "base-ds" {
		t.Fatalf("%+v %v", res.Kits, err)
	}
	if !sameMap(kitContent(t, a, "base-ds", "2.0.0"), kitContent(t, b, "base-ds", "2.0.0")) {
		t.Fatal("the parent kit differs")
	}
}

func TestPackWithRestrictedKitFilesNeedsConfirmation(t *testing.T) {
	a, ids := packSource(t)
	writeFile(t, filepath.Join(a.KitsDir(), "acme-ds", "1.0.0", "kit.json"),
		`{"id":"acme-ds","version":"1.0.0","fonts":[{"id":"f","family":"Corp Sans","prefix":"corp","license":{"kind":"restricted","holder":"Corp"}}]}`)
	writeFile(t, filepath.Join(a.KitsDir(), "acme-ds", "1.0.0", "fonts", "corp-regular.woff2"), "font")
	var buf bytes.Buffer
	_, err := a.PackExport(context.Background(), PackExportOptions{Projects: ids[:1]}, &buf)
	var re *PackRestrictedError
	if !errors.As(err, &re) || len(re.Restricted) != 1 {
		t.Fatalf("%v", err)
	}
	buf.Reset()
	if _, err := a.PackExport(context.Background(), PackExportOptions{Projects: ids[:1], AcceptRestricted: true}, &buf); err != nil {
		t.Fatal(err)
	}
}

func TestPackOfKitsOnlyAndOfAProjectWhoseKitIsMissing(t *testing.T) {
	a, ids := packSource(t)
	pack := exportPack(t, a, PackExportOptions{Kits: []string{"acme-ds"}})
	b := emptyLibrary(t)
	res, err := b.PackImport(bytes.NewReader(pack), PackImportOptions{})
	if err != nil || len(res.Projects) != 0 || len(res.Kits) != 1 || res.Kits[0].Status != "added" {
		t.Fatalf("%+v %v", res, err)
	}
	if err := a.SetKitPin(ids[1], &KitPin{ID: "ghost-ds", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PackExport(context.Background(), PackExportOptions{Projects: ids[1:]}, io.Discard); err == nil || !strings.Contains(err.Error(), "ghost-ds") {
		t.Fatalf("a pin to a missing kit: %v", err)
	}
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestPackRefusesAProjectWhoseHashIsNotItsFilesAndBadKitNames(t *testing.T) {
	a, ids := packSource(t)
	pack := exportPack(t, a, PackExportOptions{Projects: ids[:1]})
	forged := repack(t, pack, func(name string, body []byte) []byte {
		if name == PackManifestName {
			return []byte(strings.Replace(string(body), `"sha256": "`, `"sha256": "0`, 1))
		}
		return nil
	})
	b := emptyLibrary(t)
	if _, err := b.PackImport(bytes.NewReader(forged), PackImportOptions{}); err == nil {
		t.Fatal("a manifest with a forged hash was accepted")
	}
	for _, bad := range []string{"draft", "../x", "Acme DS", "acme-ds@draft", "acme-ds@../1"} {
		if _, err := a.PackExport(context.Background(), PackExportOptions{Kits: []string{bad}}, io.Discard); err == nil {
			t.Errorf("kit %q was packed", bad)
		}
	}
}
