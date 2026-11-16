package imap

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/charset"
	gomail "github.com/emersion/go-message/mail"

	"github.com/ly-solutions/harnessly/internal/connections"
)

const (
	// textMax runes of a letter's text go to the agent; the rest is cut with a word.
	textMax = 60000
	// partsMax parts of one letter are looked at: a letter of ten thousand parts is not walked to the end.
	partsMax = 500
	depthMax = 20
)

var words = &mime.WordDecoder{CharsetReader: charset.Reader}

// letter is a parsed letter: what the agent reads.
type letter struct {
	MessageID  string
	InReplyTo  []string
	References []string
	From       string
	To, Cc     string
	ReplyTo    string
	Subject    string
	Date       time.Time
	Text       string
	HTML       bool
	Parts      []part
	// Broken says the letter could not be read to its end (cut, a broken part); what was read is kept.
	Broken bool
}

type part struct {
	Index int
	Name  string
	Type  string
	Size  int64
}

// parseLetter reads a raw letter tolerantly: an unknown charset, a broken part or a cut end keep what was read. With
// want >= 1 it writes the body of attachment number want to w instead of collecting the text.
func parseLetter(raw io.Reader, want int, w io.Writer) (*letter, error) {
	e, err := message.ReadWithOptions(raw, &message.ReadOptions{MaxHeaderBytes: 1 << 20})
	if e == nil {
		return nil, fmt.Errorf("not a letter: %w", err)
	}
	l := &letter{}
	h := gomail.Header{Header: e.Header}
	l.MessageID = bareID(h.Get("Message-Id"))
	l.InReplyTo = msgIDs(h.Get("In-Reply-To"))
	l.References = msgIDs(h.Get("References"))
	l.From = decodeAddrs(h.Get("From"))
	l.To = decodeAddrs(h.Get("To"))
	l.Cc = decodeAddrs(h.Get("Cc"))
	l.ReplyTo = decodeAddrs(h.Get("Reply-To"))
	l.Subject = decodeWords(h.Get("Subject"))
	if d, err := h.Date(); err == nil {
		l.Date = d
	}
	p := &walker{l: l, want: want, w: w}
	p.walk(e, 0)
	if want == 0 {
		l.Text = strings.TrimSpace(p.plain.String())
		if l.Text == "" && p.html.Len() > 0 {
			l.Text, l.HTML = cleanHTML(p.html.String()), true
		}
		l.Text = cut(l.Text, textMax)
	}
	if want > 0 && !p.wrote {
		return l, errors.New("no such attachment")
	}
	return l, nil
}

type walker struct {
	l           *letter
	plain, html strings.Builder
	parts       int
	want        int
	w           io.Writer
	wrote       bool
}

func (p *walker) walk(e *message.Entity, depth int) {
	if p.parts >= partsMax || depth > depthMax {
		p.l.Broken = true
		return
	}
	p.parts++
	if mr := e.MultipartReader(); mr != nil {
		for {
			child, err := mr.NextPart()
			if err == io.EOF {
				return
			}
			if child == nil {
				// a broken boundary or a cut letter: keep what was read
				p.l.Broken = true
				return
			}
			p.walk(child, depth+1)
		}
	}
	ctype, params, _ := e.Header.ContentType()
	if ctype == "" {
		ctype = "text/plain"
	}
	disp, dparams, _ := e.Header.ContentDisposition()
	name := decodeWords(dparams["filename"])
	if name == "" {
		name = decodeWords(params["name"])
	}
	isText := ctype == "text/plain" || ctype == "text/html"
	if disp != "attachment" && name == "" && isText {
		if p.want > 0 {
			return
		}
		b, err := io.ReadAll(io.LimitReader(e.Body, 4*textMax))
		if err != nil {
			p.l.Broken = true
		}
		s := validUTF8(b)
		if ctype == "text/plain" {
			if p.plain.Len() > 0 {
				p.plain.WriteString("\n\n")
			}
			p.plain.WriteString(s)
		} else if p.html.Len() < 4*textMax {
			p.html.WriteString(s)
		}
		return
	}
	if disp == "inline" && name == "" && strings.HasPrefix(ctype, "multipart/") {
		return
	}
	if name == "" {
		name = "part" + fmt.Sprint(len(p.l.Parts)+1) + extOf(ctype)
	}
	pt := part{Index: len(p.l.Parts) + 1, Name: safeName(name), Type: ctype}
	if p.want == pt.Index && p.w != nil {
		n, err := io.Copy(p.w, e.Body)
		pt.Size = n
		p.wrote = err == nil || errors.Is(err, errTooBig)
		if err != nil {
			p.l.Broken = true
		}
	} else {
		n, err := io.Copy(io.Discard, e.Body)
		pt.Size = n
		if err != nil {
			p.l.Broken = true
		}
	}
	p.l.Parts = append(p.l.Parts, pt)
}

var errTooBig = errors.New("the attachment is larger than the limit")

func extOf(ctype string) string {
	if ctype == "message/rfc822" {
		return ".eml"
	}
	if ext, _ := mime.ExtensionsByType(ctype); len(ext) > 0 {
		return ext[0]
	}
	return ".bin"
}

// safeName is a file name without a directory, control characters or a leading dot.
func safeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if name == "" {
		name = "attachment"
	}
	if utf8.RuneCountInString(name) > 120 {
		ext := filepath.Ext(name)
		r := []rune(strings.TrimSuffix(name, ext))
		name = string(r[:100]) + ext
	}
	return name
}

func validUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return string(bytes.ToValidUTF8(b, []byte("�")))
}

func decodeWords(s string) string {
	if s == "" {
		return ""
	}
	if d, err := words.DecodeHeader(s); err == nil {
		s = d
	}
	return strings.Join(strings.Fields(validUTF8([]byte(s))), " ")
}

func decodeAddrs(s string) string {
	if s == "" {
		return ""
	}
	p := mail.AddressParser{WordDecoder: words}
	list, err := p.ParseList(s)
	if err != nil {
		return decodeWords(s)
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.String())
	}
	return decodeWords(strings.Join(out, ", "))
}

// addrsOf are the bare addresses of a header value, lower-cased.
func addrsOf(s string) []string {
	p := mail.AddressParser{WordDecoder: words}
	list, err := p.ParseList(s)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, strings.ToLower(a.Address))
	}
	return out
}

var idRe = regexp.MustCompile(`<([^<>\s]+)>`)

// msgIDs are the ids of In-Reply-To or References, bare (without < >).
func msgIDs(s string) []string {
	var out []string
	for _, m := range idRe.FindAllStringSubmatch(s, 50) {
		out = append(out, m[1])
	}
	return out
}

// bareID is a Message-ID without its angle brackets and spaces: the form events, reads and actions use.
func bareID(s string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "<>"))
}

var (
	blockRe = regexp.MustCompile(`(?is)<(script|style|head)[^>]*>.*?</(script|style|head)>`)
	breakRe = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/li|/h[1-6])[^>]*>`)
)

// cleanHTML is a html letter's text: no scripts, styles or tags, lines where the html broke them.
func cleanHTML(s string) string {
	s = blockRe.ReplaceAllString(s, " ")
	s = breakRe.ReplaceAllString(s, "\n")
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, ln := range lines {
		ln = strings.Join(strings.Fields(connections.HTMLText(ln)), " ")
		if ln != "" {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "\n… (the text is cut here)"
}
