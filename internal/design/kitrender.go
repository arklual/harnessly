package design

// RenderKit draws a kit folder (before it is packed) in this machine's headless browser through the very pages the
// kit screen uses: /kitv/<id>/<version>/<theme>/_play.dc.html for a component and <layout>.dc.html for a layout, served
// by a library of its own over the folder. What renders here renders on the screen, and what fails here (a script
// error, a file the page cannot load, a component that draws nothing) fails there too. With Shots it also writes the
// pictures the canvas shows far away: _harnessly/thumbs/components/<id>.png and _harnessly/thumbs/layouts/<id>.png in
// each theme's folder, and their paths into design-system.json.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// KitRenderOptions are RenderKit's.
type KitRenderOptions struct {
	// Shots writes the pictures and their paths into the folder.
	Shots bool
	// Only, when not empty, renders just these components and layouts (by id).
	Only map[string]bool
	// Chrome is the browser to drive; the usual places when empty.
	Chrome string
	// Tabs is how many pages draw at once (4 when 0).
	Tabs int
}

// KitRenderItem is how one component or layout rendered.
type KitRenderItem struct {
	Theme  string   `json:"theme"`
	Node   string   `json:"node"`
	Status string   `json:"status"` // ok, error (a script error or a failed request), blank (draws nothing)
	Errors []string `json:"errors,omitempty"`
	Shot   string   `json:"shot,omitempty"` // the picture written, relative to the kit folder
	Ms     int64    `json:"ms"`
}

const (
	kitRenderSettle  = 600 * time.Millisecond
	kitRenderWait    = 3 * time.Second
	kitRenderPadding = 12
	kitLayoutWidth   = 1440
)

// The visible content of a page, without the wrappers that stretch to the window: elements with text of their own, a
// picture or a control, or a box (background, border, shadow) of their own. Answers {n, x, y, w, h} as JSON.
const kitMeasure = `(() => {
  let x1 = Infinity, y1 = Infinity, x2 = -Infinity, y2 = -Infinity, n = 0;
  const own = (el, s) => {
    if (/^(IMG|SVG|CANVAS|VIDEO|INPUT|BUTTON|SELECT|TEXTAREA|PROGRESS|METER|IFRAME)$/i.test(el.tagName)) return true;
    for (const c of el.childNodes) if (c.nodeType === 3 && c.textContent.trim() !== '') return true;
    const bg = s.backgroundColor;
    if (bg && bg !== 'transparent' && !/rgba\([^)]*,\s*0\)$/.test(bg)) return true;
    if (s.backgroundImage !== 'none' || s.boxShadow !== 'none') return true;
    return ['Top', 'Right', 'Bottom', 'Left'].some((d) => parseFloat(s['border' + d + 'Width']) > 0 && s['border' + d + 'Style'] !== 'none');
  };
  const subject = document.querySelector('[data-kit-subject]');
  for (const el of document.body.querySelectorAll('*')) {
    if (/^(SCRIPT|STYLE|HELMET|TEMPLATE|LINK|META)$/i.test(el.tagName)) continue;
    // The kit's root and the page around the component: their box is the page's, not the component's.
    if (subject && el.contains(subject)) continue;
    const s = getComputedStyle(el);
    if (s.display === 'none' || s.visibility === 'hidden' || Number(s.opacity) === 0) continue;
    const b = el.getBoundingClientRect();
    if (b.width < 1 || b.height < 1 || !own(el, s)) continue;
    // A dialog's backdrop covers the whole page: the picture is the dialog, not the page behind it.
    if ((s.position === 'fixed' || s.position === 'absolute') && b.width >= innerWidth * 0.9 && b.height >= innerHeight * 0.9 &&
        ![...el.childNodes].some((c) => c.nodeType === 3 && c.textContent.trim() !== '')) continue;
    n++; x1 = Math.min(x1, b.left); y1 = Math.min(y1, b.top); x2 = Math.max(x2, b.right); y2 = Math.max(y2, b.bottom);
  }
  return JSON.stringify(n ? { n, x: x1, y: y1, w: x2 - x1, h: y2 - y1 } : { n: 0 });
})()`

// kitSettle resolves when the page's finite animations have finished (3 s at most) and two frames have been drawn.
const kitSettle = `(async () => {
  const end = Date.now() + 3000;
  for (;;) {
    const running = document.getAnimations().filter((a) => a.playState === 'running' && a.effect && a.effect.getComputedTiming().endTime !== Infinity);
    if (!running.length || Date.now() > end) break;
    await Promise.race([Promise.allSettled(running.map((a) => a.finished)), new Promise((r) => setTimeout(r, Math.max(0, end - Date.now())))]);
  }
  await new Promise((r) => setTimeout(r, 150));
  await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
  return true;
})()`

