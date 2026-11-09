package preview

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeDomain(t *testing.T) {
	for in, want := range map[string]string{
		"preview.example.com":          "preview.example.com",
		" *.Preview.Example.com. ":     "preview.example.com",
		"https://preview.example.com/": "preview.example.com",
		"pv-1.dev.example.org":         "pv-1.dev.example.org",
	} {
		got, err := NormalizeDomain(in)
		if err != nil || got != want {
			t.Errorf("NormalizeDomain(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "localhost", "a..b", "-x.example.com", "preview.example.com:443", "pv.exa_mple.com"} {
		if got, err := NormalizeDomain(bad); err == nil {
			t.Errorf("NormalizeDomain(%q) = %q, want an error", bad, got)
		}
	}
}

func TestDomainOrigins(t *testing.T) {
	got := DomainOrigins("preview.example.com", "ab12", 3)
	want := []string{"https://s1-ab12.preview.example.com", "https://s2-ab12.preview.example.com", "https://s3-ab12.preview.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestPreviewIDOrder(t *testing.T) {
	dir := t.TempDir()
	if id, err := PreviewID("Box-1", "m-9", dir); err != nil || id != "box-1" {
		t.Fatalf("named: %q %v", id, err)
	}
	if _, err := PreviewID("bad id", "", dir); err == nil {
		t.Fatal("an id with a space was taken")
	}
	if id, err := PreviewID("", "m-9", dir); err != nil || id != "m-9" {
		t.Fatalf("machine: %q %v", id, err)
	}
	if _, err := os.Stat(filepath.Join(dir, PreviewIDFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a named or machine id wrote a file")
	}
}

// The id the runtime makes is kept: the next start (a restart, an update, another service manager) names the same slots.
func TestPreviewIDSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := PreviewID("", "", dir)
	if err != nil || !ValidID(first) {
		t.Fatalf("first: %q %v", first, err)
	}
	second, err := PreviewID("", "", dir)
	if err != nil || second != first {
		t.Fatalf("second start: %q %v, first %q", second, err, first)
	}
	if !reflect.DeepEqual(DomainOrigins("preview.example.com", first, 8), DomainOrigins("preview.example.com", second, 8)) {
		t.Fatal("the origins changed")
	}
	if err := os.WriteFile(filepath.Join(dir, PreviewIDFile), []byte("NOT VALID\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewID("", "", dir); err == nil {
		t.Fatal("a broken id file was taken")
	}
}

func TestCheckOrigins(t *testing.T) {
	lookup := func(_ context.Context, host string) ([]string, error) {
		switch host {
		case "s1-a.preview.example.com":
			return []string{"203.0.113.5"}, nil
		case "s2-a.preview.example.com":
			return []string{"198.51.100.7"}, nil
		case "s3-a.preview.example.com":
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return nil, &net.DNSError{Err: "i/o timeout", Name: host, IsTimeout: true}
	}
	self := map[string]bool{"203.0.113.5": true}
	got := CheckOrigins(context.Background(), []string{
		"https://s1-a.preview.example.com", "https://s2-a.preview.example.com", "https://s3-a.preview.example.com",
		"https://s4-a.preview.example.com", "http://127.0.0.1:8023",
	}, lookup, self)
	states := make([]string, len(got))
	for i, c := range got {
		states[i] = c.State
	}
	want := []string{OriginOK, OriginElsewhere, OriginNotFound, OriginError, OriginLiteral}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("states %v, want %v", states, want)
	}
	if h := OriginsHint(got); !strings.Contains(h, "not in DNS") {
		t.Fatalf("hint %q", h)
	}
	if h := OriginsHint(got[:2]); !strings.Contains(h, "another address") {
		t.Fatalf("hint %q", h)
	}
	if h := OriginsHint(got[:1]); h != "" {
		t.Fatalf("hint %q", h)
	}
}
