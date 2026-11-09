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
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/publicsuffix"

	"github.com/ly-solutions/harnessly/internal/apptoken"
	"github.com/ly-solutions/harnessly/internal/secrets/vaultkv"
)

const (
	OrgsOff    = "off"
	OrgsShadow = "shadow"
	OrgsOn     = "on"

	EditionSelfHosted = "self-hosted"
	EditionCloud      = "cloud"
	SignupOpen        = "open"
	SignupInvite      = "invite"
)

// parseBaseURL reads http(s)://host[:port][/path]: the URL and its path without a trailing slash ("" at the root). A
// query, a fragment, a login, an empty segment or a dot segment is refused: the path becomes a prefix of routes and
// cookies, so it must be one plain thing.
func parseBaseURL(raw string) (*url.URL, string, error) {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return nil, "", err
	case u.Scheme != "http" && u.Scheme != "https":
		return nil, "", errors.New("the scheme is http or https")
	case u.Host == "":
		return nil, "", errors.New("no host")
	case u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.RawPath != "":
		return nil, "", errors.New("no query, fragment, login or escapes")
	}
	base := strings.TrimRight(u.Path, "/")
	if base != "" {
		if first, _, _ := strings.Cut(base[1:], "/"); slices.Contains(reservedFirstSegments, first) {
			return nil, "", fmt.Errorf("/%s is a path of the hub itself", first)
		}
		for _, seg := range strings.Split(base[1:], "/") {
			if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "\\ ;%") {
				return nil, "", errors.New("the path is plain segments like /harnessly")
			}
		}
	}
	return u, base, nil
}

// reservedFirstSegments are the paths the hub answers at its root: a base path starting with one would be ambiguous.
var reservedFirstSegments = []string{"api", "agent", "dl", "m", "assets", "sites", "healthz", "readyz", "signin", "invite"}

// CookiePath is the Path of the hub's cookies: the base path, or / at the root of the host.
func (c *Config) CookiePath() string {
	if c.BasePath == "" {
		return "/"
	}
	return c.BasePath
}

// CookiePrefix is the name prefix a Secure cookie of the hub carries. __Host- needs Path=/, so under a base path the
// weaker __Secure- is what a browser can still be held to.
func (c *Config) CookiePrefix() string {
	switch {
	case !c.Secure:
		return ""
	case c.BasePath != "":
		return "__Secure-"
	default:
		return "__Host-"
	}
}

