package shots

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenServesOnlyPictures(t *testing.T) {
	dir := t.TempDir()
	put := func(name string, body []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	f, err := Open(put("a.png", png))
	if err != nil || f.Mime != "image/png" {
		t.Fatalf("png: %+v %v", f, err)
	}
	f.Body.Close()
	refused := map[string]string{
		"text named like a picture": put("b.png", []byte("secret: hunter2\n")),
		"svg":                       put("c.svg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")),
		"empty":                     put("d.png", nil),
		"missing":                   filepath.Join(dir, "none.png"),
		"a folder":                  filepath.Join(dir, "dir.png"),
	}
	if err := os.Mkdir(refused["a folder"], 0o700); err != nil {
		t.Fatal(err)
	}
	for why, p := range refused {
		if _, err := Open(p); !errors.Is(err, ErrNotServed) {
			t.Fatalf("%s: %v", why, err)
		}
	}
	big := put("e.png", append(png, make([]byte, MaxBytes)...))
	if _, err := Open(big); !errors.Is(err, ErrNotServed) {
		t.Fatalf("too big: %v", err)
	}
}
