package jscompat

import "testing"

// The expected strings are what node 22 printed for Buffer#toString and TextDecoder#decode of the same bytes.
func TestDecodeUTF8(t *testing.T) {
	cases := []struct {
		in           []byte
		buffer, text string
	}{
		{[]byte{0xe2, 0x82, 0x41}, "\ufffdA", "\ufffdA"},
		{[]byte{0xf0, 0x9f, 0x98}, "\ufffd", "\ufffd"},
		{[]byte{0xed, 0xa0, 0x80}, "\ufffd\ufffd\ufffd", "\ufffd\ufffd\ufffd"},
		{[]byte{0xc0, 0xaf}, "\ufffd\ufffd", "\ufffd\ufffd"},
		{[]byte{0xef, 0xbb, 0xbf, 0x41}, "\ufeffA", "A"},
		{[]byte{0xff, 0xe2, 0x82, 0xac, 0x80}, "\ufffd€\ufffd", "\ufffd€\ufffd"},
		{[]byte{0xf4, 0x90, 0x80, 0x80}, "\ufffd\ufffd\ufffd\ufffd", "\ufffd\ufffd\ufffd\ufffd"},
		{[]byte{0xe0, 0x80, 0x80}, "\ufffd\ufffd\ufffd", "\ufffd\ufffd\ufffd"},
	}
	for _, c := range cases {
		if got := DecodeUTF8(c.in); got != c.buffer {
			t.Errorf("DecodeUTF8(% x) = %q, want %q", c.in, got, c.buffer)
		}
		d := UTF8Decoder{StripBOM: true}
		if got := d.Decode(c.in, false); got != c.text {
			t.Errorf("Decode(% x) = %q, want %q", c.in, got, c.text)
		}
	}
	// A sequence cut between two reads is kept for the next one; a BOM only counts at the very start.
	d := UTF8Decoder{StripBOM: true}
	if got := d.Decode([]byte{0xef, 0xbb, 0xbf, 'a', 0xe2, 0x82}, true) + d.Decode([]byte{0xac, 0xef, 0xbb, 0xbf}, true); got != "a€\ufeff" {
		t.Errorf("streamed: %q", got)
	}
}
