package design

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// PPTX export: each slide is opened in the browser, its texts measured and taken out of the picture (a block of text
// the browser lays out in a way a text box cannot hold stays in the picture), the picture taken, and the texts laid
// over it as text boxes. The speaker notes go into the notes pages.

func init() {
	RegisterDeckExporter(DeckExportPPTX, DeckExporter{Needs: []string{"chrome"}, Ext: "pptx", Run: runDeckPPTX})
}

// deckMeasure runs in a slide. It collects the blocks of text (a block is the text of an element and its inline
// children, or a run of text beside a block child), writes them down by their place and style, and hides them in the
// page so that the picture taken next is without them. Whatever it cannot say in a text box it leaves alone.
const deckMeasure = `(() => {
  const W = innerWidth, H = innerHeight;
  const GENERIC = new Set(['serif','sans-serif','monospace','cursive','fantasy','system-ui','ui-sans-serif','ui-serif','ui-monospace','ui-rounded','emoji','math','fangsong','-apple-system','blinkmacsystemfont']);
  const SKIP = new Set(['SCRIPT','STYLE','TEMPLATE','NOSCRIPT','HEAD','svg','CANVAS','VIDEO','AUDIO','IFRAME','INPUT','TEXTAREA','SELECT','OBJECT','EMBED','IMG','PICTURE']);
  const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
  const colors = new Map();
  const rgba = (css) => {
    if (colors.has(css)) return colors.get(css);
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = '#000';
    ctx.fillStyle = css;
    ctx.fillRect(0, 0, 1, 1);
    const d = ctx.getImageData(0, 0, 1, 1).data;
    const a = d[3] / 255;
    const out = a === 0 ? { c: '000000', a: 0 } : {
      c: [0, 1, 2].map((i) => Math.min(255, Math.round(d[i] / a)).toString(16).padStart(2, '0')).join(''),
      a,
    };
    colors.set(css, out);
    return out;
  };
  const blocks = [];
  const mine = [];
  const cs = (el) => getComputedStyle(el);
  const rotated = (el) => {
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
      const t = cs(n).transform;
      if (!t || t === 'none') continue;
      const m = t.match(/^matrix(3d)?\((.+)\)$/);
      if (!m) return true;
      const v = m[2].split(',').map(Number);
      if (m[1]) {
        if (Math.abs(v[0] - 1) > 1e-3 || Math.abs(v[5] - 1) > 1e-3 || Math.abs(v[1]) > 1e-3 || Math.abs(v[4]) > 1e-3 || Math.abs(v[2]) > 1e-3 || Math.abs(v[6]) > 1e-3) return true;
      } else if (Math.abs(v[0] - 1) > 1e-3 || Math.abs(v[3] - 1) > 1e-3 || Math.abs(v[1]) > 1e-3 || Math.abs(v[2]) > 1e-3) return true;
    }
    return false;
  };
  const opacity = (el) => {
    let o = 1;
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) o *= Number(cs(n).opacity);
    return o;
  };
  const hidden = (el) => {
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
      const s = cs(n);
      if (s.display === 'none' || s.visibility === 'hidden') return true;
    }
    return false;
  };
  const clipped = (el) => {
    for (let n = el, depth = 0; n && n.nodeType === 1 && depth < 6; n = n.parentElement, depth++) {
      const s = cs(n);
      if (s.webkitLineClamp && s.webkitLineClamp !== 'none') return true;
      if (s.textOverflow === 'ellipsis' && s.overflow !== 'visible') return true;
    }
    return false;
  };
  const isInline = (el) => {
    if (SKIP.has(el.tagName) || el.tagName === 'BR') return false;
    const d = cs(el).display;
    if (d !== 'inline') return false;
    for (const c of el.children) if (c.tagName !== 'BR' && !isInline(c)) return false;
    return true;
  };
  const preserves = (s) => /^pre/.test(s.whiteSpace) || s.whiteSpace === 'break-spaces';
  const transform = (text, s) => {
    switch (s.textTransform) {
      case 'uppercase': return text.toUpperCase();
      case 'lowercase': return text.toLowerCase();
      case 'capitalize': return text.replace(/(^|\s)(\S)/g, (m, a, b) => a + b.toUpperCase());
      default: return text;
    }
  };
  const family = (s) => {
    for (const raw of s.fontFamily.split(',')) {
      const f = raw.trim().replace(/^["']|["']$/g, '');
      if (f && !GENERIC.has(f.toLowerCase())) return f;
    }
    const g = s.fontFamily.toLowerCase();
    return g.includes('mono') ? 'Courier New' : g.includes('serif') && !g.includes('sans') ? 'Times New Roman' : null;
  };
  const make = (nodes, owner) => {
    const runs = [];
    const styled = [];
    let spaceBefore = true;
    let skip = false;
    const push = (text, el) => {
      const s = cs(el);
      if (s.backgroundClip === 'text' || s.webkitBackgroundClip === 'text') { skip = true; return; }
      const col = rgba(s.webkitTextFillColor && s.webkitTextFillColor !== s.color ? s.webkitTextFillColor : s.color);
      const px = parseFloat(s.fontSize);
      const ls = parseFloat(s.letterSpacing);
      runs.push({
        t: text, sz: px, b: parseInt(s.fontWeight, 10) >= 600 || s.fontWeight === 'bold', i: /italic|oblique/.test(s.fontStyle),
        u: s.textDecorationLine.includes('underline'), s: s.textDecorationLine.includes('line-through'),
        c: col.c, a: col.a, f: family(s), spc: Number.isFinite(ls) ? ls : 0,
      });
      styled.push(el);
    };
    const walk = (node, parentEl) => {
      if (node.nodeType === 3) {
        const s = cs(parentEl);
        let text = node.data;
        if (preserves(s)) {
          text.split('\n').forEach((part, k) => {
            if (k > 0) { runs.push({ br: true }); spaceBefore = true; }
            if (part) { push(transform(part, s), parentEl); spaceBefore = false; }
          });
          return;
        }
        text = text.replace(/[ \t\r\n\f]+/g, ' ');
        if (spaceBefore && text.startsWith(' ')) text = text.slice(1);
        if (!text) return;
        spaceBefore = text.endsWith(' ');
        push(transform(text, s), parentEl);
      } else if (node.nodeType === 1) {
        if (node.tagName === 'BR') { runs.push({ br: true }); spaceBefore = true; return; }
        for (const c of node.childNodes) walk(c, node);
      }
    };
    for (const n of nodes) walk(n, n.nodeType === 1 ? owner : owner);
    while (runs.length && runs[runs.length - 1].br) runs.pop();
    const last = runs[runs.length - 1];
    if (last && last.t) last.t = last.t.replace(/ +$/, '');
    if (skip || !runs.some((r) => r.t && r.t.trim())) return;
    const os = cs(owner);
    if (hidden(owner) || rotated(owner) || clipped(owner)) return;
    const op = opacity(owner);
    if (op < 0.05) return;
    const range = document.createRange();
    range.setStartBefore(nodes[0]);
    range.setEndAfter(nodes[nodes.length - 1]);
    const rects = [...range.getClientRects()].filter((r) => r.width > 0.5 && r.height > 0.5);
    if (!rects.length) return;
    const left = Math.min(...rects.map((r) => r.left)), top = Math.min(...rects.map((r) => r.top));
    const right = Math.max(...rects.map((r) => r.right)), bottom = Math.max(...rects.map((r) => r.bottom));
    if (right < 0 || bottom < 0 || left > W || top > H) return;
    const firstTop = rects.reduce((m, r) => Math.min(m, r.top), Infinity);
    const first = rects.filter((r) => Math.abs(r.top - firstTop) < 2);
    const h1 = Math.max(...first.map((r) => r.height));
    const lines = new Set(rects.map((r) => Math.round(r.top / 3))).size;
    const fs = parseFloat(os.fontSize);
    const lh = os.lineHeight === 'normal' ? fs * 1.2 : parseFloat(os.lineHeight);
    const align = { left: 'l', start: 'l', center: 'ctr', right: 'r', end: 'r', justify: 'just' }[os.textAlign] || 'l';
    if (runs.some((r) => r.a === 0)) return;
    for (const r of runs) if (r.t !== undefined) r.a = Math.round(r.a * op * 1000) / 1000;
    blocks.push({ x: left, y: firstTop - (lh - h1) / 2, w: right - left, h: bottom - top, align, lh, wrap: lines > 1 || /pre|break/.test(os.whiteSpace) && runs.some((r) => r.br), runs });
    mine.push(...new Set(styled), owner);
  };
  const group = (parent) => {
    let cur = [];
    const flush = () => {
      if (cur.some((n) => n.nodeType === 1 || (n.nodeType === 3 && n.data.trim()))) make(cur, parent);
      cur = [];
    };
    for (const c of parent.childNodes) {
      if (c.nodeType === 3) cur.push(c);
      else if (c.nodeType === 1) {
        if (SKIP.has(c.tagName)) { flush(); continue; }
        if (c.tagName === 'BR' || isInline(c)) cur.push(c);
        else { flush(); group(c); }
      }
    }
    flush();
  };
  const roots = [document.body];
  for (const r of roots) group(r);
  const limited = blocks.length > 400 ? [] : blocks;
  if (limited.length) {
    for (const el of new Set(mine)) {
      for (const [k, v] of [['color', 'transparent'], ['-webkit-text-fill-color', 'transparent'], ['text-shadow', 'none'], ['-webkit-text-stroke-color', 'transparent'], ['text-decoration-color', 'transparent']]) {
        el.style.setProperty(k, v, 'important');
      }
    }
  }
  return JSON.stringify(limited);
})()`

