package proxy

import "testing"

// The looks sit beside the file store's rooms/, never inside what its collection lists.
func TestStoreLooksPrefix(t *testing.T) {
	for in, want := range map[string]string{"design-blobs": "design-blobs/looks/", "/a/b/": "a/b/looks/", "": "looks/"} {
		if got := storeLooksPrefix(in); got != want {
			t.Errorf("storeLooksPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
