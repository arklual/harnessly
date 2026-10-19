package jscompat

import (
	"strings"
	"unicode/utf8"
)

// DecodeUTF8 is Buffer#toString('utf8') of bytes: each malformed sequence becomes one U+FFFD, as the WHATWG decoder
// does (Go's own conversion replaces byte by byte), and a byte order mark stays.
func DecodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var d UTF8Decoder
	return d.Decode(b, false)
}

// UTF8Decoder is a TextDecoder('utf-8'): Decode with stream keeps an unfinished sequence for the next call, without it
// an unfinished sequence ends as U+FFFD. StripBOM drops a byte order mark at the start of the stream, as TextDecoder
// does by default.
type UTF8Decoder struct {
	StripBOM bool

	started          bool
	cp, needed, seen int
	lower, upper     byte
}

func (d *UTF8Decoder) reset() {
	d.cp, d.needed, d.seen, d.lower, d.upper = 0, 0, 0, 0x80, 0xbf
}

// Decode turns the next bytes into text.
func (d *UTF8Decoder) Decode(b []byte, stream bool) string {
	if d.lower == 0 {
		d.lower, d.upper = 0x80, 0xbf
	}
	var out strings.Builder
	out.Grow(len(b))
	emit := func(r rune) {
		if !d.started {
			d.started = true
			if d.StripBOM && r == 0xfeff {
				return
			}
		}
		out.WriteRune(r)
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		if d.needed == 0 {
			switch {
			case c < 0x80:
				emit(rune(c))
			case c >= 0xc2 && c <= 0xdf:
				d.needed, d.cp = 1, int(c&0x1f)
			case c >= 0xe0 && c <= 0xef:
				if c == 0xe0 {
					d.lower = 0xa0
				}
				if c == 0xed {
					d.upper = 0x9f
				}
				d.needed, d.cp = 2, int(c&0xf)
			case c >= 0xf0 && c <= 0xf4:
				if c == 0xf0 {
					d.lower = 0x90
				}
				if c == 0xf4 {
					d.upper = 0x8f
				}
				d.needed, d.cp = 3, int(c&0x7)
			default:
				emit(utf8.RuneError)
			}
			continue
		}
		if c < d.lower || c > d.upper {
			d.reset()
			emit(utf8.RuneError)
			i-- // the byte starts over
			continue
		}
		d.lower, d.upper = 0x80, 0xbf
		d.cp = d.cp<<6 | int(c&0x3f)
		d.seen++
		if d.seen == d.needed {
			r := rune(d.cp)
			d.reset()
			emit(r)
		}
	}
	if !stream && d.needed != 0 {
		d.reset()
		emit(utf8.RuneError)
	}
	return out.String()
}
