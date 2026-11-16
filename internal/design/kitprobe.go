package design

// ProbeKit reads, in this machine's headless browser, the design a kit's components see when they are drawn: the CSS
// custom properties at the root of the page and around a component inside the kit's provider, with every value that is
// made of others (calc(), color-mix(), var() chains) resolved by the browser, and their values under the dark selector
// the page's styles answer to. A package that builds its theme in JavaScript (MUI, Ant Design, Chakra, emotion,
// styled-components) has no stylesheet to read the tokens from: it has this.

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// KitProbeOptions are ProbeKit's.
type KitProbeOptions struct {
	Chrome  string
	Offline bool
	KitsDir string
}

// KitProbeVar is one custom property: Raw as the page's styles declare it, Value resolved (the same when it needs no
// resolving), Dark its value under the dark selector when that differs.
type KitProbeVar struct {
	Name  string `json:"name"`
	Raw   string `json:"raw"`
	Value string `json:"value"`
	Dark  string `json:"dark,omitempty"`
}

// KitProbe is what ProbeKit read.
type KitProbe struct {
	Vars      []KitProbeVar     `json:"vars"`
	Theme     map[string]string `json:"theme,omitempty"` // the theme object the kit's provider published as CSS variables
	DarkBy    string            `json:"darkBy,omitempty"`
	Component string            `json:"component"`
	Errors    []string          `json:"errors,omitempty"`
}

