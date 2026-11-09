package egress

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Wire is the network policy of a deployment as the hub holds and serves it (<hub>/dl/network.json, the file network.json
// in the hub's dist folder): public things only — the names of internal hosts, the company's root certificates, the
// proxy and its exceptions, and who may sign the apps. A client certificate is a secret and stays in the machine's safe
// (network.clientCert of an app).
type Wire struct {
	InternalHosts []string `json:"internalHosts,omitempty"`
	// CAPem are PEM certificates added to the machines' roots.
	CAPem   string   `json:"caPem,omitempty"`
	Proxy   string   `json:"proxy,omitempty"`
	NoProxy []string `json:"noProxy,omitempty"`
	// Publishers are the keys an app package may be signed with; RequireSigned makes a signature from one of
	// them the condition of an install.
	Publishers    []Publisher `json:"publishers,omitempty"`
	RequireSigned bool        `json:"requireSigned,omitempty"`
}

// Publisher is a name and the public key (base64 of 32 bytes, ed25519) of one who signs app packages.
type Publisher struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// Problem is one thing wrong with a field of a policy, in words for the admin.
type Problem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

var (
	hostRe   = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
	noProxyR = regexp.MustCompile(`^(\*\.|\.)?[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
)

// Problems lists everything wrong with a policy before it is served or taken: the field it is in, and what to do.
func (w Wire) Problems() []Problem {
	var ps []Problem
	add := func(field, msg string) { ps = append(ps, Problem{field, msg}) }
	for _, h := range w.InternalHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if !hostRe.MatchString(h) {
			add("internalHosts", fmt.Sprintf("«%s» не имя хоста: пишите crm.bank.local или *.bank.local, без http:// и пути", h))
		}
	}
	if w.CAPem != "" {
		switch {
		case strings.Contains(w.CAPem, "PRIVATE KEY"):
			add("caPem", "в поле закрытый ключ — здесь только корневые сертификаты, ключ уберите")
		default:
			if _, err := AddRoots([]byte(w.CAPem)); err != nil {
				add("caPem", "это не сертификат в формате PEM (-----BEGIN CERTIFICATE-----)")
			}
		}
	}
	if w.Proxy != "" {
		if _, err := ParseProxy(w.Proxy); err != nil {
			add("proxy", "прокси пишется как http://хост:порт или https://хост:порт")
		}
	}
	for _, e := range w.NoProxy {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "*" || noProxyR.MatchString(e) {
			continue
		}
		if _, err := netip.ParsePrefix(e); err == nil {
			continue
		}
		if _, err := netip.ParseAddr(e); err == nil {
			continue
		}
		add("noProxy", fmt.Sprintf("«%s» не имя, не адрес и не сеть: пишите host.corp, .corp, 10.0.0.0/8", e))
	}
	seen := map[string]bool{}
	for _, pub := range w.Publishers {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pub.Key))
		switch {
		case strings.TrimSpace(pub.Name) == "":
			add("publishers", "у издателя нет названия")
		case err != nil || len(raw) != ed25519.PublicKeySize:
			add("publishers", fmt.Sprintf("ключ издателя «%s» не открытый ключ ed25519 (base64, 32 байта): возьмите его командой harnessly app key", pub.Name))
		case seen[strings.TrimSpace(pub.Key)]:
			add("publishers", fmt.Sprintf("ключ издателя «%s» уже в списке", pub.Name))
		default:
			seen[strings.TrimSpace(pub.Key)] = true
		}
	}
	if w.RequireSigned && len(w.Publishers) == 0 {
		add("requireSigned", "подпись обязательна, а доверенных издателей нет: никто не сможет установить приложение — добавьте издателя")
	}
	return ps
}

// Validate says what is wrong with a policy: the first of its problems.
func (w Wire) Validate() error {
	if ps := w.Problems(); len(ps) > 0 {
		return fmt.Errorf("%s: %s", ps[0].Field, ps[0].Message)
	}
	return nil
}

// Policy is the wire policy as a Policy.
func (w Wire) Policy() (Policy, error) {
	if err := w.Validate(); err != nil {
		return Policy{}, err
	}
	p := Policy{Internal: splitList(strings.Join(w.InternalHosts, ",")), NoProxy: splitList(strings.Join(w.NoProxy, ",")),
		Publishers: w.Publishers, RequireSigned: w.RequireSigned}
	if w.CAPem != "" {
		roots, _ := AddRoots([]byte(w.CAPem))
		p.Roots = roots
	}
	if w.Proxy != "" {
		p.Proxy, _ = ParseProxy(w.Proxy)
	}
	return p, nil
}

// Overlay is the hub's policy with the machine's own settings on top: a setting the environment names replaces the
// hub's whole value of it (the list of internal hosts, the roots, the proxy, the exceptions); one it leaves out stays.
func Overlay(hub, env Policy) Policy {
	out := hub
	if len(env.Internal) > 0 {
		out.Internal = env.Internal
	}
	if env.Roots != nil {
		out.Roots = env.Roots
	}
	if env.Proxy != nil {
		out.Proxy = env.Proxy
	}
	if len(env.NoProxy) > 0 {
		out.NoProxy = env.NoProxy
	}
	if env.ClientCert != nil {
		out.ClientCert = env.ClientCert
	}
	if env.RequireSigned {
		out.RequireSigned = true
	}
	return out
}

// PollEvery is how often a machine asks the hub for the policy: an admin's change reaches it in this time, no restart.
const PollEvery = 5 * time.Minute

// CacheFile is where a machine keeps the hub's policy last taken, so that a restart without the hub has it.
const CacheFile = "network-policy.json"

// Fetch reads the hub's policy once. A hub that has none (404) answers the zero wire and no error.
func Fetch(ctx context.Context, hubURL string) (Wire, error) {
	var w Wire
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(hubURL, "/")+"/dl/network.json", nil)
	if err != nil {
		return w, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return w, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return w, nil
	}
	if res.StatusCode != http.StatusOK {
		return w, fmt.Errorf("hub answered %d", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&w); err != nil {
		return w, err
	}
	return w, w.Validate()
}

// Keep runs the machine's side: it takes the hub's policy now and every hour, sets Overlay(hub, env) as the process's
// policy and writes the hub's part into dir (CacheFile), which it reads first, so the policy is there before the hub
// answers and after a restart without it. It stops with ctx. log is told what went wrong.
func Keep(ctx context.Context, hubURL, dir string, env Policy, log func(msg string, err error)) {
	cache := filepath.Join(dir, CacheFile)
	apply := func(w Wire) {
		hub, err := w.Policy()
		if err != nil {
			log("network policy of the hub is not taken", err)
			return
		}
		Set(Overlay(hub, env))
	}
	if raw, err := os.ReadFile(cache); err == nil {
		var w Wire
		if json.Unmarshal(raw, &w) == nil {
			apply(w)
		}
	}
	if hubURL == "" {
		return
	}
	go func() {
		for {
			w, err := Fetch(ctx, hubURL)
			if err != nil {
				log("network policy of the hub is not read", err)
			} else {
				apply(w)
				if raw, err := json.Marshal(w); err == nil {
					_ = os.MkdirAll(dir, 0o755)
					_ = os.WriteFile(cache, raw, 0o644)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(PollEvery):
			}
		}
	}()
}
