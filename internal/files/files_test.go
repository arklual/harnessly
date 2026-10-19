package files

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// project is a tmp folder with a project, a folder outside it and a link out of it.
func project(t *testing.T) (tmp, root string) {
	t.Helper()
	tmp, _ = filepath.EvalSymlinks(t.TempDir())
	root = filepath.Join(tmp, "project")
	write(t, filepath.Join(root, "hello.txt"), "Привет\n")
	write(t, filepath.Join(tmp, "outside", "private.txt"), "private")
	if err := os.Symlink(filepath.Join(tmp, "outside"), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	return tmp, root
}

func TestEditorRefusesEverythingOutsideTheProject(t *testing.T) {
	tmp, root := project(t)
	_ = os.Symlink("loop-b", filepath.Join(root, "loop-a"))
	_ = os.Symlink("loop-a", filepath.Join(root, "loop-b"))
	cases := map[string]EditorError{
		"../outside/private.txt":                     {403, "Путь находится за пределами проекта."},
		filepath.Join(tmp, "outside", "private.txt"): {403, "Можно открывать только файлы текущего проекта."},
		"escape/private.txt":                         {403, "Ссылка ведёт за пределы проекта."},
		"escape":                                     {403, "Ссылка ведёт за пределы проекта."},
		"a\x00b":                                     {403, "Можно открывать только файлы текущего проекта."},
		"..":                                         {403, "Путь находится за пределами проекта."},
		"loop-a":                                     {403, "Нет доступа к этому файлу."},
		"missing.txt":                                {404, "Файл или папка больше не существуют."},
		"hello.txt/x":                                {404, "Файл или папка больше не существуют."},
		"../project/../outside/private.txt":          {403, "Путь находится за пределами проекта."},
	}
	for rel, want := range cases {
		if _, err := ReadProjectFile(root, rel); err == nil || *err != want {
			t.Errorf("read %q: %v, want %v", rel, err, want)
		}
	}
	if _, err := ListProject(root, "escape"); err == nil || err.Status != 403 {
		t.Errorf("listing through a link out: %v", err)
	}
	// A name that only climbs back into the project is fine.
	if v, err := ReadProjectFile(root, "../project/hello.txt"); err != nil || v[1].V != "Привет\n" {
		t.Errorf("climbing back in: %v %v", v, err)
	}
}

func TestEditorReadsOnlyRegularUTF8TextUpToOneMegabyte(t *testing.T) {
	_, root := project(t)
	write(t, filepath.Join(root, "bin"), "A\x00B")
	write(t, filepath.Join(root, "invalid"), "\xff")
	write(t, filepath.Join(root, "exact"), strings.Repeat("a", editorMaxBytes))
	write(t, filepath.Join(root, "over"), strings.Repeat("a", editorMaxBytes+1))
	write(t, filepath.Join(root, "bom"), "\ufeffx")
	_ = os.Mkdir(filepath.Join(root, "folder"), 0o755)
	_ = syscall.Mkfifo(filepath.Join(root, "fifo"), 0o644)
	for rel, want := range map[string]int{"bin": 415, "invalid": 415, "over": 415, "folder": 415, "fifo": 415} {
		if _, err := ReadProjectFile(root, rel); err == nil || err.Status != want {
			t.Errorf("%s: %v", rel, err)
		}
	}
	v, err := ReadProjectFile(root, "exact")
	if err != nil || v[3].V != editorMaxBytes {
		t.Fatalf("exact: %v", err)
	}
	v, err = ReadProjectFile(root, "bom")
	if err != nil || v[1].V != "\ufeffx" {
		t.Fatalf("a byte order mark is kept: %v %v", v, err)
	}
	// The revision is tied to the project's folder and the file, not only to the text.
	a, _ := ReadProjectFile(root, "hello.txt")
	other := filepath.Join(filepath.Dir(root), "other")
	write(t, filepath.Join(other, "hello.txt"), "Привет\n")
	b, _ := ReadProjectFile(other, "hello.txt")
	if a[2].V == b[2].V || len(a[2].V.(string)) != 64 {
		t.Fatalf("revisions %v %v", a[2].V, b[2].V)
	}
}

func TestEditorListsFoldersFirstByLocaleAndLeavesOutLinks(t *testing.T) {
	_, root := project(t)
	for _, n := range []string{"b.txt", "B.txt", "a.txt", "_x", "10", "9"} {
		write(t, filepath.Join(root, "src", n), "x")
	}
	_ = os.Mkdir(filepath.Join(root, "src", "zdir"), 0o755)
	_ = os.Symlink("a.txt", filepath.Join(root, "src", "link"))
	v, err := ListProject(root, "src/")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range v[2].V.([]Object) {
		names = append(names, e[0].V.(string)+":"+e[1].V.(string))
	}
	want := []string{"zdir:src/zdir", "_x:src/_x", "10:src/10", "9:src/9", "a.txt:src/a.txt", "b.txt:src/b.txt", "B.txt:src/B.txt"}
	if !reflect.DeepEqual(names, want) || v[1].V != "project" || v[3].V != false {
		t.Fatalf("%v %v", names, v)
	}
	for i := 0; i < editorMaxEntries+5; i++ {
		write(t, filepath.Join(root, "many", "f"+strconv.Itoa(i)), "")
	}
	v, _ = ListProject(root, "many")
	if v[3].V != true || len(v[2].V.([]Object)) != editorMaxEntries {
		t.Fatalf("truncated listing: %v entries, truncated %v", len(v[2].V.([]Object)), v[3].V)
	}
}

func timeAgo() time.Time { return time.Now().Add(-time.Hour) }

func TestRepoPathAndRef(t *testing.T) {
	for p, ok := range map[string]bool{"a/b": true, "a/../b": true, "a/../../b": false, "/etc": false, "/dev/null": false, "a\x00": false, "": false, "./x": true, "..": false} {
		if _, got := RepoPath([]string{p}); got != ok {
			t.Errorf("RepoPath(%q) = %v", p, got)
		}
	}
	if _, ok := RepoPath([]string{"a", "b"}); ok {
		t.Error("a repeated path is no path")
	}
	for r, want := range map[string]string{"": "FB", "HEAD~1": "HEAD~1", "WORKTREE": "WORKTREE", "-x": "", "a b": "", "main^": "main^", strings.Repeat("a", 202): ""} {
		got, ok := Ref([]string{r}, "FB")
		if !ok {
			got = ""
		}
		if got != want {
			t.Errorf("Ref(%q) = %q", r, got)
		}
	}
}

func TestWorktreeSideStaysInTheFolderByNameOnly(t *testing.T) {
	tmp, root := project(t)
	if s := fromWorktree(root, "../outside/private.txt"); s != (Side{}) {
		t.Errorf("outside by name: %+v", s)
	}
	// Links are followed here, as the diff view always did.
	if s := fromWorktree(root, "escape/private.txt"); s.Content != "private" {
		t.Errorf("through a link: %+v", s)
	}
	write(t, filepath.Join(root, "big"), strings.Repeat("a", diffMaxBytes+1))
	write(t, filepath.Join(root, "bin"), "\x00")
	write(t, filepath.Join(root, "bad"), "a\xffb")
	if s := fromWorktree(root, "big"); !s.Truncated {
		t.Errorf("big: %+v", s)
	}
	if s := fromWorktree(root, "bin"); !s.Binary {
		t.Errorf("bin: %+v", s)
	}
	if s := fromWorktree(root, "bad"); s.Content != "a�b" {
		t.Errorf("bad: %+v", s)
	}
	if s := fromWorktree(tmp, "project"); s != (Side{}) {
		t.Errorf("a folder: %+v", s)
	}
	got := FileContent(Side{Content: "a"}, Side{Binary: true})
	if got[0].V != "" || got[1].V != "" || got[3].V != true {
		t.Errorf("binary hides both sides: %v", got)
	}
}

func TestPreviewRootsSecretsTypesAndSizes(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	elsewhere, _ := filepath.EvalSymlinks(t.TempDir())
	write(t, filepath.Join(tmp, "a.png"), "png")
	write(t, filepath.Join(tmp, "doc.MD"), "md")
	write(t, filepath.Join(tmp, "noext"), "x")
	write(t, filepath.Join(tmp, ".ssh", "k.png"), "k")
	write(t, filepath.Join(tmp, "app", ".config", "tool", "x.png"), "k")
	write(t, filepath.Join(tmp, "id_ed25519"), "k")
	write(t, filepath.Join(tmp, ".ENV"), "k")
	write(t, filepath.Join(elsewhere, "out.png"), "o")
	_ = os.Symlink(filepath.Join(elsewhere, "out.png"), filepath.Join(tmp, "link.png"))
	f, _ := os.Create(filepath.Join(tmp, "big.png"))
	_ = f.Truncate(MaxPreviewBytes + 1)
	f.Close()
	f, _ = os.Create(filepath.Join(tmp, "v.mp4"))
	_ = f.Truncate(MaxPreviewBytes + 1)
	f.Close()
	a := NewAccess([]string{tmp}, []string{".config/tool"})
	cases := map[string]int{
		"a.png": 0, "doc.MD": 0, "v.mp4": 0, "noext": 415, ".ssh/k.png": 403, "app/.config/tool/x.png": 403,
		"id_ed25519": 403, ".ENV": 403, "link.png": 403, "missing.png": 404, "big.png": 413, ".": 415,
	}
	for name, want := range cases {
		got := a.Preview(filepath.Join(tmp, name))
		if got.Status != want || got.OK != (want == 0) {
			t.Errorf("preview %s: %+v, want %d", name, got, want)
		}
	}
	if got := a.Preview("relative.png"); got.Status != 400 || got.Text != "Нужен абсолютный путь" {
		t.Errorf("relative: %+v", got)
	}
	if got := a.Download(filepath.Join(tmp, "noext")); !got.OK {
		t.Errorf("any type downloads: %+v", got)
	}
	if got := a.Download(filepath.Join(tmp, ".ssh", "k.png")); got.Status != 403 {
		t.Errorf("secrets do not download: %+v", got)
	}
	// Folders that merely share a prefix with a root are not inside it.
	if (&Access{Roots: []string{"/tmp/a"}}).allowed("/tmp/ab/x") {
		t.Error("prefix is not containment")
	}
}

func TestSecretPatternIsASCIICaseInsensitiveOnly(t *testing.T) {
	a := NewAccess(nil, []string{".config/tool"})
	for p, want := range map[string]bool{
		"/a/.env": true, "/a/.Env.local": true, "/a/.SSH/x": true, "/a/.config/TOOL/x": true, "/a/x.PEM": true,
		"/a/id_RSA": true, "/a/ok.png": false, "/a/.ſsh": false, "/a/id_K": false, "/a/x\n.pem": false, "/a/.envy": false,
	} {
		if got := a.Secret(p); got != want {
			t.Errorf("Secret(%q) = %v", p, got)
		}
	}
}

func TestRanges(t *testing.T) {
	type r struct{ s, e int64 }
	prev := map[string]*r{"bytes=0-": {0, 9}, "bytes=-5": {5, 9}, "bytes=-0": nil, "bytes=5-2": nil, "bytes=10-": nil,
		"bytes=0-100": {0, 9}, " bytes=1-2 ": {1, 2}, "bytes=1-2,3-4": nil, "bytes=-15": {0, 9}}
	for h, want := range prev {
		s, e, ok := PreviewRange(h, 10)
		if ok != (want != nil) || ok && (s != want.s || e != want.e) {
			t.Errorf("PreviewRange(%q) = %d %d %v", h, s, e, ok)
		}
	}
	if _, _, ok := PreviewRange("bytes=0-", 0); ok {
		t.Error("an empty file has no range")
	}
	if _, _, present, sat := UploadRange("bytes=-0", 10); !present || sat {
		t.Error("bytes=-0 of an upload is unsatisfiable")
	}
	if _, _, present, _ := UploadRange("items=0-1", 10); present {
		t.Error("not a byte range")
	}
	if s, e, _, sat := UploadRange("bytes=-15", 10); !sat || s != 0 || e != 9 {
		t.Error("a suffix longer than the file is the file")
	}
}

func TestUploadPaths(t *testing.T) {
	dir := "/data/uploads"
	for segs, ok := range map[string]bool{"ws/tab/a.png": true, "a.png": true, "ws/../a.png": false, "ws/./a.png": false, "ws//a.png": false, "a\\b.png": false, "a%2Fb": true} {
		parts := strings.Split(segs, "/")
		if segs == "ws//a.png" {
			parts = []string{"ws", "", "a.png"}
		}
		if _, got := UploadPath(dir, parts); got != ok {
			t.Errorf("UploadPath(%q) = %v", segs, got)
		}
	}
	if _, ok := UploadPath(dir, []string{"x/y"}); ok {
		t.Error("a decoded slash inside a segment")
	}
	if _, ok := UploadPath(dir, nil); ok {
		t.Error("no segments")
	}
	for p, want := range map[string]string{"/x/a.PNG": "image/png", "/x/.png": "", "/x/a.ogv": "video/ogg", "/x/a.txt": ""} {
		if got := UploadType(p); got != want {
			t.Errorf("UploadType(%q) = %q", p, got)
		}
	}
	for p, open := range map[string]bool{"/api/uploads/w/t/a.png": false, "/api/uploads/w/t/a.mp4": false, "/api/uploads/x.pngx": false, "/api/uploads/a.pdf": false, "/icon.png": true} {
		if OpenWithoutSession(p) != open {
			t.Errorf("OpenWithoutSession(%q)", p)
		}
	}
}

func TestContextSearch(t *testing.T) {
	tmp, root := project(t)
	write(t, filepath.Join(root, "src", "index.ts"), "")
	write(t, filepath.Join(root, "node_modules", "m", "index.js"), "")
	write(t, filepath.Join(root, ".git", "HEAD"), "")
	write(t, filepath.Join(root, ".hidden", "index.ts"), "")
	write(t, filepath.Join(root, ".env"), "")
	write(t, filepath.Join(root, "key.PEM"), "")
	write(t, filepath.Join(root, "build"), "a file named like an ignored folder")
	write(t, filepath.Join(root, ".prettierrc"), "")
	got := SearchContext(root, QueryUnits(""))
	want := []string{"hello.txt", ".prettierrc", "src/index.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("all files: %v", got)
	}
	if got := SearchContext(root, QueryUnits("IDX")); !reflect.DeepEqual(got, []string{"src/index.ts"}) {
		t.Fatalf("fuzzy: %v", got)
	}
	if !AllowedContextRoot(root, []string{tmp}) || AllowedContextRoot(root, []string{root + "x"}) || AllowedContextRoot(root, []string{"", "/nonexistent"}) {
		t.Fatal("allowed roots")
	}
	if s, ok := FuzzyScore("src/index.ts", QueryUnits("idx")); !ok || s != 4.4 {
		t.Fatalf("score %v", s)
	}
	if s, _ := FuzzyScore("😀x/y", QueryUnits("y")); s != 0.4 {
		t.Fatalf("offsets count UTF-16 units: %v", s)
	}
	if len(QueryUnits(strings.Repeat("я", 300))) != ContextMaxQuery {
		t.Fatal("query cap")
	}
}

func TestMemoryNotes(t *testing.T) {
	cases := []struct{ raw, title, desc, body string }{
		{"---\nname: Foo\ndescription: \"Bar\"\n---\n# Title\nbody", "Title", "Bar", "# Title\nbody"},
		{"---\nname:\n  next\n---\nx", "next", "", "x"},
		{"#\nfoo", "foo", "", "#\nfoo"},
		{"#  \n", "n", "", "#"},
		{"---\nname: \"\n---\n", "n", "", ""},
		{"---\nname: a\rdescription: b\n---\n", "a", "b", ""},
		{"plain", "n", "", "plain"},
	}
	for _, c := range cases {
		o := ParseMemoryNote("n.md", c.raw)
		if o[1].V != c.title || o[2].V != c.desc || o[3].V != c.body {
			t.Errorf("%q: %v", c.raw, o)
		}
	}
	home := t.TempDir()
	a, b := filepath.Join(home, "projects"), filepath.Join(home, "sessions")
	write(t, filepath.Join(a, "p1", "memory", "x.md"), "old")
	write(t, filepath.Join(b, "s1", "memory", "x.md"), "# New\nnew")
	old := filepath.Join(a, "p1", "memory", "x.md")
	_ = os.Chtimes(old, timeAgo(), timeAgo())
	o, ok := ReadMemoryNote("x.md", []string{a, b})
	if !ok || o[1].V != "New" {
		t.Fatalf("freshest copy: %v %v", o, ok)
	}
	for _, bad := range []string{"../x.md", "x.txt", "", "a/x.md", "x.md\n"} {
		if _, ok := ReadMemoryNote(bad, []string{a, b}); ok {
			t.Errorf("name %q", bad)
		}
	}
}

func TestQueryAndPathsAsTheOldRouterSawThem(t *testing.T) {
	q := ParseQuery("a=1&a=2&p=%2Ftmp%2Fx&s=hello+world&bad=%zz&u=%E0%A4")
	if _, ok := q.String("a"); ok {
		t.Error("a repeated key is not a string")
	}
	if v, _ := q.String("p"); v != "/tmp/x" {
		t.Error(v)
	}
	if v, _ := q.String("s"); v != "hello world" {
		t.Error(v)
	}
	if v, _ := q.String("bad"); v != "%zz" {
		t.Error(v)
	}
	if v, _ := q.String("u"); v != "�" {
		t.Error(v)
	}
	for target, want := range map[string]bool{
		"/api/editor/files": true, "/agents/api/editor/files": true, "/api/editor/files/": true, "/api//editor/files": true,
		"/api/editor/%66iles": true, "/api/editor/files/x": false, "/agents/agents/api/editor/files": false, "/apix/editor/files": false,
	} {
		r := httptest.NewRequest(http.MethodGet, "http://h"+target+"?x=1", nil)
		if got := RouteIs(r, "/api/editor/files"); got != want {
			t.Errorf("RouteIs(%q) = %v", target, got)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "http://h/agents/api/uploads/w/a%2Fb.png", nil)
	parts, p, _ := APIParts(r)
	if !reflect.DeepEqual(parts, []string{"api", "uploads", "w", "a/b.png"}) || p != "/api/uploads/w/a%2Fb.png" {
		t.Errorf("%v %q", parts, p)
	}
}

func TestJSONAndETagAsTheOldRuntimeWroteThem(t *testing.T) {
	body := AppendJSON(nil, Object{{"a", "<script>&\u2028\x01\b"}, {"n", 3}, {"l", []string{"x"}}})
	if string(body) != "{\"a\":\"<script>&\u2028\\u0001\\b\",\"n\":3,\"l\":[\"x\"]}" {
		t.Fatalf("%s", body)
	}
	// From the old runtime: JSON.stringify({a:"plain"}).
	if got := ETag([]byte(`{"a":"plain"}`)); got != `"5kSgGFtI6HlSQxdFB6PJhVLZ2CU"` {
		t.Fatalf("etag %s", got)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("If-None-Match", `W/"x", "y"`)
	if !Fresh(r, `"y"`) || Fresh(r, `"z"`) {
		t.Fatal("fresh")
	}
	r.Header.Set("Cache-Control", "max-age=0, no-cache")
	if Fresh(r, `"y"`) {
		t.Fatal("no-cache revalidates")
	}
	w := httptest.NewRecorder()
	WriteJSON(w, httptest.NewRequest(http.MethodGet, "/", nil), 404, Error("Not found"))
	if w.Code != 404 || w.Header().Get("ETag") == "" || w.Body.String() != `{"error":"Not found"}` || w.Header().Get("Content-Length") != "21" {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body)
	}
}

func TestServeUploadAndPreviewBodies(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "w", "t", "a.mp3"), "0123456789")
	get := func(h http.HandlerFunc, rng string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if rng != "" {
			r.Header.Set("Range", rng)
		}
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}
	up := func(segs ...string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { ServeUpload(w, r, dir, segs) }
	}
	if w := get(up("w", "t", "a.mp3"), "bytes=2-4"); w.Code != 206 || w.Body.String() != "234" || w.Header().Get("Content-Range") != "bytes 2-4/10" {
		t.Fatalf("%d %v %q", w.Code, w.Header(), w.Body)
	}
	if w := get(up("w", "t", "a.mp3"), "bytes=20-"); w.Code != 416 || w.Header().Get("Content-Range") != "bytes */10" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if w := get(up("w", "t", "a.mp3"), ""); w.Code != 200 || w.Body.Len() != 10 || w.Header().Get("Cache-Control") != "private, max-age=86400" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if w := get(up("w", "..", "a.mp3"), ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := get(up("w", "t", "b.mp3"), ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := get(up("w", "t", "a.txt"), ""); w.Code != 415 {
		t.Fatal(w.Code)
	}
	if w := get(up("w", "t"), ""); w.Code != 415 {
		t.Fatal(w.Code)
	}
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.mp3"), "secret")
	_ = os.Symlink(filepath.Join(outside, "secret.mp3"), filepath.Join(dir, "w", "t", "out.mp3"))
	_ = os.Symlink(outside, filepath.Join(dir, "w", "linked"))
	_ = os.Symlink("a.mp3", filepath.Join(dir, "w", "t", "in.mp3"))
	for _, segs := range [][]string{{"w", "t", "out.mp3"}, {"w", "linked", "secret.mp3"}} {
		if w := get(up(segs...), ""); w.Code != 403 || w.Body.String() != `{"error":"Path not allowed"}` {
			t.Fatalf("%v: %d %s", segs, w.Code, w.Body)
		}
	}
	if w := get(up("w", "t", "in.mp3"), ""); w.Code != 200 || w.Body.String() != "0123456789" {
		t.Fatalf("a link inside: %d", w.Code)
	}

	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	write(t, filepath.Join(tmp, "p.html"), "<b>x</b>")
	write(t, filepath.Join(tmp, "v.webm"), "0123456789")
	write(t, filepath.Join(tmp, "Отчёт \"1\".bin"), "zz")
	a := NewAccess([]string{tmp}, nil)
	pv := func(query string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { a.ServePreview(w, r, ParseQuery(query)) }
	}
	w := get(pv("path="+filepath.Join(tmp, "p.html")), "")
	if w.Code != 200 || w.Body.String() != "<b>x</b>" || w.Header().Get("Content-Security-Policy") != PreviewCSP || w.Header().Get("ETag") == "" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	w = get(pv("path="+filepath.Join(tmp, "v.webm")), "bytes=-3")
	if w.Code != 206 || w.Body.String() != "789" || w.Header().Get("Content-Security-Policy") != "" {
		t.Fatalf("%d %v %q", w.Code, w.Header(), w.Body)
	}
	if w = get(pv("path="+filepath.Join(tmp, "v.webm")), "bytes=x"); w.Code != 416 {
		t.Fatal(w.Code)
	}
	w = get(pv("download=1&path="+EncodeURIComponent(filepath.Join(tmp, "Отчёт \"1\".bin"))), "")
	if w.Code != 200 || w.Header().Get("Content-Disposition") != `attachment; filename="_____ _1_.bin"; filename*=UTF-8''%D0%9E%D1%82%D1%87%D1%91%D1%82%20%221%22.bin` {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if w = get(pv("path=x"), ""); w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code)
	}
}
