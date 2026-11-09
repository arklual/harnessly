//go:build linux

package main

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The preview by path in a real browser (GitHub #44 and #45): the bridge takes the prefix off the document's URL for
// the page's router, and the page still gets its relative styles and scripts, the ones a loader writes with
// document.write, and those it adds after pushState on a nested route. What is checked is what the browser made of
// them (computed styles, scripts that ran), not that the files answered 200.
//
// It needs Chrome and the bridge as the page's build makes it (cd web && vite build --config vite.bridge.config.ts,
// or HL_PREVIEW_BRIDGE_JS naming the file).
func TestPreviewByPathInBrowser(t *testing.T) {
	chrome := ""
	for _, name := range []string{os.Getenv("HL_CHROME"), "google-chrome", "chromium", "chromium-browser"} {
		if found, err := exec.LookPath(name); name != "" && err == nil {
			chrome = found
			break
		}
	}
	if chrome == "" {
		t.Skip("no Chrome")
	}
	bridge := os.Getenv("HL_PREVIEW_BRIDGE_JS")
	if bridge == "" {
		bridge, _ = filepath.Abs("../../web/dist/preview-bridge.js")
	}
	if _, err := os.Stat(bridge); err != nil {
		t.Skip("no built bridge: cd web && vite build --config vite.bridge.config.ts")
	}

	var mu sync.Mutex
	asked := map[string]bool{}
	file := func(w http.ResponseWriter, kind, body string) {
		w.Header().Set("Content-Type", kind)
		_, _ = io.WriteString(w, body)
	}
	dev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked[r.URL.Path] = true
		mu.Unlock()
		switch p := r.URL.Path; {
		case p == "/demo/style.css":
			file(w, "text/css", `#box{color:rgb(1, 2, 3)} #pic{width:auto}`)
		case p == "/demo/app.js":
			file(w, "text/javascript", `window.appPath = location.pathname;`)
		case p == "/demo/loader.js":
			// As Designly's support loader: its dependencies by document.write, while the page is parsed.
			file(w, "text/javascript", `(function () {
  var own = document.currentScript.src, prefix = new URL(own).pathname.replace(/\/demo\/loader\.js$/, '');
  document.write('<script src="/vendor/dep.js"><\/script>', '<link rel=stylesheet href=/vendor/dep.css>');
  document.writeln("<script src='" + prefix + "/vendor/pre.js'><\/script><script src=lib/near.js><\/script>");
  document.write('<img id="ext" src="https://cdn.example.invalid/x.png" hidden><a id="frag" href="#top">top</a>');
})();`)
		case p == "/vendor/dep.js":
			file(w, "text/javascript", `window.depRan = (window.depRan || 0) + 1;`)
		case p == "/vendor/dep.css":
			file(w, "text/css", `#box{background-color:rgb(4, 5, 6)}`)
		case p == "/vendor/pre.js":
			file(w, "text/javascript", `window.preRan = true;`)
		case strings.HasSuffix(p, "/lib/near.js"):
			file(w, "text/javascript", `window.nearRan = true;`)
		case p == "/demo/users/7/nested.js":
			file(w, "text/javascript", `window.nestedRan = location.pathname;`)
		case p == "/demo/img/dot.svg":
			file(w, "image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg" width="3" height="2"></svg>`)
		case p == "/site/based.css":
			file(w, "text/css", `#box{color:rgb(7, 8, 9)}`)
		case p == "/based/index.html":
			file(w, "text/html", `<!doctype html><html><head><base href="/site/"><link rel="stylesheet" href="based.css"></head>`+
				`<body><div id="box">x</div><pre id="out"></pre><script>addEventListener("load", function () {`+
				`document.getElementById("out").textContent = JSON.stringify({color: getComputedStyle(document.getElementById("box")).color,`+
				`path: location.pathname});});</script></body></html>`)
		case strings.HasPrefix(p, "/demo/"):
			// The page, at /demo/index.html and at the nested route /demo/users/7 (a router's).
			file(w, "text/html", `<!doctype html><html><head><link rel="stylesheet" href="`+demoRef(p, "style.css")+`">`+
				`<script src="`+demoRef(p, "app.js")+`"></script><script src="`+demoRef(p, "loader.js")+`"></script>`+
				`</head><body><div id="box">x</div><img id="pic" src="`+demoRef(p, "img/dot.svg")+`"><pre id="out"></pre>`+
				`<script>addEventListener("load", function () {
  var attr = function (id, name) { var el = document.getElementById(id); return el ? el.getAttribute(name) : ""; };
  var box = getComputedStyle(document.getElementById("box")), loaded = location.pathname;
  history.pushState({}, "", "/demo/users/7/edit");
  var pushed = location.pathname;
  history.replaceState({}, "", "/demo/users/7/edit?tab=2");
  var s = document.createElement("script");
  s.src = "nested.js";
  s.onload = s.onerror = function () {
    document.getElementById("out").textContent = JSON.stringify({color: box.color, background: box.backgroundColor,
      picWidth: document.getElementById("pic").naturalWidth, appPath: window.appPath, dep: window.depRan || 0,
      pre: !!window.preRan, near: !!window.nearRan, nested: window.nestedRan || "", loaded: loaded, pushed: pushed,
      replaced: location.pathname + location.search, ext: attr("ext", "src"), frag: attr("frag", "href")});
  };
  document.head.appendChild(s);
});</script></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer dev.Close()
	devPort := portOf(strings.TrimPrefix(dev.URL, "http://"))

	base, session, client := startPathRuntime(t, bridge)
	open := func(at string) (string, string) {
		t.Helper()
		req, _ := http.NewRequest("POST", base+"/api/preview/open",
			strings.NewReader(`{"target":{"kind":"port","port":`+strconv.Itoa(devPort)+`},"slot":1,"path":"`+at+`"}`))
		req.Header.Set("Cookie", session)
		req.Header.Set("Origin", base)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var o struct {
			Origin   string `json:"origin"`
			EnterURL string `json:"enterUrl"`
		}
		raw, _ := io.ReadAll(res.Body)
		if err := json.Unmarshal(raw, &o); err != nil || o.EnterURL == "" {
			t.Fatalf("open %s: %d %s", at, res.StatusCode, raw)
		}
		return o.EnterURL, strings.TrimPrefix(o.Origin, base)
	}

	for _, at := range []string{"/demo/index.html", "/demo/users/7"} {
		enter, prefix := open(at)
		var got struct {
			Color, Background, AppPath, Nested, Loaded, Pushed, Replaced, Ext, Frag string
			PicWidth, Dep                                                           int
			Pre, Near                                                               bool
		}
		readResult(t, runChrome(t, chrome, enter), &got)
		want := struct{ color, background string }{"rgb(1, 2, 3)", "rgb(4, 5, 6)"}
		switch {
		case got.Color != want.color:
			t.Errorf("%s: the relative style did not apply: %+v", at, got)
		case got.Background != want.background || got.Dep != 1 || !got.Pre || !got.Near:
			t.Errorf("%s: what document.write loaded did not run once each: %+v", at, got)
		case got.PicWidth != 3:
			t.Errorf("%s: the relative picture: %+v", at, got)
		case got.AppPath != at || got.Loaded != at:
			t.Errorf("%s: the page sees its path with the prefix: %+v", at, got)
		case got.Pushed != "/demo/users/7/edit" || got.Replaced != "/demo/users/7/edit?tab=2" || got.Nested != "/demo/users/7/edit":
			t.Errorf("%s: pushState, replaceState and a relative script after them: %+v", at, got)
		case got.Ext != "https://cdn.example.invalid/x.png" || got.Frag != "#top":
			t.Errorf("%s: an external address or a fragment was rewritten: %+v", at, got)
		}
		if strings.Contains(prefix, "/demo") || !strings.HasPrefix(prefix, "/agents/__pv/1-") {
			t.Fatalf("prefix: %s", prefix)
		}
	}

	// A page with a <base href> of its own resolves against it, under the prefix.
	enter, _ := open("/based/index.html")
	var based struct{ Color, Path string }
	readResult(t, runChrome(t, chrome, enter), &based)
	if based.Color != "rgb(7, 8, 9)" || based.Path != "/based/index.html" {
		t.Errorf("explicit base: %+v", based)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, p := range []string{"/demo/style.css", "/vendor/dep.js", "/vendor/dep.css", "/vendor/pre.js", "/demo/lib/near.js",
		"/demo/users/lib/near.js", "/demo/users/7/nested.js", "/site/based.css"} {
		if !asked[p] {
			t.Errorf("the dev server never got %s through the slot: %v", p, asked)
		}
	}
}

// demoRef is how a page of /demo/ names one of its files relatively: `./style.css` from /demo/index.html,
// `../../demo/style.css` from the nested route /demo/users/7.
func demoRef(page, file string) string {
	dir := path.Dir(page)
	if dir == "/demo" {
		return "./" + file
	}
	return strings.Repeat("../", strings.Count(dir, "/")) + "demo/" + file
}

// runChrome loads the page in a headless Chrome until it settles and gives its DOM.
func runChrome(t *testing.T, chrome, address string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nice", chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run",
		"--no-default-browser-check", "--disable-extensions", "--user-data-dir="+t.TempDir(), "--virtual-time-budget=15000",
		"--dump-dom", address)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		t.Fatalf("chrome: %v", err)
	}
	return string(out)
}

var outRe = regexp.MustCompile(`(?s)<pre id="out">(.*?)</pre>`)

func readResult(t *testing.T, dom string, into any) {
	t.Helper()
	m := outRe.FindStringSubmatch(dom)
	if m == nil || m[1] == "" {
		t.Fatalf("the page did not finish: %s", tail(dom, 3000))
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), into); err != nil {
		t.Fatalf("result: %v %s", err, m[1])
	}
}
