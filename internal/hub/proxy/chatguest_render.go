package proxy

import (
	"bufio"
	"html"
	"io"

	xhtml "golang.org/x/net/html"
)

// writeGuestTranscript projects the redacted export into a fixed, inert vocabulary.
// No source attributes, URLs, style sheets or embedded documents are copied.
func writeGuestTranscript(w io.Writer, source io.Reader) {
	out := bufio.NewWriter(w)
	defer out.Flush()
	_, _ = io.WriteString(out, `<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Chat</title><style>body{max-width:76ch;margin:2rem auto;padding:0 1rem;font:1rem/1.55 system-ui,sans-serif;overflow-wrap:anywhere}pre{white-space:pre-wrap}article{border-block-start:1px solid #aaa;padding-block:1rem}</style><body>`)
	z := xhtml.NewTokenizer(io.LimitReader(source, maxChatShare))
	var muted string
	for {
		typ := z.Next()
		if typ == xhtml.ErrorToken {
			break
		}
		if typ == xhtml.TextToken {
			if muted == "" {
				_, _ = io.WriteString(out, html.EscapeString(string(z.Text())))
			}
			continue
		}
		if typ != xhtml.StartTagToken && typ != xhtml.EndTagToken && typ != xhtml.SelfClosingTagToken {
			continue
		}
		raw, _ := z.TagName()
		tag := string(raw)
		if muted != "" {
			if typ == xhtml.EndTagToken && tag == muted {
				muted = ""
			}
			continue
		}
		switch tag {
		case "head", "title", "script", "style", "template", "iframe", "svg", "math", "object", "noscript":
			if typ == xhtml.StartTagToken {
				muted = tag
			}
		case "p", "pre", "code", "blockquote", "br", "ul", "ol", "li", "h1", "h2", "h3", "h4", "article", "section", "header", "footer", "div", "strong", "em", "details", "summary", "table", "tr", "th", "td", "hr":
			if typ == xhtml.EndTagToken {
				_, _ = io.WriteString(out, "</"+tag+">")
			} else {
				_, _ = io.WriteString(out, "<"+tag+">")
			}
		}
	}
	_, _ = io.WriteString(out, "</body></html>")
}
