package imap

import (
	"context"
	"encoding/xml"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// discovered servers of a domain are kept a while: the source and every read resolve them, the domain is asked once.
type found struct {
	imap, smtp endpoint
	ok         bool
	at         time.Time
}

var (
	discoverMu  sync.Mutex
	discovered  = map[string]found{}
	discoverTTL = 12 * time.Hour
	// autoconfigURLs are where a domain publishes its settings (Thunderbird's autoconfig); tests point them elsewhere.
	autoconfigURLs = func(domain, email string) []string {
		q := url.QueryEscape(email)
		return []string{
			"https://autoconfig." + domain + "/mail/config-v1.1.xml?emailaddress=" + q,
			"https://" + domain + "/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=" + q,
		}
	}
	lookupSRV = net.DefaultResolver.LookupSRV
)

// discover finds the servers of a domain: its autoconfig file, then the SRV records of RFC 6186/8314. Only TLS ones.
func discover(ctx context.Context, in connectors.Instance, email, domain string) (endpoint, endpoint, bool) {
	discoverMu.Lock()
	f, hit := discovered[domain]
	discoverMu.Unlock()
	if hit && time.Since(f.at) < discoverTTL {
		return f.imap, f.smtp, f.ok
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	f = found{at: time.Now()}
	f.imap, f.smtp, f.ok = autoconfig(ctx, in.HTTP, domain, email)
	if !f.ok {
		f.imap, f.smtp, f.ok = srv(ctx, domain)
	}
	discoverMu.Lock()
	discovered[domain] = f
	discoverMu.Unlock()
	return f.imap, f.smtp, f.ok
}

type clientConfig struct {
	Incoming []acServer `xml:"emailProvider>incomingServer"`
	Outgoing []acServer `xml:"emailProvider>outgoingServer"`
}

type acServer struct {
	Type   string `xml:"type,attr"`
	Host   string `xml:"hostname"`
	Port   int    `xml:"port"`
	Socket string `xml:"socketType"`
}

func autoconfig(ctx context.Context, hc *http.Client, domain, email string) (endpoint, endpoint, bool) {
	if hc == nil {
		hc = http.DefaultClient
	}
	for _, u := range autoconfigURLs(domain, email) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		res, err := hc.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 256<<10))
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			continue
		}
		var cc clientConfig
		if xml.Unmarshal(body, &cc) != nil {
			continue
		}
		var i, s endpoint
		for _, x := range cc.Incoming {
			if x.Type == "imap" && strings.EqualFold(x.Socket, "SSL") && i.host == "" {
				i = endpoint{host: x.Host, port: x.Port}
			}
		}
		for _, x := range cc.Outgoing {
			if x.Type != "smtp" || s.host != "" {
				continue
			}
			switch strings.ToUpper(x.Socket) {
			case "SSL":
				s = endpoint{host: x.Host, port: x.Port}
			case "STARTTLS":
				s = endpoint{host: x.Host, port: x.Port, startTLS: true}
			}
		}
		if i.host != "" && s.host != "" && i.port > 0 && s.port > 0 {
			return i, s, true
		}
	}
	return endpoint{}, endpoint{}, false
}

func srv(ctx context.Context, domain string) (endpoint, endpoint, bool) {
	first := func(service string) (endpoint, bool) {
		_, addrs, err := lookupSRV(ctx, service, "tcp", domain)
		if err != nil {
			return endpoint{}, false
		}
		for _, a := range addrs {
			if h := strings.TrimSuffix(a.Target, "."); h != "" && a.Port > 0 && trusted(h, domain) {
				return endpoint{host: h, port: int(a.Port)}, true
			}
		}
		return endpoint{}, false
	}
	i, ok := first("imaps")
	if !ok {
		return endpoint{}, endpoint{}, false
	}
	if s, ok := first("submissions"); ok {
		return i, s, true
	}
	if s, ok := first("submission"); ok {
		s.startTLS = true
		return i, s, true
	}
	return endpoint{}, endpoint{}, false
}

// trusted says a server found by SRV may get the person's password: a host of the address's own domain, or of a known
// provider. DNS answers are not signed, so an SRV record naming a stranger's host is not followed; the person gives
// such servers by hand. (The autoconfig file comes over HTTPS from the domain itself and is taken as it says.)
func trusted(host, domain string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == domain || strings.HasSuffix(host, "."+domain) {
		return true
	}
	for _, p := range presets {
		for _, e := range p {
			if suffix := e.host[strings.IndexByte(e.host, '.')+1:]; host == e.host || strings.HasSuffix(host, "."+suffix) {
				return true
			}
		}
	}
	return false
}
