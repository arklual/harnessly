package timeline

import (
	"bytes"
	"os"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The live-reply reader and the first screen's summary against the previous runtime's own answers
// (testdata/pieces.json, written by testdata/gen/pieces.mjs). TIMELINE_PIECES names a larger generated set to check
// once.
func TestPiecesGolden(t *testing.T) {
	files := []string{"testdata/pieces.json"}
	if extra := os.Getenv("TIMELINE_PIECES"); extra != "" {
		files = append(files, extra)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsvalue.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		failed := 0
		for i, v := range doc.([]any) {
			c := v.(*jsvalue.Object)
			switch c.String("kind") {
			case "live":
				if got := readLiveReply(c.String("screen")); got != c.String("text") {
					failed++
					if failed < 5 {
						t.Errorf("%s case %d: live reply\n got %q\nwant %q", f, i, got, c.String("text"))
					}
				}
			case "meta":
				var entries []*jsvalue.Object
				for _, e := range c.Array("entries") {
					entries = append(entries, e.(*jsvalue.Object))
				}
				size, _ := c.Number("fileSize")
				var created, title *string
				if v, _ := c.Get("createdAt"); v != nil {
					s := v.(string)
					created = &s
				}
				if v, _ := c.Get("customTitle"); v != nil {
					s := v.(string)
					title = &s
				}
				meta, err := computeInitMeta(entries, int64(size), created, title)
				var got []byte
				if err != nil {
					got = jsvalue.Stringify(jsvalue.NewObject("error", "RangeError"), "")
				} else {
					got = jsvalue.Stringify(meta, "")
				}
				want, _ := c.Get("meta")
				if !bytes.Equal(got, jsvalue.Stringify(want, "")) {
					failed++
					if failed < 5 {
						t.Errorf("%s case %d: meta\n got %s\nwant %s", f, i, got, jsvalue.Stringify(want, ""))
					}
				}
			}
		}
		t.Logf("%s: %d cases, %d differ", f, len(doc.([]any)), failed)
	}
}

func TestDecodeUTF8(t *testing.T) {
	cases := map[string]string{
		"plain":            "plain",
		"\xe2\x82":         "�",
		"a\xffb":           "a�b",
		"\xf0\x9f\x98":     "�",
		"\xed\xa0\x80":     "���",
		"\xe0\x80\xaf":     "���",
		"\xf4\x90\x80\x80": "����",
		"\xc3":             "�",
		"ok\xe2\x82\xac":   "ok€",
	}
	for in, want := range cases {
		if got := decodeUTF8([]byte(in)); got != want {
			t.Errorf("decodeUTF8(%q) = %q, want %q", in, got, want)
		}
	}
}