// Config is the validated configuration of one hub process.
type Config struct {
	// Replicas turns on several hub replicas (HUB_REPLICAS=on, docs/hub/replicas.md): machine links get a lease and a
	// fencing token in the database and a request reaches the replica that holds the link. ReplicaID names this replica
	// (random when empty), ReplicaAddr is where the other replicas reach it, ReplicaSecret is shared by all of them.
	Replicas                              bool
	ReplicaID, ReplicaAddr, ReplicaSecret string

	Listen string
	// PublicURL is the address people open the hub at, without a trailing slash. It may carry a path
	// (https://example.com/harnessly) when a reverse proxy serves the hub under one: every link the hub builds starts with it.
	PublicURL string
	// Origin is PublicURL without the path: what a browser calls the origin (sign-in with a passkey is bound to it).
	Origin string
	// BasePath is the path of PublicURL ("/harnessly"), "" when the hub is at the root of its host. A request may come
	// with it or without (the proxy may strip it); the hub answers both.
	BasePath string
	// Host is the public URL's host (with :port when not default), lower-case: what a machine signs its dial for.
	Host string
	// Secure is whether the public URL is https: cookies carry Secure and the __Host- prefix.
	Secure bool

	// AgentURL (HUB_AGENT_URL) is the address machines reach the hub at, without a trailing slash; the default is
	// PublicURL. Every URL built for a machine (installer, manifest, registration, ssh install, runtime domain) and the
	// host a machine signs its dial for come from it; people's links, cookies and sign-in come from PublicURL.
	AgentURL string
	// AgentHost is the agent URL's host (with :port when not default), lower-case.
	AgentHost string
	// AgentSecure is whether the agent URL is https.
	AgentSecure bool

	// Features (HUB_FEATURES_FORCE, HUB_FEATURES_DEFAULT) are the instance's say over the organizations' flags.
	Features Features

	DatabaseURL string
	// DBSchema (HUB_DB_SCHEMA) is the Postgres schema for the hub's tables; empty: the connection's search_path.
	DBSchema string
	// DBPooler (HUB_DB_POOLER) is "transaction" behind a connection pooler in transaction mode (PgBouncer), else "".
	DBPooler string
	// DBMigrateURL (HUB_DB_MIGRATE_URL) is a direct Postgres URL the migrations use instead of DatabaseURL.
	DBMigrateURL string
	// DBCheck (HUB_DB_CHECK) is what the hub does when the database is newer than the binary or its schema drifted:
	// "warn" (the default) logs, "strict" refuses to start, "off" does not look.
	DBCheck string
	DataDir string

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
	// AuthEmailOff (HUB_AUTH_EMAIL=off) closes the sign-in by a code mailed to the person: the hub signs people in only
	// through an OpenID Connect provider, GitHub, Yandex or the gateway, needs no SMTP server, and gives invitations
	// as links. Off by default (the code sign-in stays).
	AuthEmailOff bool

	Passkey bool
	RPID    string
	RPName  string

	GitHub GitHub
	Yandex Yandex
	OIDC   []OIDC
	// ProxyJWT signs people in on the address of a gateway in front of the hub that vouches for them with a signed
	// assertion of its own (Teleport application access); off when JWKSURL is empty.
	ProxyJWT ProxyJWT

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
	// Storage is where the hub keeps its files: "disk" (the data folder, the default) or "s3" (HUB_STORAGE=s3 with
	// HUB_S3_*: one bucket for every kind of file the hub holds). S3 is that bucket when Storage is "s3".
	Storage string
	S3      SitesS3
	// Store (internal/filestore, docs/hub/store.md): the revisions and contents of the spaces' collections, which the
	// machines sync with. On unless HUB_STORE=off.
	Store StoreConfig
	// FleetSites lets a fleet gateway mint narrow publishing sessions without registering its machines in the hub.
	FleetSitesSecret string
	FleetSitesSpace  string
	// SafeVault is the Vault-compatible server the hub keeps its machines' safes on (HUB_SECRETS_VAULT_*, docs/safe.md);
	// nil: the hub keeps none. SafePrefix is the hub's folder in the mount, SafeRefPrefixes the folders a machine may
	// read references from (HUB_SECRETS_REF_PREFIXES).
	SafeVault       *vaultkv.Config
	SafePrefix      string
	SafeRefPrefixes []string
	// AppToken is the platform's token for apps' backends (docs/build-apps/10-auth.md).
	AppToken AppToken
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
	// Orgs (HUB_ORGS) is how far organizations are switched on: OrgsOff (the schema is there, the hub behaves as before),
	// OrgsShadow (the new endpoints answer instance administrators only: checking and seeding), OrgsOn (everyone).
	Orgs string
	// MachineTrust (HUB_MACHINE_TRUST=on) lets an owner of a space trust a machine to run on the hub's own origin when
	// the hub has no HUB_RUNTIME_DOMAIN; off: only machines an instance administrator connected open there.
	MachineTrust bool
	// OIDCEmailTrusted (HUB_OIDC_EMAIL_TRUSTED=on) takes the email of an OpenID Connect provider as verified even when
	// the provider does not say email_verified: for a provider the operator runs or trusts. It is how the
	// first person to sign in through it becomes the administrator of a fresh installation that names none.
	OIDCEmailTrusted bool
	// Docs (HUB_DOCS=on) switches on the documents and boards side: the hub answers <platform api>/docs and the agents'
	// doc.publish / doc.acl (docs/hub/docs.md). Off: nothing of it is served and the page says so.
	Docs bool
	// ArtifactsURL and ArtifactsToken (HUB_ARTIFACTS_URL, HUB_ARTIFACTS_TOKEN) reach the documents service with an editor
	// token. BoardsURL and BoardsToken (HUB_BOARDS_URL, HUB_BOARDS_TOKEN) reach the boards service. A document needs the
	// first pair, boards need the second; either may be missing and the page then says which one.
	ArtifactsURL, ArtifactsToken string
	BoardsURL, BoardsToken       string
	// UsageIngest (HUB_USAGE_INGEST=on) lets the hub pull the machines' spend facts (docs/hub/usage.md); off: it never
	// asks, and the spend tables stay empty.
	UsageIngest bool
	// History (HUB_HISTORY, on by default) keeps the people's chats in the hub (docs/hub/history.md): the machines'
	// agents send their runtime's chat list and feeds, and the workspace reads them from here while a machine is off.
	History bool
	// Peers (HUB_PEERS=on) lets the machines' agents exchange messages between their chats through the hub
	// (docs/hub/peers.md); off: POST /agent/peers/* answer 404.
	Peers bool
	// Memory (HUB_MEMORY_SYNC=on) keeps each person's agent memory for all of the person's machines
	// (docs/hub/memory-sync.md); off: POST /agent/memory/* answer 404.
	Memory bool
	// DesignShareURL (HUB_DESIGN_SHARE_URL) is the origin of the guest links to designs (docs/hub/design-access.md); it
	// must be an origin of its own, not the hub's and not a machine's. Empty: links to designs are off.
	DesignShareURL string
	// DesignPlatformAPI (HUB_DESIGN_PLATFORM_API) is the runtime's platform prefix the design page calls, /api/platform
	// by default; the hub learns a machine's own prefix from its page's first request.
	DesignPlatformAPI string
	// MRSourceURL, MRSourceGroups and MRSourceToken (HUB_MR_SOURCE_GITLAB_URL, _GROUPS, _TOKEN) name the GitLab whose merged
	// merge requests the hub polls for the KPI of docs/hub/analytics.md; no URL: it polls nothing. The token needs read_api.
	MRSourceURL    string
	MRSourceGroups []string
	MRSourceToken  string
	// LimitsEnforce (HUB_LIMITS_ENFORCE) is what the machines do with the limits (docs/hub/limits.md): off sends them
	// nothing (the hub still shows the limits), warn makes a stop only a notice, stop stops new chats.
	LimitsEnforce string
	// EnvFeatures (HUB_ENV_FEATURES) are the parts of the environment section that are on for every organization: "env"
	// (the layers and the machines' lists), "catalog" (the catalog of skills, MCP servers, repositories and documents);
	// empty: off, the endpoints answer 404 not_enabled (docs/hub/environment.md).
	EnvFeatures []string
	// EnvRecommended (HUB_ENV_RECOMMENDED, on by default) puts the platform's group «Рекомендованное» into every
	// organization's environment (docs/hub/environment.md, «Recommended»); off leaves it out everywhere.
	EnvRecommended bool
	// Machines holds the settings of the machine resources and access (machines.go).
	Machines MachinesConfig
	// Extensions holds the settings of the extension protocol (extensions.go).
	Extensions ExtensionsConfig
	// RatesURL (HUB_RATES_URL) is where the Central Bank's daily rates are read; "" is cbr.ru, "off" never fetches (money
	// is then shown in dollars).
	RatesURL string
	TermsURL string
	WebDir   string
	LogLevel string
}

