// Package config reads the hub's configuration: every HUB_* variable of docs/hub/api.md, each also as <NAME>_FILE.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/publicsuffix"
)

const (
	EditionSelfHosted = "self-hosted"
	EditionCloud      = "cloud"
	SignupOpen        = "open"
	SignupInvite      = "invite"
)

// Config is the validated configuration of one hub process.
type Config struct {
	Listen    string
	PublicURL string // no trailing slash
	// Host is the public URL's host (with :port when not default), lower-case: what a machine signs its dial for.
	Host string
	// Secure is whether the public URL is https: cookies carry Secure and the __Host- prefix.
	Secure bool

	DatabaseURL string
	DataDir     string

	Edition string
	Signup  string
	// AdminEmails are lower-cased. Empty: the first account becomes the instance admin.
	AdminEmails    []string
	TrustedProxies []netip.Prefix

	SMTPURL  string
	MailFrom string
	// MailLogAllowed (HUB_MAIL_LOG=allow) accepts that, with no SMTP server, sign-in codes and invitation links are
	// written to the hub's log even though the hub is on https or in the cloud edition, where the log is read by more
	// people than the person signing in.
	MailLogAllowed bool

	Passkey bool
	RPID    string
	RPName  string

	GitHub GitHub
	Yandex Yandex
	OIDC   []OIDC

	RuntimeDomain string
	// AllowSameSiteRuntime (HUB_ALLOW_SAME_SITE_RUNTIME) lets the cloud edition run machines on a domain that shares its
	// registrable domain with the hub (app.example.com and run.example.com). Other editions only warn.
	AllowSameSiteRuntime bool
	DistDir              string

	// Sites (internal/sites): published static sites. SitesDomain is the wildcard domain they live under
	// (<site>--<space>.<domain>); without it a self-hosted hub serves them under /sites/ on its own origin, sandboxed.
	SitesOff      bool
	SitesDomain   string
	SitesAbuseURL string
	// SitesDir holds the sites' files when no bucket is set.
	SitesDir string
	SitesS3  SitesS3
	// Warnings are what the hub starts with although its operator should look at it again: the binary logs each at
	// error level and check-config prints them.
	Warnings []string

	ContainerImage string
	LocalCodeFile  string
	// LocalCodeGID is the group that may read the local code file (HUB_LOCAL_CODE_GROUP); -1 when only the hub may.
	LocalCodeGID int
	SSH          bool
	// SSHAllowPrivate lets the SSH way connect to addresses that are not on the public internet (HUB_SSH_ALLOW_PRIVATE):
	// servers on the hub's LAN. Never on in the cloud edition.
	SSHAllowPrivate bool
	// SecretKey is what the hub encrypts the secrets it keeps for later with (HUB_SECRET_KEY); empty: a key file in
	// the data folder.
	SecretKey       string
	MinAgentVersion string
	TermsURL        string
	WebDir          string
	LogLevel        string
}

// SitesS3 is an S3-compatible bucket for the sites' files (MinIO, Selectel). Endpoint empty: a folder.
type SitesS3 struct{ Endpoint, Region, Bucket, Prefix, AccessKey, SecretKey string }

// SitesMode says how published sites are reached: "subdomain" (own address per site under SitesDomain), "path"
// (/sites/<site>--<space>/ on the hub's origin, sandboxed) or "off".
func (c *Config) SitesMode() string {
	switch {
	case c.SitesOff:
		return "off"
	case c.SitesDomain != "":
		return "subdomain"
	case c.Edition == EditionCloud:
		// A stranger's page on the cloud hub's own origin is not something a sandbox makes safe enough.
		return "off"
	}
	return "path"
}

// SitesHost is the sites domain with the public URL's port when it has one (development).
func (c *Config) SitesHost() string {
	if c.SitesDomain == "" {
		return ""
	}
	h := c.SitesDomain
	if _, port, ok := strings.Cut(c.Host, ":"); ok && port != "" {
		h += ":" + port
	}
	return h
}

