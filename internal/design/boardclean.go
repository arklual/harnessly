package design

import (
	"bytes"
	"errors"
	"strings"

	"golang.org/x/net/html"
)

// CleanTemplateBoard is a template's board as it may land on someone else's project. The board is read the way a
// browser reads it (html.Parse: raw-text and foreign-content rules included), the tree is cut by an allowlist and
// written again, so what is checked is what the browser will see, not a token stream that reads differently.
//
//   - elements that run or redirect go with their content: script (except the board engine), base, meta http-equiv,
//     object, embed, applet, noscript, noembed, noframes, xmp, plaintext, math, and in svg: script, style, foreignObject,
//     animate, set, animateTransform, animateMotion;
//   - attributes: on*, srcdoc, and every address attribute (href, src, xlink:href, action, formaction, poster, data,
//     values, to, from, by, ...) whose value runs (javascript:, vbscript:, file:, data: other than a picture).
//
// The engine <script src="./support.js"> stays as exactly that node. base is always cut, so the relative address
// cannot be pointed elsewhere. The result is written again until it reads back as itself; a board that does not settle
// is refused.
func CleanTemplateBoard(src []byte) ([]byte, error) {
	cur := src
	for i := 0; i < 3; i++ {
		doc, err := html.Parse(bytes.NewReader(cur))
		if err != nil {
			return nil, err
		}
		cleanTree(doc)
		var out bytes.Buffer
		if err := html.Render(&out, doc); err != nil {
			return nil, err
		}
		if bytes.Equal(out.Bytes(), cur) {
			return cur, nil
		}
		cur = out.Bytes()
	}
	return nil, errors.New("the board does not settle after cleaning")
}

var (
	cutHTML = map[string]bool{"script": true, "base": true, "object": true, "embed": true, "applet": true, "noscript": true,
		"noembed": true, "noframes": true, "xmp": true, "plaintext": true}
	cutSVG = map[string]bool{"script": true, "style": true, "foreignobject": true, "animate": true, "set": true,
		"animatetransform": true, "animatemotion": true}
	addressAttrs = map[string]bool{"href": true, "src": true, "action": true, "formaction": true, "poster": true, "data": true,
		"values": true, "to": true, "from": true, "by": true, "ping": true, "background": true, "codebase": true, "cite": true,
		"manifest": true, "longdesc": true, "usemap": true, "profile": true, "srcset": true, "imagesrcset": true}
)

func cutElement(n *html.Node) bool {
	tag := strings.ToLower(n.Data)
	switch n.Namespace {
	case "":
		if tag == "meta" {
			for _, a := range n.Attr {
				if strings.EqualFold(a.Key, "http-equiv") {
					return true
				}
			}
		}
		if tag == "script" && engineNode(n) {
			return false
		}
		return cutHTML[tag]
	case "svg":
		return cutSVG[tag]
	}
	return true // math and any other foreign content
}

// engineNode: the board engine's own script, as the boards load it.
func engineNode(n *html.Node) bool {
	return len(n.Attr) == 1 && n.Attr[0].Namespace == "" && n.Attr[0].Key == "src" && n.Attr[0].Val == "./support.js"
}

func addressSafe(v string) bool {
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }) {
		if !safeURLValue(part) {
			return false
		}
	}
	return safeURLValue(v)
}

func cleanTree(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && cutElement(c) {
			n.RemoveChild(c)
		} else {
			if c.Type == html.ElementNode {
				if tag := strings.ToLower(c.Data); tag == "script" && c.Namespace == "" {
					for d := c.FirstChild; d != nil; {
						nd := d.NextSibling
						c.RemoveChild(d)
						d = nd
					}
				}
				kept := c.Attr[:0]
				for _, a := range c.Attr {
					k := strings.ToLower(a.Key)
					if strings.HasPrefix(k, "on") || k == "srcdoc" || (addressAttrs[k] && !addressSafe(a.Val)) {
						continue
					}
					kept = append(kept, a)
				}
				c.Attr = kept
			}
			cleanTree(c)
		}
		c = next
	}
}
