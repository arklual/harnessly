package designkit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type entry struct {
	name, body string
	typ        byte
	link       string
}

func tarball(t *testing.T, gz bool, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	var w interface {
		Write([]byte) (int, error)
		Close() error
	} = nopCloser{&buf}
	if gz {
		w = gzip.NewWriter(&buf)
	}
	tw := tar.NewWriter(w)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o644, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	_ = w.Close()
	return buf.Bytes()
}

type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

func kitFiles(extra ...entry) []entry {
	return append([]entry{
		{name: "./", typ: tar.TypeDir},
		{name: "./bundle.js", body: "window.Kit={}"},
		{name: "./fonts/", typ: tar.TypeDir},
		{name: "./fonts/a.woff2", body: "font"},
		{name: "./design-system.json", body: `{"acme":{"id":"acme-ds","version":"0.16.0"}}`},
	}, extra...)
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(1_700_000_000, 0) }
}

func TestInstallUnpacksAKitByVersion(t *testing.T) {
	dir := t.TempDir()
	res, err := Install(dir, "acme-ds", "0.16.0", bytes.NewReader(tarball(t, true, kitFiles()...)), Options{Now: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 3 || res.Unchanged || res.Replaced || res.InstalledAt != 1_700_000_000 || len(res.SHA256) != 64 {
		t.Fatalf("result: %+v", res)
	}
	if got := Resolve(dir, "acme-ds", "0.16.0", []string{"fonts", "a.woff2"}); got == "" {
		t.Fatal("an installed file is not found")
	}
	if got := Files(dir, "acme-ds", "0.16.0"); !reflect.DeepEqual(got, []string{"bundle.js", "design-system.json", "fonts/a.woff2"}) {
		t.Fatalf("files: %v", got)
	}
	kits := List(dir)
	if len(kits) != 1 || kits[0].ID != "acme-ds" || kits[0].Versions[0].Version != "0.16.0" || kits[0].Versions[0].SHA256 != res.SHA256 {
		t.Fatalf("list: %+v", kits)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "acme-ds"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".unpack-") || strings.HasPrefix(e.Name(), ".replaced-") {
			t.Fatalf("a temporary folder is left behind: %s", e.Name())
		}
	}
}

func TestInstallTakesAPlainTarAndAWrappingFolder(t *testing.T) {
	dir := t.TempDir()
	wrapped := []entry{
		{name: "dist-browser/", typ: tar.TypeDir},
		{name: "dist-browser/bundle.js", body: "x"},
	}
	if _, err := Install(dir, "k", "1.0.0", bytes.NewReader(tarball(t, false, wrapped...)), Options{}); err != nil {
		t.Fatal(err)
	}
	if Resolve(dir, "k", "1.0.0", []string{"bundle.js"}) == "" {
		t.Fatal("the wrapping folder was not removed")
	}
}

func TestInstallAgainIsANoOpAndOtherContentNeedsForce(t *testing.T) {
	dir := t.TempDir()
	first := tarball(t, true, kitFiles()...)
	if _, err := Install(dir, "acme-ds", "0.16.0", bytes.NewReader(first), Options{Now: fixedClock()}); err != nil {
		t.Fatal(err)
	}
	again, err := Install(dir, "acme-ds", "0.16.0", bytes.NewReader(first), Options{})
	if err != nil || !again.Unchanged {
		t.Fatalf("the same tarball: %+v %v", again, err)
	}
	other := tarball(t, true, kitFiles(entry{name: "./extra.txt", body: "y"})...)
	if _, err := Install(dir, "acme-ds", "0.16.0", bytes.NewReader(other), Options{}); !errors.Is(err, ErrExists) {
		t.Fatalf("other content under the same version: %v", err)
	}
	if Resolve(dir, "acme-ds", "0.16.0", []string{"extra.txt"}) != "" {
		t.Fatal("a refused install changed the installed version")
	}
	forced, err := Install(dir, "acme-ds", "0.16.0", bytes.NewReader(other), Options{Force: true})
	if err != nil || !forced.Replaced || Resolve(dir, "acme-ds", "0.16.0", []string{"extra.txt"}) == "" {
		t.Fatalf("force: %+v %v", forced, err)
	}
}

func TestInstallRefusesWhatCouldEscapeTheKit(t *testing.T) {
	bad := map[string][]entry{
		"parent path":   {{name: "../evil.js", body: "x"}},
		"nested parent": {{name: "a/../../evil.js", body: "x"}},
		"absolute":      {{name: "/etc/evil.js", body: "x"}},
		"backslash":     {{name: `a\b.js`, body: "x"}},
		"symlink":       {{name: "link", typ: tar.TypeSymlink, link: "/etc/passwd"}},
		"hard link":     {{name: "hard", typ: tar.TypeLink, link: "bundle.js"}},
		"device":        {{name: "dev", typ: tar.TypeChar}},
		"empty":         {{name: "only-dir/", typ: tar.TypeDir}},
	}
	for name, entries := range bad {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			all := append([]entry{{name: "bundle.js", body: "x"}}, entries...)
			if name == "empty" {
				all = entries
			}
			if _, err := Install(dir, "k", "1.0.0", bytes.NewReader(tarball(t, true, all...)), Options{}); err == nil {
				t.Fatal("installed")
			}
			if List(dir) == nil || len(List(dir)) != 0 {
				t.Fatalf("a refused install left a kit: %+v", List(dir))
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.js")); err == nil {
				t.Fatal("a file was written outside the kits folder")
			}
		})
	}
}

