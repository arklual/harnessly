package hubproto

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

const machine = "abcdefghijklmnopqrst"

func TestDialRoundTrip(t *testing.T) {
	pub, priv := newKey(t)
	now := time.Unix(1_800_000_000, 0)
	header, err := SignDial(priv, machine, now, "Hub.Example.com")
	if err != nil {
		t.Fatal(err)
	}
	a, err := ParseDialHeader(header)
	if err != nil {
		t.Fatalf("%v: %q", err, header)
	}
	if a.MachineID != machine || a.T != now.Unix() {
		t.Fatalf("%+v", a)
	}
	if err := VerifyDial(pub, a, "hub.example.com", now); err != nil {
		t.Fatalf("own dial refused: %v", err)
	}
	// The host is compared lower-cased on both sides.
	if err := VerifyDial(pub, a, "HUB.example.com", now); err != nil {
		t.Fatalf("host case: %v", err)
	}
}

func TestDialRefusals(t *testing.T) {
	pub, priv := newKey(t)
	otherPub, _ := newKey(t)
	now := time.Unix(1_800_000_000, 0)
	header, _ := SignDial(priv, machine, now, "hub.example.com")
	good, err := ParseDialHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	tamper := func(f func(*DialAuth)) DialAuth { a := good; f(&a); return a }
	tests := []struct {
		name string
		pub  ed25519.PublicKey
		a    DialAuth
		host string
		at   time.Time
	}{
		{"another key", otherPub, good, "hub.example.com", now},
		{"another host", pub, good, "evil.example.com", now},
		{"another port", pub, good, "hub.example.com:8040", now},
		{"another machine", pub, tamper(func(a *DialAuth) { a.MachineID = "tsrqponmlkjihgfedcba" }), "hub.example.com", now},
		{"another time", pub, tamper(func(a *DialAuth) { a.T++ }), "hub.example.com", now},
		{"another nonce", pub, tamper(func(a *DialAuth) { a.Nonce = strings.Repeat("A", 22) }), "hub.example.com", now},
		{"too old", pub, good, "hub.example.com", now.Add(DialWindow + time.Second)},
		{"too new", pub, good, "hub.example.com", now.Add(-DialWindow - time.Second)},
		{"short key", pub[:5], good, "hub.example.com", now},
		{"bad signature text", pub, tamper(func(a *DialAuth) { a.Sig = "!!" }), "hub.example.com", now},
	}
	for _, tc := range tests {
		if err := VerifyDial(tc.pub, tc.a, tc.host, tc.at); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
	// The window is inclusive at its edge.
	if err := VerifyDial(pub, good, "hub.example.com", now.Add(DialWindow)); err != nil {
		t.Errorf("edge: %v", err)
	}
}

func TestParseDialHeader(t *testing.T) {
	_, priv := newKey(t)
	header, _ := SignDial(priv, machine, time.Now(), "hub")
	good := strings.TrimPrefix(header, DialScheme+" ")
	parts := strings.Split(good, ", ")
	if len(parts) != 4 {
		t.Fatalf("shape: %q", header)
	}
	tests := []struct {
		name, header string
		ok           bool
	}{
		{"good", header, true},
		{"no space after commas", DialScheme + " " + strings.Join(parts, ","), true},
		{"other order", DialScheme + " " + strings.Join([]string{parts[3], parts[2], parts[1], parts[0]}, ", "), true},
		{"other scheme", "Bearer " + good, false},
		{"no scheme", good, false},
		{"empty", "", false},
		{"missing part", DialScheme + " " + strings.Join(parts[:3], ", "), false},
		{"duplicate", DialScheme + " " + strings.Join([]string{parts[0], parts[0], parts[2], parts[3]}, ", "), false},
		{"unknown key", DialScheme + " " + good + ", x=1", false},
		{"bad time", DialScheme + " " + strings.Join([]string{parts[0], "t=soon", parts[2], parts[3]}, ", "), false},
		{"negative time", DialScheme + " " + strings.Join([]string{parts[0], "t=-5", parts[2], parts[3]}, ", "), false},
		{"short signature", DialScheme + " " + strings.Join([]string{parts[0], parts[1], parts[2], "s=AAAA"}, ", "), false},
		{"newline in id", DialScheme + " " + strings.Join([]string{"m=ab\ncd", parts[1], parts[2], parts[3]}, ", "), false},
	}
	for _, tc := range tests {
		if _, err := ParseDialHeader(tc.header); (err == nil) != tc.ok {
			t.Errorf("%s: err=%v", tc.name, err)
		}
	}
}

func TestDialHost(t *testing.T) {
	tests := map[string]string{
		"wss://Hub.Example.com/agent/v1":      "hub.example.com",
		"wss://hub.example.com:443/agent/v1":  "hub.example.com",
		"ws://hub.example.com:80/agent/v1":    "hub.example.com",
		"ws://127.0.0.1:18540/agent/v1":       "127.0.0.1:18540",
		"wss://hub.example.com:8443/agent/v1": "hub.example.com:8443",
		"ws://hub.example.com:443/agent/v1":   "hub.example.com:443",
		"https://[::1]:8040":                  "[::1]:8040",
	}
	for in, want := range tests {
		got, err := DialHost(in)
		if err != nil || got != want {
			t.Errorf("%s: %q, %v (want %q)", in, got, err, want)
		}
	}
	if _, err := DialHost("not a url"); err == nil {
		t.Error("a non-url was accepted")
	}
}

func TestDialMessageLines(t *testing.T) {
	got := string(DialMessage("m1", 42, "n1", "h"))
	if got != "harnessly-agent-dial-v1\nm1\n42\nn1\nh" {
		t.Fatalf("%q", got)
	}
}