const kitProbeJS = `(async () => {
  const doc = document, rootEl = doc.documentElement;
  const subject = doc.querySelector('[data-kit-subject]');
  const holder = (subject && subject.parentElement) || doc.body;
  const skip = /^--(dc-|hx-|harnessly|tw-|webkit|kit-subject|x-)/;
  const names = new Set();
  const addComputed = (el) => { const cs = getComputedStyle(el); for (let i = 0; i < cs.length; i++) { const n = cs[i]; if (n.startsWith('--') && !skip.test(n)) names.add(n); } };
  [rootEl, doc.body, holder, subject].filter(Boolean).forEach(addComputed);
  const darkSels = [];
  const walk = (rules) => { for (const r of rules) {
    if (r.style && r.selectorText) {
      let has = false;
      for (let i = 0; i < r.style.length; i++) { const n = r.style[i]; if (n.startsWith('--')) { has = true; if (!skip.test(n) && /^(:where\(\s*)?(:root|html|:host|body)\b/.test(r.selectorText.trim())) names.add(n); } }
      if (has && /dark/i.test(r.selectorText)) for (const s of r.selectorText.split(',')) if (/dark/i.test(s) && !/[ >+~]/.test(s.trim().replace(/\[[^\]]*\]/g, '')) ) darkSels.push(s.trim());
    }
    if (r.cssRules) walk(r.cssRules);
  } };
  for (const s of doc.styleSheets) { try { walk(s.cssRules); } catch (e) {} }
  const tv = (window.__kitTheme && window.__kitTheme.vars) || {};
  for (const n in tv) names.add(n);

  const wrap = doc.createElement('span');
  wrap.style.cssText = 'position:absolute;visibility:hidden;color:rgb(1, 2, 3);letter-spacing:7px';
  const probe = doc.createElement('i');
  wrap.appendChild(probe);
  holder.appendChild(wrap);
  const toHex = (c) => {
    let m = /^rgba?\(\s*([\d.]+)[ ,]+([\d.]+)[ ,]+([\d.]+)(?:\s*[,/]\s*([\d.%]+))?\s*\)$/.exec(c), k = 1;
    if (!m) { m = /^color\(srgb\s+([\d.e-]+)\s+([\d.e-]+)\s+([\d.e-]+)(?:\s*\/\s*([\d.%]+))?\s*\)$/.exec(c); k = 255; }
    if (!m) return c;
    const h = (v) => Math.max(0, Math.min(255, Math.round(parseFloat(v) * k))).toString(16).padStart(2, '0');
    let a = m[4] === undefined ? 1 : (String(m[4]).endsWith('%') ? parseFloat(m[4]) / 100 : parseFloat(m[4]));
    return '#' + h(m[1]) + h(m[2]) + h(m[3]) + (a < 1 ? Math.round(a * 255).toString(16).padStart(2, '0') : '');
  };
  const NEEDS = /var\(|calc\(|color-mix\(|clamp\(|\b(?:min|max)\(|\b(?:rgb|hsl|oklch|oklab|lab|lch)\(\s*from\b/;
  const read = (name) => {
    let raw = getComputedStyle(holder).getPropertyValue(name).trim();
    if (!raw && tv[name]) raw = String(tv[name]);
    if (!raw) return null;
    if (!NEEDS.test(raw)) return { raw, value: raw };
    probe.style.color = ''; probe.style.letterSpacing = '';
    probe.style.color = 'var(' + name + ')';
    const c = getComputedStyle(probe).color;
    if (c && c !== 'rgb(1, 2, 3)') return { raw, value: toHex(c) };
    probe.style.color = ''; probe.style.letterSpacing = 'var(' + name + ')';
    const l = getComputedStyle(probe).letterSpacing;
    if (l && l !== '7px' && l !== 'normal') return { raw, value: l };
    return { raw, value: raw };
  };
  const light = {};
  for (const n of names) { const r = read(n); if (r) light[n] = r; }

  // Dark: the selector the page's styles have for it, applied to <html>, and what changes.
  const cands = [...new Set(darkSels)].concat(['[data-theme="dark"]']);
  let darkBy = '';
  const dark = {};
  for (const sel of cands) {
    const attrs = [], classes = [];
    const clean = sel.replace(/^:where\(|\)$/g, '');
    for (const m of clean.matchAll(/\[([\w-]+)(?:[~|^$*]?=["']?([^"'\]]*)["']?)?\]/g)) attrs.push([m[1], m[2] === undefined ? '' : m[2]]);
    for (const m of clean.matchAll(/\.([\w-]+)/g)) classes.push(m[1]);
    if (!attrs.length && !classes.length) continue;
    const old = attrs.map(([a]) => rootEl.getAttribute(a));
    attrs.forEach(([a, v]) => rootEl.setAttribute(a, v));
    classes.forEach((c) => rootEl.classList.add(c));
    await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
    let n = 0;
    for (const name of names) { const r = read(name); if (r && light[name] && r.value !== light[name].value) { dark[name] = r.value; n++; } }
    attrs.forEach(([a], i) => { if (old[i] === null) rootEl.removeAttribute(a); else rootEl.setAttribute(a, old[i]); });
    classes.forEach((c) => rootEl.classList.remove(c));
    if (n > 0) { darkBy = sel; break; }
  }
  wrap.remove();
  const vars = Object.keys(light).sort().map((n) => ({ name: n, raw: light[n].raw, value: light[n].value, dark: dark[n] || '' }));
  return JSON.stringify({ vars, theme: tv, darkBy });
})()`

