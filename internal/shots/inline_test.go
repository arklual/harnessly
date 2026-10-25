package shots

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNamedFindsThePicturesAMessageWrites(t *testing.T) {
	text := "Готово.\n\nСкриншот: /tmp/claude-1001/-home-x/s1/scratchpad/home.png, ещё `/w/app/a b.png` и (/w/app/two.JPG).\n" +
		"Ссылка https://host/x.png, ./rel.png, ~/home.png, /w/a.png.bak, /w/c.pngx, /w/d.webp: и снова /tmp/claude-1001/-home-x/s1/scratchpad/home.png.\n" +
		"/home/coder/.harnessly/uploads/unknown/unknown/1792894270382-e2af4dde-image.png"
	got := Named(text)
	want := []string{
		"/tmp/claude-1001/-home-x/s1/scratchpad/home.png",
		"/w/app/two.JPG",
		"/w/d.webp",
		"/home/coder/.harnessly/uploads/unknown/unknown/1792894270382-e2af4dde-image.png",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestAllowedShowsOnlyPicturesInsideTheRoots(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	put := func(dir, name string, body []byte) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	link := func(target, name string) string {
		p := filepath.Join(root, name)
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	roots := []string{root}
	ok := put(root, "shots/home.png", png)
	f, err := OpenIn(ok, roots)
	if err != nil || f.Mime != "image/png" {
		t.Fatalf("inside: %+v %v", f, err)
	}
	f.Body.Close()
	// A link that stays inside is the file it points to.
	if f, err := OpenIn(link(ok, "inside.png"), roots); err != nil {
		t.Fatalf("link inside: %v", err)
	} else {
		f.Body.Close()
	}
	secret := put(outside, "secret.png", png)
	if err := os.Mkdir(filepath.Join(root, "dir.png"), 0o700); err != nil {
		t.Fatal(err)
	}
	refused := map[string]string{
		"outside the roots":     secret,
		"a link out":            link(secret, "escape.png"),
		"a folder linked out":   filepath.Join(link(outside, "out"), "secret.png"),
		"dot-dot out":           filepath.Join(root, "..", filepath.Base(outside), "secret.png"),
		"text named a picture":  put(root, "notes.png", []byte("password=hunter2\n")),
		"not named a picture":   put(root, "image.txt", png),
		"a link to a non-image": link(put(root, "key.pem", png), "key.png"),
		"missing":               filepath.Join(root, "gone.png"),
		"a folder":              filepath.Join(root, "dir.png"),
		"empty":                 put(root, "empty.png", nil),
		"relative":              "shots/home.png",
		"the root's parent":     filepath.Dir(root) + "/x.png",
	}
	for why, p := range refused {
		if _, err := OpenIn(p, roots); !errors.Is(err, ErrNotServed) {
			t.Fatalf("%s (%s): %v", why, p, err)
		}
	}
	if _, err := OpenIn(ok, []string{"", "relative", "/"}); !errors.Is(err, ErrNotServed) {
		t.Fatalf("no usable root: %v", err)
	}
}