type kitRenderJob struct {
	theme, folder, node, id string
	layout                  bool
	url                     string
	width, height           int
}

// RenderKit renders the kit in folder; see the file's comment.
func RenderKit(folder string, opt KitRenderOptions) ([]KitRenderItem, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return nil, err
	}
	id, version, err := designkit.Identity(abs)
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "harnessly-kit-render-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	kits := filepath.Join(work, "kits")
	if err := os.MkdirAll(filepath.Join(kits, id), 0o755); err != nil {
		return nil, err
	}
	if err := os.Symlink(abs, filepath.Join(kits, id, version)); err != nil {
		return nil, err
	}
	l := New(Config{LibraryDir: filepath.Join(work, "library"), DataDir: filepath.Join(work, "data"), KitsDir: kits,
		Chrome: opt.Chrome, Texts: Texts{Tag: "harnessly-kit", CLI: "harnessly"}})
	port, err := l.EnsureServer()
	if err != nil {
		return nil, err
	}
	defer l.StopServer()
	base := "http://127.0.0.1:" + strconv.Itoa(port) + "/kitv/" + id + "/" + version + "/"

	var jobs []kitRenderJob
	for _, th := range designkit.Themes(kits, id, version) {
		c, err := designkit.ReadCanvas(kits, id, version, th.ID)
		if err != nil {
			continue
		}
		page := base + url.PathEscape(th.ID) + "/"
		for _, g := range c.Groups {
			for _, k := range g.Components {
				// A part draws inside its parent and a provider draws nothing: neither is a picture of its own.
				if k.Part != "" || (len(opt.Only) > 0 && !opt.Only[k.ID]) {
					continue
				}
				props := map[string]any{}
				for n, v := range k.Sample {
					props[n] = v
				}
				if len(k.Variants) > 0 {
					for n, v := range k.Variants[0].Props {
						props[n] = v
					}
				}
				raw, _ := json.Marshal(props)
				q := url.Values{}
				q.Set("c", k.ID)
				q.Set("p", string(raw))
				jobs = append(jobs, kitRenderJob{theme: th.ID, folder: th.Folder, node: k.NodeID, id: k.ID,
					url: page + "_play.dc.html?" + q.Encode(), width: 1200, height: 800})
			}
		}
		for _, t := range c.Templates {
			for _, s := range t.Sections {
				for _, ly := range s.Layouts {
					if ly.Board != "_harnessly/layouts/"+ly.ID+".dc.html" || (len(opt.Only) > 0 && !opt.Only[ly.ID]) {
						continue
					}
					jobs = append(jobs, kitRenderJob{theme: th.ID, folder: th.Folder, node: ly.NodeID, id: ly.ID, layout: true,
						url: page + url.PathEscape(ly.ID) + ".dc.html", width: kitLayoutWidth, height: kitLayoutWidth * t.Aspect[1] / t.Aspect[0]})
				}
			}
		}
	}
	if len(jobs) == 0 {
		return nil, errors.New("nothing to render: no components or layouts with boards (is it the kit's folder?)")
	}

	bin := l.findChrome()
	if bin == "" {
		return nil, errors.New("no Chrome or Chromium on this machine: the render needs a headless browser")
	}
	b, err := l.launchBrowser(bin)
	if err != nil {
		return nil, err
	}
	defer b.close()

	out := make([]KitRenderItem, len(jobs))
	tabs := opt.Tabs
	if tabs <= 0 {
		tabs = 4
	}
	var next int
	var mu sync.Mutex
	var wg sync.WaitGroup
	var tabErr error
	for w := 0; w < min(tabs, len(jobs)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t, err := b.cdp.send("Target.createTarget", map[string]any{"url": "about:blank"}, "")
			if err == nil {
				var a map[string]any
				if a, err = b.cdp.send("Target.attachToTarget", map[string]any{"targetId": t["targetId"], "flatten": true}, ""); err == nil {
					session, _ := a["sessionId"].(string)
					for _, d := range []string{"Page", "Runtime", "Log", "Network"} {
						if _, err = b.cdp.send(d+".enable", nil, session); err != nil {
							break
						}
					}
					for err == nil {
						mu.Lock()
						i := next
						next++
						mu.Unlock()
						if i >= len(jobs) {
							return
						}
						out[i] = renderKitJob(b.cdp, session, jobs[i], abs, opt.Shots)
					}
				}
			}
			mu.Lock()
			tabErr = err
			mu.Unlock()
		}()
	}
	wg.Wait()
	if tabErr != nil {
		return nil, tabErr
	}
	if opt.Shots {
		if err := writeKitThumbs(abs, out, jobs); err != nil {
			return out, err
		}
	}
	return out, nil
}