// ProbeKit reads the kit in folder; see the file's comment.
func ProbeKit(folder string, opt KitProbeOptions) (*KitProbe, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return nil, err
	}
	id, version, err := designkit.Identity(abs)
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "harnessly-kit-probe-")
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
	if opt.KitsDir != "" {
		if others, err := os.ReadDir(opt.KitsDir); err == nil {
			for _, e := range others {
				if e.IsDir() && e.Name() != id && designkit.ValidID(e.Name()) {
					_ = os.Symlink(filepath.Join(opt.KitsDir, e.Name()), filepath.Join(kits, e.Name()))
				}
			}
		}
	}
	themes := designkit.Themes(kits, id, version)
	if len(themes) == 0 {
		return nil, errors.New("the kit has no theme to draw")
	}
	c, err := designkit.ReadCanvas(kits, id, version, themes[0].ID)
	if err != nil {
		return nil, err
	}
	comp := ""
	for _, g := range c.Groups {
		for _, k := range g.Components {
			if k.Part == "" && comp == "" {
				comp = k.ID
			}
		}
	}
	if comp == "" {
		return nil, errors.New("the kit has no component to draw the page with")
	}
	l := New(Config{Offline: opt.Offline, LibraryDir: filepath.Join(work, "library"), DataDir: filepath.Join(work, "data"), KitsDir: kits,
		Chrome: opt.Chrome, Texts: Texts{Tag: "harnessly-kit", CLI: "harnessly"}})
	port, err := l.EnsureServer()
	if err != nil {
		return nil, err
	}
	defer l.StopServer()
	bin := l.findChrome()
	if bin == "" {
		return nil, errors.New("no Chrome or Chromium on this machine")
	}
	b, err := l.launchBrowser(bin)
	if err != nil {
		return nil, err
	}
	defer b.close()
	t, err := b.cdp.send("Target.createTarget", map[string]any{"url": "about:blank"}, "")
	if err != nil {
		return nil, err
	}
	a, err := b.cdp.send("Target.attachToTarget", map[string]any{"targetId": t["targetId"], "flatten": true}, "")
	if err != nil {
		return nil, err
	}
	session, _ := a["sessionId"].(string)
	for _, d := range []string{"Page", "Runtime"} {
		if _, err := b.cdp.send(d+".enable", nil, session); err != nil {
			return nil, err
		}
	}
	probe := &KitProbe{Component: comp}
	loaded := make(chan struct{})
	closed := false
	stop := b.cdp.on(func(ev cdpEvent) {
		if ev.SessionID != session {
			return
		}
		switch ev.Method {
		case "Page.loadEventFired":
			if !closed {
				closed = true
				close(loaded)
			}
		case "Runtime.exceptionThrown":
			d, _ := ev.Params["exceptionDetails"].(map[string]any)
			if len(probe.Errors) < 6 {
				probe.Errors = append(probe.Errors, line(jsUndef(d, "text")))
			}
		}
	})
	defer stop()
	_, _ = b.cdp.send("Emulation.setDeviceMetricsOverride", map[string]any{"width": 1200, "height": 800, "deviceScaleFactor": 1, "mobile": false}, session)
	q := url.Values{}
	q.Set("c", comp)
	q.Set("p", "{}")
	page := "http://127.0.0.1:" + strconv.Itoa(port) + "/kitv/" + id + "/" + version + "/" + url.PathEscape(themes[0].ID) + "/_play.dc.html?" + q.Encode()
	if _, err := b.cdp.send("Page.navigate", map[string]any{"url": page}, session); err != nil {
		return nil, err
	}
	select {
	case <-loaded:
	case <-time.After(checkLoadTimeout):
		return nil, errors.New("the page did not load in " + checkLoadTimeout.String())
	}
	// The engine mounts the board after load: wait for the subject's marker, then for styles to be injected.
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		res, err := b.cdp.send("Runtime.evaluate", map[string]any{"expression": `!!document.querySelector('[data-kit-subject]') && document.querySelector('[data-kit-subject]').children.length > 0`, "returnByValue": true}, session)
		if err == nil {
			if r, _ := res["result"].(map[string]any); r["value"] == true {
				break
			}
		}
	}
	time.Sleep(800 * time.Millisecond)
	res, err := b.cdp.send("Runtime.evaluate", map[string]any{"expression": kitProbeJS, "awaitPromise": true, "returnByValue": true}, session)
	if err != nil {
		return nil, err
	}
	r, _ := res["result"].(map[string]any)
	if ex, ok := res["exceptionDetails"].(map[string]any); ok {
		return nil, errors.New("the probe failed in the page: " + line(jsUndef(ex, "text")+" "+jscompat.String(ex["exception"])))
	}
	var got struct {
		Vars   []KitProbeVar     `json:"vars"`
		Theme  map[string]string `json:"theme"`
		DarkBy string            `json:"darkBy"`
	}
	if err := json.Unmarshal([]byte(jsStringOr(r["value"])), &got); err != nil {
		return nil, err
	}
	probe.Vars, probe.Theme, probe.DarkBy = got.Vars, got.Theme, got.DarkBy
	return probe, nil
}
