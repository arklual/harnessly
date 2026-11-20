package wire

import "testing"

func TestParseHubUpload(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, ok := ParseHubUpload("hub:" + sha + "?n=%D1%84%D0%B0%D0%B9%D0%BB.png&t=image%2Fpng")
	if !ok || h.Sha != sha || h.Name != "файл.png" || h.Mime != "image/png" {
		t.Fatalf("got %+v %v", h, ok)
	}
	bare, ok := ParseHubUpload("hub:" + sha)
	if !ok || bare.Name != "attachment-01234567" || bare.Mime != "" {
		t.Fatalf("bare %+v %v", bare, ok)
	}
	for _, bad := range []string{"", "up_ABC", "hub:", "hub:abc", "hub:" + sha[:63], "hub:" + sha + "0", "hub:" + sha + "?n=%zz"} {
		if _, ok := ParseHubUpload(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
	if h.LocalID() == bare.LocalID() || h.LocalID() != h.LocalID() || len(h.LocalID()) < 8 {
		t.Fatal("the local id names the file")
	}
}