// renderKitJob opens one page in the tab and judges it.
func renderKitJob(cdp *cdpPipe, session string, j kitRenderJob, root string, shots bool) KitRenderItem {
	started := time.Now()
	item := KitRenderItem{Theme: j.theme, Node: j.node}
	var mu sync.Mutex
	var errs []string
	push := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		if s = line(s); s != "" && len(errs) < 8 {
			errs = append(errs, s)
		}
	}
	loaded := make(chan struct{})
	var once sync.Once
	urls := map[string]string{}
	stop := cdp.on(func(ev cdpEvent) {
		if ev.SessionID != session {
			return
		}
		p := ev.Params
		switch ev.Method {
		case "Page.loadEventFired":
			once.Do(func() { close(loaded) })
		case "Runtime.exceptionThrown":
			d, _ := p["exceptionDetails"].(map[string]any)
			msg := jsUndef(d, "text")
			if ex, ok := d["exception"].(map[string]any); ok {
				if desc, ok := ex["description"].(string); ok {
					msg, _, _ = strings.Cut(desc, "\n")
				}
			}
			push(msg)
		case "Runtime.consoleAPICalled":
			if p["type"] != "error" {
				return
			}
			args, _ := p["args"].([]any)
			parts := make([]string, 0, len(args))
			for _, a := range args {
				if m, ok := a.(map[string]any); ok {
					v := m["value"]
					if v == nil {
						v = m["description"]
					}
					parts = append(parts, jscompat.String(v))
				}
			}
			push(strings.Join(parts, " "))
		case "Network.requestWillBeSent":
			req, _ := p["request"].(map[string]any)
			mu.Lock()
			urls[jscompat.String(p["requestId"])] = jsStringOr(req["url"])
			mu.Unlock()
		case "Network.loadingFailed":
			if !jscompat.Truthy(p["canceled"]) {
				mu.Lock()
				u := urls[jscompat.String(p["requestId"])]
				mu.Unlock()
				push(jsUndef(p, "errorText") + " " + u)
			}
		case "Network.responseReceived":
			resp, _ := p["response"].(map[string]any)
			if status, _ := resp["status"].(float64); status >= 400 && !faviconRe.MatchString(jsStringOr(resp["url"])) {
				push(jsUndef(resp, "status") + " " + jsUndef(resp, "url"))
			}
		}
	})
	defer stop()
	done := func(status string) KitRenderItem {
		mu.Lock()
		item.Errors = append([]string(nil), errs...)
		mu.Unlock()
		if status == statusOK && len(item.Errors) > 0 {
			status = statusError
		}
		item.Status = status
		item.Ms = time.Since(started).Milliseconds()
		return item
	}
	if _, err := cdp.send("Emulation.setDeviceMetricsOverride", map[string]any{"width": j.width, "height": j.height,
		"deviceScaleFactor": 2, "mobile": false}, session); err != nil {
		push(err.Error())
		return done(statusError)
	}
	if _, err := cdp.send("Page.navigate", map[string]any{"url": j.url}, session); err != nil {
		push(err.Error())
		return done(statusError)
	}
	select {
	case <-loaded:
	case <-time.After(checkLoadTimeout):
		push("the page did not load in " + checkLoadTimeout.String())
		return done(statusError)
	}
	time.Sleep(kitRenderSettle)
	// The engine mounts the board after load (React, Babel): wait for content a little before calling it blank.
	var box struct {
		N          int
		X, Y, W, H float64
	}
	for deadline := time.Now().Add(kitRenderWait); ; time.Sleep(250 * time.Millisecond) {
		res, err := cdp.send("Runtime.evaluate", map[string]any{"expression": kitMeasure, "returnByValue": true}, session)
		if err != nil {
			push(err.Error())
			return done(statusError)
		}
		r, _ := res["result"].(map[string]any)
		_ = json.Unmarshal([]byte(jsStringOr(r["value"])), &box)
		if box.N > 0 || time.Now().After(deadline) {
			break
		}
	}
	if box.N == 0 {
		return done(statusBlank)
	}
	// Overlays and lists animate in: a picture taken mid-way is a half-transparent dialog. Wait for the finite
	// animations of the page (spinners run forever and are left), then measure again.
	if _, err := cdp.send("Runtime.evaluate", map[string]any{"expression": kitSettle, "awaitPromise": true, "returnByValue": true}, session); err == nil {
		if res, err := cdp.send("Runtime.evaluate", map[string]any{"expression": kitMeasure, "returnByValue": true}, session); err == nil {
			r, _ := res["result"].(map[string]any)
			_ = json.Unmarshal([]byte(jsStringOr(r["value"])), &box)
		}
	}
	if shots {
		// Script-driven animations (a dialog fading in by requestAnimationFrame) are not in getAnimations(): the
		// picture is taken when two frames 200 ms apart are the same, 4 s at most.
		var data string
		for deadline := time.Now().Add(4 * time.Second); ; time.Sleep(200 * time.Millisecond) {
			if res, err := cdp.send("Runtime.evaluate", map[string]any{"expression": kitMeasure, "returnByValue": true}, session); err == nil {
				r, _ := res["result"].(map[string]any)
				_ = json.Unmarshal([]byte(jsStringOr(r["value"])), &box)
			}
			params := map[string]any{"format": "png"}
			if !j.layout && box.N > 0 {
				pad := float64(kitRenderPadding)
				x, y := max(0, box.X-pad), max(0, box.Y-pad)
				params["clip"] = map[string]any{"x": x, "y": y, "width": min(box.W+2*pad, float64(j.width)-x),
					"height": min(box.H+2*pad, float64(j.height)-y), "scale": 1}
			}
			pic, err := cdp.send("Page.captureScreenshot", params, session)
			if err != nil {
				push(err.Error())
				return done(statusError)
			}
			next := jsStringOr(pic["data"])
			if next == data || time.Now().After(deadline) {
				data = next
				break
			}
			data = next
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			push(err.Error())
			return done(statusError)
		}
		kind := "components"
		if j.layout {
			kind = "layouts"
		}
		// The id comes from the kit's own index: it names a file only when it is a plain name, and the file stays
		// inside the kit's folder whatever the theme's folder says.
		if !kitThumbNameRe.MatchString(j.id) || strings.Contains(j.id, "..") {
			push(fmt.Sprintf("id %q cannot name a picture file", j.id))
			return done(statusError)
		}
		rel := path.Join("_harnessly", "thumbs", kind, j.id+".png")
		file := filepath.Join(root, filepath.FromSlash(j.folder), filepath.FromSlash(rel))
		if back, err := filepath.Rel(root, file); err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
			push(fmt.Sprintf("picture %s would land outside the kit", rel))
			return done(statusError)
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			push(err.Error())
			return done(statusError)
		}
		if err := os.WriteFile(file, raw, 0o644); err != nil {
			push(err.Error())
			return done(statusError)
		}
		item.Shot = path.Join(j.folder, rel)
	}
	return done(statusOK)
}