// GitHub is the GitHub OAuth app. URL is the base of GitHub Enterprise, https://github.com by default.
type GitHub struct{ ClientID, ClientSecret, URL string }

// Yandex is the Yandex ID app.
type Yandex struct{ ClientID, ClientSecret string }

// OIDC is one generic OpenID Connect provider. Scopes is empty for the default `openid email profile`.
type OIDC struct {
	ID, Name, Issuer, ClientID, ClientSecret string
	Scopes                                   []string
}

func (c *Config) GitHubOn() bool { return c.GitHub.ClientID != "" && c.GitHub.ClientSecret != "" }
func (c *Config) YandexOn() bool { return c.Yandex.ClientID != "" && c.Yandex.ClientSecret != "" }
func (c *Config) IsAdminEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, e := range c.AdminEmails {
		if e == email {
			return true
		}
	}
	return false
}

// MailLogRefused says the hub would write sign-in codes and invitation links to its log (no SMTP server) where that is
// not acceptable: on an https hub and in the cloud edition the log is kept and read by people other than the one who
// asked for the code, and a code in it is a sign-in as that person. HUB_MAIL_LOG=allow takes the decision from here.
func (c *Config) MailLogRefused() bool {
	return c.SMTPURL == "" && (c.Secure || c.Edition == EditionCloud) && !c.MailLogAllowed
}

// SubdomainMode is whether every machine gets its own origin under RuntimeDomain.
func (c *Config) SubdomainMode() bool { return c.RuntimeDomain != "" }

// RuntimeHost is the host of a machine's own origin in subdomain mode: <id>.<runtime domain>, with the public URL's
// port when it has one (development).
func (c *Config) RuntimeHost(machineID string) string {
	h := machineID + "." + c.RuntimeDomain
	if _, port, ok := strings.Cut(c.Host, ":"); ok && port != "" {
		h += ":" + port
	}
	return h
}

// hostName is a host without its port, lower-case.
func hostName(host string) string {
	h := strings.ToLower(host)
	if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return h
}

// MachineIDFromHost reads the machine id out of a request host of the form <id>.<runtime domain>[:port]; "" when the
// host is not one. The hub's own host is never one, whatever the runtime domain is.
func (c *Config) MachineIDFromHost(host string) string {
	if c.RuntimeDomain == "" {
		return ""
	}
	h := hostName(host)
	if h == hostName(c.Host) {
		return ""
	}
	id, ok := strings.CutSuffix(h, "."+c.RuntimeDomain)
	if !ok || id == "" || strings.Contains(id, ".") {
		return ""
	}
	return id
}

// sameSite says that two hosts share a registrable domain (the public suffix list's eTLD+1), which makes their origins
// the same site: a browser sends a person's cookies from one to the other, and a page of one may set cookies for the
// other's parent domain. False when either has none (an address, localhost, a public suffix itself).
func sameSite(a, b string) bool {
	if net.ParseIP(a) != nil || net.ParseIP(b) != nil {
		return false
	}
	ra, errA := publicsuffix.EffectiveTLDPlusOne(a)
	rb, errB := publicsuffix.EffectiveTLDPlusOne(b)
	return errA == nil && errB == nil && ra == rb
}

