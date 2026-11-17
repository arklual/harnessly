package basepkg

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ubuntu is a fake Ubuntu machine whose chromium package is only a snap shim: it puts /usr/bin/chromium-browser in place,
// a script that runs the snap.
func ubuntu(arch string) (*fake, *Installer, *[]AptSource) {
	f := &fake{have: allBut("chrome"), free: 50 << 30}
	in := f.installer()
	in.Distro, in.Arch = "ubuntu", arch
	in.Snap = func(p string) bool { return p == "/usr/bin/chromium-browser" }
	added := &[]AptSource{}
	in.AddSource = func(_ context.Context, src AptSource, _ string) error {
		*added = append(*added, src)
		return nil
	}
	return f, in, added
}

func TestUbuntuGetsChromeFromGoogleNotTheSnap(t *testing.T) {
	f, in, added := ubuntu("amd64")
	f.have["chromium-browser"] = true // the shim of an earlier install
	f.answer = func(args []string) (string, error) {
		if args[len(args)-1] == "google-chrome-stable" {
			f.have["google-chrome"] = true
		}
		return "", nil
	}
	st := in.Install(context.Background(), Inbox{})
	if s := status(st, "chrome"); s.Status != OK {
		t.Fatalf("chrome %+v ran %v", s, f.ran)
	}
	if len(*added) != 1 || (*added)[0].Name != "google-chrome" {
		t.Fatalf("added %v", *added)
	}
	last := f.ran[len(f.ran)-1]
	if last[1] != "install" || last[len(last)-1] != "google-chrome-stable" || f.ran[len(f.ran)-2][1] != "update" {
		t.Fatalf("ran %v", f.ran)
	}
}

func TestUbuntuArmHasNoChromeAndSaysWhy(t *testing.T) {
	f, in, added := ubuntu("arm64")
	st := in.Install(context.Background(), Inbox{})
	s := status(st, "chrome")
	if s.Status != Missing || !strings.Contains(s.Message, "snap") || len(*added) != 0 {
		t.Fatalf("%+v added %v", s, *added)
	}
	for _, r := range f.ran {
		if r[1] == "install" {
			t.Fatalf("installed %v", r)
		}
	}
}

func TestSourceThatFailsIsNoAccess(t *testing.T) {
	f, in, _ := ubuntu("amd64")
	in.AddSource = func(context.Context, AptSource, string) error { return errors.New("dial tcp: i/o timeout") }
	st := in.Install(context.Background(), Inbox{})
	if s := status(st, "chrome"); s.Status != NoAccess || s.Host != "dl.google.com" {
		t.Fatalf("%+v", s)
	}
	for _, r := range f.ran {
		if r[1] == "install" {
			t.Fatalf("installed %v", r)
		}
	}
}

func TestDebianKeepsItsOwnChromium(t *testing.T) {
	f := &fake{have: allBut("chrome"), free: 50 << 30}
	in := f.installer()
	in.Distro, in.Arch = "debian", "amd64"
	in.AddSource = func(context.Context, AptSource, string) error { t.Fatal("no vendor on Debian"); return nil }
	if s := status(in.Install(context.Background(), Inbox{}), "chrome"); s.Status != OK {
		t.Fatalf("%+v", s)
	}
	if last := f.ran[len(f.ran)-1]; last[len(last)-1] != "chromium" {
		t.Fatalf("ran %v", f.ran)
	}
}

func TestViewDoesNotCountTheSnapShim(t *testing.T) {
	have := allBut("chrome")
	have["chromium-browser"] = true
	l := Look{LookPath: lookWith(have), Manager: Apt, Voice: Voice{Ready: true}}
	l.Snap = func(p string) bool { return strings.HasSuffix(p, "/chromium-browser") }
	for _, it := range MakeView(l).Items {
		if it.ID == "chrome" && it.Status == OK {
			t.Fatalf("snap shim counted: %+v", it)
		}
	}
}

func TestIsSnapReadsTheWrapper(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "chromium-browser")
	if err := os.WriteFile(shim, []byte("#! /bin/sh\nexec /snap/bin/chromium \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "chrome")
	if err := os.WriteFile(real, []byte("\x7fELF..."), 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsSnap(shim) || IsSnap(real) || IsSnap(filepath.Join(dir, "none")) {
		t.Fatal("IsSnap")
	}
}

func TestDearmor(t *testing.T) {
	key := []byte{0x99, 0x01, 0x0d, 0x04, 1, 2, 3, 4, 5}
	b64 := base64.StdEncoding.EncodeToString(key)
	armored := "-----BEGIN PGP PUBLIC KEY BLOCK-----\nVersion: GnuPG\n\n" + b64[:4] + "\n" + b64[4:] +
		"\n=abcd\n-----END PGP PUBLIC KEY BLOCK-----\n"
	got, err := Dearmor([]byte(armored + armored))
	if err != nil || string(got) != string(append(append([]byte{}, key...), key...)) {
		t.Fatalf("%v %x", err, got)
	}
	if _, err := Dearmor([]byte("<html>")); err == nil {
		t.Fatal("html is not a key")
	}
}

func TestSourcesIsWhatChromeWritesItself(t *testing.T) {
	want := "Types: deb\nURIs: https://dl.google.com/linux/chrome-stable/deb/\nSuites: stable\nComponents: main\n" +
		"Architectures: amd64\nSigned-By: /usr/share/keyrings/google-chrome.gpg\n"
	if got := googleChrome.Sources(); got != want {
		t.Fatalf("%q", got)
	}
}
