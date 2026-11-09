package buildinfo

import "testing"

func TestResolve(t *testing.T) {
	for _, c := range []struct{ linked, env, want string }{
		{"dev", "", "dev"},
		{"dev", "0.133.0", "0.133.0"},
		{"dev-1a2b3c4d", " 0.133.0\n", "0.133.0"},
		{"0.134.0", "0.133.0", "0.134.0"},
		{"dev", "0.133.0; rm -rf /", "dev"},
		{"dev", "\x1b[31m", "dev"},
	} {
		if got := resolve(c.linked, c.env); got != c.want {
			t.Errorf("resolve(%q, %q) = %q, want %q", c.linked, c.env, got, c.want)
		}
	}
}
