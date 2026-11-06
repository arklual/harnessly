package agentbrowser

import "testing"

func TestProfileResolutionDefault(t *testing.T) {
	for _, tt := range []struct {
		name     string
		resolver func(string) string
		want     string
	}{
		{"unset", nil, DefaultProfile},
		{"empty", func(string) string { return "" }, DefaultProfile},
		{"bound", func(conv string) string { return "profile-" + conv }, "profile-chat"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Config{ProfileOf: tt.resolver})
			defer m.Close()
			if id := m.profileFor("chat"); id != tt.want {
				t.Fatalf("profile = %q, want %q", id, tt.want)
			}
		})
	}
}
