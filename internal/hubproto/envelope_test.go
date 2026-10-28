package hubproto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func machineKeys(t *testing.T) (priv, pub []byte) {
	t.Helper()
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k.Bytes(), k.PublicKey().Bytes()
}

func TestSealOpen(t *testing.T) {
	priv, pub := machineKeys(t)
	secret := []byte("sk-ant-api03-тест-" + strings.Repeat("x", 100))
	e, err := Seal(pub, machine, "key:claude", secret)
	if err != nil {
		t.Fatal(err)
	}
	if e.V != 1 {
		t.Fatalf("version %d", e.V)
	}
	got, err := Open(priv, machine, "key:claude", e)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("%q, %v", got, err)
	}
	if strings.Contains(e.Ct, string(secret[:12])) {
		t.Fatal("the plaintext is visible in the envelope")
	}
	// Two seals of one plaintext differ.
	e2, _ := Seal(pub, machine, "key:claude", secret)
	if e2.Ct == e.Ct || e2.Epk == e.Epk {
		t.Fatal("a seal repeated itself")
	}
}

func TestOpenRefusals(t *testing.T) {
	priv, pub := machineKeys(t)
	otherPriv, _ := machineKeys(t)
	e, _ := Seal(pub, machine, "env:GH_TOKEN", []byte("ghp_secret"))
	flip := func(s string) string {
		raw, _ := base64.StdEncoding.DecodeString(s)
		raw[len(raw)-1] ^= 1
		return base64.StdEncoding.EncodeToString(raw)
	}
	tests := []struct {
		name string
		priv []byte
		id   string
		purp string
		env  Envelope
	}{
		{"another machine", priv, "tsrqponmlkjihgfedcba", "env:GH_TOKEN", e},
		{"another purpose", priv, machine, "env:GL_TOKEN", e},
		{"a key purpose for an env envelope", priv, machine, "key:claude", e},
		{"another key", otherPriv, machine, "env:GH_TOKEN", e},
		{"version", priv, machine, "env:GH_TOKEN", Envelope{V: 2, Epk: e.Epk, Nonce: e.Nonce, Ct: e.Ct}},
		{"flipped tag", priv, machine, "env:GH_TOKEN", Envelope{V: 1, Epk: e.Epk, Nonce: e.Nonce, Ct: flip(e.Ct)}},
		{"flipped epk", priv, machine, "env:GH_TOKEN", Envelope{V: 1, Epk: flip(e.Epk), Nonce: e.Nonce, Ct: e.Ct}},
		{"short nonce", priv, machine, "env:GH_TOKEN", Envelope{V: 1, Epk: e.Epk, Nonce: "AAAA", Ct: e.Ct}},
		{"not base64", priv, machine, "env:GH_TOKEN", Envelope{V: 1, Epk: "!", Nonce: e.Nonce, Ct: e.Ct}},
		{"empty", priv, machine, "env:GH_TOKEN", Envelope{}},
		{"short private key", priv[:5], machine, "env:GH_TOKEN", e},
	}
	for _, tc := range tests {
		got, err := Open(tc.priv, tc.id, tc.purp, tc.env)
		if err != ErrEnvelope || got != nil {
			t.Errorf("%s: %q, %v", tc.name, got, err)
		}
	}
}

func TestSealLimits(t *testing.T) {
	priv, pub := machineKeys(t)
	big := bytes.Repeat([]byte("a"), MaxSealed)
	e, err := Seal(pub, machine, "env:BIG", big)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Open(priv, machine, "env:BIG", e); err != nil || !bytes.Equal(got, big) {
		t.Fatalf("the longest plaintext: %v", err)
	}
	if _, err := Seal(pub, machine, "env:BIG", append(big, 'a')); err == nil {
		t.Fatal("a plaintext over the limit was sealed")
	}
	if _, err := Seal([]byte("short"), machine, "env:A", []byte("x")); err == nil {
		t.Fatal("a bad machine key was accepted")
	}
	// A low-order public key as the machine's gives a zero shared secret: refused.
	if _, err := Seal(make([]byte, 32), machine, "env:A", []byte("x")); err == nil {
		t.Fatal("a zero public key was accepted")
	}
}

