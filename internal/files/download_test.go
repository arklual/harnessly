package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func downloadFixture(t *testing.T) (root, outside string) {
	t.Helper()
	base, _ := Realpath(t.TempDir())
	root = filepath.Join(base, "proj")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{root + "/src/fonts", root + "/.ssh", root + "/empty", outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(root+"/index.html", "<h1>hi</h1>")
	write(root+"/src/app.js", "console.log(1)")
	write(root+"/src/fonts/Inter.woff2", "font")
	write(root+"/.env", "TOKEN=secret")
	write(root+"/.ssh/id_rsa", "key")
	write(root+"/server.pem", "pem")
	write(outside+"/passwd", "root")
	write(root+"/src/inside.txt", "inside")
	if err := os.Symlink(outside+"/passwd", root+"/src/out-link"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root+"/src/inside.txt", root+"/in-link"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root+"/dir-link"); err != nil {
		t.Fatal(err)
	}
	return root, outside
}

func zipNames(t *testing.T, body []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func TestZipFolderKeepsSecretsAndLinksOut(t *testing.T) {
	root, _ := downloadFixture(t)
	a := NewAccess([]string{root}, nil)
	allow := a.Allowed
	it, refused := a.ResolveDownload(root, allow)
	if refused != nil || !it.Dir {
		t.Fatalf("folder not resolved: %+v %+v", it, refused)
	}
	rec := httptest.NewRecorder()
	a.ServeZipDownload(rec, httptest.NewRequest(http.MethodGet, "/", nil), []DownloadItem{it}, ZipName([]DownloadItem{it}, ""), allow)
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, `filename="proj.zip"`) {
		t.Fatalf("disposition %q", got)
	}
	names := zipNames(t, rec.Body.Bytes())
	var list []string
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	want := []string{"proj/", "proj/empty/", "proj/in-link", "proj/index.html", "proj/src/", "proj/src/app.js", "proj/src/fonts/", "proj/src/fonts/Inter.woff2", "proj/src/inside.txt"}
	if strings.Join(list, ",") != strings.Join(want, ",") {
		t.Fatalf("entries\n got %v\nwant %v", list, want)
	}
	if names["proj/in-link"] != "inside" || names["proj/src/fonts/Inter.woff2"] != "font" {
		t.Fatalf("contents %v", names)
	}
}

func TestResolveDownloadRefusesSecretsAndOutside(t *testing.T) {
	root, outside := downloadFixture(t)
	a := NewAccess([]string{root}, nil)
	for _, p := range []string{root + "/.env", root + "/.ssh/id_rsa", root + "/server.pem", outside + "/passwd", root + "/src/out-link", "relative"} {
		if _, refused := a.ResolveDownload(p, a.Allowed); refused == nil {
			t.Errorf("%s handed over", p)
		}
	}
	if _, refused := a.ResolveDownload(root+"/src/fonts/Inter.woff2", a.Allowed); refused != nil {
		t.Errorf("a font refused: %+v", refused)
	}
}

func TestFileDownloadResumes(t *testing.T) {
	root, _ := downloadFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=4-")
	rec := httptest.NewRecorder()
	ServeFileDownload(rec, req, root+"/index.html")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "hi</h1>" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("headers %v", rec.Header())
	}
}

func TestZipOfSeveralNamesThemApart(t *testing.T) {
	root, _ := downloadFixture(t)
	a := NewAccess([]string{root}, nil)
	one, _ := a.ResolveDownload(root+"/index.html", a.Allowed)
	two, _ := a.ResolveDownload(root+"/src", a.Allowed)
	same := one
	items := []DownloadItem{one, two, same}
	rec := httptest.NewRecorder()
	a.ServeZipDownload(rec, httptest.NewRequest(http.MethodGet, "/", nil), items, ZipName(items, ""), a.Allowed)
	names := zipNames(t, rec.Body.Bytes())
	for _, n := range []string{"index.html", "index (2).html", "src/app.js"} {
		if _, ok := names[n]; !ok {
			t.Errorf("no %s in %v", n, names)
		}
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "files.zip") {
		t.Errorf("name %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestTarListsAndHandsOneFileOut(t *testing.T) {
	root, _ := downloadFixture(t)
	p := root + "/site.tar.gz"
	f, _ := os.Create(p)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range []struct{ name, body string }{{"./index.html", "<p>x</p>"}, {"../evil", "no"}, {"css/a.css", "b{}"}} {
		_ = tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	f.Close()
	a := NewAccess([]string{root}, nil)
	rec := httptest.NewRecorder()
	a.ServePreview(rec, httptest.NewRequest(http.MethodGet, "/", nil), ParseQuery("zip=list&path="+EncodeURIComponent(p)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"index.html"`) || strings.Contains(rec.Body.String(), "evil") {
		t.Fatalf("list %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	a.ServePreview(rec, httptest.NewRequest(http.MethodGet, "/", nil), ParseQuery("zip=entry&name=css%2Fa.css&path="+EncodeURIComponent(p)))
	if rec.Code != 200 || rec.Body.String() != "b{}" {
		t.Fatalf("entry %d %q", rec.Code, rec.Body.String())
	}
}
