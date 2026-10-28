package design

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// PDF: one page per slide, the page the size of the slide. The boards are shown in one print page, each in an iframe
// of its own size, and the browser prints it. When that does not give a page per slide (a board that prints
// differently from how it shows), the slides are printed as pictures instead.

func init() {
	RegisterDeckExporter(DeckExportPDF, DeckExporter{Needs: []string{"chrome"}, Ext: "pdf", Run: runDeckPDF})
}

// pxToInch is the browser's CSS pixel in inches.
const pxToInch = 1.0 / 96.0

// deckPrintPage is the page that holds every slide as a frame, one print page each. The page is from the library's own
// server, so the script can wait for the frames' fonts.
func deckPrintPage(urls []string, slides []deckSlideSrc) string {
	var css, body strings.Builder
	css.WriteString("html,body{margin:0;padding:0;background:#fff}iframe{display:block;border:0;overflow:hidden;break-after:page;page-break-after:always}iframe:last-child{break-after:auto;page-break-after:auto}")
	for i, sl := range slides {
		fmt.Fprintf(&css, "@page s%d{size:%dpx %dpx;margin:0}.s%d{page:s%d;width:%dpx;height:%dpx}", i, sl.W, sl.H, i, i, sl.W, sl.H)
		fmt.Fprintf(&body, `<iframe class="s%d" src="%s"></iframe>`, i, html.EscapeString(urls[i]))
	}
	return "<!doctype html><meta charset=utf-8><title>deck</title><style>" + css.String() + "</style>" + body.String()
}

const deckFramesReady = `(async () => {
  const frames = [...document.querySelectorAll('iframe')];
  await Promise.all(frames.map((f) => new Promise((ok) => {
    const done = () => ok();
    if (f.contentDocument && f.contentDocument.readyState === 'complete') done(); else f.addEventListener('load', done, {once: true});
    setTimeout(done, 25000);
  })));
  await Promise.all(frames.map(async (f) => {
    const d = f.contentDocument;
    if (!d) return;
    d.querySelectorAll('[data-tk-library-bar]').forEach((n) => n.remove());
    if (d.fonts && d.fonts.ready) await d.fonts.ready;
  }));
  return frames.length;
})()`

// deckPDFPages counts the pages of a PDF: its page objects.
var deckPDFPage = regexp.MustCompile(`/Type\s*/Page[^s]`)

func deckPDFPages(pdf []byte) int { return len(deckPDFPage.FindAll(pdf, -1)) }

func printOptions(w, h int) map[string]any {
	return map[string]any{
		"paperWidth": float64(w) * pxToInch, "paperHeight": float64(h) * pxToInch,
		"marginTop": 0, "marginBottom": 0, "marginLeft": 0, "marginRight": 0,
		"printBackground": true, "preferCSSPageSize": true, "displayHeaderFooter": false,
	}
}

func printToPDF(tab *deckTab, w, h int) ([]byte, error) {
	r, err := tab.callLong("Page.printToPDF", printOptions(w, h))
	if err != nil {
		return nil, err
	}
	data, _ := r["data"].(string)
	if data == "" {
		return nil, errors.New("the browser gave no PDF")
	}
	return base64.StdEncoding.DecodeString(data)
}

// pdfByFrames prints the boards where they stand; nil, nil when the result is not a page per slide.
func pdfByFrames(ctx context.Context, sh *deckShooter, tab *deckTab, slides []deckSlideSrc, run DeckExportRun) ([]byte, error) {
	if err := tab.view(slides[0].W, slides[0].H, 1); err != nil {
		return nil, err
	}
	// Any page of the library's server is of its origin, which lets the script reach into the frames.
	if err := tab.navigate(sh.url(slides[0])); err != nil {
		return nil, err
	}
	urls := make([]string, len(slides))
	for i, sl := range slides {
		urls[i] = sh.url(sl)
	}
	doc := deckPrintPage(urls, slides)
	if _, err := tab.eval("document.open();document.write(" + jsString(doc) + ");document.close();true"); err != nil {
		return nil, err
	}
	run.Progress(0.1)
	if _, err := tab.eval(deckFramesReady); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run.Progress(0.4)
	sleepSettle()
	pdf, err := printToPDF(tab, slides[0].W, slides[0].H)
	if err != nil {
		return nil, err
	}
	if deckPDFPages(pdf) != len(slides) {
		return nil, nil
	}
	return pdf, nil
}

// pdfByPictures prints the slides as pictures, a page each, when the boards would not print as they show.
func pdfByPictures(ctx context.Context, tab *deckTab, slides []deckSlideSrc, run DeckExportRun) ([]byte, error) {
	var css, body strings.Builder
	css.WriteString("html,body{margin:0;padding:0}img{display:block;break-after:page;page-break-after:always}img:last-child{break-after:auto;page-break-after:auto}")
	for i, sl := range slides {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := tab.open(sl, deckScale(sl.W)); err != nil {
			return nil, fmt.Errorf("%s: %w", sl.Title, err)
		}
		pic, err := tab.png()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sl.Title, err)
		}
		fmt.Fprintf(&css, "@page s%d{size:%dpx %dpx;margin:0}.s%d{page:s%d;width:%dpx;height:%dpx}", i, sl.W, sl.H, i, i, sl.W, sl.H)
		fmt.Fprintf(&body, `<img class="s%d" src="data:image/png;base64,%s">`, i, base64.StdEncoding.EncodeToString(pic))
		run.Progress(0.1 + 0.6*float64(i+1)/float64(len(slides)))
	}
	if err := tab.view(slides[0].W, slides[0].H, 1); err != nil {
		return nil, err
	}
	doc := "<!doctype html><meta charset=utf-8><style>" + css.String() + "</style>" + body.String()
	if _, err := tab.eval("document.open();document.write(" + jsString(doc) + ");document.close();new Promise((ok)=>setTimeout(()=>ok(true),300))"); err != nil {
		return nil, err
	}
	pdf, err := printToPDF(tab, slides[0].W, slides[0].H)
	if err != nil {
		return nil, err
	}
	if n := deckPDFPages(pdf); n != len(slides) {
		return nil, errors.New(locale.Pick("The PDF came out with "+strconv.Itoa(n)+" pages for "+strconv.Itoa(len(slides))+" slides",
			"В PDF получилось страниц: "+strconv.Itoa(n)+", слайдов: "+strconv.Itoa(len(slides))))
	}
	return pdf, nil
}

func runDeckPDF(ctx context.Context, run DeckExportRun) error {
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
	pdf, err := pdfByFrames(ctx, sh, tab, slides, run)
	if err != nil {
		return err
	}
	if pdf == nil {
		if pdf, err = pdfByPictures(ctx, tab, slides, run); err != nil {
			return err
		}
	}
	return os.WriteFile(run.Out, pdf, 0o666)
}
