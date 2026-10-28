package design

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"os"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// HTML export: one file that opens offline. Every slide is shown in the browser, taken as it stands (the page the
// board's engine built), with its styles, fonts and pictures written into it as data, its scripts left out; the
// slides go into one page as frames, with a little viewer (arrows, a counter, full screen) in front of them.

func init() {
	RegisterDeckExporter(DeckExportHTML, DeckExporter{Needs: []string{"chrome"}, Ext: "html", Run: runDeckHTML})
}

const (
	// deckHTMLMax bounds the file; deckSnapshotMax one slide's page, checked in the browser before it is sent over.
	deckHTMLMax     = 256 << 20
	deckSnapshotMax = 64 << 20
)

// deckSnapshot runs in a slide and gives back the page as one self-contained HTML text.
const deckSnapshotJS = `(async () => {
  const cache = new Map();
  const dataOf = (url) => {
    if (!cache.has(url)) {
      cache.set(url, fetch(url).then(async (r) => {
        if (!r.ok) throw new Error(r.status + ' ' + url);
        const b = await r.blob();
        return await new Promise((ok, no) => { const f = new FileReader(); f.onload = () => ok(f.result); f.onerror = () => no(f.error); f.readAsDataURL(b); });
      }).catch(() => 'data:,'));
    }
    return cache.get(url);
  };
  const absolute = (u, base) => { try { return new URL(u, base).href; } catch (e) { return null; } };
  const URL_RE = /url\(\s*(?:"([^"]*)"|'([^']*)'|([^)"'\s]+))\s*\)/g;
  const inlineUrls = async (text, base) => {
    const found = [];
    text.replace(URL_RE, (m, a, b, c) => { found.push(a ?? b ?? c); return m; });
    const map = new Map();
    await Promise.all([...new Set(found)].map(async (u) => {
      if (!u || u.startsWith('data:') || u.startsWith('#')) return;
      const abs = absolute(u, base);
      if (abs) map.set(u, await dataOf(abs));
    }));
    return text.replace(URL_RE, (m, a, b, c) => {
      const u = a ?? b ?? c;
      return map.has(u) ? 'url("' + map.get(u) + '")' : m;
    });
  };
  const unusedFace = (rule) => {
    const fam = rule.style.getPropertyValue('font-family').replace(/^["']|["']$/g, '');
    const weight = rule.style.getPropertyValue('font-weight') || 'normal';
    const style = rule.style.getPropertyValue('font-style') || 'normal';
    let same = 0, loaded = 0;
    document.fonts.forEach((f) => {
      if (f.family.replace(/^["']|["']$/g, '') === fam && f.weight === weight && f.style === style) { same++; if (f.status === 'loaded') loaded++; }
    });
    return same > 0 && loaded === 0;
  };
  const sheetText = async (sheet, out, seen) => {
    if (!sheet || seen.has(sheet)) return;
    seen.add(sheet);
    let rules;
    try { rules = sheet.cssRules; } catch (e) { return; }
    const base = sheet.href || document.baseURI;
    for (const rule of rules) {
      if (rule instanceof CSSImportRule) { await sheetText(rule.styleSheet, out, seen); continue; }
      if (rule instanceof CSSFontFaceRule && unusedFace(rule)) continue;
      out.push(await inlineUrls(rule.cssText, base));
    }
  };
  const css = [];
  const seen = new Set();
  for (const s of document.styleSheets) if (!s.disabled) await sheetText(s, css, seen);
  for (const s of document.adoptedStyleSheets || []) await sheetText(s, css, seen);

  document.querySelectorAll('canvas').forEach((c) => { try { c.setAttribute('data-snapshot', c.toDataURL('image/png')); } catch (e) {} });
  const root = document.documentElement.cloneNode(true);
  root.querySelectorAll('script,noscript,template,link,style,base,iframe,object,embed,[data-tk-library-bar]').forEach((n) => n.remove());
  root.querySelectorAll('meta[http-equiv]').forEach((n) => n.remove());
  const head = root.querySelector('head') || root.insertBefore(document.createElement('head'), root.firstChild);
  const style = document.createElement('style');
  style.textContent = css.join('\n');
  head.appendChild(style);
  const base = document.baseURI;
  const attr = async (el, name) => {
    const v = el.getAttribute(name);
    if (!v || v.startsWith('data:') || v.startsWith('#') || v.startsWith('blob:')) return;
    const abs = absolute(v, base);
    if (abs) el.setAttribute(name, await dataOf(abs));
  };
  const jobs = [];
  root.querySelectorAll('img[src],video[poster],image[href],image[*|href],use[href]').forEach((el) => {
    if (el.tagName === 'use') { const h = el.getAttribute('href'); if (h && !h.startsWith('#') && !h.startsWith('data:')) jobs.push(attr(el, 'href')); return; }
    jobs.push(attr(el, el.tagName === 'VIDEO' ? 'poster' : el.hasAttribute('src') ? 'src' : 'href'));
  });
  root.querySelectorAll('img[srcset],source').forEach((el) => { el.removeAttribute('srcset'); if (el.tagName === 'SOURCE') el.remove(); });
  root.querySelectorAll('video,audio').forEach((el) => el.remove());
  root.querySelectorAll('canvas[data-snapshot]').forEach((c) => {
    const img = document.createElement('img');
    img.src = c.getAttribute('data-snapshot');
    img.className = c.className;
    img.style.cssText = c.style.cssText;
    img.width = c.width; img.height = c.height;
    c.replaceWith(img);
  });
  root.querySelectorAll('[style*="url("]').forEach((el) => {
    jobs.push(inlineUrls(el.getAttribute('style'), base).then((t) => el.setAttribute('style', t)));
  });
  await Promise.all(jobs);
  // The page is shown as a picture, never run: nothing in it may act.
  const BAD_URL = /^\s*(javascript|vbscript|data\s*:\s*text\/html)/i;
  const BAD_TAGS = 'animate,animatetransform,animatemotion,set,handler,listener,foreignobject,meta[http-equiv],form,frame,frameset,applet,portal,dialog';
  root.querySelectorAll(BAD_TAGS).forEach((n) => n.remove());
  for (const el of root.querySelectorAll('*')) {
    for (const a of [...el.attributes]) {
      const name = a.name.toLowerCase();
      if (name.startsWith('on') || name === 'srcdoc' || name === 'formaction' || name === 'action') { el.removeAttribute(a.name); continue; }
      if ((name === 'href' || name === 'xlink:href' || name === 'src' || name === 'poster' || name === 'data') && BAD_URL.test(a.value.replace(/[\u0000-\u0020]+/g, ''))) el.removeAttribute(a.name);
    }
  }
  const html = '<!doctype html>' + root.outerHTML;
  if (html.length > MAXLEN) throw new Error('the slide is too big for one HTML file');
  return html;
})()`

