package hubproto

import (
	"crypto/ed25519"
	"testing"
	"time"
)

// A peers signature is good for the peers only, and a dial's or a site's is not good for the peers.
func TestPeersSignatureDomain(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now()
	parse := func(h string) DialAuth {
		a, err := ParseDialHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	peers, _ := SignPeers(priv, "abc", now, "hub.example.com")
	if err := VerifyPeers(pub, parse(peers), "hub.example.com", now); err != nil {
		t.Fatal(err)
	}
	if VerifyDial(pub, parse(peers), "hub.example.com", now) == nil {
		t.Fatal("a peers signature opens the link")
	}
	if VerifyPeers(pub, parse(peers), "other.example.com", now) == nil {
		t.Fatal("a peers signature for another hub")
	}
	dial, _ := SignDial(priv, "abc", now, "hub.example.com")
	if VerifyPeers(pub, parse(dial), "hub.example.com", now) == nil {
		t.Fatal("a dial signature sends messages")
	}
}
