package hlcli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseVideoTime(t *testing.T) {
	ok := map[string]int64{"8s": 8000, "1.5s": 1500, "1500ms": 1500, "0:06": 6000, "1:02.5": 62500, "8": 8000, "0.25": 250, " 3s ": 3000}
	for in, want := range ok {
		if got, err := parseVideoTime(in); err != nil || got != want {
			t.Errorf("%q: %d %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-1s", "0:75", "x:10", "1.2.3s", "ms"} {
		if _, err := parseVideoTime(in); err == nil {
			t.Errorf("%q should be refused", in)
		}
	}
	if d, err := parseVideoDur("auto"); err != nil || d != nil {
		t.Errorf("auto: %v %v", d, err)
	}
	if d, err := parseVideoDur("8s"); err != nil || d == nil || *d != 8000 {
		t.Errorf("8s: %v %v", d, err)
	}
}

func TestParseVideoStrip(t *testing.T) {
	got, n, err := parseVideoStrip("0:6:1s")
	if err != nil || got != "0:6000:1000" || n != 7 {
		t.Fatalf("0:6:1s: %q %d %v", got, n, err)
	}
	if _, n, err := parseVideoStrip("0:59:1s"); err != nil || n != 60 {
		t.Errorf("the cap itself: %d %v", n, err)
	}
	for _, bad := range []string{"0:60:1s", "0:6", "0:6:0", "6:0:1s", "a:b:c", "0:6:1s:2"} {
		if _, _, err := parseVideoStrip(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestParseVideoTransition(t *testing.T) {
	if m, err := parseVideoTransition("fade:400"); err != nil || m["kind"] != "fade" || m["ms"] != 400 {
		t.Errorf("fade: %v %v", m, err)
	}
	if m, err := parseVideoTransition("none"); err != nil || m["kind"] != "none" {
		t.Errorf("none: %v %v", m, err)
	}
	if m, err := parseVideoTransition("default"); err != nil || m != nil {
		t.Errorf("default: %v %v", m, err)
	}
	for _, bad := range []string{"fade", "fade:9000", "push:300", "fade:x"} {
		if _, err := parseVideoTransition(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

type videoFake struct {
	mu    sync.Mutex
	calls []call
	polls int
}

func (v *videoFake) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	v.mu.Lock()
	v.calls = append(v.calls, call{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("x-cli-token"), body})
	v.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()
	switch {
	case r.URL.Path == "/api/cli/design/video" && q.Get("what") == "state":
		_, _ = w.Write([]byte(`{"project":"clip","state":{"rev":1,"settings":{"v":1,"formats":["16:9","9:16"],"fps":30,"seconds":60,"voice":"none","subs":true},` +
			`"durMs":14000,"scenes":[{"id":"S1","title":"Intro","start":0,"dur":6000,"auto":true,"transition":{"kind":"fade","ms":400},"moments":[{"block":"Title","at":500}],"words":12},` +
			`{"id":"S2","title":"Next","start":6000,"dur":8000,"auto":false,"transition":{"kind":"","ms":0},"moments":[],"words":0}],` +
			`"comments":[{"id":"C1","page":"S1","view":"video","resolved":false,"text":"x"},{"id":"C2","page":"S1","view":"video","resolved":true,"text":"y"}],` +
			`"renders":[{"format":"16:9","published":true,"fresh":false},{"format":"9:16","published":false,"fresh":false}],"job":null}}`))
	case r.URL.Path == "/api/cli/design/video" && q.Get("what") == "comments":
		_, _ = w.Write([]byte(`{"project":"clip","comments":[{"id":"C1","page":"S1","view":"video","t0":1200,"t1":2400,"format":"9:16","block":"Title","text":"bigger","frame":".media/video/frames/k.jpg"}]}`))
	case r.URL.Path == "/api/cli/design/pages":
		_, _ = w.Write([]byte(`{"project":"clip","page":{"id":"S3","title":"Outro","file":"Outro.dc.html"}}`))
	case r.URL.Path == "/api/cli/design/video/scene":
		_, _ = w.Write([]byte(`{"project":"clip","scene":{"id":"S3","title":"Outro","start":14000,"dur":4000,"auto":false}}`))
	case r.URL.Path == "/api/cli/design/video/overlay":
		_, _ = w.Write([]byte(`{"project":"clip","overlay":{"id":"callout-1","kind":"callout","text":"Тут","at":1200,"out":4000,"x":0.5,"y":0.1,"w":0.3,"h":0.1,"tx":0.4,"ty":0.5,"anchor":"left","enter":"pop","leave":"fade","theme":"accent","author":"human","updatedAt":"2026-11-03T10:00:00Z"},` +
			`"overlays":[{"id":"callout-1","kind":"callout","text":"Тут","at":1200,"out":4000,"x":0.5,"y":0.1,"w":0.3,"h":0.1,"tx":0.4,"ty":0.5,"anchor":"left","enter":"pop","leave":"fade","theme":"accent","author":"human","updatedAt":"2026-11-03T10:00:00Z"}]}`))
	case r.URL.Path == "/api/cli/design/video/frame":
		_, _ = w.Write([]byte(`{"frames":[{"t":0,"path":"/tmp/a.jpg"},{"t":1000,"path":"/tmp/b.jpg"}]}`))
	case r.URL.Path == "/api/cli/design/video/check":
		_, _ = w.Write([]byte(`{"issues":[{"scene":"S1","format":null,"at":null,"block":null,"code":"timer","level":"error","msg":"setTimeout is used"},` +
			`{"scene":"S1","format":"9:16","at":1500,"block":"Title","code":"overflow","level":"warn","msg":"cut off"}]}`))
	case r.URL.Path == "/api/cli/design/video/render" && r.Method == http.MethodPost:
		_, _ = w.Write([]byte(`{"id":"J1","format":"16:9","draft":true,"state":"queued","progress":0,"at":"now"}`))
	case r.URL.Path == "/api/cli/design/video/render":
		v.mu.Lock()
		v.polls++
		n := v.polls
		v.mu.Unlock()
		if n < 2 {
			_, _ = w.Write([]byte(`{"id":"J1","format":"16:9","state":"running","progress":0.5,"at":"now"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"J1","format":"16:9","state":"done","progress":1,"file":"/lib/clip/.media/video/renders/16x9/h.mp4","at":"now"}`))
	default:
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not-found"}`))
	}
}

func videoRun(t *testing.T, args ...string) (*videoFake, result) {
	t.Helper()
	v := &videoFake{}
	srv := httptest.NewServer(http.HandlerFunc(v.serve))
	t.Cleanup(srv.Close)
	videoPoll = time.Millisecond
	t.Cleanup(func() { videoPoll = 2 * time.Second })
	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok"}
	return v, runWith(t, t.TempDir(), env, "", args...)
}

func TestDesignVideoGet(t *testing.T) {
	_, r := videoRun(t, "design", "video", "get", "clip")
	if r.code != 0 || !strings.HasPrefix(r.out, "Titles, notes and comments below come from") {
		t.Fatalf("get: %d %s %s", r.code, r.out, r.errs)
	}
	for _, want := range []string{"S1\t0s\t6s auto\tfade:400\t1\t12\t1\tIntro", "S2\t6s\t8s set\tnone\t0\t0\t0\tNext",
		"14s, 2 scenes, formats 16:9 9:16, about 60 s, subtitles on", "Render 16:9: published, outdated", "Render 9:16: not rendered"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
	_, r = videoRun(t, "--json", "design", "video", "get", "clip")
	if r.code != 0 || !strings.Contains(r.out, `"durMs": 14000`) {
		t.Errorf("json: %d %s", r.code, r.out)
	}
	if _, r := videoRun(t, "design", "video", "get"); r.code != 2 {
		t.Errorf("no project: %d", r.code)
	}
}

func TestDesignVideoSceneCommands(t *testing.T) {
	v, r := videoRun(t, "design", "video", "scene", "add", "clip", "--title", "Outro", "--dur", "4s")
	if r.code != 0 || !strings.Contains(r.out, `Scene added: "Outro"`) {
		t.Fatalf("add: %d %s %s", r.code, r.out, r.errs)
	}
	if len(v.calls) != 2 || v.calls[0].Body["action"] != "create" || v.calls[0].Body["title"] != "Outro" ||
		v.calls[1].Body["page"] != "S3" || v.calls[1].Body["dur"] != float64(4000) {
		t.Errorf("add calls: %+v", v.calls)
	}
	v, r = videoRun(t, "design", "video", "scene", "set", "clip", "Intro", "--dur", "auto", "--transition", "fade:400")
	b := v.calls[0].Body
	_, hasDur := b["dur"]
	tr, _ := b["transition"].(map[string]any)
	if r.code != 0 || !hasDur || b["dur"] != nil || tr["kind"] != "fade" || tr["ms"] != float64(400) || b["page"] != "Intro" {
		t.Errorf("set auto: %d %v %s", r.code, b, r.errs)
	}
	v, _ = videoRun(t, "design", "video", "scene", "set", "clip", "Intro", "--dur", "1500ms")
	if _, has := v.calls[0].Body["transition"]; has || v.calls[0].Body["dur"] != float64(1500) {
		t.Errorf("only dur: %v", v.calls[0].Body)
	}
	v, _ = videoRun(t, "design", "video", "scene", "set", "clip", "Intro", "--transition", "default")
	if tr, has := v.calls[0].Body["transition"]; !has || tr != nil {
		t.Errorf("default transition is null: %v", v.calls[0].Body)
	}
	for _, args := range [][]string{{"scene", "set", "clip", "Intro"}, {"scene", "set", "clip", "Intro", "--dur", "soon"}, {"scene", "add", "clip"},
		{"scene", "set", "clip", "Intro", "--transition", "wipe"}} {
		if _, r := videoRun(t, append([]string{"design", "video"}, args...)...); r.code != 2 {
			t.Errorf("%v: exit %d", args, r.code)
		}
	}
}

func TestDesignVideoFrame(t *testing.T) {
	v, r := videoRun(t, "design", "video", "frame", "clip", "Intro", "--at", "2.4s", "--format", "9:16", "--draft")
	if r.code != 0 || r.out != "0s\t/tmp/a.jpg\n1s\t/tmp/b.jpg\n" {
		t.Fatalf("frame: %d %q %s", r.code, r.out, r.errs)
	}
	q := v.calls[0].Query
	for _, want := range []string{"at=2400", "format=9%3A16", "draft=1", "page=Intro", "project=clip"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q lacks %q", q, want)
		}
	}
	v, _ = videoRun(t, "design", "video", "frame", "clip", "Intro", "--strip", "0:6:1s")
	if !strings.Contains(v.calls[0].Query, "strip=0%3A6000%3A1000") {
		t.Errorf("strip query: %s", v.calls[0].Query)
	}
	for _, args := range [][]string{{"clip", "Intro"}, {"clip", "Intro", "--strip", "0:100:1s"}, {"clip", "Intro", "--at", "1s", "--format", "4:3"}} {
		if _, r := videoRun(t, append([]string{"design", "video", "frame"}, args...)...); r.code != 2 {
			t.Errorf("%v: exit %d", args, r.code)
		}
	}
}

func TestDesignVideoCheckExitsOnErrors(t *testing.T) {
	_, r := videoRun(t, "design", "video", "check", "clip", "--scene", "Intro", "--format", "9:16")
	if r.code != 1 || !strings.Contains(r.out, "error\ttimer\tS1\tany format\tsetTimeout is used") ||
		!strings.Contains(r.out, "warn\toverflow\tS1\t9:16 at 1.5s [Title]\tcut off") {
		t.Errorf("check: %d %s %s", r.code, r.out, r.errs)
	}
}

func TestDesignVideoRenderWaits(t *testing.T) {
	v, r := videoRun(t, "design", "video", "render", "clip", "--draft", "--wait")
	if r.code != 0 || !strings.Contains(r.out, "running 50%") || !strings.Contains(r.out, "done: /lib/clip/.media/video/renders/16x9/h.mp4") {
		t.Fatalf("render: %d %s %s", r.code, r.out, r.errs)
	}
	// No --format: the first enabled one of the state, so a state read comes first.
	if v.calls[0].Method != "GET" || v.calls[1].Body["format"] != "16:9" || v.calls[1].Body["draft"] != true {
		t.Errorf("calls: %+v", v.calls)
	}
	_, r = videoRun(t, "design", "video", "render", "clip", "--format", "9:16", "--scene", "Intro")
	if r.code != 0 || !strings.Contains(r.out, "Render J1 started") {
		t.Errorf("no wait: %d %s", r.code, r.out)
	}
}

func TestDesignVideoComments(t *testing.T) {
	v, r := videoRun(t, "design", "video", "comments", "clip", "--open")
	if r.code != 0 || !strings.Contains(r.out, "C1\topen\tS1\t1.2s..2.4s\t9:16\tTitle\t\"bigger\"") || !strings.Contains(r.out, "frame: .media/video/frames/k.jpg") ||
		!strings.Contains(v.calls[0].Query, "open=1") {
		t.Errorf("comments: %d %s", r.code, r.out)
	}
}

func TestDesignVideoHelpGolden(t *testing.T) {
	r := runWith(t, t.TempDir(), nil, "", "design", "video", "--help")
	if r.code != 0 {
		t.Fatalf("help: %d %s", r.code, r.errs)
	}
	const golden = "testdata/design_video_help.golden"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(r.out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil || string(want) != r.out {
		t.Errorf("help differs from %s (UPDATE_GOLDEN=1 rewrites it):\n%s", golden, r.out)
	}
	if !strings.Contains(helpText, "harnessly design video get") || !strings.Contains(commandHelp["design"], "design video --help") {
		t.Error("the main help and the design topic mention the video commands")
	}
}

func TestDesignVideoOverlayCommands(t *testing.T) {
	v, r := videoRun(t, "design", "video", "overlay", "add", "clip", "Intro", "--kind", "callout", "--text", "Тут", "--at", "1.2s", "--out", "4s",
		"--x", "0.5", "--tx", "0.4", "--ty", "0.5", "--enter", "pop", "--portrait", "0.1,0.2,0.5,0.1,0.3,0.4")
	b := v.calls[0].Body
	pt, _ := b["portrait"].(map[string]any)
	if r.code != 0 || b["action"] != "add" || b["page"] != "Intro" || b["kind"] != "callout" || b["at"] != float64(1200) || b["out"] != float64(4000) ||
		b["x"] != 0.5 || b["tx"] != 0.4 || pt["h"] != 0.1 || pt["ty"] != 0.4 {
		t.Fatalf("add: %d %v %s", r.code, b, r.errs)
	}
	if !strings.Contains(r.out, "Overlay added: callout-1") || !strings.Contains(r.out, "human (a person edited it: --force") {
		t.Errorf("the answer says who wrote it:\n%s", r.out)
	}
	v, r = videoRun(t, "design", "video", "overlay", "set", "clip", "Intro", "callout-1", "--out", "end", "--portrait", "none", "--force")
	b = v.calls[0].Body
	if _, has := b["out"]; r.code != 0 || !has || b["out"] != nil || b["portrait"] != nil || b["force"] != true || b["id"] != "callout-1" || b["action"] != "set" {
		t.Errorf("set: %d %v %s", r.code, b, r.errs)
	}
	v, _ = videoRun(t, "design", "video", "overlay", "rm", "clip", "Intro", "callout-1")
	if b = v.calls[0].Body; b["action"] != "rm" || b["force"] != false || b["id"] != "callout-1" {
		t.Errorf("rm: %v", b)
	}
	v, r = videoRun(t, "design", "video", "overlay", "ls", "clip", "Intro")
	if v.calls[0].Body["action"] != "ls" || !strings.Contains(r.out, "callout-1\tcallout\t1.2s-4s") {
		t.Errorf("ls: %v\n%s", v.calls[0].Body, r.out)
	}
	for _, args := range [][]string{{"overlay", "add", "clip", "Intro"}, {"overlay", "set", "clip", "Intro", "x"}, {"overlay", "set", "clip", "Intro", "x", "--at", "soon"},
		{"overlay", "add", "clip", "Intro", "--kind", "title", "--x", "left"}, {"overlay", "rm", "clip"}, {"overlay", "set", "clip", "Intro", "x", "--portrait", "1,2"}} {
		if _, r := videoRun(t, append([]string{"design", "video"}, args...)...); r.code != 2 {
			t.Errorf("%v: exit %d", args, r.code)
		}
	}
}
