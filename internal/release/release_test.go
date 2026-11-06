package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
)

const h64 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func sample() *Manifest {
	return &Manifest{
		Schema: Schema, Version: "1.4.0",
		Components: map[string]Component{
			Hub:     {Version: "1.4.0", Files: map[string]File{"hub-linux-amd64": {SHA256: h64, Size: 5}}},
			Runtime: {Version: "1.4.0", Files: map[string]File{"runtime-linux-amd64": {SHA256: h64, Size: 5}}, Requires: map[string]string{Hub: "1.3.0"}},
			CLI:     {Version: "1.4.0", Files: map[string]File{"harnessly-linux-amd64": {SHA256: h64, Size: 5}}},
			UI:      {Version: "1.4.0"},
		},
		ExtensionAPI: map[string]Range{"harnessly.machine-provider": {Min: 1, Max: 2}},
		Data:         Data{SchemaVersion: 14, Additive: true},
	}
}

func TestValidateAndParse(t *testing.T) {
	body, _ := json.Marshal(sample())
	if _, err := Parse(body); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Manifest){
		"schema":       func(m *Manifest) { m.Schema = 1 },
		"version":      func(m *Manifest) { m.Version = "dev" },
		"unknown part": func(m *Manifest) { m.Components["bogus"] = Component{Version: "1.0.0"} },
		"bad hash":     func(m *Manifest) { m.Components[Hub].Files["hub-linux-amd64"] = File{SHA256: "x", Size: 1} },
		"foreign file": func(m *Manifest) { m.Components[Hub].Files["agent-linux-amd64"] = File{SHA256: h64, Size: 1} },
		"unmet requires": func(m *Manifest) {
			c := m.Components[Runtime]
			c.Requires = map[string]string{Hub: "2.0.0"}
			m.Components[Runtime] = c
		},
		"missing part": func(m *Manifest) {
			c := m.Components[Runtime]
			c.Requires = map[string]string{Agent: "1.0.0"}
			m.Components[Runtime] = c
		},
		"bad range": func(m *Manifest) { m.ExtensionAPI["harnessly.machine-provider"] = Range{Min: 3, Max: 2} },
	} {
		m := sample()
		mut(m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Parse([]byte(`{"schema":2,"version":"1.0.0","surprise":1}`)); err == nil {
		t.Error("an unknown field was accepted")
	}
}

func TestSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	body := []byte(`{"a":1}`)
	sig, err := Sign(priv, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, body, sig); err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, []byte(`{"a":2}`), sig); err != ErrBadSignature {
		t.Errorf("a changed body: %v", err)
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := Verify(other, body, sig); err == nil || !strings.Contains(err.Error(), "pinned key") {
		t.Errorf("another key: %v", err)
	}
}

func TestCheckExtension(t *testing.T) {
	m := sample()
	for proto, want := range map[string]Verdict{
		"harnessly.machine-provider/v1": Compatible,
		"harnessly.machine-provider/v2": Compatible,
		"harnessly.machine-provider/v3": Incompatible,
		"harnessly.credentials/v1":      Unknown,
		"nonsense":                      Incompatible,
	} {
		if got := m.CheckExtension("x", proto).Verdict; got != want {
			t.Errorf("%s: %s, want %s", proto, got, want)
		}
	}
}

func TestVersionOrder(t *testing.T) {
	a, _ := ParseVersion("v1.9.0")
	b, _ := ParseVersion("1.10.0-rc1")
	if !a.Less(b) || b.Less(a) {
		t.Error("1.9.0 < 1.10.0")
	}
}
