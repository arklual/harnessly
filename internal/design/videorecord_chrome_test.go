package design

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// recordSite is a small product: a login with a password, a button that opens a modal that fades in, a field to type in,
// and a long page to scroll. Its CSS transitions and a requestAnimationFrame counter show that the page follows the
// recorder's clock.
const recordSite = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Acme</title><style>
body{margin:0;font:28px/1.4 sans-serif;background:#f4f5f7;color:#111}
header{height:90px;background:#1b2a49;color:#fff;display:flex;align-items:center;padding:0 40px;font-size:32px}
main{padding:40px}
input{font:inherit;padding:12px 16px;border:2px solid #99a;border-radius:8px;display:block;margin:12px 0;width:520px}
button{font:inherit;padding:14px 28px;border:0;border-radius:10px;background:#2d6cdf;color:#fff;transition:background .4s}
button:hover{background:#0f3d99}
#modal{position:fixed;inset:0;background:#0006;display:flex;align-items:center;justify-content:center;opacity:0;pointer-events:none;transition:opacity .5s}
#modal.on{opacity:1;pointer-events:auto}
#modal div{background:#fff;padding:40px;border-radius:16px;width:640px}
.tall{height:2400px;background:linear-gradient(#fff,#cde)}
#raf{position:fixed;right:10px;bottom:10px;font:20px monospace}
</style></head><body><header>Acme Cloud</header><main>
<h1>Sign in</h1>
<input id="email" placeholder="Email"><input id="pw" type="password" placeholder="Password">
<button id="go">Sign in</button> <button id="more">Open details</button>
<div class="tall"><h2 id="bottom" style="padding-top:2000px">The end of the page</h2></div></main>
<div id="modal"><div><h2>Details</h2><input id="note" placeholder="Note"><button id="close">Close</button></div></div>
<div id="raf"></div>
<script>
var n=0;(function f(){n++;document.getElementById('raf').textContent='raf '+n;requestAnimationFrame(f)})();
document.getElementById('more').onclick=function(){document.getElementById('modal').className='on'};
document.getElementById('close').onclick=function(){document.getElementById('modal').className=''};
</script></body></html>`

func recordTestShell(t *testing.T) {
	t.Helper()
	if FindHeadlessShell() == "" {
		t.Skip("no chrome-headless-shell")
	}
	if DeckFFmpeg() == "" {
		t.Skip("no ffmpeg")
	}
}

func TestRecordPlaysAScriptFrameByFrame(t *testing.T) {
	recordTestShell(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(recordSite))
	}))
	defer srv.Close()
	const password = "S3cret-pw-9431" // gitleaks:allow (a test value, not a secret)
	sc, err := ParseRecordScenario([]byte(`
name: acme
url: ` + srv.URL + `
viewport: {width: 960, height: 540}
scale: 2
fps: 30
steps:
  - {do: type, into: "#email", text: "ann@acme.test"}
  - {do: type, into: "#pw", secret: ACME_PW}
  - {do: click, on: "text=Sign in"}
  - {do: click, on: "#more", pause: 300}
  - {do: type, into: "#note", text: "Hello"}
  - {do: click, on: "#close"}
  - {do: scroll, by: 700, ms: 700}
`))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res, err := Record(context.Background(), sc, RecordOptions{Dir: dir, CRF: 12, Preset: "ultrafast",
		Getenv: func(k string) string {
			if k == "ACME_PW" {
				return password
			}
			return ""
		}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Dropped != 0 {
		t.Fatalf("dropped %d frames", res.Dropped)
	}
	raw, err := os.ReadFile(res.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), password) {
		t.Fatal("the password is in the log")
	}
	var lg RecordLog
	if err := json.Unmarshal(raw, &lg); err != nil {
		t.Fatal(err)
	}
	if len(lg.Cursor) != lg.Frames || lg.Frames != res.Frames {
		t.Fatalf("cursor samples %d, frames %d, result %d", len(lg.Cursor), lg.Frames, res.Frames)
	}
	kinds := map[string]int{}
	masked := false
	for _, e := range lg.Events {
		kinds[e.Type]++
		masked = masked || (e.Type == "type" && e.Masked && e.Text == RecordMask)
	}
	if kinds["click"] < 4 || kinds["type"] != 3 || kinds["scroll"] != 1 || !masked {
		t.Fatalf("events: %v masked=%v", kinds, masked)
	}
	// the clip holds as many frames as the log says, at the size of the shot
	out, err := exec.Command("ffprobe", "-v", "error", "-count_frames", "-select_streams", "v:0", "-show_entries", "stream=nb_read_frames,width,height",
		"-of", "csv=p=0", res.Video).Output()
	if err != nil {
		t.Skip("no ffprobe: " + err.Error())
	}
	want := "1920,1080," + strconv.Itoa(lg.Frames)
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("clip is %s, want %s", got, want)
	}
	t.Logf("%d frames in %s, %s", res.Frames, res.Wall, filepath.Base(res.Video))
}

func TestRecordRefusesASecretOutsideAPasswordField(t *testing.T) {
	recordTestShell(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(recordSite))
	}))
	defer srv.Close()
	sc, err := ParseRecordScenario([]byte(`{"url": "` + srv.URL + `", "viewport": {"width": 640, "height": 400}, "scale": 1, "steps": [{"do": "type", "into": "#email", "secret": "ACME_PW"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Record(context.Background(), sc, RecordOptions{Dir: t.TempDir(), Preset: "ultrafast", Getenv: func(string) string { return "S3cret-pw" }})
	if err == nil || !strings.Contains(err.Error(), "password") && !strings.Contains(err.Error(), "пароля") {
		t.Fatalf("a secret into a plain field must be refused, got %v", err)
	}
}
