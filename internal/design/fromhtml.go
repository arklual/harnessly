package design

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// FromHTMLMax is how long a mockup of a chat's feed may be to become a board.
const FromHTMLMax = 200 << 10

// MockupKey names a mockup by its source: the same HTML of the same chat is the same board.
func MockupKey(conv, src string) string {
	sum := sha256.Sum256([]byte(conv + "\x00" + src))
	return hex.EncodeToString(sum[:16])
}

// mockupBase is the chat's frame's own base (web/app/next/features/chat/html-doc.ts): a mockup was drawn on it and
// names only the colours of its own boxes, so the board keeps it to look the same.
const mockupBase = ":where(html){color-scheme:light;background:#fff;color:#1a1a1f;font:14px/1.5 system-ui,sans-serif}:where(body){margin:12px}"

// cutMockup are the elements of a mockup that have no place on a board beyond what CleanTemplateBoard cuts: frames, and
// the board engine's own tags, which would make the person's page run as a board's logic.
var cutMockup = map[string]bool{"iframe": true, "frame": true, "frameset": true, "portal": true, "title": true}

// engineTags are the board engine's elements; in a mockup they are plain boxes.
var engineTags = map[string]bool{"x-dc": true, "helmet": true, "x-import": true, "dc-import": true, "sc-if": true, "sc-for": true}

// BoardFromHTML makes a board of an ```html answer of the chat, the way the chat showed it: the page's styles
// and links go to <helmet>, its body's content into <main class="board"> (the body's own class and style with it), on
// the chat frame's base. The HTML is not trusted: it is read as a browser reads it and cut as a template's board is
// (scripts, handlers, addresses that run, frames), the engine's tags become plain boxes, and {{ }} in its text is not a
// substitution. Its scripts did not run in the chat and do not run on the board; making it live is the agent's work.
func BoardFromHTML(src string) []byte {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		doc = &html.Node{Type: html.DocumentNode}
	}
	cleanTree(doc)
	cutMockupTree(doc)
	var head, body *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Namespace == "" {
			switch n.DataAtom {
			case atom.Head:
				if head == nil {
					head = n
				}
			case atom.Body:
				if body == nil {
					body = n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	var helmet bytes.Buffer
	helmet.WriteString("<style>\n" + mockupBase + "\n</style>\n")
	// What the head had that a board keeps: its styles and links; the rest (title, meta) is the frame's, not the page's.
	keep := func(n *html.Node) {
		if n.Type != html.ElementNode || n.Namespace != "" {
			return
		}
		if n.DataAtom == atom.Style || (n.DataAtom == atom.Link && mockupAttr(n, "href") != "") {
			_ = html.Render(&helmet, n)
			helmet.WriteByte('\n')
		}
	}
	if head != nil {
		for c := head.FirstChild; c != nil; c = c.NextSibling {
			keep(c)
		}
	}
	var main bytes.Buffer
	if body != nil {
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			// A style in the body is the page's too; it goes up, so that the board's markup is markup.
			if c.Type == html.ElementNode && c.Namespace == "" && (c.DataAtom == atom.Style || c.DataAtom == atom.Link) {
				keep(c)
				continue
			}
			_ = html.Render(&main, c)
		}
	}
	class, style := "board", ""
	if body != nil {
		if v := strings.TrimSpace(mockupAttr(body, "class")); v != "" {
			class += " " + v
		}
		style = strings.TrimSpace(mockupAttr(body, "style"))
	}
	var out bytes.Buffer
	out.WriteString("<!doctype html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<script src=\"./support.js\"></script>\n</head>\n<body>\n<x-dc>\n<helmet>\n")
	out.Write(helmet.Bytes())
	out.WriteString("</helmet>\n<main class=\"" + html.EscapeString(class) + "\"")
	if style != "" {
		out.WriteString(" style=\"" + html.EscapeString(style) + "\"")
	}
	out.WriteString(">\n")
	out.Write(main.Bytes())
	out.WriteString("\n</main>\n</x-dc>\n</body>\n</html>\n")
	return out.Bytes()
}

func mockupAttr(n *html.Node, key string) string {
	v, _ := attrOf(n, key)
	return v
}

// noSubstitution breaks every {{ of a mockup's text with a zero-width space: the board engine reads {{ }} as a path, the
// mockup meant the characters.
func noSubstitution(s string) string { return strings.ReplaceAll(s, "{{", "{\u200b{") }

func cutMockupTree(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch c.Type {
		case html.ElementNode:
			tag := strings.ToLower(c.Data)
			if c.Namespace == "" && cutMockup[tag] {
				n.RemoveChild(c)
				c = next
				continue
			}
			if engineTags[tag] {
				// A box keeps only its look: the engine's attributes (from, component-from-global-scope, list) go.
				c.Data, c.DataAtom, c.Namespace = "div", atom.Div, ""
				kept := c.Attr[:0]
				for _, a := range c.Attr {
					if k := strings.ToLower(a.Key); a.Namespace == "" && (k == "class" || k == "style") {
						kept = append(kept, a)
					}
				}
				c.Attr = kept
			}
			for i := range c.Attr {
				c.Attr[i].Val = noSubstitution(c.Attr[i].Val)
			}
			cutMockupTree(c)
		case html.TextNode:
			// Text of a style is CSS, where {{ never means anything to the engine (helmet is copied as written).
			if p := c.Parent; p == nil || p.Type != html.ElementNode || p.DataAtom != atom.Style {
				c.Data = noSubstitution(c.Data)
			}
		default:
			cutMockupTree(c)
		}
		c = next
	}
}
