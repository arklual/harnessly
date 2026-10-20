package usagereport

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
)

func answerBody(t *testing.T, rt *Route, target, cookie string) (int, string) {
	t.Helper()
	r := httptest.NewRequest("GET", target, nil)
	if cookie != "" {
		r.Header.Set("Cookie", "session-token="+cookie)
	}
	status, v, _ := rt.Answer(r)
	if raw, ok := v.(gate.Raw); ok {
		return status, string(raw.Body)
	}
	return status, ""
}

// The facts file the previous runtime wrote is read as it is: transcripts it already reduced are not read again.
func TestReadsThePreviousRuntimesFactsFile(t *testing.T) {
	g := loadGolden(t)
	root, fill := layOut(t, g)
	var cacheText, want string
	for _, s := range g.Steps {
		if s.CacheWritten != "" {
			cacheText, want = fill(s.CacheWritten), fill(s.Answer)
			break
		}
	}
	var cache map[string]any
	if err := json.Unmarshal([]byte(cacheText), &cache); err != nil {
		t.Fatal(err)
	}
	// Every transcript it knows is replaced by one of the same size and time that says nothing.
	for name, e := range cache["files"].(map[string]any) {
		st, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		e.(map[string]any)["size"] = float64(st.Size())
		if err := os.WriteFile(name, []byte(strings.Repeat(" ", int(st.Size()))), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(name, st.ModTime(), st.ModTime())
	}
	clock := &fakeClock{t: time.UnixMilli(int64(g.Now))}
	rt, c := newTestRoute(root, clock)
	raw, _ := json.Marshal(cache)
	_ = os.MkdirAll(filepath.Dir(c.cfg.CacheFile), 0o755)
	if err := os.WriteFile(c.cfg.CacheFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, got := answerBody(t, rt, "/api/platform/stats", "sub:dev@example.com"); got != want {
		t.Fatalf("answer\n got %s\nwant %s", got, want)
	}
}

// A build longer than the wait is answered with the progress, the previous report kept until the new one is done; all
// callers share one build.
func TestSlowBuildAnswersWithProgress(t *testing.T) {
	g := loadGolden(t)
	root, _ := layOut(t, g)
	clock := &fakeClock{t: time.UnixMilli(int64(g.Now))}
	rt, c := newTestRoute(root, clock)
	rt.Wait = 20 * time.Millisecond
	hold := make(chan struct{})
	builds := 0
	var mu sync.Mutex
	c.beforeBuild = func() {
		mu.Lock()
		builds++
		mu.Unlock()
		<-hold
	}
	status, body := answerBody(t, rt, "/api/platform/stats?shape=report&wait=0", "sub:dev@example.com")
	want := `{"version":1,"person":"dev@example.com","machine":"box","generatedAt":"2026-10-18T00:00:00.000Z","timezone":"Europe/Moscow","days":{},"usage":[],"sessions":[],"collecting":{"done":0,"total":0}}`
	if status != 200 || body != want {
		t.Fatalf("cold answer %d %s", status, body)
	}
	if _, body = answerBody(t, rt, "/api/platform/stats?shape=report", "good"); !strings.Contains(body, `"person":"agent@box"`) || !strings.Contains(body, `"collecting":{`) {
		t.Fatalf("waited answer %s", body)
	}
	close(hold)
	rt.Wait = 10 * time.Second
	_, full := answerBody(t, rt, "/api/platform/stats?shape=report", "sub:dev@example.com")
	if !strings.Contains(full, `"collecting":null`) || !strings.Contains(full, `"sessions":[{`) {
		t.Fatalf("built answer %s", full)
	}
	// Past the rescan the next build runs; meanwhile the previous report is served with the progress, which until the
	// new pass starts is the last one's (six transcripts read, the broken one included).
	clock.add(61 * time.Second)
	hold = make(chan struct{})
	_, stale := answerBody(t, rt, "/api/platform/stats?shape=report&wait=0", "sub:dev@example.com")
	if want := strings.Replace(full, `"collecting":null`, `"collecting":{"done":6,"total":6}`, 1); stale != want {
		t.Fatalf("stale answer\n got %s\nwant %s", stale, want)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answerBody(t, rt, "/api/platform/stats?wait=0", "good")
		}()
	}
	wg.Wait()
	close(hold)
	answerBody(t, rt, "/api/platform/stats", "good")
	mu.Lock()
	defer mu.Unlock()
	if builds != 2 {
		t.Fatalf("%d builds, want 2", builds)
	}
}
