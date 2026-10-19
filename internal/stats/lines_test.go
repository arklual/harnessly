package stats

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
	"testing/iotest"
)

// The line reader splits as Node's readline does whatever the read sizes: \n, \r\n and a lone \r end a line.
func TestLineReaderSplitsLikeReadline(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	alphabet := []byte("ab\r\n\n\r{}\xff")
	for n := 0; n < 3000; n++ {
		data := make([]byte, rng.Intn(40))
		for i := range data {
			data[i] = alphabet[rng.Intn(len(alphabet))]
		}
		want := splitLines(data)
		var r io.Reader = bytes.NewReader(data)
		if n%2 == 0 {
			r = iotest.OneByteReader(r)
		}
		lr := newLineReaderSize(r, 1+rng.Intn(4))
		var got [][]byte
		for {
			line, ok := lr.next()
			if !ok {
				break
			}
			got = append(got, append([]byte(nil), line...))
		}
		if len(got) != len(want) {
			t.Fatalf("%q: %d lines, want %d (%q vs %q)", data, len(got), len(want), got, want)
		}
		for i := range want {
			if !bytes.Equal(got[i], decodeText(want[i])) {
				t.Fatalf("%q: line %d %q, want %q", data, i, got[i], want[i])
			}
		}
	}
}

func TestSplitLines(t *testing.T) {
	got := splitLines([]byte("a\r\nb\rc\n\nd"))
	want := []string{"a", "b", "c", "", "d"}
	if len(got) != len(want) {
		t.Fatalf("%q", got)
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Fatalf("%q", got)
		}
	}
}
