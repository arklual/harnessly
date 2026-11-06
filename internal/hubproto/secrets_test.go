package hubproto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestSecretsSignatureBindsTheBody(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	body := []byte(`{"op":"read","name":"gh-token"}`)
	auth, err := SignSecrets(priv, "m1", now, "hub.example.com", body)
	if err != nil {
		t.Fatal(err)
	}
	a, err := ParseDialHeader(auth)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySecrets(pub, a, "hub.example.com", now, body); err != nil {
		t.Fatalf("good signature: %v", err)
	}
	if VerifySecrets(pub, a, "hub.example.com", now, []byte(`{"op":"read","name":"other"}`)) == nil {
		t.Fatal("a signature held for another body")
	}
	if VerifySecrets(pub, a, "evil.example.com", now, body) == nil {
		t.Fatal("a signature held for another host")
	}
	dial, _ := SignDial(priv, "m1", now, "hub.example.com")
	d, _ := ParseDialHeader(dial)
	if VerifySecrets(pub, d, "hub.example.com", now, body) == nil {
		t.Fatal("a dial signature opened the safe")
	}
}

func TestSealSecretInPartsBoundToTheRequest(t *testing.T) {
	key, _ := ecdh.X25519().GenerateKey(rand.Reader)
	for _, size := range []int{0, 10, MaxSealed, MaxSealed + 1, 3*MaxSealed + 7} {
		plain := bytes.Repeat([]byte{'s'}, size)
		parts, err := SealSecret(key.PublicKey().Bytes(), "m1", "n1", plain)
		if err != nil {
			t.Fatalf("%d: %v", size, err)
		}
		got, err := OpenSecret(key.Bytes(), "m1", "n1", parts)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("%d: %v", size, err)
		}
		if _, err := OpenSecret(key.Bytes(), "m1", "n2", parts); err == nil {
			t.Fatalf("%d: opened for another request", size)
		}
		if _, err := OpenSecret(key.Bytes(), "m2", "n1", parts); err == nil {
			t.Fatalf("%d: opened for another machine", size)
		}
		if len(parts) > 1 {
			swapped := append([]Envelope{parts[1], parts[0]}, parts[2:]...)
			if _, err := OpenSecret(key.Bytes(), "m1", "n1", swapped); err == nil {
				t.Fatalf("%d: reordered parts opened", size)
			}
			if _, err := OpenSecret(key.Bytes(), "m1", "n1", parts[:len(parts)-1]); err == nil {
				t.Fatalf("%d: a cut answer opened", size)
			}
		}
	}
	if _, err := SealSecret(key.PublicKey().Bytes(), "m1", "n1", make([]byte, (maxSealedParts+1)*MaxSealed)); err == nil {
		t.Fatal("sealed more than the parts allow")
	}
	if _, err := OpenSecret(key.Bytes(), "m1", "n1", nil); err == nil {
		t.Fatal("opened nothing")
	}
}