// Features is the instance level of the organizations' flags (internal/hub/features). A name in Force is decided by the
// instance, on or off, whatever an organization chose (`name` forces it on, `-name` off); a name in Default is what an
// organization without a choice gets (`name` on, `-name` off). Without either a flag is off.
type Features struct {
	Force   map[string]bool
	Default map[string]bool
}

// SitesS3 is an S3-compatible bucket for the sites' files (MinIO, Selectel). Endpoint empty: a folder.
type SitesS3 struct{ Endpoint, Region, Bucket, Prefix, AccessKey, SecretKey string }

// StoreConfig is where the store keeps its revisions and contents.
type StoreConfig struct {
	Off bool
	// DatabaseURL is the store's metadata: sqlite:///folder or postgres://… . Set by HUB_STORE_DATABASE_URL, it is a
	// database of the store's own (a design-sync center's database opens as it is); unset, the store shares the hub's
	// Postgres under PGPrefix, or keeps SQLite in Dir next to a SQLite hub.
	DatabaseURL string
	PGPrefix    string
	// Dir is the store's folder: its SQLite and, without a bucket, its blobs (HUB_STORE_DIR, default <data>/store).
	Dir string
	// S3 is the bucket of the contents when the hub keeps its files in one (HUB_STORAGE=s3): the hub's bucket under
	// the prefix store/, or HUB_STORE_S3_BUCKET and HUB_STORE_S3_PREFIX for a bucket that holds a design-sync
	// center's blobs already. Endpoint empty: blobs in Dir.
	S3 SitesS3
	// LegacySpace (HUB_STORE_LEGACY_SPACE, a space's slug) is the space whose collections are the rooms of a store
	// that served one place (`design`, not `design-<space>`): how a design-sync center's library becomes a space's.
	LegacySpace string
}

// StorageS3 says the hub keeps its files in an S3-compatible bucket (HUB_STORAGE=s3), not in its data folder.
func (c *Config) StorageS3() bool { return c.Storage == "s3" }

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

// ProxyJWT is a gateway that answers for its people with a JWT on every request (HUB_PROXY_JWT_*). The hub
// believes the assertion only on Host, the address people reach the gateway by, and sets no cookie there.
type ProxyJWT struct {
	JWKSURL    string
	Issuer     string // empty: any issuer the keys sign for
	Audience   string
	Header     string
	EmailClaim string
	Host       string // lower-case host[:port]
}

// On says whether the hub takes the assertions.
func (p ProxyJWT) On() bool { return p.JWKSURL != "" }

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
	return c.SMTPURL == "" && !c.AuthEmailOff && (c.Secure || c.Edition == EditionCloud) && !c.MailLogAllowed
}

// SubdomainMode is whether every machine gets its own origin under RuntimeDomain.
func (c *Config) SubdomainMode() bool { return c.RuntimeDomain != "" }