// vector is one line of testdata/envelope.json, which the web test reproduces with the same ephemeral key and nonce.
type vector struct {
	MachineID        string   `json:"machineId"`
	Purpose          string   `json:"purpose"`
	EncPrivate       string   `json:"encPrivate"`
	EncKey           string   `json:"encKey"`
	EphemeralPrivate string   `json:"ephemeralPrivate"`
	Nonce            string   `json:"nonce"`
	Plaintext        string   `json:"plaintext"`
	Envelope         Envelope `json:"envelope"`
}

// seeds are fixed byte patterns, never used for anything real.
func seeded(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func makeVectors(t *testing.T) []vector {
	t.Helper()
	std := base64.StdEncoding
	cases := []struct {
		id, purpose, plaintext string
		enc, eph, nonce        byte
	}{
		{"abcdefghijklmnopqrst", "key:claude", "sk-ant-api03-EXAMPLE-NOT-A-KEY", 0x11, 0x22, 0x33},
		{"abcdefghijklmnopqrst", "env:GH_TOKEN", "ghp_EXAMPLE_not_a_token_ключ", 0x44, 0x55, 0x66},
		{"tsrqponmlkjihgfedcba", "key:codex", "", 0x77, 0x88, 0x99},
	}
	var out []vector
	for _, c := range cases {
		encPriv, err := ecdh.X25519().NewPrivateKey(seeded(c.enc))
		if err != nil {
			t.Fatal(err)
		}
		eph, err := ecdh.X25519().NewPrivateKey(seeded(c.eph))
		if err != nil {
			t.Fatal(err)
		}
		nonce := bytes.Repeat([]byte{c.nonce}, gcmNonce)
		e, err := sealWith(eph, nonce, encPriv.PublicKey().Bytes(), c.id, c.purpose, []byte(c.plaintext))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, vector{MachineID: c.id, Purpose: c.purpose, EncPrivate: std.EncodeToString(encPriv.Bytes()),
			EncKey: std.EncodeToString(encPriv.PublicKey().Bytes()), EphemeralPrivate: std.EncodeToString(eph.Bytes()),
			Nonce: std.EncodeToString(nonce), Plaintext: c.plaintext, Envelope: e})
	}
	return out
}

const vectorsFile = "testdata/envelope.json"

// The vectors are regenerated with HUBPROTO_UPDATE_VECTORS=1; the web test (web/app/hub/lib/seal.test.ts) and this
// one read the same file, so the browser and the machine cannot drift apart.
func TestEnvelopeVectors(t *testing.T) {
	want := makeVectors(t)
	if os.Getenv("HUBPROTO_UPDATE_VECTORS") == "1" {
		raw, _ := json.MarshalIndent(want, "", "  ")
		if err := os.WriteFile(vectorsFile, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(vectorsFile)
	if err != nil {
		t.Fatal(err)
	}
	var have []vector
	if err := json.Unmarshal(raw, &have); err != nil {
		t.Fatal(err)
	}
	if len(have) != len(want) {
		t.Fatalf("%d vectors in the file, %d made", len(have), len(want))
	}
	std := base64.StdEncoding
	for i, v := range have {
		if v != want[i] {
			t.Errorf("vector %d differs from the one made: run with HUBPROTO_UPDATE_VECTORS=1 if the format changed on purpose", i)
		}
		priv, _ := std.DecodeString(v.EncPrivate)
		got, err := Open(priv, v.MachineID, v.Purpose, v.Envelope)
		if err != nil || string(got) != v.Plaintext {
			t.Errorf("vector %d does not open: %q, %v", i, got, err)
		}
		if _, err := Open(priv, v.MachineID, v.Purpose+"x", v.Envelope); err == nil {
			t.Errorf("vector %d opens under another purpose", i)
		}
	}
}
