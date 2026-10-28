package sites

import (
	"strings"
	"testing"
)

const h1 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func TestCleanPath(t *testing.T) {
	good := map[string]string{"index.html": "index.html", "a/b/c.js": "a/b/c.js", "./a.css": "a.css", ".well-known/x": ".well-known/x", "файл.html": "файл.html"}
	for in, want := range good {
		got, err := CleanPath(in)
		if err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "/etc/passwd", "../a", "a/../../b", "a//b", "a\\b", "a\x00b", "a/", ".", "..", ".git/config", "x/.git/HEAD", strings.Repeat("a", 600)} {
		if _, err := CleanPath(in); err == nil {
			t.Errorf("%q should be refused", in)
		}
	}
}

func TestManifestValidate(t *testing.T) {
	m := Manifest{"index.html": {Hash: h1, Size: 10}}
	if err := m.Validate(DefaultLimits); err != nil {
		t.Fatal(err)
	}
	if err := (Manifest{}).Validate(DefaultLimits); err == nil {
		t.Error("empty manifest")
	}
	if err := (Manifest{"a": {Hash: "xyz", Size: 1}}).Validate(DefaultLimits); err == nil {
		t.Error("bad hash")
	}
	if err := (Manifest{"a": {Hash: strings.ToUpper(h1), Size: 1}}).Validate(DefaultLimits); err == nil {
		t.Error("upper-case hash")
	}
	if err := (Manifest{"a": {Hash: h1, Size: DefaultLimits.MaxFileBytes + 1}}).Validate(DefaultLimits); err == nil {
		t.Error("big file")
	}
	if err := (Manifest{"a": {Hash: h1, Size: -1}}).Validate(DefaultLimits); err == nil {
		t.Error("negative size")
	}
	lim := Limits{MaxDeployBytes: 100, MaxFileBytes: 60, MaxFiles: 2}
	if err := (Manifest{"a": {Hash: h1, Size: 60}, "b": {Hash: h1, Size: 60}}).Validate(lim); err == nil {
		t.Error("deploy too big")
	}
	if err := (Manifest{"a": {Hash: h1, Size: 1}, "b": {Hash: h1, Size: 1}, "c": {Hash: h1, Size: 1}}).Validate(lim); err == nil {
		t.Error("too many files")
	}
	if err := (Manifest{"../a": {Hash: h1, Size: 1}}).Validate(DefaultLimits); err == nil {
		t.Error("bad path")
	}
}

func TestManifestHashesAndTotal(t *testing.T) {
	h2 := strings.Repeat("a", 64)
	m := Manifest{"a": {Hash: h1, Size: 1}, "b": {Hash: h2, Size: 2}, "c": {Hash: h2, Size: 2}}
	if got := m.Hashes(); len(got) != 2 || got[0] != h1 || got[1] != h2 {
		t.Errorf("hashes %v", got)
	}
	if m.TotalBytes() != 5 {
		t.Errorf("total %d", m.TotalBytes())
	}
}

func TestScanPaths(t *testing.T) {
	m := Manifest{
		"index.html": {Hash: h1}, ".env": {Hash: h1}, "sub/.env.production": {Hash: h1}, ".env.example": {Hash: h1},
		"certs/server.pem": {Hash: h1}, "id_rsa": {Hash: h1}, "key.p12": {Hash: h1}, "app.js": {Hash: h1}, "docs/environment.md": {Hash: h1},
	}
	paths := map[string]bool{}
	for _, f := range ScanPaths(m) {
		paths[f.Path] = true
	}
	for _, want := range []string{".env", "sub/.env.production", "certs/server.pem", "id_rsa", "key.p12"} {
		if !paths[want] {
			t.Errorf("%s not found in %v", want, paths)
		}
	}
	for _, no := range []string{"index.html", ".env.example", "app.js", "docs/environment.md"} {
		if paths[no] {
			t.Errorf("%s must pass", no)
		}
	}
}

func TestScanContent(t *testing.T) {
	// The samples are assembled so that this file does not itself look like a leak.
	cases := []struct {
		name, body string
		hit        bool
	}{
		{"private key", "x\n-----BEGIN " + "RSA PRIVATE KEY-----\nabc", true},
		{"openssh", "-----BEGIN " + "OPENSSH PRIVATE KEY-----", true},
		{"aws", "const k = '" + "AKIA" + "IOSFODNN7EXAMPLE'", true},
		{"github", "token=gh" + "p_" + strings.Repeat("a", 36), true},
		{"gitlab", "glp" + "at-" + strings.Repeat("x", 20), true},
		{"stripe", "sk_" + "live_" + strings.Repeat("1", 24), true},
		{"anthropic", "sk-" + "ant-api03-" + strings.Repeat("A", 30), true},
		{"slack", "xo" + "xb-123456789012-abcdefghij", true},
		{"telegram", "1234567890:" + "AA" + strings.Repeat("b", 33), true},
		{"public key is fine", "-----BEGIN PUBLIC KEY-----", false},
		{"plain html", "<html><body>hello AKIA short</body></html>", false},
		{"publishable key is fine", "pk_" + "live_" + strings.Repeat("1", 24), false},
	}
	for _, c := range cases {
		got := ScanContent([]byte(c.body))
		if (got != "") != c.hit {
			t.Errorf("%s: got %q, hit=%v", c.name, got, c.hit)
		}
	}
	bin := append([]byte{0, 1, 2, 3}, []byte("-----BEGIN "+"RSA PRIVATE KEY-----")...)
	if ScanContent(bin) != "" {
		t.Error("binary files are not scanned")
	}
}
