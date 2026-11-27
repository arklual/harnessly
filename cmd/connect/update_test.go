package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		tag, have string
		want      bool
	}{{"connect-v1.2.0", "1.1.9", true}, {"connect-v1.2.0", "1.2.0", false}, {"connect-v1.10.0", "1.9.3", true},
		{"connect-v1.2", "1.2.1", false}, {"connect-vX", "1.0.0", false}} {
		if got := newer(c.tag, c.have); got != c.want {
			t.Errorf("newer(%s, %s) = %v", c.tag, c.have, got)
		}
	}
}

func TestVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	key := base64.StdEncoding.EncodeToString(pub)
	sums := []byte("abc  harnessly-connect_linux_amd64.tar.gz\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)))
	if err := verify(sums, sig, key); err != nil {
		t.Fatal(err)
	}
	if verify(append(sums, 'x'), sig, key) == nil {
		t.Fatal("a changed checksums.txt passed")
	}
}
