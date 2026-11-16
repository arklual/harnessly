package design

import (
	"bufio"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Which registry the button «Подключить UI-кит» may ask npm for a package: the public one, the ones the
// machine's own .npmrc names, and the hosts of HARNESSLY_KIT_REGISTRIES (comma separated). A request never adds one: an
// address from a request would make npm send the machine's token, and fetch from a host the request chose.

const defaultKitRegistryHost = "registry.npmjs.org"

// AllowedKitRegistryHosts are the registry hosts a connect request may name.
func AllowedKitRegistryHosts() map[string]bool {
	hosts := map[string]bool{defaultKitRegistryHost: true}
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			hosts[strings.ToLower(u.Hostname())] = true
		}
	}
	for _, h := range strings.Split(os.Getenv("HARNESSLY_KIT_REGISTRIES"), ",") {
		add(h)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if f, err := os.Open(filepath.Join(home, ".npmrc")); err == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
				k = strings.TrimSpace(k)
				if ok && (k == "registry" || strings.HasSuffix(k, ":registry")) {
					add(v)
				}
			}
		}
	}
	return hosts
}

// checkKitRegistry is nil for an empty registry (npm's own default) or an https address of an allowed host; http only
// to the machine itself, and then only when it is listed too (a local mirror). Anything else is refused in words for the person.
func checkKitRegistry(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Hostname() == "" {
		return &KitPinError{locale.Pick("The registry is not an address: use https://host/path", "Реестр — не адрес: нужен https://хост/путь")}
	}
	host := strings.ToLower(u.Hostname())
	loop := host == "localhost" || net.ParseIP(host).IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && loop) {
		return &KitPinError{locale.Pick("The registry must be https", "Реестр должен быть по https")}
	}
	if !AllowedKitRegistryHosts()[host] {
		return &KitPinError{locale.Pick(
			"The registry "+host+" is not allowed: only registry.npmjs.org, the registries of this machine's .npmrc and HARNESSLY_KIT_REGISTRIES",
			"Реестр "+host+" не разрешён: только registry.npmjs.org, реестры из .npmrc машины и HARNESSLY_KIT_REGISTRIES")}
	}
	return nil
}