// RuntimeHost is the host of a machine's own origin in subdomain mode: <id>.<runtime domain>, with the public URL's
// port when it has one (development).
func (c *Config) RuntimeHost(machineID string) string {
	h := machineID + "." + c.RuntimeDomain
	if _, port, ok := strings.Cut(c.AgentHost, ":"); ok && port != "" {
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
	if h == hostName(c.Host) || h == hostName(c.AgentHost) {
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
		Listen:           def("HUB_LISTEN", "0.0.0.0:8040"),
		DataDir:          def("HUB_DATA_DIR", "/var/lib/harnessly-hub"),
		Edition:          def("HUB_EDITION", EditionSelfHosted),
		Signup:           def("HUB_SIGNUP", SignupOpen),
		SMTPURL:          get("HUB_SMTP_URL"),
		MailFrom:         get("HUB_MAIL_FROM"),
		RPID:             get("HUB_RP_ID"),
		RPName:           def("HUB_RP_NAME", "harnessly"),
		RuntimeDomain:    strings.ToLower(strings.Trim(get("HUB_RUNTIME_DOMAIN"), ".")),
		DistDir:          def("HUB_DIST_DIR", "/opt/harnessly/dist"),
		SitesDomain:      strings.ToLower(strings.Trim(get("HARNESSLY_SITES_DOMAIN"), ".")),
		SitesAbuseURL:    get("HARNESSLY_SITES_ABUSE_URL"),
		FleetSitesSecret: get("HARNESSLY_FLEET_SITES_SECRET"),
		FleetSitesSpace:  get("HARNESSLY_FLEET_SITES_SPACE"),
		ContainerImage:   get("HUB_CONTAINER_IMAGE"),
		LocalCodeFile:    get("HUB_LOCAL_CODE_FILE"),
		LocalCodeGID:     -1,
		MinAgentVersion: strings.TrimPrefix(
			get("HUB_MIN_AGENT_VERSION"), "v"),
		TermsURL: get("HUB_TERMS_URL"),
		WebDir:   get("HUB_WEB_DIR"),
		LogLevel: strings.ToLower(def("HUB_LOG_LEVEL", "info")),
	}
	c.DBSchema = get("HUB_DB_SCHEMA")
	c.DBPooler = strings.ToLower(get("HUB_DB_POOLER"))
	c.DBMigrateURL = get("HUB_DB_MIGRATE_URL")
	c.DBCheck = strings.ToLower(def("HUB_DB_CHECK", "warn"))
	c.DatabaseURL = get("HUB_DATABASE_URL")
	if c.DatabaseURL == "" {
		c.DatabaseURL = "sqlite://" + filepath.Join(c.DataDir, "hub.db")
	}

	// Public URL: the one required setting.
	raw := strings.TrimRight(get("HUB_PUBLIC_URL"), "/")
	switch u, base, err := parseBaseURL(raw); {
	case raw == "":
		bad("HUB_PUBLIC_URL is required, e.g. https://hub.example.com")
	case err != nil:
		bad("HUB_PUBLIC_URL must be http(s)://host[:port][/path], got %q (%v)", raw, err)
	default:
		c.PublicURL = raw
		c.Origin = u.Scheme + "://" + u.Host
		c.BasePath = base
		c.Host = strings.ToLower(u.Host)
		c.Secure = u.Scheme == "https"
		if c.RPID == "" {
			c.RPID = strings.ToLower(u.Hostname())
		}
	}

	// The machines' address: the public one unless HUB_AGENT_URL says otherwise.
	c.AgentURL, c.AgentHost, c.AgentSecure = c.PublicURL, c.Host, c.Secure
	if raw := strings.TrimRight(get("HUB_AGENT_URL"), "/"); raw != "" {
		switch u, _, err := parseBaseURL(raw); {
		case err != nil:
			bad("HUB_AGENT_URL must be http(s)://host[:port][/path], got %q (%v)", raw, err)
		default:
			c.AgentURL, c.AgentHost, c.AgentSecure = raw, strings.ToLower(u.Host), u.Scheme == "https"
		}
	}
	c.Features.Force = featureList(get("HUB_FEATURES_FORCE"), "HUB_FEATURES_FORCE", bad)
	c.Features.Default = featureList(get("HUB_FEATURES_DEFAULT"), "HUB_FEATURES_DEFAULT", bad)

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
	switch strings.ToLower(def("HUB_OIDC_EMAIL_TRUSTED", "off")) {
	case "on", "true", "1", "yes":
		c.OIDCEmailTrusted = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_OIDC_EMAIL_TRUSTED must be on or off")
	}
	switch strings.ToLower(def("HUB_MACHINE_TRUST", "off")) {
	case "on", "true", "1", "yes":
		c.MachineTrust = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_MACHINE_TRUST must be on or off")
	}
	switch strings.ToLower(def("HUB_REPLICAS", "off")) {
	case "on", "true", "1", "yes":
		c.Replicas = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_REPLICAS must be on or off")
	}
	c.ReplicaID, c.ReplicaAddr, c.ReplicaSecret = def("HUB_REPLICA_ID", ""), strings.TrimRight(def("HUB_REPLICA_ADDR", ""), "/"), def("HUB_REPLICA_SECRET", "")
	if c.Replicas {
		if u, err := url.Parse(c.ReplicaAddr); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			bad("HUB_REPLICAS=on needs HUB_REPLICA_ADDR, the address the other replicas reach this one at (http://<pod ip>:8040)")
		}
		if len(c.ReplicaSecret) < 16 {
			bad("HUB_REPLICAS=on needs HUB_REPLICA_SECRET of at least 16 characters, the same on every replica")
		}
	}
	switch c.Orgs = strings.ToLower(def("HUB_ORGS", OrgsOff)); c.Orgs {
	case OrgsOff, OrgsShadow, OrgsOn:
	default:
		bad("HUB_ORGS must be off, shadow or on, got %q", c.Orgs)
	}
	switch strings.ToLower(def("HUB_DOCS", "off")) {
	case "on", "true", "1", "yes":
		c.Docs = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_DOCS must be on or off")
	}
	c.ArtifactsURL, c.ArtifactsToken = strings.TrimRight(get("HUB_ARTIFACTS_URL"), "/"), get("HUB_ARTIFACTS_TOKEN")
	c.BoardsURL, c.BoardsToken = strings.TrimRight(get("HUB_BOARDS_URL"), "/"), get("HUB_BOARDS_TOKEN")
	for _, u := range []struct{ name, v string }{{"HUB_ARTIFACTS_URL", c.ArtifactsURL}, {"HUB_BOARDS_URL", c.BoardsURL}} {
		if u.v != "" && !strings.HasPrefix(u.v, "http://") && !strings.HasPrefix(u.v, "https://") {
			bad("%s must be an http(s) address", u.name)
		}
	}
	for _, f := range strings.FieldsFunc(strings.ToLower(def("HUB_ENV_FEATURES", "")), func(r rune) bool { return r == ',' || r == ' ' }) {
		switch f {
		case "env", "catalog":
			c.EnvFeatures = append(c.EnvFeatures, f)
		case "all":
			c.EnvFeatures = append(c.EnvFeatures, "env", "catalog")
		default:
			bad("HUB_ENV_FEATURES takes env, catalog or all, got %q", f)
		}
	}
	switch strings.ToLower(def("HUB_ENV_RECOMMENDED", "on")) {
	case "on", "true", "1", "yes":
		c.EnvRecommended = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_ENV_RECOMMENDED must be on or off")
	}
	switch strings.ToLower(def("HUB_HISTORY", "on")) {
	case "on", "true", "1", "yes":
		c.History = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_HISTORY must be on or off")
	}
	switch strings.ToLower(def("HUB_USAGE_INGEST", "off")) {
	case "on", "true", "1", "yes":
		c.UsageIngest = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_USAGE_INGEST must be on or off")
	}
	switch strings.ToLower(def("HUB_PEERS", "off")) {
	case "on", "true", "1", "yes":
		c.Peers = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_PEERS must be on or off")
	}
	switch strings.ToLower(def("HUB_MEMORY_SYNC", "off")) {
	case "on", "true", "1", "yes":
		c.Memory = true
	case "off", "false", "0", "no":
	default:
		bad("HUB_MEMORY_SYNC must be on or off")
	}
	if u := get("HUB_DESIGN_SHARE_URL"); u != "" {
		pu, err := url.Parse(u)
		if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" || (pu.Path != "" && pu.Path != "/") {
			bad("HUB_DESIGN_SHARE_URL must be an http(s) origin")
		} else if hu, err := url.Parse(c.PublicURL); err == nil && strings.EqualFold(hu.Host, pu.Host) {
			bad("HUB_DESIGN_SHARE_URL must not be the hub's own origin")
		}
		c.DesignShareURL = strings.TrimRight(u, "/")
	}
	c.DesignPlatformAPI = "/api/platform"
	if p := get("HUB_DESIGN_PLATFORM_API"); p != "" {
		if !regexp.MustCompile(`^/api/[A-Za-z0-9_-]+$`).MatchString(p) {
			bad("HUB_DESIGN_PLATFORM_API must look like /api/<name>")
		}
		c.DesignPlatformAPI = p
	}
	if u := get("HUB_MR_SOURCE_GITLAB_URL"); u != "" {
		pu, err := url.Parse(u)
		if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
			bad("HUB_MR_SOURCE_GITLAB_URL must be an http(s) URL")
		}
		c.MRSourceURL = strings.TrimRight(u, "/")
		for _, g := range strings.Split(get("HUB_MR_SOURCE_GITLAB_GROUPS"), ",") {
			if g = strings.Trim(strings.TrimSpace(g), "/"); g != "" {
				c.MRSourceGroups = append(c.MRSourceGroups, g)
			}
		}
		if len(c.MRSourceGroups) == 0 {
			bad("HUB_MR_SOURCE_GITLAB_GROUPS must list at least one group when HUB_MR_SOURCE_GITLAB_URL is set")
		}
		if c.MRSourceToken = get("HUB_MR_SOURCE_GITLAB_TOKEN"); c.MRSourceToken == "" {
			bad("HUB_MR_SOURCE_GITLAB_TOKEN is required when HUB_MR_SOURCE_GITLAB_URL is set")
		}
	}
	switch c.LimitsEnforce = strings.ToLower(def("HUB_LIMITS_ENFORCE", "off")); c.LimitsEnforce {
	case "off", "warn", "stop":
	default:
		bad("HUB_LIMITS_ENFORCE must be off, warn or stop, got %q", c.LimitsEnforce)
	}
	c.RatesURL = get("HUB_RATES_URL")
	c.Machines = loadMachines(lookup, def, bad)
	c.Extensions = loadExtensions(lookup, def, bad)
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

	switch c.DBCheck {
	case "warn", "strict", "off":
	default:
		bad("HUB_DB_CHECK must be warn, strict or off")
	}
	switch c.DBPooler {
	case "", "off", "transaction":
		if c.DBPooler == "off" {
			c.DBPooler = ""
		}
	default:
		bad("HUB_DB_POOLER must be transaction (PgBouncer in transaction mode) or empty")
	}
	if c.DBSchema != "" && !regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`).MatchString(c.DBSchema) {
		bad("HUB_DB_SCHEMA must be lower-case letters, digits and _, starting with a letter or _ (63 characters at most)")
	}
	if !c.IsPostgres() && (c.DBSchema != "" || c.DBPooler != "" || c.DBMigrateURL != "") {
		bad("HUB_DB_SCHEMA, HUB_DB_POOLER and HUB_DB_MIGRATE_URL work with a postgres:// HUB_DATABASE_URL only")
	}
	if c.DBMigrateURL != "" {
		if u, err := url.Parse(c.DBMigrateURL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			bad("HUB_DB_MIGRATE_URL must be a postgres:// URL")
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
	switch strings.ToLower(def("HUB_AUTH_EMAIL", "on")) {
	case "on", "true", "1", "yes":
	case "off", "false", "0", "no":
		c.AuthEmailOff = true
	default:
		bad("HUB_AUTH_EMAIL must be on or off")
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

	c.ProxyJWT = ProxyJWT{
		JWKSURL: get("HUB_PROXY_JWT_JWKS_URL"), Issuer: get("HUB_PROXY_JWT_ISSUER"), Audience: get("HUB_PROXY_JWT_AUDIENCE"),
		Header: def("HUB_PROXY_JWT_HEADER", "Teleport-Jwt-Assertion"), EmailClaim: def("HUB_PROXY_JWT_EMAIL_CLAIM", "sub"),
		Host: strings.ToLower(strings.TrimSpace(get("HUB_PROXY_JWT_HOST"))),
	}
	if c.ProxyJWT.On() {
		if u, err := url.Parse(c.ProxyJWT.JWKSURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			bad("HUB_PROXY_JWT_JWKS_URL must be an http(s) URL")
		}
		if c.ProxyJWT.Audience == "" || c.ProxyJWT.Host == "" {
			bad("HUB_PROXY_JWT_JWKS_URL needs HUB_PROXY_JWT_AUDIENCE and HUB_PROXY_JWT_HOST")
		}
		if c.ProxyJWT.Host == c.Host {
			bad("HUB_PROXY_JWT_HOST must be the gateway's address, not the hub's own %q", c.Host)
		}
	}

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
	c.loadSafe(lookup, bad)
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
		c.ProxyJWT = ProxyJWT{}
		c.LocalCodeFile = ""
		c.SSHAllowPrivate = false
	}
	if c.AuthEmailOff && !c.GitHubOn() && !c.YandexOn() && len(c.OIDC) == 0 && !c.ProxyJWT.On() {
		bad("HUB_AUTH_EMAIL=off leaves no way to sign in: set an OpenID Connect provider (HUB_OIDC_*), GitHub, Yandex or the gateway sign-in (HUB_PROXY_JWT_*)")
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

// loadSafe reads the HUB_SECRETS_* settings: a server for the machines' safes, set when HUB_SECRETS_VAULT_ADDR is.
// The names are read as they are (not NAME_FILE): the logins name their files themselves.
func (c *Config) loadSafe(lookup func(string) (string, bool), bad func(string, ...any)) {
	plain := func(k string) string {
		v, _ := lookup(k)
		return strings.TrimSpace(v)
	}
	if plain("HUB_SECRETS_VAULT_ADDR") == "" {
		return
	}
	vc, err := vaultkv.FromEnv("HUB_SECRETS_VAULT_", plain)
	if err != nil {
		bad("%v", err)
		return
	}
	c.SafeVault = &vc
	c.SafePrefix = strings.Trim(plain("HUB_SECRETS_VAULT_PREFIX"), "/")
	if c.SafePrefix == "" {
		c.SafePrefix = "harnessly"
	}
	if !vaultkv.ValidPath(c.SafePrefix) {
		bad("HUB_SECRETS_VAULT_PREFIX must be a path like harnessly")
	}
	c.SafeRefPrefixes = vaultkv.SplitPrefixes(plain("HUB_SECRETS_REF_PREFIXES"))
}

// loadStore reads HUB_STORE_*: the store reuses the hub's database and bucket unless told otherwise.
func (c *Config) loadStore(get func(string) string, def func(string, string) string, bad func(string, ...any)) {
	switch strings.ToLower(def("HUB_STORE", "on")) {
	case "on", "true", "1", "yes":
	case "off", "false", "0", "no":
		c.Store.Off = true
	default:
		bad("HUB_STORE must be on or off")
	}
	c.Store.Dir = def("HUB_STORE_DIR", filepath.Join(c.DataDir, "store"))
	c.Store.LegacySpace = strings.TrimSpace(get("HUB_STORE_LEGACY_SPACE"))
	switch raw := get("HUB_STORE_DATABASE_URL"); {
	case raw != "":
		c.Store.DatabaseURL = raw
		if u, err := url.Parse(raw); err != nil || (u.Scheme != "sqlite" && u.Scheme != "postgres" && u.Scheme != "postgresql") {
			bad("HUB_STORE_DATABASE_URL must be sqlite:///folder or postgres://…")
		}
	case c.IsPostgres():
		// The direct connection when there is one: a pooler in transaction mode may drop the search_path the store's
		// rooms are kept apart by.
		c.Store.DatabaseURL = c.DatabaseURL
		if c.DBMigrateURL != "" {
			c.Store.DatabaseURL = c.DBMigrateURL
		}
		c.Store.PGPrefix = "filestore_"
	default:
		c.Store.DatabaseURL = "sqlite://" + c.Store.Dir
	}
	if c.StorageS3() {
		c.Store.S3 = c.S3
		c.Store.S3.Prefix = def("HUB_STORE_S3_PREFIX", c.S3.Prefix+"store/")
		c.Store.S3.Bucket = def("HUB_STORE_S3_BUCKET", c.S3.Bucket)
	} else if get("HUB_STORE_S3_PREFIX") != "" || get("HUB_STORE_S3_BUCKET") != "" {
		bad("HUB_STORE_S3_PREFIX and HUB_STORE_S3_BUCKET need HUB_STORAGE=s3: the store uses the hub's bucket and keys")
	}
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
	switch strings.ToLower(def("HUB_STORAGE", "disk")) {
	case "disk":
		c.Storage = "disk"
	case "s3":
		c.Storage = "s3"
		c.S3 = SitesS3{Endpoint: get("HUB_S3_ENDPOINT"), Region: get("HUB_S3_REGION"), Bucket: get("HUB_S3_BUCKET"),
			Prefix: get("HUB_S3_PREFIX"), AccessKey: get("HUB_S3_ACCESS_KEY"), SecretKey: get("HUB_S3_SECRET_KEY")}
		if c.S3.Endpoint == "" || c.S3.Bucket == "" || c.S3.AccessKey == "" || c.S3.SecretKey == "" {
			bad("HUB_STORAGE=s3 needs HUB_S3_ENDPOINT, HUB_S3_BUCKET, HUB_S3_ACCESS_KEY and HUB_S3_SECRET_KEY")
		}
		if u, err := url.Parse(c.S3.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			bad("HUB_S3_ENDPOINT must be http(s)://host[:port]")
		}
	default:
		bad("HUB_STORAGE must be disk or s3")
	}
	c.SitesS3 = SitesS3{
		Endpoint: get("HARNESSLY_SITES_S3_ENDPOINT"), Region: get("HARNESSLY_SITES_S3_REGION"), Bucket: get("HARNESSLY_SITES_S3_BUCKET"),
		Prefix: get("HARNESSLY_SITES_S3_PREFIX"), AccessKey: get("HARNESSLY_SITES_S3_ACCESS_KEY"), SecretKey: get("HARNESSLY_SITES_S3_SECRET_KEY"),
	}
	if c.SitesS3.Endpoint != "" && (c.SitesS3.Bucket == "" || c.SitesS3.AccessKey == "" || c.SitesS3.SecretKey == "") {
		bad("HARNESSLY_SITES_S3_ENDPOINT needs HARNESSLY_SITES_S3_BUCKET, HARNESSLY_SITES_S3_ACCESS_KEY and HARNESSLY_SITES_S3_SECRET_KEY")
	}
	if c.StorageS3() && c.SitesS3.Endpoint == "" {
		// The one setting: the sites go to the hub's bucket, under their own prefix. HARNESSLY_SITES_S3_* still wins.
		c.SitesS3 = c.S3
		c.SitesS3.Prefix = c.S3.Prefix + "sites/"
	}
	c.loadStore(get, def, bad)
	c.loadAppToken(get, def, bad)
	if c.SitesAbuseURL != "" {
		if u, err := url.Parse(c.SitesAbuseURL); err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto") {
			bad("HARNESSLY_SITES_ABUSE_URL must be an http(s) or mailto: address")
		}
	}
	if (c.FleetSitesSecret == "") != (c.FleetSitesSpace == "") {
		bad("HARNESSLY_FLEET_SITES_SECRET and HARNESSLY_FLEET_SITES_SPACE must be set together")
	}
	if c.FleetSitesSecret != "" && len(c.FleetSitesSecret) < 32 {
		bad("HARNESSLY_FLEET_SITES_SECRET must be at least 32 characters")
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

var featureName = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

// featureList reads `a,b,-c` into a→true, b→true, c→false.
func featureList(raw, env string, bad func(string, ...any)) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.Split(raw, ",") {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		on := true
		if rest, ok := strings.CutPrefix(f, "-"); ok {
			f, on = rest, false
		}
		if !featureName.MatchString(f) {
			bad("%s: %q is not a feature name", env, f)
			continue
		}
		out[f] = on
	}
	return out
}

// AppToken is how the hub signs the platform's token for apps' backends (HUB_APP_TOKEN_*). KeyFile is the Ed25519 key
// (PKCS#8 PEM; made there when missing; every replica reads the same file); "" when HUB_APP_TOKEN=off. PreviousKeys are
// public keys (PKIX PEM) of earlier rotations, still published. Exchange is the hub's client at the organization's
// identity provider for token exchange (RFC 8693); its TokenURL is "" when there is none. ExchangeAudience is the aud
// of the platform token the provider is handed (the provider's issuer or the hub's client id there).
type AppToken struct {
	KeyFile          string
	PreviousKeys     []string
	Exchange         apptoken.ExchangeConfig
	ExchangeAudience string
	ExchangeScope    string
	// ExchangeAudiences are the provider audiences the hub exchanges for, each with the origin of the backend it may be
	// sent to (HUB_APP_TOKEN_EXCHANGE_AUDIENCES=aud=https://host,…): an app gets no exchange for anything else.
	ExchangeAudiences map[string]string
}

// On says whether the hub mints tokens.
func (a AppToken) On() bool { return a.KeyFile != "" }

func (c *Config) loadAppToken(get func(string) string, def func(string, string) string, bad func(string, ...any)) {
	switch strings.ToLower(def("HUB_APP_TOKEN", "on")) {
	case "on", "true", "1", "yes":
		c.AppToken.KeyFile = def("HUB_APP_TOKEN_KEY_FILE", filepath.Join(c.DataDir, "app-token.pem"))
	case "off", "false", "0", "no":
		return
	default:
		bad("HUB_APP_TOKEN must be on or off")
	}
	for _, f := range strings.Split(get("HUB_APP_TOKEN_PREVIOUS_KEYS"), ",") {
		if f = strings.TrimSpace(f); f != "" {
			c.AppToken.PreviousKeys = append(c.AppToken.PreviousKeys, f)
		}
	}
	x := apptoken.ExchangeConfig{TokenURL: get("HUB_APP_TOKEN_EXCHANGE_URL"), ClientID: get("HUB_APP_TOKEN_EXCHANGE_CLIENT_ID"),
		ClientSecret: get("HUB_APP_TOKEN_EXCHANGE_CLIENT_SECRET"), SubjectTokenType: get("HUB_APP_TOKEN_EXCHANGE_SUBJECT_TOKEN_TYPE")}
	if x.TokenURL == "" {
		return
	}
	if u, err := url.Parse(x.TokenURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		bad("HUB_APP_TOKEN_EXCHANGE_URL must be an http(s) URL")
	}
	if x.ClientID == "" {
		bad("HUB_APP_TOKEN_EXCHANGE_URL needs HUB_APP_TOKEN_EXCHANGE_CLIENT_ID")
	}
	c.AppToken.Exchange = x
	c.AppToken.ExchangeAudience = def("HUB_APP_TOKEN_EXCHANGE_AUDIENCE", x.ClientID)
	c.AppToken.ExchangeScope = get("HUB_APP_TOKEN_EXCHANGE_SCOPE")
	c.AppToken.ExchangeAudiences = map[string]string{}
	for _, pair := range strings.Split(get("HUB_APP_TOKEN_EXCHANGE_AUDIENCES"), ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}
		aud, origin, ok := strings.Cut(pair, "=")
		u, err := url.Parse(origin)
		if !ok || aud == "" || err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" {
			bad("HUB_APP_TOKEN_EXCHANGE_AUDIENCES: %q is not audience=https://host[:port]", pair)
			continue
		}
		c.AppToken.ExchangeAudiences[aud] = strings.ToLower(u.Scheme + "://" + u.Host)
	}
	if len(c.AppToken.ExchangeAudiences) == 0 {
		bad("HUB_APP_TOKEN_EXCHANGE_URL needs HUB_APP_TOKEN_EXCHANGE_AUDIENCES: the audiences apps may get, each with its backend")
	}
}