// deckViewer is the page the slides stand in.
const deckViewerHead = `<!doctype html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s</title><style>
html,body{margin:0;height:100%%;background:#0b0b0d;overflow:hidden;font:13px system-ui,sans-serif;color:#fff}
#st{position:fixed;left:0;top:0;transform-origin:0 0}
.f{position:absolute;left:0;top:0;border:0;background:#fff;opacity:0;pointer-events:none;transition:opacity .25s ease-out}
.f[data-on]{opacity:1}
#bar{position:fixed;left:0;right:0;bottom:0;display:flex;justify-content:center;align-items:center;gap:12px;padding:10px;opacity:0;transition:opacity .25s ease-out}
body:hover #bar,#bar:focus-within{opacity:1}
#bar button{font:inherit;color:#fff;background:rgba(255,255,255,.14);border:0;border-radius:8px;padding:6px 12px;cursor:pointer}
#bar button:hover{background:rgba(255,255,255,.26)}
#n{min-width:64px;text-align:center;font-variant-numeric:tabular-nums}
</style></head><body><div id="st">`

const deckViewerTail = `</div><div id="bar"><button id="p" type="button" aria-label="%s">&#8592;</button><span id="n"></span><button id="x" type="button" aria-label="%s">&#8594;</button><button id="z" type="button">%s</button></div>
<script>(function(){
var fs=[].slice.call(document.querySelectorAll('.f')),st=document.getElementById('st'),n=document.getElementById('n'),at=0;
function fit(){var f=fs[at],w=+f.getAttribute('data-w'),h=+f.getAttribute('data-h'),s=Math.min(innerWidth/w,innerHeight/h);
st.style.width=w+'px';st.style.height=h+'px';st.style.transform='translate('+(innerWidth-w*s)/2+'px,'+(innerHeight-h*s)/2+'px) scale('+s+')';}
function go(i){at=Math.max(0,Math.min(fs.length-1,i));fs.forEach(function(f,k){if(k===at)f.setAttribute('data-on','');else f.removeAttribute('data-on');});
n.textContent=(at+1)+' / '+fs.length;fit();try{history.replaceState(null,'','#'+(at+1));}catch(e){}}
document.getElementById('p').onclick=function(){go(at-1)};document.getElementById('x').onclick=function(){go(at+1)};
document.getElementById('z').onclick=function(){if(document.fullscreenElement)document.exitFullscreen();else document.documentElement.requestFullscreen&&document.documentElement.requestFullscreen()};
addEventListener('keydown',function(e){var k=e.key;if(k==='ArrowRight'||k==='PageDown'||k===' ')go(at+1);else if(k==='ArrowLeft'||k==='PageUp')go(at-1);else if(k==='Home')go(0);else if(k==='End')go(fs.length-1);else if(k==='f'||k==='F')document.getElementById('z').click();else return;e.preventDefault()});
addEventListener('resize',fit);
fs.forEach(function(f){f.style.width=f.getAttribute('data-w')+'px';f.style.height=f.getAttribute('data-h')+'px'});
go(Math.max(0,(parseInt(location.hash.slice(1),10)||1)-1));
})();</script></body></html>`