var oidcID = regexp.MustCompile(`^[a-z0-9]+$`)
var domainName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$|^localhost$`)

// Load reads the configuration through lookup (os.LookupEnv in the binary) and reports every problem at once.
func Load(lookup func(string) (string, bool)) (*Config, error) {
	var problems []string
	bad := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	// get returns NAME, else the content of the file NAME_FILE names; ok says a non-empty value was found.
	get := func(name string) string {
		if v, ok := lookup(name); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if path, ok := lookup(name + "_FILE"); ok && strings.TrimSpace(path) != "" {
			b, err := os.ReadFile(strings.TrimSpace(path))
			if err != nil {
				bad("%s_FILE: %v", name, err)
				return ""
			}
			return strings.TrimSpace(string(b))
		}
		return ""
	}
	def := func(name, fallback string) string {
		if v := get(name); v != "" {
			return v
		}
		return fallback
	}

	c := &Config{
		Listen:         def("HUB_LISTEN", "0.0.0.0:8040"),
		DataDir:        def("HUB_DATA_DIR", "/var/lib/harnessly-hub"),
		Edition:        def("HUB_EDITION", EditionSelfHosted),
		Signup:         def("HUB_SIGNUP", SignupOpen),
		SMTPURL:        get("HUB_SMTP_URL"),
		MailFrom:       get("HUB_MAIL_FROM"),
		RPID:           get("HUB_RP_ID"),
		RPName:         def("HUB_RP_NAME", "harnessly"),
		RuntimeDomain:  strings.ToLower(strings.Trim(get("HUB_RUNTIME_DOMAIN"), ".")),
		DistDir:        def("HUB_DIST_DIR", "/opt/harnessly/dist"),
		SitesDomain:    strings.ToLower(strings.Trim(get("HARNESSLY_SITES_DOMAIN"), ".")),
		SitesAbuseURL:  get("HARNESSLY_SITES_ABUSE_URL"),
		ContainerImage: get("HUB_CONTAINER_IMAGE"),
		LocalCodeFile:  get("HUB_LOCAL_CODE_FILE"),
		LocalCodeGID:   -1,
		MinAgentVersion: strings.TrimPrefix(
			get("HUB_MIN_AGENT_VERSION"), "v"),
		TermsURL: get("HUB_TERMS_URL"),
		WebDir:   get("HUB_WEB_DIR"),
		LogLevel: strings.ToLower(def("HUB_LOG_LEVEL", "info")),
	}
	c.DatabaseURL = get("HUB_DATABASE_URL")
	if c.DatabaseURL == "" {
		c.DatabaseURL = "sqlite://" + filepath.Join(c.DataDir, "hub.db")
	}

	// Public URL: the one required setting.
	raw := strings.TrimRight(get("HUB_PUBLIC_URL"), "/")
	switch u, err := url.Parse(raw); {
	case raw == "":
		bad("HUB_PUBLIC_URL is required, e.g. https://hub.example.com")
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil:
		bad("HUB_PUBLIC_URL must be http(s)://host[:port] without a path, got %q", raw)
	default:
		c.PublicURL = raw
		c.Host = strings.ToLower(u.Host)
		c.Secure = u.Scheme == "https"
		if c.RPID == "" {
			c.RPID = strings.ToLower(u.Hostname())
		}
	}

	if c.Edition != EditionSelfHosted && c.Edition != EditionCloud {
		bad("HUB_EDITION must be self-hosted or cloud, got %q", c.Edition)
	}
	if c.Signup != SignupOpen && c.Signup != SignupInvite {
		bad("HUB_SIGNUP must be open or invite, got %q", c.Signup)
	}
	switch strings.ToLower(def("HUB_PASSKEY", "on")) {
	case "on", "true", "1", "yes":
		c.Passkey = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_PASSKEY must be on or off")
	}
	switch strings.ToLower(def("HUB_SSH", "on")) {
	case "on", "true", "1", "yes":
		c.SSH = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_SSH must be on or off")
	}
	switch strings.ToLower(def("HUB_SSH_ALLOW_PRIVATE", "off")) {
	case "on", "true", "1", "yes":
		c.SSHAllowPrivate = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_SSH_ALLOW_PRIVATE must be on or off")
	}
	if c.SecretKey = get("HUB_SECRET_KEY"); c.SecretKey != "" && len(c.SecretKey) < 32 {
		bad("HUB_SECRET_KEY must be at least 32 characters (openssl rand -base64 32)")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		bad("HUB_LOG_LEVEL must be debug, info, warn or error")
	}

	// Database.
	if u, err := url.Parse(c.DatabaseURL); err != nil {
		bad("HUB_DATABASE_URL is not a URL")
	} else {
		switch u.Scheme {
		case "sqlite":
		case "postgres", "postgresql":
			if u.Host == "" && u.Query().Get("host") == "" {
				bad("HUB_DATABASE_URL: postgres needs a host")
			}
		default:
			bad("HUB_DATABASE_URL must start with sqlite:// or postgres://")
		}
	}

	for _, e := range strings.Split(get("HUB_ADMIN_EMAILS"), ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			c.AdminEmails = append(c.AdminEmails, e)
		}
	}

	proxies := def("HUB_TRUSTED_PROXIES", "127.0.0.1/32,::1/128")
	for _, p := range strings.Split(proxies, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(p); err == nil {
			c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
		} else if addr, err := netip.ParseAddr(p); err == nil {
			c.TrustedProxies = append(c.TrustedProxies, netip.PrefixFrom(addr, addr.BitLen()))
		} else {
			bad("HUB_TRUSTED_PROXIES: %q is not a CIDR", p)
		}
	}

	if c.SMTPURL != "" {
		u, err := url.Parse(c.SMTPURL)
		if err != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Hostname() == "" {
			bad("HUB_SMTP_URL must be smtp://[user:pass@]host[:587] or smtps://[user:pass@]host[:465]")
		}
	}
	switch strings.ToLower(get("HUB_MAIL_LOG")) {
	case "":
	case "allow":
		c.MailLogAllowed = true
	default:
		bad("HUB_MAIL_LOG must be allow, or not set")
	}
	if c.MailLogRefused() {
		bad("HUB_SMTP_URL is required on an https hub and in the cloud edition: without it every sign-in code and invitation link " +
			"is written to the hub's log, and whoever can read the log can sign in as anyone (HUB_MAIL_LOG=allow accepts that, " +
			"for a hub whose log is as private as its database)")
	}
	if c.MailFrom == "" {
		host := "localhost"
		if c.Host != "" {
			host, _, _ = strings.Cut(c.Host, ":")
		}
		c.MailFrom = "harnessly <no-reply@" + host + ">"
	}

	c.GitHub = GitHub{ClientID: get("HUB_GITHUB_CLIENT_ID"), ClientSecret: get("HUB_GITHUB_CLIENT_SECRET"), URL: strings.TrimRight(def("HUB_GITHUB_URL", "https://github.com"), "/")}
	if (c.GitHub.ClientID == "") != (c.GitHub.ClientSecret == "") {
		bad("HUB_GITHUB_CLIENT_ID and HUB_GITHUB_CLIENT_SECRET go together")
	}
	if u, err := url.Parse(c.GitHub.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		bad("HUB_GITHUB_URL must be an http(s) URL")
	}
	c.Yandex = Yandex{ClientID: get("HUB_YANDEX_CLIENT_ID"), ClientSecret: get("HUB_YANDEX_CLIENT_SECRET")}
	if (c.Yandex.ClientID == "") != (c.Yandex.ClientSecret == "") {
		bad("HUB_YANDEX_CLIENT_ID and HUB_YANDEX_CLIENT_SECRET go together")
	}

	// OIDC providers: the cloud edition runs none.
	seen := map[string]bool{}
	for _, id := range strings.Split(get("HUB_OIDC_PROVIDERS"), ",") {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if !oidcID.MatchString(id) {
			bad("HUB_OIDC_PROVIDERS: %q must be [a-z0-9]+", id)
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		prefix := "HUB_OIDC_" + strings.ToUpper(id) + "_"
		p := OIDC{ID: id, Issuer: get(prefix + "ISSUER"), ClientID: get(prefix + "CLIENT_ID"), ClientSecret: get(prefix + "CLIENT_SECRET"), Name: get(prefix + "NAME")}
		p.Scopes = append(p.Scopes, strings.Fields(strings.ReplaceAll(get(prefix+"SCOPES"), ",", " "))...)
		if p.Name == "" {
			p.Name = id
		}
		if p.Issuer == "" || p.ClientID == "" || p.ClientSecret == "" {
			bad("%sISSUER, %sCLIENT_ID and %sCLIENT_SECRET are required", prefix, prefix, prefix)
		} else if u, err := url.Parse(p.Issuer); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			bad("%sISSUER must be an http(s) URL", prefix)
		}
		c.OIDC = append(c.OIDC, p)
	}
	sort.Slice(c.OIDC, func(i, j int) bool { return c.OIDC[i].ID < c.OIDC[j].ID })

	switch strings.ToLower(def("HUB_ALLOW_SAME_SITE_RUNTIME", "0")) {
	case "1", "on", "true", "yes":
		c.AllowSameSiteRuntime = true
	case "0", "off", "false", "no":
	default:
		bad("HUB_ALLOW_SAME_SITE_RUNTIME must be 1 or 0")
	}
	if c.RuntimeDomain != "" {
		if !domainName.MatchString(c.RuntimeDomain) {
			bad("HUB_RUNTIME_DOMAIN must be a domain name like m.example.com")
		}
		switch hub := hostName(c.Host); {
		case hub == c.RuntimeDomain:
			bad("HUB_RUNTIME_DOMAIN must differ from the hub's own host")
		case strings.HasSuffix(hub, "."+c.RuntimeDomain):
			// MachineIDFromHost("app.example.com") would read "app" and send every request of the hub to the proxy.
			bad("HUB_RUNTIME_DOMAIN %s is a parent of the hub's own host %s: use a domain that is not above it", c.RuntimeDomain, hub)
		case sameSite(hub, c.RuntimeDomain):
			msg := fmt.Sprintf("HUB_RUNTIME_DOMAIN %s and the hub's host %s are on one registrable domain, so they are one site: "+
				"a script of any machine can set cookies for the whole domain and send the hub's and the other machines' pages requests with a person's cookies; "+
				"the __Host- cookies and the cross-site checks narrow that, they do not remove it. Use a domain of its own for machines, or get it onto the public suffix list",
				c.RuntimeDomain, hub)
			switch {
			case c.AllowSameSiteRuntime:
				c.Warnings = append(c.Warnings, "HUB_ALLOW_SAME_SITE_RUNTIME is set: "+msg)
			case c.Edition == EditionCloud:
				bad("%s (HUB_ALLOW_SAME_SITE_RUNTIME=1 accepts it in the cloud edition)", msg)
			default:
				c.Warnings = append(c.Warnings, msg)
			}
		}
	}
	c.loadSites(get, def, bad)
	if c.Edition == EditionCloud && c.RuntimeDomain == "" {
		// The cloud's machines belong to people who do not trust each other (and the hub): a machine's page is its own
		// script, and on the hub's origin it would act with the signed-in person's session.
		bad("HUB_EDITION=cloud needs HUB_RUNTIME_DOMAIN: machines of strangers must not run on the hub's origin")
	}
	if g := get("HUB_LOCAL_CODE_GROUP"); g != "" {
		if gid, err := strconv.Atoi(g); err == nil && gid >= 0 {
			c.LocalCodeGID = gid
		} else if gr, err := user.LookupGroup(g); err == nil {
			c.LocalCodeGID, _ = strconv.Atoi(gr.Gid)
		} else {
			bad("HUB_LOCAL_CODE_GROUP must be a group id or the name of a group on this host, got %q", g)
		}
	}
	if c.Edition == EditionCloud {
		// The cloud runs no OIDC and no machine next to the hub (docs/hub/architecture.md, OSS and cloud).
		c.OIDC = nil
		c.LocalCodeFile = ""
		c.SSHAllowPrivate = false
	}

	if len(problems) > 0 {
		return nil, errors.New("configuration: " + strings.Join(problems, "; "))
	}
	return c, nil
}

// FromEnv is Load over the process environment.
func FromEnv() (*Config, error) { return Load(os.LookupEnv) }

// SQLitePath is the file of a sqlite:// database URL; "" for any other.
func (c *Config) SQLitePath() string {
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u.Scheme != "sqlite" {
		return ""
	}
	// sqlite:///abs/path.db → Path /abs/path.db; sqlite://rel.db → Host rel.db.
	return u.Host + u.Path
}

// IsPostgres says whether DatabaseURL is a Postgres DSN.
func (c *Config) IsPostgres() bool {
	u, err := url.Parse(c.DatabaseURL)
	return err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql")
}

// ParseIP is net.ParseIP with the zone and brackets a Host may carry removed.
func ParseIP(s string) net.IP {
	s = strings.Trim(s, "[]")
	if i := strings.IndexByte(s, '%'); i >= 0 {
		s = s[:i]
	}
	return net.ParseIP(s)
}

// loadSites reads the HARNESSLY_SITES_* settings and checks the domain the way the runtime domain is checked: sites run
// pages of strangers, so their domain must not be one the hub's cookies reach.
func (c *Config) loadSites(get func(string) string, def func(string, string) string, bad func(string, ...any)) {
	switch strings.ToLower(def("HARNESSLY_SITES", "on")) {
	case "on", "true", "1", "yes":
	case "off", "false", "0", "no":
		c.SitesOff = true
	default:
		bad("HARNESSLY_SITES must be on or off")
	}
	c.SitesDir = def("HARNESSLY_SITES_DIR", filepath.Join(c.DataDir, "sites"))
	c.SitesS3 = SitesS3{
		Endpoint: get("HARNESSLY_SITES_S3_ENDPOINT"), Region: get("HARNESSLY_SITES_S3_REGION"), Bucket: get("HARNESSLY_SITES_S3_BUCKET"),
		Prefix: get("HARNESSLY_SITES_S3_PREFIX"), AccessKey: get("HARNESSLY_SITES_S3_ACCESS_KEY"), SecretKey: get("HARNESSLY_SITES_S3_SECRET_KEY"),
	}
	if c.SitesS3.Endpoint != "" && (c.SitesS3.Bucket == "" || c.SitesS3.AccessKey == "" || c.SitesS3.SecretKey == "") {
		bad("HARNESSLY_SITES_S3_ENDPOINT needs HARNESSLY_SITES_S3_BUCKET, HARNESSLY_SITES_S3_ACCESS_KEY and HARNESSLY_SITES_S3_SECRET_KEY")
	}
	if c.SitesAbuseURL != "" {
		if u, err := url.Parse(c.SitesAbuseURL); err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto") {
			bad("HARNESSLY_SITES_ABUSE_URL must be an http(s) or mailto: address")
		}
	}
	if c.SitesDomain == "" {
		return
	}
	if !domainName.MatchString(c.SitesDomain) {
		bad("HARNESSLY_SITES_DOMAIN must be a domain name like sites.example.com")
		return
	}
	hub := hostName(c.Host)
	switch {
	case hub == c.SitesDomain:
		bad("HARNESSLY_SITES_DOMAIN must differ from the hub's own host")
	case strings.HasSuffix(hub, "."+c.SitesDomain):
		bad("HARNESSLY_SITES_DOMAIN %s is a parent of the hub's own host %s: use a domain that is not above it", c.SitesDomain, hub)
	case c.RuntimeDomain != "" && (c.SitesDomain == c.RuntimeDomain || strings.HasSuffix(c.SitesDomain, "."+c.RuntimeDomain) || strings.HasSuffix(c.RuntimeDomain, "."+c.SitesDomain)):
		bad("HARNESSLY_SITES_DOMAIN %s and HUB_RUNTIME_DOMAIN %s must not contain one another", c.SitesDomain, c.RuntimeDomain)
	case sameSite(hub, c.SitesDomain):
		msg := fmt.Sprintf("HARNESSLY_SITES_DOMAIN %s and the hub's host %s are on one registrable domain, so they are one site: "+
			"a published page could set cookies for the whole domain and send the hub requests with a person's cookies. "+
			"Use a domain of its own for sites, or get it onto the public suffix list", c.SitesDomain, hub)
		if c.Edition == EditionCloud && !c.AllowSameSiteRuntime {
			bad("%s (HUB_ALLOW_SAME_SITE_RUNTIME=1 accepts it in the cloud edition)", msg)
		} else {
			c.Warnings = append(c.Warnings, msg)
		}
	}
}
