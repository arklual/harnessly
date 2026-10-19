package design

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"harnessly/internal/jsvalue"
	"harnessly/internal/nodejson"
)

type checkGolden struct {
	Dom []struct {
		Dom     map[string]any `json:"dom"`
		Content struct{ Text, Media int }
	} `json:"dom"`
	Statuses []struct {
		Errors, Failed []string
		Blank          bool
		Status         string
	} `json:"statuses"`
	Worst []struct {
		In  []string `json:"in"`
		Out string   `json:"out"`
	} `json:"worst"`
	Boards []struct {
		Board struct {
			Path string
			W, H *int
		}
		View struct {
			Width, Height int
			Mobile        bool
		}
		Name string
	} `json:"boards"`
	Shots []struct {
		Ref, File string
		Path      *string
	} `json:"shots"`
	Files []struct {
		Name, Content string
		Mtime         float64
	} `json:"files"`
	Attached []struct {
		ID    string          `json:"id"`
		Cover *string         `json:"cover"`
		Check json.RawMessage `json:"check"`
	} `json:"attached"`
	Reports []struct {
		ID   string `json:"id"`
		JSON string `json:"json"`
	} `json:"reports"`
	Versions []struct {
		ID, Board string
		Version   *string
	} `json:"versions"`
	Sample json.RawMessage `json:"sample"`
	Saved  string          `json:"saved"`
}