var srcdocEscape = strings.NewReplacer("&", "&amp;", `"`, "&quot;")

// deckSnapshot is a slide's page, taken in the browser; a page over the limit is refused there, before it is sent.
func deckSnapshot() string {
	return strings.Replace(deckSnapshotJS, "MAXLEN", strconv.Itoa(deckSnapshotMax), 1)
}

// deckFrame is a slide's frame. The page in it is only to be looked at: the frame has no right to run a script.
func deckFrame(sl deckSlideSrc, page string) string {
	return fmt.Sprintf(`<iframe class="f" sandbox="" data-w="%d" data-h="%d" title="%s" srcdoc="%s"></iframe>`, sl.W, sl.H,
		html.EscapeString(sl.Title), srcdocEscape.Replace(page))
}

func deckViewerOpen(title string) string {
	return fmt.Sprintf(deckViewerHead, locale.Pick("en", "ru"), html.EscapeString(title))
}

func deckViewerClose() string {
	return fmt.Sprintf(deckViewerTail, locale.Pick("Previous slide", "Предыдущий слайд"), locale.Pick("Next slide", "Следующий слайд"),
		locale.Pick("Full screen", "Во весь экран"))
}

// runDeckHTML writes the file as the slides come, so that a big deck is never held whole in memory.
func runDeckHTML(ctx context.Context, run DeckExportRun) error {
	slides, err := run.L.deckSlides(run.Artifact)
	if err != nil {
		return err
	}
	sh, err := run.L.newDeckShooter(run.Artifact)
	if err != nil {
		return err
	}
	defer sh.close()
	tab, err := sh.tab()
	if err != nil {
		return err
	}
	defer tab.close()
	title := ""
	if a := run.L.ReadArtifact(run.Artifact); a != nil {
		title = a.Title
	}
	out, err := os.Create(run.Out)
	if err != nil {
		return err
	}
	defer out.Close()
	w := bufio.NewWriterSize(out, 1<<20)
	total := 0
	write := func(s string) error {
		total += len(s)
		if total > deckHTMLMax {
			return fmt.Errorf("%s", locale.Pick("The deck is too big for one HTML file", "Презентация слишком велика для одного HTML-файла"))
		}
		_, err := w.WriteString(s)
		return err
	}
	if err := write(deckViewerOpen(title)); err != nil {
		return err
	}
	script := deckSnapshot()
	for i, sl := range slides {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tab.open(sl, 1); err != nil {
			return fmt.Errorf("%s: %w", sl.Title, err)
		}
		v, err := tab.eval(script)
		if err != nil {
			return fmt.Errorf("%s: %w", sl.Title, err)
		}
		page, _ := v.(string)
		if page == "" {
			return fmt.Errorf("%s: %s", sl.Title, locale.Pick("the slide gave no page", "слайд не отдал страницу"))
		}
		// The size counted is the escaped one, which is what lands in the file.
		if err := write(deckFrame(sl, page)); err != nil {
			return err
		}
		run.Progress(0.95 * float64(i+1) / float64(len(slides)))
	}
	if err := write(deckViewerClose()); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return out.Close()
}
