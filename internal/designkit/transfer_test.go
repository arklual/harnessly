package designkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const kitJSON = `{"id":"acme-ds","version":"0.16.0","upstream":{"tested":["r1"]},"fonts":[{"id":"acme-sans","family":"Acme-Sans","license":{"kind":"restricted","holder":"Acme Corp"}}]}`

func installedKit(t *testing.T, dir string) {
	t.Helper()
	entries := kitFiles(entry{name: "./kit.json", body: kitJSON}, entry{name: "./fonts/Acme-Sans_Regular.woff2", body: "tsans"}, entry{name: "./sources/deck.pptx", body: "heavy"})
	if _, err := Install(dir, "acme-ds", "0.16.0", bytes.NewReader(tarball(t, true, entries...)), Options{Now: fixedClock()}); err != nil {
		t.Fatal(err)
	}
}

func export(t *testing.T, dir string, opt ExportOptions) []byte {
	t.Helper()
	m, err := BuildManifest(dir, "acme-ds", "0.16.0", opt)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteArchive(dir, m, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExportWarnsAboutRestrictedFilesUntilConfirmed(t *testing.T) {
	dir := t.TempDir()
	installedKit(t, dir)
	m, err := BuildManifest(dir, "acme-ds", "0.16.0", ExportOptions{})
	if !errors.Is(err, ErrRestricted) || m == nil || len(m.Restricted) != 1 || m.Restricted[0].Path != "fonts/Acme-Sans_Regular.woff2" {
		t.Fatalf("restricted: %v %+v", err, m)
	}
	if m.WithSources {
		t.Fatal("sources are not exported unless asked")
	}
	for _, f := range m.Files {
		if strings.HasPrefix(f.Path, "sources/") {
			t.Fatalf("sources leaked into the export: %s", f.Path)
		}
	}
	if _, err := BuildManifest(dir, "acme-ds", "0.16.0", ExportOptions{AcceptRestricted: true, WithSources: true}); err != nil {
		t.Fatal(err)
	}
}

func TestExportThenImportElsewhereGivesTheSameBytes(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	installedKit(t, src)
	archive := export(t, src, ExportOptions{AcceptRestricted: true, WithSources: true})

	res, err := Import(dst, bytes.NewReader(archive), ImportOptions{UpstreamRelease: "r1", Now: fixedClock()})
	if err != nil || !res.Installed || res.Unchanged || len(res.Warnings) != 0 {
		t.Fatalf("import: %v %+v", err, res)
	}
	a, _ := hashTree(Resolve(src, "acme-ds", "0.16.0", nil), nil)
	b, _ := hashTree(Resolve(dst, "acme-ds", "0.16.0", nil), nil)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("trees differ:\n%v\n%v", a, b)
	}
	if _, err := os.Stat(filepath.Join(dst, "acme-ds", "0.16.0", ExportManifest)); err == nil {
		t.Fatal("the export manifest must not end up inside the kit")
	}
	again, err := Import(dst, bytes.NewReader(archive), ImportOptions{})
	if err != nil || !again.Unchanged || again.Installed {
		t.Fatalf("second import: %v %+v", err, again)
	}
}

func TestImportRefusesDamagedAndForeignFiles(t *testing.T) {
	src := t.TempDir()
	installedKit(t, src)
	good := export(t, src, ExportOptions{AcceptRestricted: true})
	// flip a byte in the packed font: the archive stays a valid tar, the hash no longer matches
	m, _ := BuildManifest(src, "acme-ds", "0.16.0", ExportOptions{AcceptRestricted: true})
	files := []entry{}
	for _, f := range m.Files {
		body, _ := os.ReadFile(filepath.Join(Resolve(src, "acme-ds", "0.16.0", nil), f.Path))
		if f.Path == "bundle.js" {
			body = []byte("window.Evil={}")
		}
		files = append(files, entry{name: f.Path, body: string(body)})
	}
	raw, _ := os.ReadFile(filepath.Join(t.TempDir(), "none"))
	_ = raw
	manifest, _ := jsonOf(m)
	damaged := tarball(t, true, append([]entry{{name: ExportManifest, body: manifest}}, files...)...)
	dst := t.TempDir()
	if _, err := Import(dst, bytes.NewReader(damaged), ImportOptions{}); err == nil || !strings.Contains(err.Error(), "bundle.js") {
		t.Fatalf("a changed file must be refused: %v", err)
	}
	extra := tarball(t, true, append(append([]entry{{name: ExportManifest, body: manifest}}, files...), entry{name: "evil.js", body: "x"})...)
	if _, err := Import(dst, bytes.NewReader(extra), ImportOptions{}); err == nil {
		t.Fatal("a file the manifest does not list must be refused")
	}
	plain := tarball(t, true, kitFiles()...)
	if _, err := Import(dst, bytes.NewReader(plain), ImportOptions{}); err == nil || !strings.Contains(err.Error(), ExportManifest) {
		t.Fatalf("a tarball without a manifest: %v", err)
	}
	if kits := List(dst); len(kits) != 0 {
		t.Fatalf("a refused import installed something: %+v", kits)
	}
	if _, err := Import(dst, bytes.NewReader(good), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestImportNeverOverwritesAndShowsTheDifference(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	installedKit(t, src)
	archive := export(t, src, ExportOptions{AcceptRestricted: true})
	// the other machine has the same id and version with other content
	other := kitFiles(entry{name: "./kit.json", body: kitJSON}, entry{name: "./fonts/Acme-Sans_Regular.woff2", body: "tsans"})
	other[1].body = "window.Other={}"
	if _, err := Install(dst, "acme-ds", "0.16.0", bytes.NewReader(tarball(t, true, other...)), Options{}); err != nil {
		t.Fatal(err)
	}
	res, err := Import(dst, bytes.NewReader(archive), ImportOptions{})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrExists) || !reflect.DeepEqual(conflict.Diff.Changed, []string{"bundle.js"}) || res.Conflict == nil {
		t.Fatalf("conflict: %v %+v", err, res)
	}
	body, _ := os.ReadFile(filepath.Join(dst, "acme-ds", "0.16.0", "bundle.js"))
	if string(body) != "window.Other={}" {
		t.Fatal("the installed kit was overwritten")
	}

	copyRes, err := Import(dst, bytes.NewReader(archive), ImportOptions{AsVersion: "0.16.1"})
	if err != nil || !copyRes.Installed || copyRes.Version != "0.16.1" {
		t.Fatalf("new version: %v %+v", err, copyRes)
	}
	asID, err := Import(dst, bytes.NewReader(archive), ImportOptions{AsID: "acme-copy"})
	if err != nil || !asID.Installed {
		t.Fatalf("copy under another id: %v %+v", err, asID)
	}
	rec, _ := os.ReadFile(filepath.Join(dst, "acme-copy", "0.16.0", KitFile))
	if !strings.Contains(string(rec), `"acme-copy"`) {
		t.Fatalf("kit.json keeps the old id: %s", rec)
	}
}

func TestImportDryRunChangesNothingAndNeedsRequiredKits(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	entries := kitFiles(entry{name: "./kit.json", body: `{"id":"acme-ds","version":"0.16.0","requires":[{"id":"base","version":"1.0.0"}]}`})
	if _, err := Install(src, "acme-ds", "0.16.0", bytes.NewReader(tarball(t, true, entries...)), Options{}); err != nil {
		t.Fatal(err)
	}
	archive := export(t, src, ExportOptions{})
	if _, err := Import(dst, bytes.NewReader(archive), ImportOptions{}); err == nil || !strings.Contains(err.Error(), "base 1.0.0") {
		t.Fatalf("a missing required kit: %v", err)
	}
	if _, err := Install(dst, "base", "1.0.0", bytes.NewReader(tarball(t, true, entry{name: "x.js", body: "x"})), Options{}); err != nil {
		t.Fatal(err)
	}
	res, err := Import(dst, bytes.NewReader(archive), ImportOptions{DryRun: true, UpstreamRelease: "r9"})
	if err != nil || res.Installed || len(res.Warnings) != 1 {
		t.Fatalf("dry run: %v %+v", err, res)
	}
	if Resolve(dst, "acme-ds", "0.16.0", nil) != "" {
		t.Fatal("a dry run installed the kit")
	}
}

func jsonOf(m *Manifest) (string, error) {
	raw, err := json.Marshal(m)
	return string(raw), err
}