func TestInstallChecksWhichKitAndVersionTheArchiveIs(t *testing.T) {
	dir := t.TempDir()
	data := tarball(t, true, kitFiles()...)
	if _, err := Install(dir, "other-kit", "0.16.0", bytes.NewReader(data), Options{}); err == nil || !strings.Contains(err.Error(), "acme-ds") {
		t.Fatalf("wrong id: %v", err)
	}
	if _, err := Install(dir, "acme-ds", "0.15.0", bytes.NewReader(data), Options{}); err == nil || !strings.Contains(err.Error(), "0.16.0") {
		t.Fatalf("wrong version: %v", err)
	}
	if len(List(dir)) != 0 {
		t.Fatal("a mismatching archive was installed")
	}
}

func TestInstallRefusesNamesThatAreNotKitsOrVersions(t *testing.T) {
	data := tarball(t, true, kitFiles()...)
	for _, c := range [][2]string{{"../x", "1.0.0"}, {"Kit", "1.0.0"}, {"", "1.0.0"}, {"k", "1.0"}, {"k", "../1.0.0"}, {"k", "1.0.0/x"}, {"k", "v1.0.0"}} {
		if _, err := Install(t.TempDir(), c[0], c[1], bytes.NewReader(data), Options{}); err == nil {
			t.Fatalf("installed as %q %q", c[0], c[1])
		}
	}
}

func TestInstallRefusesAnArchiveThatIsNotOne(t *testing.T) {
	if _, err := Install(t.TempDir(), "k", "1.0.0", strings.NewReader("hello"), Options{}); err == nil {
		t.Fatal("plain text installed")
	}
	if _, err := Install(t.TempDir(), "k", "1.0.0", bytes.NewReader([]byte{0x1f, 0x8b, 1, 2, 3}), Options{}); err == nil {
		t.Fatal("a broken gzip installed")
	}
}

func TestResolveStaysInsideTheVersionFolder(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, "k", "1.0.0", bytes.NewReader(tarball(t, true, entry{name: "bundle.js", body: "x"})), Options{}); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "k", "1.0.0", "link.txt")); err != nil {
		t.Fatal(err)
	}
	for _, segs := range [][]string{{".."}, {"..", "..", "secret.txt"}, {"link.txt"}, {"bundle.js", ".."}, {""}, {"a\\b"}, {"missing.js"}} {
		if got := Resolve(dir, "k", "1.0.0", segs); got != "" {
			t.Fatalf("%q resolved to %s", segs, got)
		}
	}
	if Resolve(dir, "k", "1.0.0", []string{"bundle.js"}) == "" {
		t.Fatal("a real file is not found")
	}
	if Resolve(dir, "k", "2.0.0", []string{"bundle.js"}) != "" || Resolve(dir, "nope", "1.0.0", []string{"bundle.js"}) != "" {
		t.Fatal("a kit or version that is not installed resolved")
	}
	if Resolve("", "k", "1.0.0", []string{"bundle.js"}) != "" {
		t.Fatal("no kits folder resolved")
	}
}

func TestListOrdersVersionsNewestFirstAndIgnoresLeftovers(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"0.9.0", "0.16.0", "0.16.0-rc.1", "0.10.2"} {
		if _, err := Install(dir, "k", v, bytes.NewReader(tarball(t, true, entry{name: "bundle.js", body: v})), Options{}); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.MkdirAll(filepath.Join(dir, "k", ".unpack-123"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "k", "not-a-version"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "Bad_ID", "1.0.0"), 0o755)
	var got []string
	for _, v := range List(dir)[0].Versions {
		got = append(got, v.Version)
	}
	if want := []string{"0.16.0", "0.16.0-rc.1", "0.10.2", "0.9.0"}; !reflect.DeepEqual(got, want) || len(List(dir)) != 1 {
		t.Fatalf("versions: %v (%d kits)", got, len(List(dir)))
	}
	if len(List(filepath.Join(dir, "missing"))) != 0 {
		t.Fatal("a missing folder lists kits")
	}
}