// deckBlock is a block of text as deckMeasure writes it.
type deckBlock struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
	Align string  `json:"align"`
	LH    float64 `json:"lh"`
	Wrap  bool    `json:"wrap"`
	Runs  []struct {
		T  string  `json:"t"`
		Br bool    `json:"br"`
		Sz float64 `json:"sz"`
		B  bool    `json:"b"`
		I  bool    `json:"i"`
		U  bool    `json:"u"`
		S  bool    `json:"s"`
		C  string  `json:"c"`
		A  float64 `json:"a"`
		F  *string `json:"f"`
		Sp float64 `json:"spc"`
	} `json:"runs"`
}

func (b deckBlock) text() pptxText {
	t := pptxText{X: b.X, Y: b.Y, W: b.W, H: b.H, Align: b.Align, LineH: b.LH, Wrap: b.Wrap}
	for _, r := range b.Runs {
		run := pptxRun{Text: r.T, Br: r.Br, Size: r.Sz, Bold: r.B, Italic: r.I, Underline: r.U, Strike: r.S, Color: r.C, Alpha: r.A, Spacing: r.Sp}
		if r.F != nil {
			run.Font = *r.F
		}
		t.Runs = append(t.Runs, run)
	}
	return t
}

func runDeckPPTX(ctx context.Context, run DeckExportRun) error {
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
	deck := pptxDeck{Title: title, W: slides[0].W, H: slides[0].H, Lang: locale.Pick("en-US", "ru-RU")}
	for i, sl := range slides {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tab.open(sl, deckScale(sl.W)); err != nil {
			return fmt.Errorf("%s: %w", sl.Title, err)
		}
		v, err := tab.eval(deckMeasure)
		if err != nil {
			return fmt.Errorf("%s: %w", sl.Title, err)
		}
		raw, _ := v.(string)
		var blocks []deckBlock
		if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
			return fmt.Errorf("%s: %w", sl.Title, err)
		}
		pic, err := tab.png()
		if err != nil {
			return fmt.Errorf("%s: %w", sl.Title, err)
		}
		ps := pptxSlide{Title: sl.Title, Picture: pic, Notes: sl.Notes}
		// A slide of another size than the first is shown at the first's: its boxes are scaled to it.
		k := float64(deck.W) / float64(sl.W)
		for _, b := range blocks {
			t := b.text()
			if k != 1 {
				t.X, t.Y, t.W, t.H, t.LineH = t.X*k, t.Y*k, t.W*k, t.H*k, t.LineH*k
				for j := range t.Runs {
					t.Runs[j].Size *= k
					t.Runs[j].Spacing *= k
				}
			}
			ps.Texts = append(ps.Texts, t)
		}
		deck.Slides = append(deck.Slides, ps)
		run.Progress(0.9 * float64(i+1) / float64(len(slides)))
	}
	var buf bytes.Buffer
	if err := writePPTX(&buf, deck); err != nil {
		return err
	}
	return os.WriteFile(run.Out, buf.Bytes(), 0o666)
}
