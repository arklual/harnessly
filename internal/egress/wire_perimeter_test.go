package egress

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func pubKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub)
}

func TestWireProblemsNameTheFieldAndSayWhatToDo(t *testing.T) {
	caPEM, _, _ := selfSigned(t, "ca")
	withKeyBlock := string(caPEM) + "-----BEGIN " + "PRIVATE KEY-----\nxx\n-----END " + "PRIVATE KEY-----"
	cases := []struct {
		name  string
		w     Wire
		field string
		text  string
	}{
		{"url as host", Wire{InternalHosts: []string{"https://crm.bank.local/x"}}, "internalHosts", "без http://"},
		{"space in host", Wire{InternalHosts: []string{"crm bank"}}, "internalHosts", "не имя хоста"},
		{"bad pem", Wire{CAPem: "nope"}, "caPem", "PEM"},
		{"key in roots", Wire{CAPem: withKeyBlock}, "caPem", "закрытый ключ"},
		{"proxy scheme", Wire{Proxy: "ftp://x"}, "proxy", "http://хост:порт"},
		{"no proxy junk", Wire{NoProxy: []string{"a b"}}, "noProxy", "10.0.0.0/8"},
		{"publisher bad key", Wire{Publishers: []Publisher{{Name: "Банк", Key: "abc"}}}, "publishers", "ed25519"},
		{"publisher no name", Wire{Publishers: []Publisher{{Key: pubKey(t)}}}, "publishers", "нет названия"},
		{"require with nobody", Wire{RequireSigned: true}, "requireSigned", "добавьте издателя"},
	}
	for _, c := range cases {
		ps := c.w.Problems()
		if len(ps) == 0 || ps[0].Field != c.field || !strings.Contains(ps[0].Message, c.text) {
			t.Errorf("%s: %+v", c.name, ps)
		}
		if err := c.w.Validate(); err == nil || !strings.HasPrefix(err.Error(), c.field+":") {
			t.Errorf("%s: Validate %v", c.name, err)
		}
	}
	k := pubKey(t)
	ok := Wire{InternalHosts: []string{"crm.bank.local", "*.bank.local", "10.1.2.3"}, CAPem: string(caPEM), Proxy: "proxy.bank.local:3128",
		NoProxy:    []string{"*", ".bank.local", "*.lan", "10.0.0.0/8", "127.0.0.1", "localhost"},
		Publishers: []Publisher{{Name: "Банк", Key: k}}, RequireSigned: true}
	if ps := ok.Problems(); len(ps) != 0 {
		t.Fatalf("a good policy has no problems: %+v", ps)
	}
	p, err := ok.Policy()
	if err != nil || !p.RequireSigned || len(p.Publishers) != 1 || p.Roots == nil {
		t.Fatalf("%+v %v", p, err)
	}
	dup := Wire{Publishers: []Publisher{{Name: "A", Key: k}, {Name: "B", Key: k}}}
	if ps := dup.Problems(); len(ps) != 1 || !strings.Contains(ps[0].Message, "уже в списке") {
		t.Errorf("a key twice: %+v", ps)
	}
}

func TestWithClientCertLeavesTheProcessPolicyAlone(t *testing.T) {
	_, ca, caKey := selfSigned(t, "ca")
	own := issue(t, ca, caKey, "x", true)
	base := Policy{Internal: []string{"a.corp"}}
	got := base.WithClientCert(&own)
	if got.ClientCert == nil || base.ClientCert != nil || len(got.Internal) != 1 {
		t.Fatalf("%+v %+v", got, base)
	}
}

func TestEnvRequireSignedOverlay(t *testing.T) {
	env, err := FromEnv(func(k string) string { return map[string]string{EnvRequireSigned: "1"}[k] })
	if err != nil || !env.RequireSigned {
		t.Fatalf("%+v %v", env, err)
	}
	if got := Overlay(Policy{}, env); !got.RequireSigned {
		t.Error("the machine can require signatures even when the hub does not")
	}
	if got := Overlay(Policy{RequireSigned: true}, Policy{}); !got.RequireSigned {
		t.Error("the hub's rule stands")
	}
}