func loadCheckGolden(t *testing.T) (checkGolden, *Library, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/check-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g checkGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, f := range g.Files {
		p := filepath.Join(root, f.Name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
		sec, frac := math.Modf(f.Mtime)
		m := time.Unix(int64(sec), int64(math.Round(frac*1e3))*1e6)
		if err := os.Chtimes(p, m, m); err != nil {
			t.Fatal(err)
		}
	}
	l := New(Config{LibraryDir: root, PlatformAPI: "<api>", Texts: Texts{Tag: "test-design", CLI: "acme"}})
	return g, l, root
}

func TestCheckGoldenPure(t *testing.T) {
	g, l, root := loadCheckGolden(t)
	for i, c := range g.Dom {
		text, media := pageContent(c.Dom)
		if text != c.Content.Text || media != c.Content.Media {
			t.Errorf("dom %d: %d/%d, want %d/%d", i, text, media, c.Content.Text, c.Content.Media)
		}
	}
	for _, c := range g.Statuses {
		if got := shotStatus(c.Errors, c.Failed, c.Blank); got != c.Status {
			t.Errorf("shotStatus %v %v %v = %s, want %s", c.Errors, c.Failed, c.Blank, got, c.Status)
		}
	}
	for _, c := range g.Worst {
		if got := worstStatus(c.In); got != c.Out {
			t.Errorf("worstStatus %v = %s, want %s", c.In, got, c.Out)
		}
	}
	for _, c := range g.Boards {
		v := thumbView(Board{Path: c.Board.Path, W: c.Board.W, H: c.Board.H})
		if v.Width != c.View.Width || v.Height != c.View.Height || v.Mobile != c.View.Mobile {
			t.Errorf("thumbView %s = %+v, want %+v", c.Board.Path, v, c.View)
		}
		if got := thumbFileName(c.Board.Path, "lx3k-1f"); got != c.Name {
			t.Errorf("thumbFileName %s = %s, want %s", c.Board.Path, got, c.Name)
		}
	}
	for _, c := range g.Shots {
		got := l.checkShotPath(c.Ref, c.File)
		want := ""
		if c.Path != nil {
			want = strings.Replace(*c.Path, "<root>", root, 1)
		}
		if got != want {
			t.Errorf("checkShotPath %s %s = %q, want %q", c.Ref, c.File, got, want)
		}
	}
}

func TestCheckGoldenFiles(t *testing.T) {
	g, l, _ := loadCheckGolden(t)
	defer l.StopWatching()
	for _, c := range g.Versions {
		got := l.thumbVersion(c.ID, c.Board)
		want := ""
		if c.Version != nil {
			want = *c.Version
		}
		if got != want {
			t.Errorf("thumbVersion %s %s = %q, want %q", c.ID, c.Board, got, want)
		}
	}
	for _, c := range g.Attached {
		a := l.ReadArtifact(c.ID)
		if a == nil {
			t.Fatalf("no artifact %s", c.ID)
		}
		l.AttachChecks([]*Artifact{a})
		if !reflect.DeepEqual(a.Cover, c.Cover) {
			t.Errorf("%s cover = %v, want %v", c.ID, a.Cover, c.Cover)
		}
		var want bytes.Buffer
		_ = json.Compact(&want, c.Check)
		if got := nodejson.Marshal(a.Check); !bytes.Equal(got, want.Bytes()) {
			t.Errorf("%s check = %s, want %s", c.ID, got, want.Bytes())
		}
	}
	for _, c := range g.Reports {
		v, _ := l.ReadCheckReport(c.ID)
		if got := nodejson.Marshal(jsvalue.NewObject("report", reportValue(v))); string(got) != c.JSON {
			t.Errorf("%s report =\n%s\nwant\n%s", c.ID, got, c.JSON)
		}
	}
	r := parseReport(g.Sample)
	if r == nil {
		t.Fatal("sample does not parse")
	}
	if err := l.saveReport(r); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(filepath.Join(l.CheckDir(r.Artifact), reportName))
	if string(saved) != g.Saved {
		t.Errorf("saved report =\n%s\nwant\n%s", saved, g.Saved)
	}
}

func TestForAgentAndCheckRoute(t *testing.T) {
	_, l, root := loadCheckGolden(t)
	defer l.StopWatching()
	_, r := l.ReadCheckReport("alpha")
	fa := l.ForAgent(r)
	if !fa.Finished || len(fa.Boards) != 4 || *fa.Boards[0].Shots[0].Screenshot != filepath.Join(root, "alpha", ".check", "001-desktop.jpg") || fa.Boards[0].Shots[1].Screenshot != nil {
		t.Fatalf("forAgent = %s", nodejson.Marshal(fa))
	}
	if !strings.HasPrefix(string(nodejson.Marshal(fa)), `{"artifact":"alpha","finished":true,"failure":null,"boards":[{"title":"Main","path":"Main.dc.html","status":"error","shots":[{"view":"desktop","status":"error","screenshot":"`) {
		t.Fatalf("forAgent shape = %s", nodejson.Marshal(fa))
	}
	a := &API{L: l, Session: func(r *http.Request) bool { return r.Header.Get("Cookie") == "ok" },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	req := httptest.NewRequest("GET", "/api/x/design/library/check?artifact=gamma", nil)
	req.Header.Set("Cookie", "ok")
	ans := a.Check(req)
	if ans.Status != 200 || string(ans.Body.(json.RawMessage)) != `{"report":{"artifact":"gamma","startedAt":7,"finishedAt":1760000060000,"done":4,"total":4,"boards":[],"cover":"","failure":null}}` ||
		ans.Header.Get("Cache-Control") != "no-store" || ans.Header.Get("ETag") == "" {
		t.Fatalf("check GET = %d %s %v", ans.Status, ans.Body, ans.Header)
	}
	req = httptest.NewRequest("GET", "/api/x/design/library/check?artifact=gamma", nil)
	if ans := a.Check(req); ans.Status != 403 || string(ans.Body.(json.RawMessage)) != `{"error":"Forbidden"}` {
		t.Fatalf("check without session = %d %s", ans.Status, ans.Body)
	}
	req = httptest.NewRequest("PUT", "/x?artifact=gamma", nil)
	req.Header.Set("Cookie", "ok")
	if ans := a.Check(req); ans.Status != 405 || ans.Header.Get("Allow") != "GET, POST" || ans.Body != nil {
		t.Fatalf("check PUT = %d %v", ans.Status, ans.Header)
	}
	req = httptest.NewRequest("POST", "/x", strings.NewReader(`{"artifact":"Bad"}`))
	req.Header.Set("Cookie", "ok")
	req.Header.Set("Content-Type", "application/json")
	if ans := a.Check(req); ans.Status != 400 || string(ans.Body.(json.RawMessage)) != `{"error":"Неизвестный макет"}` {
		t.Fatalf("check POST bad = %d %s", ans.Status, ans.Body)
	}

	jpeg := []byte("\xff\xd8picture")
	if err := os.WriteFile(filepath.Join(root, "alpha", ".check", "001-desktop.jpg"), jpeg, 0o644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/x?artifact=alpha&file=001-desktop.jpg", nil)
	req.Header.Set("Cookie", "ok")
	a.Shot(w, req)
	tag := nodejson.ETag(jpeg)
	if w.Code != 200 || w.Body.String() != string(jpeg) || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("ETag") != tag ||
		w.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" || w.Header().Get("Content-Length") != "9" {
		t.Fatalf("shot = %d %v", w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	req.Header.Set("If-None-Match", tag)
	a.Shot(w, req)
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatalf("shot again = %d", w.Code)
	}
	w = httptest.NewRecorder()
	a.Shot(w, httptest.NewRequest("GET", "/x?artifact=alpha&file=001-desktop.jpg", nil))
	if w.Code != 403 || w.Body.Len() != 0 {
		t.Fatalf("shot without session = %d", w.Code)
	}

	// The shadow's writer cannot flush: an earlier picture is served as is and nothing is drawn.
	thumbs := filepath.Join(root, "alpha", ThumbDirname)
	_ = os.MkdirAll(thumbs, 0o755)
	old := filepath.Join(thumbs, thumbHash("Main.dc.html")+"-old.jpg")
	if err := os.WriteFile(old, jpeg, 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &plainWriter{h: http.Header{}}
	req = httptest.NewRequest("GET", "/x?artifact=alpha&board=Main.dc.html&v=1", nil)
	req.Header.Set("Cookie", "ok")
	a.Thumb(rec, req)
	if rec.code != 200 || rec.h.Get("Cache-Control") != "private, no-cache" || rec.body.String() != string(jpeg) {
		t.Fatalf("thumb in the shadow = %d %v", rec.code, rec.h)
	}
	if n := len(l.thumbs().inflight); n != 0 {
		t.Fatalf("the shadow started %d drawings", n)
	}
	rec = &plainWriter{h: http.Header{}}
	req = httptest.NewRequest("GET", "/x?artifact=alpha&board=..%2Fx.html", nil)
	req.Header.Set("Cookie", "ok")
	a.Thumb(rec, req)
	if rec.code != 400 {
		t.Fatalf("thumb of a bad board = %d", rec.code)
	}
}

type plainWriter struct {
	h    http.Header
	code int
	body bytes.Buffer
}

func (p *plainWriter) Header() http.Header { return p.h }
func (p *plainWriter) WriteHeader(c int) {
	if p.code == 0 {
		p.code = c
	}
}
func (p *plainWriter) Write(b []byte) (int, error) {
	if p.code == 0 {
		p.code = 200
	}
	return p.body.Write(b)
}
