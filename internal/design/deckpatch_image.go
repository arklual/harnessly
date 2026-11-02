package design

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// A drawn picture put into an element of a board (the deck's and the canvas's alike): an img gets the picture as its
// src, any other element as a covering background. The element remembers what it was drawn from (data-gen-prompt)
// and which job drew it (data-gen-job, the name of the job's files), so the page can draw it again or show the others.

const (
	genPromptMax = 1000
	// genFromMax is how many earlier pictures of a changed one an element remembers.
	genFromMax = 12
	// GenAttrPrompt and GenAttrJob are the attributes an element drawn by a job carries.
	GenAttrPrompt = "data-gen-prompt"
	GenAttrJob    = "data-gen-job"
	// GenAttrFrom is the picture a changed one was made from (a path of the project).
	GenAttrFrom = "data-gen-from"
)

var (
	// genSrcRe is a picture the job's copy makes: assets/<job>-<n>.<ext>, one folder deep.
	genSrcRe = regexp.MustCompile(`^assets/(gen-[a-z0-9]{4,32})-[0-9]{1,2}\.(png|jpg|jpeg|webp)$`)
	genJobRe = regexp.MustCompile(`^gen-[a-z0-9]{4,32}$`)
	// pictureSrcRe is any picture of the project that can be put back (the original of a changed one): plain path
	// characters only, since it goes into a url().
	pictureSrcRe = regexp.MustCompile(`^[\p{L}\p{N}_./-]{1,300}\.(?i:png|jpg|jpeg|webp)$`)
)

// pictureSrcOK says whether a path is a picture of the project in a form that is safe to write into a board.
func pictureSrcOK(src string) bool {
	if !pictureSrcRe.MatchString(src) || strings.HasPrefix(src, "/") || strings.HasPrefix(src, ".") {
		return false
	}
	for _, part := range strings.Split(src, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// SlotAttr marks an empty place for a picture (with an optional SlotPromptAttr, the brief it can be drawn from).
const (
	SlotAttr       = "data-image-slot"
	SlotPromptAttr = "data-image-prompt"
)

// cleanPrompt is a brief fit for an attribute: one line, no template braces.
func cleanPrompt(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.NewReplacer("{{", "{ {", "}}", "} }").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > genPromptMax {
		s = string(r[:genPromptMax])
	}
	return s
}

// patchImage puts req.Src into e. The picture's own file must be the job's (req.Job names it).
func (b *dcBoard) patchImage(e *dcElem, req DeckPatchRequest) (string, error) {
	var attrs map[string]*string
	if req.Job == "" && req.Prompt == "" {
		// An original put back: any picture of the project, and the element forgets what it was drawn from.
		if !pictureSrcOK(req.Src) {
			return "", patchBad(locale.Pick("Unknown picture", "Неизвестная картинка"))
		}
		attrs = map[string]*string{GenAttrPrompt: nil, GenAttrJob: nil, GenAttrFrom: nil}
	} else {
		m := genSrcRe.FindStringSubmatch(req.Src)
		if m == nil || !genJobRe.MatchString(req.Job) || m[1] != req.Job {
			return "", patchBad(locale.Pick("Unknown picture", "Неизвестная картинка"))
		}
		prompt := cleanPrompt(req.Prompt)
		if prompt == "" {
			return "", patchBad(locale.Pick("No prompt", "Нет описания картинки"))
		}
		attrs = map[string]*string{GenAttrPrompt: &prompt, GenAttrJob: &req.Job, GenAttrFrom: nil}
		if req.From != "" {
			// The pictures this one was made from, the oldest first (what the editor shows as its versions).
			trail := strings.Fields(req.From)
			if len(trail) > genFromMax {
				trail = trail[len(trail)-genFromMax:]
			}
			for _, from := range trail {
				if !pictureSrcOK(from) {
					return "", patchBad(locale.Pick("Unknown picture", "Неизвестная картинка"))
				}
			}
			from := strings.Join(trail, " ")
			attrs[GenAttrFrom] = &from
		}
	}
	// The slot mark is spent once a picture is in.
	attrs[SlotAttr] = nil
	next := b.src
	tpl := e.tpl
	if e.name == "img" {
		src := req.Src
		attrs["src"] = &src
		attrs["srcset"] = nil
	} else {
		url := "url(" + req.Src + ")"
		cover, center, none := "cover", "center", "no-repeat"
		set := map[string]*string{"background-image": &url, "background-size": &cover, "background-position": &center, "background-repeat": &none}
		var err error
		if next, err = b.patchStyle(e, set); err != nil {
			return "", err
		}
	}
	nb, ok := parseDcBoard(next)
	if !ok {
		return "", patchChanged()
	}
	e2 := nb.elem(tpl)
	if e2 == nil || e2.name != e.name {
		return "", patchChanged()
	}
	return nb.patchAttrs(e2, attrs)
}

// patchAttrs sets (a value) or removes (nil) attributes of an element's start tag.
func (b *dcBoard) patchAttrs(e *dcElem, set map[string]*string) (string, error) {
	tok := b.tagTok(e)
	raw := b.src[b.abs(tok.start):b.abs(tok.end)]
	for name := range set {
		for _, a := range startTagAttrs(raw) {
			if a.name == name && a.hasValue && strings.Contains(raw[a.valStart:a.valEnd], "{{") {
				return "", patchDynamic()
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	// Sorted so the same edit writes the same bytes.
	sort.Strings(names)
	for _, name := range names {
		raw = withAttr(raw, name, set[name])
	}
	return b.src[:b.abs(tok.start)] + raw + b.src[b.abs(tok.end):], nil
}

// withAttr is a raw start tag with one attribute set to v or, for nil, taken off.
func withAttr(raw, name string, v *string) string {
	var found *deckAttr
	for _, a := range startTagAttrs(raw) {
		if a.name == name {
			a := a
			found = &a
			break
		}
	}
	switch {
	case found == nil && v == nil:
		return raw
	case found == nil:
		end := len(raw) - 1
		if strings.HasSuffix(raw, "/>") {
			end = len(raw) - 2
		}
		pre := strings.TrimRight(raw[:end], " \t\n\r\f")
		return pre + " " + name + `="` + attrEscape(*v) + `"` + raw[len(pre):]
	case v == nil:
		from := found.start
		for from > 0 && strings.ContainsRune(" \t\n\r\f", rune(raw[from-1])) {
			from--
		}
		return raw[:from] + raw[found.end:]
	default:
		return raw[:found.start] + name + `="` + attrEscape(*v) + `"` + raw[found.end:]
	}
}
