package main

import "testing"

func TestSameSiteOrigins(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://ai-1.preview.example.com", "https://ai.example.com", true},
		{"https://slot.preview.example.com", "https://hub.example.com", true},
		{"https://slot.preview.other.org", "https://ai.example.com", false},
		{"http://ai-1.preview.example.com", "https://ai.example.com", false},
		{"https://a.github.io", "https://b.github.io", false},
		{"", "https://ai.example.com", false},
	}
	for _, c := range cases {
		if got := sameSiteOrigins(c.a, c.b); got != c.want {
			t.Errorf("sameSiteOrigins(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