// kitThumbNameRe is an id that may name a picture file: a component's (Button, Dialog.Body) or a layout's.
var kitThumbNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// writeKitThumbs puts the pictures that were taken into each theme's design-system.json: a component's and a layout's
// "thumb". The rest of the file is kept as it is, its keys sorted (Go's encoding of a map).
func writeKitThumbs(root string, items []KitRenderItem, jobs []kitRenderJob) error {
	byFolder := map[string]map[string]string{} // theme folder → node id → thumb (relative to the theme)
	for i, it := range items {
		if it.Shot == "" {
			continue
		}
		j := jobs[i]
		if byFolder[j.folder] == nil {
			byFolder[j.folder] = map[string]string{}
		}
		rel := strings.TrimPrefix(it.Shot, j.folder+"/")
		if j.folder == "" {
			rel = it.Shot
		}
		byFolder[j.folder][j.node] = rel
	}
	folders := make([]string, 0, len(byFolder))
	for f := range byFolder {
		folders = append(folders, f)
	}
	sort.Strings(folders)
	for _, folder := range folders {
		thumbs := byFolder[folder]
		file := filepath.Join(root, filepath.FromSlash(folder), designkit.ManifestFile)
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		for _, g := range asList(m["groups"]) {
			for _, k := range asList(asMap(g)["components"]) {
				km := asMap(k)
				id, _ := km["id"].(string)
				if id == "" {
					id, _ = km["name"].(string)
				}
				if t, ok := thumbs["component:"+id]; ok {
					km["thumb"] = t
				}
			}
		}
		for _, t := range asList(m["templates"]) {
			tm := asMap(t)
			tid, _ := tm["id"].(string)
			for _, s := range asList(tm["sections"]) {
				for _, ly := range asList(asMap(s)["layouts"]) {
					lm := asMap(ly)
					lid, _ := lm["id"].(string)
					if th, ok := thumbs["layout:"+tid+"/"+lid]; ok {
						lm["thumb"] = th
					}
				}
			}
		}
		out, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, append(out, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
