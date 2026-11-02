package apps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarWith(t *testing.T, hdr *tar.Header, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if body != "" {
		_, _ = tw.Write([]byte(body))
	}
	if err := errors.Join(tw.Close(), gz.Close()); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The archive is as strict as the platform's hub: ASCII names of at most 100 bytes, plain files, no PAX or GNU headers.
func TestPackRefusesNamesThatNeedAnExtendedHeader(t *testing.T) {
	long := strings.Repeat("d", 40) + "/" + strings.Repeat("e", 40) + "/" + strings.Repeat("f", 30) + ".txt"
	for name, file := range map[string]string{"too long": long, "not ASCII": "docs/справка.md"} {
		dir := versioned(t, "acme.demo", "1.0.0")
		full := filepath.Join(dir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Pack(dir); !errors.Is(err, ErrArchive) || !strings.Contains(err.Error(), file) && !strings.Contains(err.Error(), "ASCII") {
			t.Errorf("%s: pack: %v", name, err)
		}
		// Publish says so up front, with the path.
		if _, _, err := newRegistry(t).Publish(dir, ChannelDev, PublishOptions{}); !errors.Is(err, ErrArchive) {
			t.Errorf("%s: publish: %v", name, err)
		}
		pkg, ps := LoadPackage(dir)
		if got := strings.Join(publishChecks(pkg, ps), "\n"); !strings.Contains(got, "ASCII") && !strings.Contains(got, "at most 100") {
			t.Errorf("%s: public checks: %s", name, got)
		}
	}
	if msg := ArchiveNameProblem(long); !strings.Contains(msg, long) || !strings.Contains(msg, "at most 100") {
		t.Errorf("reason: %s", msg)
	}
	// 100 bytes of ASCII is the longest that travels.
	ok := strings.Repeat("a", 96) + ".txt"
	if ArchiveNameProblem(ok) != "" || ArchiveNameProblem(ok+"x") == "" {
		t.Error("the limit is 100 bytes")
	}
}

func TestUnpackRefusesExtendedHeadersAndOtherTypes(t *testing.T) {
	dst := func() string { return filepath.Join(t.TempDir(), "x") }
	// Go writes PAX for a long name and for a name beyond ASCII.
	for name, n := range map[string]string{"long": strings.Repeat("a", 120), "non-ASCII": "справка.md"} {
		raw := tarWith(t, &tar.Header{Name: n, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, "x")
		if err := Unpack(raw, dst()); !errors.Is(err, ErrArchive) {
			t.Errorf("%s: want refused, got %v", name, err)
		}
	}
	gnu := tarWith(t, &tar.Header{Name: "a.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg, Format: tar.FormatGNU}, "x")
	if err := Unpack(gnu, dst()); !errors.Is(err, ErrArchive) {
		t.Errorf("a GNU header: %v", err)
	}
	for name, h := range map[string]*tar.Header{
		"dir":      {Name: "d/", Mode: 0o755, Typeflag: tar.TypeDir},
		"hardlink": {Name: "h", Typeflag: tar.TypeLink, Linkname: "a"},
		"fifo":     {Name: "f", Typeflag: tar.TypeFifo},
		"char":     {Name: "c", Typeflag: tar.TypeChar},
	} {
		if err := Unpack(tarWith(t, h, ""), dst()); !errors.Is(err, ErrArchive) {
			t.Errorf("%s: want refused, got %v", name, err)
		}
	}
	// A plain USTAR file still unpacks.
	plain := tarWith(t, &tar.Header{Name: "ui/a.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}, "x")
	out := dst()
	if err := Unpack(plain, out); err != nil {
		t.Fatalf("plain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "ui", "a.txt")); err != nil {
		t.Fatal(err)
	}
}
