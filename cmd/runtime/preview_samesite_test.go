package main

import "testing"

func TestSameSite(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"https://p1.klodik.tech", "https://harnessly.klodik.tech", true},
		{"https://p1.example.com", "https://app.example.com", true},
		{"https://p1.preview.dev", "https://app.example.com", false},
		{"https://p1.foo.github.io", "https://app.bar.github.io", false}, // github.io is a public suffix
		{"http://127.0.0.1:8023", "http://127.0.0.1:8022", true},
		{"http://p1.example.com", "https://app.example.com", false},
		{"", "https://app.example.com", false},
	} {
		if got := sameSite(c.a, c.b); got != c.want {
			t.Errorf("sameSite(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
