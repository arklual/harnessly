package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "https://Hub.Example.com/", "HUB_SMTP_URL": "smtp://mail.example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL != "https://Hub.Example.com" || c.Host != "hub.example.com" || !c.Secure || c.RPID != "hub.example.com" {
		t.Errorf("public url: %+v", c)
	}
	if c.Listen != "0.0.0.0:8040" || c.Edition != EditionSelfHosted || c.Signup != SignupOpen || !c.Passkey || !c.SSH {
		t.Errorf("defaults: %+v", c)
	}
	if c.DatabaseURL != "sqlite:///var/lib/harnessly-hub/hub.db" || c.SQLitePath() != "/var/lib/harnessly-hub/hub.db" || c.IsPostgres() {
		t.Errorf("database: %q %q", c.DatabaseURL, c.SQLitePath())
	}
	if len(c.TrustedProxies) != 2 || c.MailFrom != "harnessly <no-reply@hub.example.com>" {
		t.Errorf("proxies %v from %q", c.TrustedProxies, c.MailFrom)
	}
}

func TestLoadFileVariants(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(env(map[string]string{
		"HUB_PUBLIC_URL":                "http://localhost:8040",
		"HUB_GITHUB_CLIENT_ID":          "id",
		"HUB_GITHUB_CLIENT_SECRET_FILE": secret,
		"HUB_DATABASE_URL":              "postgres://u:p@db/hub",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.GitHub.ClientSecret != "s3cret" || !c.GitHubOn() || c.Secure || !c.IsPostgres() {
		t.Errorf("%+v", c)
	}
}

func TestLoadOIDCAndCloud(t *testing.T) {
	base := map[string]string{
		"HUB_PUBLIC_URL":              "https://hub.example.com",
		"HUB_SMTP_URL":                "smtp://mail.example.com",
		"HUB_OIDC_PROVIDERS":          "keycloak, gitlab",
		"HUB_OIDC_KEYCLOAK_ISSUER":    "https://sso.example.com/realms/x",
		"HUB_OIDC_KEYCLOAK_CLIENT_ID": "a", "HUB_OIDC_KEYCLOAK_CLIENT_SECRET": "b", "HUB_OIDC_KEYCLOAK_NAME": "Корпоративный вход",
		"HUB_OIDC_GITLAB_ISSUER": "https://gitlab.example.com", "HUB_OIDC_GITLAB_CLIENT_ID": "c", "HUB_OIDC_GITLAB_CLIENT_SECRET": "d",
		"HUB_OIDC_GITLAB_SCOPES": "openid,email",
	}
	c, err := Load(env(base))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.OIDC) != 2 || c.OIDC[0].ID != "gitlab" || c.OIDC[0].Name != "gitlab" || len(c.OIDC[0].Scopes) != 2 || c.OIDC[1].Name != "Корпоративный вход" {
		t.Errorf("oidc: %+v", c.OIDC)
	}
	base["HUB_EDITION"] = "cloud"
	base["HUB_ORGS"], base["HUB_ACCESS_ENFORCE"], base["HUB_RESOURCE_ACCESS"] = "on", "on", "on"
	base["HUB_LOCAL_CODE_FILE"] = "/tmp/code"
	// The cloud runs machines of people who do not trust each other: each on an origin of its own, or not at all.
	if _, err := Load(env(base)); err == nil || !strings.Contains(err.Error(), "HUB_RUNTIME_DOMAIN") {
		t.Errorf("cloud without a runtime domain: %v", err)
	}
	base["HUB_RUNTIME_DOMAIN"] = "m.example.net"
	c, err = Load(env(base))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.OIDC) != 0 || c.LocalCodeFile != "" {
		t.Errorf("cloud keeps %+v / %q", c.OIDC, c.LocalCodeFile)
	}
}

func TestLoadErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"no public url":       {map[string]string{}, "HUB_PUBLIC_URL is required"},
		"public url path":     {map[string]string{"HUB_PUBLIC_URL": "https://h.example.com/api"}, "path of the hub itself"},
		"public url scheme":   {map[string]string{"HUB_PUBLIC_URL": "ftp://h.example.com"}, "http(s)://host"},
		"edition":             {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_EDITION": "saas"}, "HUB_EDITION"},
		"signup":              {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_SIGNUP": "closed"}, "HUB_SIGNUP"},
		"database":            {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_DATABASE_URL": "mysql://x"}, "HUB_DATABASE_URL"},
		"half github":         {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_GITHUB_CLIENT_ID": "x"}, "go together"},
		"half yandex":         {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_YANDEX_CLIENT_SECRET": "x"}, "go together"},
		"oidc incomplete":     {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_OIDC_PROVIDERS": "kc"}, "HUB_OIDC_KC_ISSUER"},
		"oidc id":             {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_OIDC_PROVIDERS": "my-kc"}, "[a-z0-9]+"},
		"proxies":             {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_TRUSTED_PROXIES": "banana"}, "HUB_TRUSTED_PROXIES"},
		"smtp":                {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_SMTP_URL": "http://mail"}, "HUB_SMTP_URL"},
		"runtime domain":      {map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_RUNTIME_DOMAIN": "hub.example.com"}, "differ"},
		"runtime domain bad":  {map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_RUNTIME_DOMAIN": "not a domain"}, "HUB_RUNTIME_DOMAIN"},
		"mr source no groups": {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_MR_SOURCE_GITLAB_URL": "https://git.example.com", "HUB_MR_SOURCE_GITLAB_TOKEN": "t"}, "HUB_MR_SOURCE_GITLAB_GROUPS"},
		"mr source no token":  {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_MR_SOURCE_GITLAB_URL": "https://git.example.com", "HUB_MR_SOURCE_GITLAB_GROUPS": "g"}, "HUB_MR_SOURCE_GITLAB_TOKEN"},
		"mr source bad url":   {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_MR_SOURCE_GITLAB_URL": "git.example.com"}, "HUB_MR_SOURCE_GITLAB_URL"},
		"machine trust":       {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_MACHINE_TRUST": "maybe"}, "HUB_MACHINE_TRUST"},
		"oidc email trusted":  {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_OIDC_EMAIL_TRUSTED": "maybe"}, "HUB_OIDC_EMAIL_TRUSTED"},
		"metrics listen":      {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_METRICS_LISTEN": "8042"}, "HUB_METRICS_LISTEN"},
		"metrics on public":   {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_LISTEN": "0.0.0.0:8040", "HUB_METRICS_LISTEN": "0.0.0.0:8040"}, "HUB_METRICS_LISTEN"},
		"missing file":        {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_SMTP_URL_FILE": "/nonexistent/x"}, "HUB_SMTP_URL_FILE"},
	} {
		_, err := Load(env(tc.env))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

func TestAdminEmails(t *testing.T) {
	c, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_ADMIN_EMAILS": " Root@Example.com, ,b@x.io"}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsAdminEmail("root@example.com") || !c.IsAdminEmail(" B@X.io ") || c.IsAdminEmail("c@x.io") {
		t.Errorf("%v", c.AdminEmails)
	}
}

func TestRuntimeHost(t *testing.T) {
	c, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_SMTP_URL": "smtp://mail.example.com", "HUB_RUNTIME_DOMAIN": "m.example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.RuntimeHost("abc"); got != "abc.m.example.com" {
		t.Errorf("RuntimeHost = %q", got)
	}
	dev, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://localhost:18500", "HUB_RUNTIME_DOMAIN": "m.localhost"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := dev.RuntimeHost("abc"); got != "abc.m.localhost:18500" {
		t.Errorf("RuntimeHost with a port = %q", got)
	}
	for host, want := range map[string]string{
		"abc.m.example.com": "abc", "ABC.M.EXAMPLE.COM:443": "abc", "m.example.com": "", "a.b.m.example.com": "",
		"hub.example.com": "", "abc.m.example.com.evil.net": "", "": "",
	} {
		if got := c.MachineIDFromHost(host); got != want {
			t.Errorf("MachineIDFromHost(%q) = %q, want %q", host, got, want)
		}
	}
	if (&Config{}).MachineIDFromHost("abc.m.example.com") != "" {
		t.Error("path mode has no machine hosts")
	}
}

func TestLocalCodeGroup(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040", "HUB_LOCAL_CODE_FILE": "/run/harnessly/local-code"}
	with := func(group string) (*Config, error) {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		if group != "" {
			m["HUB_LOCAL_CODE_GROUP"] = group
		}
		return Load(env(m))
	}
	if c, err := with(""); err != nil || c.LocalCodeGID != -1 {
		t.Errorf("unset: %v %v", c, err)
	}
	if c, err := with("10001"); err != nil || c.LocalCodeGID != 10001 {
		t.Errorf("by id: %v %v", c, err)
	}
	if c, err := with("root"); err != nil || c.LocalCodeGID != 0 {
		t.Errorf("by name: %v %v", c, err)
	}
	for _, bad := range []string{"-5", "no-such-group-here", "1e3"} {
		if _, err := with(bad); err == nil || !strings.Contains(err.Error(), "HUB_LOCAL_CODE_GROUP") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// With no SMTP server a sign-in code or an invitation link goes to the log, and a log is read by more people than the one
// who asked for the code. An https hub and the cloud edition refuse to start that way unless the operator accepts it.
func TestMailInTheLogIsRefusedWhereTheLogIsShared(t *testing.T) {
	for name, tc := range map[string]struct {
		env     map[string]string
		refused bool
	}{
		"https":              {map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com"}, true},
		"cloud on http":      {map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040", "HUB_EDITION": "cloud", "HUB_RUNTIME_DOMAIN": "m.localhost"}, true},
		"https and smtp":     {map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_SMTP_URL": "smtps://mail.example.com"}, false},
		"https and override": {map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_MAIL_LOG": "allow"}, false},
		"plain http host":    {map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040"}, false},
	} {
		c, err := Load(env(tc.env))
		if tc.refused {
			if err == nil || !strings.Contains(err.Error(), "HUB_SMTP_URL is required") {
				t.Errorf("%s: %v, want a refusal", name, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if c.MailLogRefused() {
			t.Errorf("%s: MailLogRefused", name)
		}
	}
	if c, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_MAIL_LOG": "ALLOW"})); err != nil || !c.MailLogAllowed {
		t.Errorf("the override: %v %+v", err, c)
	}
	if _, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040", "HUB_MAIL_LOG": "yes"})); err == nil || !strings.Contains(err.Error(), "HUB_MAIL_LOG") {
		t.Errorf("a value that is not allow: %v", err)
	}
	// the refusal is part of the configuration, so check-config and serve both stop on it
	if (&Config{Secure: true}).MailLogRefused() != true || (&Config{Secure: true, MailLogAllowed: true}).MailLogRefused() {
		t.Error("MailLogRefused")
	}
}

func TestRuntimeDomainOfItsOwn(t *testing.T) {
	load := func(hub, runtime string, extra ...string) (*Config, error) {
		m := map[string]string{"HUB_PUBLIC_URL": hub, "HUB_RUNTIME_DOMAIN": runtime, "HUB_SMTP_URL": "smtp://mail.example.com",
			"HUB_ORGS": "on", "HUB_ACCESS_ENFORCE": "on", "HUB_RESOURCE_ACCESS": "on"}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return Load(env(m))
	}
	// another registrable domain: nothing to say
	if c, err := load("https://app.harnessly.ru", "run.harnessly-run.ru", "HUB_EDITION", "cloud"); err != nil || len(c.Warnings) != 0 {
		t.Errorf("a domain of its own: %v %v", err, c)
	}
	// the same one: a warning for a self-hosted hub...
	c, err := load("https://app.harnessly.ru", "run.harnessly.ru")
	if err != nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "one registrable domain") {
		t.Errorf("same site, self-hosted: %v %v", err, c)
	}
	// ...a refusal for the cloud...
	if _, err := load("https://app.harnessly.ru", "run.harnessly.ru", "HUB_EDITION", "cloud"); err == nil || !strings.Contains(err.Error(), "one registrable domain") || !strings.Contains(err.Error(), "HUB_ALLOW_SAME_SITE_RUNTIME") {
		t.Errorf("same site, cloud: %v", err)
	}
	// ...unless the operator says it knows
	c, err = load("https://app.harnessly.ru", "run.harnessly.ru", "HUB_EDITION", "cloud", "HUB_ALLOW_SAME_SITE_RUNTIME", "1")
	if err != nil || !c.AllowSameSiteRuntime || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "HUB_ALLOW_SAME_SITE_RUNTIME is set") {
		t.Errorf("same site, cloud, accepted: %v %v", err, c)
	}
	if _, err := load("https://app.harnessly.ru", "run.harnessly-run.ru", "HUB_ALLOW_SAME_SITE_RUNTIME", "maybe"); err == nil || !strings.Contains(err.Error(), "HUB_ALLOW_SAME_SITE_RUNTIME") {
		t.Errorf("a value that is not a flag: %v", err)
	}
	// the public suffix list decides what a registrable domain is: github.io and co.uk are suffixes, not owners
	for a, b := range map[string]string{"a.github.io": "b.github.io", "hub.example.co.uk": "m.other.co.uk"} {
		if sameSite(a, b) {
			t.Errorf("%s and %s are different sites", a, b)
		}
	}
	for a, b := range map[string]string{"app.harnessly.ru": "run.harnessly.ru", "a.b.example.co.uk": "c.example.co.uk"} {
		if !sameSite(a, b) {
			t.Errorf("%s and %s are one site", a, b)
		}
	}
	// addresses and localhost have no registrable domain: development setups are left alone
	if c, err := load("http://localhost:8040", "m.localhost"); err != nil || len(c.Warnings) != 0 {
		t.Errorf("localhost: %v %v", err, c)
	}
	if c, err := load("http://127.0.0.1:8040", "m.example.test"); err != nil || len(c.Warnings) != 0 {
		t.Errorf("an address: %v %v", err, c)
	}
	// a runtime domain above the hub's own host would make every request of the hub a machine's
	if _, err := load("https://app.example.com", "example.com"); err == nil || !strings.Contains(err.Error(), "parent of the hub's own host") {
		t.Errorf("a parent domain: %v", err)
	}
	loose := &Config{RuntimeDomain: "example.com", Host: "app.example.com"}
	if got := loose.MachineIDFromHost("app.example.com"); got != "" {
		t.Errorf("the hub's host read as machine %q", got)
	}
	if got := loose.MachineIDFromHost("abc.example.com"); got != "abc" {
		t.Errorf("a machine host: %q", got)
	}
}

func TestSitesConfig(t *testing.T) {
	load := func(hub string, kv ...string) (*Config, error) {
		m := map[string]string{"HUB_PUBLIC_URL": hub, "HUB_SMTP_URL": "smtp://mail.example.com",
			"HUB_ORGS": "on", "HUB_ACCESS_ENFORCE": "on", "HUB_RESOURCE_ACCESS": "on"}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return Load(env(m))
	}
	// self-hosted without a domain: the path fallback; with one: subdomains; off on request
	c, err := load("https://hub.example.com")
	if err != nil || c.SitesMode() != "path" || c.SitesDir != "/var/lib/harnessly-hub/sites" {
		t.Errorf("default: %v %v", err, c)
	}
	if c, err = load("https://hub.example.com", "HARNESSLY_SITES_DOMAIN", "Sites.Example.NET."); err != nil || c.SitesMode() != "subdomain" || c.SitesDomain != "sites.example.net" || len(c.Warnings) != 0 {
		t.Errorf("own domain: %v %v", err, c)
	}
	if c, err = load("https://hub.example.com", "HARNESSLY_SITES", "off"); err != nil || c.SitesMode() != "off" {
		t.Errorf("off: %v %v", err, c)
	}
	// the cloud has no path fallback: a stranger's page does not run on the hub's origin
	if c, err = load("https://app.harnessly.ru", "HUB_EDITION", "cloud", "HUB_RUNTIME_DOMAIN", "run.harnessly-run.ru"); err != nil || c.SitesMode() != "off" {
		t.Errorf("cloud without a domain: %v %v", err, c)
	}
	// the same registrable domain as the hub: a warning, a refusal in the cloud
	c, err = load("https://app.harnessly.ru", "HARNESSLY_SITES_DOMAIN", "sites.harnessly.ru")
	if err != nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "HARNESSLY_SITES_DOMAIN") {
		t.Errorf("same site: %v %v", err, c)
	}
	if _, err = load("https://app.harnessly.ru", "HARNESSLY_SITES_DOMAIN", "sites.harnessly.ru", "HUB_EDITION", "cloud", "HUB_RUNTIME_DOMAIN", "run.harnessly-run.ru"); err == nil || !strings.Contains(err.Error(), "one registrable domain") {
		t.Errorf("same site, cloud: %v", err)
	}
	// a parent of the hub's host, the hub's host itself, the runtime domain and its relatives
	for _, d := range []string{"example.com", "hub.example.com"} {
		if _, err = load("https://hub.example.com", "HARNESSLY_SITES_DOMAIN", d); err == nil {
			t.Errorf("%s must be refused", d)
		}
	}
	if _, err = load("https://hub.example.com", "HUB_RUNTIME_DOMAIN", "run.other.net", "HARNESSLY_SITES_DOMAIN", "x.run.other.net"); err == nil || !strings.Contains(err.Error(), "HUB_RUNTIME_DOMAIN") {
		t.Errorf("under the runtime domain: %v", err)
	}
	if _, err = load("https://hub.example.com", "HARNESSLY_SITES_DOMAIN", "not a domain"); err == nil {
		t.Error("not a domain")
	}
	// the port of a development hub goes on the sites host
	c, err = load("http://localhost:8040", "HARNESSLY_SITES_DOMAIN", "sites.localhost")
	if err != nil || c.SitesHost() != "sites.localhost:8040" || len(c.Warnings) != 0 {
		t.Errorf("localhost: %v %v", err, c)
	}
	// a bucket needs its keys
	if _, err = load("https://hub.example.com", "HARNESSLY_SITES_S3_ENDPOINT", "https://s3.example.com"); err == nil {
		t.Error("an endpoint without a bucket and keys")
	}
	if _, err = load("https://hub.example.com", "HARNESSLY_SITES_ABUSE_URL", "javascript:alert(1)"); err == nil {
		t.Error("abuse url scheme")
	}
}

func TestProxyJWT(t *testing.T) {
	load := func(env map[string]string) (*Config, error) {
		env["HUB_PUBLIC_URL"] = "https://hub.example.com"
		env["HUB_DATA_DIR"] = t.TempDir()
		env["HUB_MAIL_LOG"] = "allow"
		return Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	}
	c, err := load(map[string]string{"HUB_PROXY_JWT_JWKS_URL": "https://tl.example.com/.well-known/jwks.json", "HUB_PROXY_JWT_AUDIENCE": "gw", "HUB_PROXY_JWT_HOST": "AI.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !c.ProxyJWT.On() || c.ProxyJWT.Host != "ai.example.com" || c.ProxyJWT.Header != "Teleport-Jwt-Assertion" || c.ProxyJWT.EmailClaim != "sub" {
		t.Errorf("proxy jwt %+v", c.ProxyJWT)
	}
	for name, env := range map[string]map[string]string{
		"no host":   {"HUB_PROXY_JWT_JWKS_URL": "https://tl.example.com/jwks", "HUB_PROXY_JWT_AUDIENCE": "gw"},
		"hub's own": {"HUB_PROXY_JWT_JWKS_URL": "https://tl.example.com/jwks", "HUB_PROXY_JWT_AUDIENCE": "gw", "HUB_PROXY_JWT_HOST": "hub.example.com"},
		"not a url": {"HUB_PROXY_JWT_JWKS_URL": "tl.example.com", "HUB_PROXY_JWT_AUDIENCE": "gw", "HUB_PROXY_JWT_HOST": "ai.example.com"},
	} {
		if _, err := load(env); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	c, err = load(map[string]string{"HUB_EDITION": "cloud", "HUB_PROXY_JWT_JWKS_URL": "https://tl.example.com/jwks", "HUB_PROXY_JWT_AUDIENCE": "gw", "HUB_PROXY_JWT_HOST": "ai.example.com", "HUB_SMTP_URL": "smtp://mail.example.com:25"})
	if err == nil && c.ProxyJWT.On() {
		t.Error("the cloud takes a gateway's assertions")
	}
}

func TestMachinesConfig(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040"}
	load := func(extra map[string]string) (*Config, error) {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		for k, v := range extra {
			env[k] = v
		}
		return Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	}
	c, err := load(nil)
	if err != nil || c.Machines.Provider != "none" || c.Machines.AccessEnforce != AccessOff || c.Machines.Resources {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c, err = load(map[string]string{"HUB_PROVIDER_URL": "https://p.example.org", "HUB_PROVIDER_TOKEN_FILE": "/run/p/token", "HUB_ACCESS_ENFORCE": "WARN", "HUB_MACHINE_RESOURCES": "ON"})
	if err != nil || c.Machines.Provider != "http" || c.Machines.ProviderTokenFile != "/run/p/token" || c.Machines.AccessEnforce != AccessWarn || !c.Machines.Resources {
		t.Fatalf("http: %+v %v", c, err)
	}
	for _, bad := range []map[string]string{{"HUB_PROVIDER": "http"}, {"HUB_PROVIDER": "cloud"}, {"HUB_ACCESS_ENFORCE": "strict"}, {"HUB_MACHINE_RESOURCES": "1"}} {
		if _, err := load(bad); err == nil {
			t.Errorf("%v must be refused", bad)
		}
	}
}

func TestAgentURLDefaultsToThePublicOne(t *testing.T) {
	c, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "https://ai.example.com", "HUB_SMTP_URL": "smtp://mail.example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.AgentURL != "https://ai.example.com" || c.AgentHost != "ai.example.com" || !c.AgentSecure {
		t.Errorf("default agent url: %q %q %v", c.AgentURL, c.AgentHost, c.AgentSecure)
	}
	c, err = Load(env(map[string]string{"HUB_PUBLIC_URL": "https://ai.example.com", "HUB_SMTP_URL": "smtp://mail.example.com",
		"HUB_AGENT_URL": "http://Hub.Example.com:8040/", "HUB_RUNTIME_DOMAIN": "m.example.net"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.AgentURL != "http://Hub.Example.com:8040" || c.AgentHost != "hub.example.com:8040" || c.AgentSecure || c.PublicURL != "https://ai.example.com" {
		t.Errorf("agent url: %q %q %v", c.AgentURL, c.AgentHost, c.AgentSecure)
	}
	// A machine's own origin carries the machines' port, and neither of the hub's hosts is one.
	if got := c.RuntimeHost("abc"); got != "abc.m.example.net:8040" {
		t.Errorf("RuntimeHost = %q", got)
	}
	if c.MachineIDFromHost("hub.example.com:8040") != "" || c.MachineIDFromHost("ai.example.com") != "" {
		t.Error("a hub host is no machine")
	}
}

func TestFeatureLists(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "https://h.example.com", "HUB_SMTP_URL": "smtp://mail.example.com"}
	with := func(kv ...string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	c, err := Load(env(with("HUB_FEATURES_FORCE", " Env, -org.events ", "HUB_FEATURES_DEFAULT", "ui.hx")))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Features.Force) != 2 || !c.Features.Force["env"] || c.Features.Force["org.events"] || !c.Features.Default["ui.hx"] {
		t.Errorf("features: %+v", c.Features)
	}
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"feature name":    {with("HUB_FEATURES_FORCE", "Bad Name"), "not a feature name"},
		"agent url query": {with("HUB_AGENT_URL", "https://a.example.com/x?y=1"), "HUB_AGENT_URL"},
		"agent url bogus": {with("HUB_AGENT_URL", "nope"), "HUB_AGENT_URL"},
	} {
		if _, err := Load(env(tc.env)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

func TestLoadMRSource(t *testing.T) {
	dir := t.TempDir()
	tok := filepath.Join(dir, "tok")
	if err := os.WriteFile(tok, []byte("glpat-x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_MR_SOURCE_GITLAB_URL": "https://git.example.com/",
		"HUB_MR_SOURCE_GITLAB_GROUPS": " a/b, c ,,", "HUB_MR_SOURCE_GITLAB_TOKEN_FILE": tok}))
	if err != nil {
		t.Fatal(err)
	}
	if c.MRSourceURL != "https://git.example.com" || len(c.MRSourceGroups) != 2 || c.MRSourceGroups[0] != "a/b" || c.MRSourceGroups[1] != "c" || c.MRSourceToken != "glpat-x" {
		t.Errorf("%+v", c)
	}
	if c, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://h"})); err != nil || c.MRSourceURL != "" {
		t.Errorf("off by default: %v %+v", err, c)
	}
}

func TestPublicURLWithAPath(t *testing.T) {
	smtp := map[string]string{"HUB_SMTP_URL": "smtp://mail.example.com"}
	load := func(url string) (*Config, error) {
		m := map[string]string{"HUB_PUBLIC_URL": url}
		for k, v := range smtp {
			m[k] = v
		}
		return Load(env(m))
	}
	c, err := load("https://Example.com/harnessly/")
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL != "https://Example.com/harnessly" || c.Origin != "https://Example.com" || c.BasePath != "/harnessly" || c.Host != "example.com" {
		t.Errorf("urls: %q %q %q %q", c.PublicURL, c.Origin, c.BasePath, c.Host)
	}
	if c.CookiePath() != "/harnessly" || c.CookiePrefix() != "__Secure-" {
		t.Errorf("cookies: %q %q", c.CookiePath(), c.CookiePrefix())
	}
	root, err := load("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if root.BasePath != "" || root.CookiePath() != "/" || root.CookiePrefix() != "__Host-" || root.PublicURL != root.Origin {
		t.Errorf("at the root nothing changes: %+v", root)
	}
	for _, bad := range []string{"https://example.com/api", "https://example.com/m/x", "https://example.com/a//b", "https://example.com/a/../b",
		"https://example.com/h?x=1", "https://example.com/h#top", "https://example.com/a%2Fb", "https://example.com/a b"} {
		if _, err := load(bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func TestTrustSwitchesAreOffByDefault(t *testing.T) {
	c, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://h"}))
	if err != nil || c.MachineTrust || c.OIDCEmailTrusted {
		t.Fatalf("defaults: %v %+v", err, c)
	}
	c, err = Load(env(map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_MACHINE_TRUST": "on", "HUB_OIDC_EMAIL_TRUSTED": "on"}))
	if err != nil || !c.MachineTrust || !c.OIDCEmailTrusted {
		t.Fatalf("on: %v %+v", err, c)
	}
}

func TestAuthEmailOff(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_AUTH_EMAIL": "off"}
	// No other way in: refused.
	if _, err := Load(env(base)); err == nil || !strings.Contains(err.Error(), "no way to sign in") {
		t.Fatalf("no provider: %v", err)
	}
	// A GitHub sign-in and no SMTP: starts on https, the log is not a mailbox.
	m := map[string]string{}
	for k, v := range base {
		m[k] = v
	}
	m["HUB_GITHUB_CLIENT_ID"], m["HUB_GITHUB_CLIENT_SECRET"] = "id", "secret"
	c, err := Load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if !c.AuthEmailOff || c.MailLogRefused() {
		t.Errorf("off: %+v", c)
	}
	// Default: the code sign-in stays and an https hub still needs SMTP.
	if _, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com"})); err == nil {
		t.Error("https without SMTP must still be refused by default")
	}
	if _, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_AUTH_EMAIL": "maybe"})); err == nil {
		t.Error("HUB_AUTH_EMAIL=maybe")
	}
}

func TestStorageS3(t *testing.T) {
	m := map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040"}
	c, err := Load(env(m))
	if err != nil || c.StorageS3() || c.SitesS3.Endpoint != "" {
		t.Fatalf("default is the disk: %v %+v", err, c)
	}
	m["HUB_STORAGE"] = "s3"
	if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "HUB_S3_ENDPOINT") {
		t.Fatalf("s3 without a bucket: %v", err)
	}
	m["HUB_S3_ENDPOINT"], m["HUB_S3_BUCKET"], m["HUB_S3_ACCESS_KEY"], m["HUB_S3_SECRET_KEY"], m["HUB_S3_PREFIX"] = "http://minio:9000", "hub", "k", "s", "p/"
	c, err = Load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if !c.StorageS3() || c.SitesS3.Endpoint != "http://minio:9000" || c.SitesS3.Bucket != "hub" || c.SitesS3.Prefix != "p/sites/" {
		t.Errorf("sites follow the one setting: %+v", c.SitesS3)
	}
	// The sites' own settings win over it.
	m["HARNESSLY_SITES_S3_ENDPOINT"], m["HARNESSLY_SITES_S3_BUCKET"], m["HARNESSLY_SITES_S3_ACCESS_KEY"], m["HARNESSLY_SITES_S3_SECRET_KEY"] = "http://other:9000", "s", "a", "b"
	if c, err = Load(env(m)); err != nil || c.SitesS3.Endpoint != "http://other:9000" {
		t.Errorf("explicit sites bucket: %v %+v", err, c.SitesS3)
	}
	m["HUB_STORAGE"] = "tape"
	if _, err := Load(env(m)); err == nil {
		t.Error("HUB_STORAGE=tape")
	}
}

func TestReplicas(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040"}
	with := func(kv ...string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	c, err := Load(env(with()))
	if err != nil || c.Replicas {
		t.Fatalf("default: %v %v", c.Replicas, err)
	}
	c, err = Load(env(with("HUB_REPLICAS", "on", "HUB_REPLICA_ADDR", "http://10.0.0.5:8040/", "HUB_REPLICA_SECRET", "secretsecretsecret")))
	if err != nil || !c.Replicas || c.ReplicaAddr != "http://10.0.0.5:8040" {
		t.Fatalf("on: %+v %v", c, err)
	}
	if c.ReplicaListen != "0.0.0.0:8041" {
		t.Errorf("the replicas' listener by default: %q", c.ReplicaListen)
	}
	for name, m := range map[string]map[string]string{
		"no address":   with("HUB_REPLICAS", "on", "HUB_REPLICA_SECRET", "secretsecretsecret"),
		"short secret": with("HUB_REPLICAS", "on", "HUB_REPLICA_ADDR", "http://10.0.0.5:8040", "HUB_REPLICA_SECRET", "short"),
		"bad value":    with("HUB_REPLICAS", "maybe"),
		"one listener": with("HUB_REPLICAS", "on", "HUB_REPLICA_ADDR", "http://10.0.0.5:8041", "HUB_REPLICA_SECRET", "secretsecretsecret", "HUB_LISTEN", "0.0.0.0:8041"),
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestLoadSafe(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "http://h"}
	c, err := Load(env(base))
	if err != nil || c.SafeVault != nil {
		t.Fatalf("no address, no safe: %+v %v", c.SafeVault, err)
	}
	with := func(kv ...string) map[string]string {
		m := map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_SECRETS_VAULT_ADDR": "https://openbao.example.com"}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	c, err = Load(env(with("HUB_SECRETS_VAULT_AUTH", "kubernetes", "HUB_SECRETS_VAULT_ROLE", "hub", "HUB_SECRETS_REF_PREFIXES", " /shared/ci/ ,,team")))
	if err != nil {
		t.Fatal(err)
	}
	if c.SafeVault == nil || c.SafeVault.Auth.Method() != "kubernetes" || c.SafePrefix != "harnessly" || strings.Join(c.SafeRefPrefixes, ",") != "shared/ci,team" {
		t.Fatalf("safe: %+v %q %v", c.SafeVault, c.SafePrefix, c.SafeRefPrefixes)
	}
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"no token":   {with(), "HUB_SECRETS_VAULT_TOKEN_FILE"},
		"bad auth":   {with("HUB_SECRETS_VAULT_AUTH", "ldap"), "HUB_SECRETS_VAULT_AUTH"},
		"bad prefix": {with("HUB_SECRETS_VAULT_TOKEN_FILE", "/x", "HUB_SECRETS_VAULT_PREFIX", "a/../b"), "HUB_SECRETS_VAULT_PREFIX"},
	} {
		if _, err := Load(env(tc.env)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

// The store reuses the hub's database and bucket; its own settings only point it elsewhere.
func TestStore(t *testing.T) {
	data := t.TempDir()
	m := map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040", "HUB_DATA_DIR": data}
	c, err := Load(env(m))
	if err != nil || c.Store.Off || c.Store.DatabaseURL != "sqlite://"+filepath.Join(data, "store") || c.Store.S3.Endpoint != "" || c.Store.PGPrefix != "" {
		t.Fatalf("default: %v %+v", err, c.Store)
	}
	if c.Store.MaxUpload != 0 {
		t.Fatalf("an upload limit by default: %d", c.Store.MaxUpload)
	}
	m["HUB_STORE_MAX_UPLOAD_MB"] = "2048"
	if c, err = Load(env(m)); err != nil || c.Store.MaxUpload != 2048<<20 {
		t.Fatalf("an upload limit: %v %d", err, c.Store.MaxUpload)
	}
	m["HUB_STORE_MAX_UPLOAD_MB"] = "-1"
	if _, err := Load(env(m)); err == nil {
		t.Fatal("a negative upload limit was taken")
	}
	delete(m, "HUB_STORE_MAX_UPLOAD_MB")
	m["HUB_DATABASE_URL"], m["HUB_DB_MIGRATE_URL"] = "postgres://u:p@pooler/hub", "postgres://u:p@db/hub"
	if c, err = Load(env(m)); err != nil || c.Store.DatabaseURL != "postgres://u:p@db/hub" || c.Store.PGPrefix != "filestore_" {
		t.Fatalf("the hub's Postgres, directly: %v %+v", err, c.Store)
	}
	m["HUB_STORE_DATABASE_URL"] = "postgres://u:p@center/design"
	if c, err = Load(env(m)); err != nil || c.Store.DatabaseURL != "postgres://u:p@center/design" || c.Store.PGPrefix != "" {
		t.Fatalf("a center's database as it is: %v %+v", err, c.Store)
	}
	m["HUB_STORE_S3_PREFIX"] = "design-blobs/"
	if _, err := Load(env(m)); err == nil {
		t.Fatal("a store prefix without HUB_STORAGE=s3")
	}
	// A center's own bucket and keys: the hub's files stay on its disk.
	own := map[string]string{"HUB_STORE_S3_ENDPOINT": "https://s3.ru-3.storage.selcloud.ru", "HUB_STORE_S3_REGION": "ru-3",
		"HUB_STORE_S3_BUCKET": "acme-docs-boards", "HUB_STORE_S3_ACCESS_KEY": "ak", "HUB_STORE_S3_SECRET_KEY": "sk"}
	for k, v := range own {
		m[k] = v
	}
	if c, err = Load(env(m)); err != nil || c.StorageS3() || c.SitesS3.Endpoint != "" || c.Store.S3.Bucket != "acme-docs-boards" ||
		c.Store.S3.Prefix != "design-blobs/" || c.Store.S3.Region != "ru-3" || c.Store.S3.SecretKey != "sk" {
		t.Fatalf("the store's own bucket: %v %+v", err, c.Store.S3)
	}
	delete(m, "HUB_STORE_S3_SECRET_KEY")
	if _, err := Load(env(m)); err == nil {
		t.Fatal("the store's own bucket without its keys")
	}
	for k := range own {
		delete(m, k)
	}
	m["HUB_STORAGE"], m["HUB_S3_ENDPOINT"], m["HUB_S3_BUCKET"], m["HUB_S3_ACCESS_KEY"], m["HUB_S3_SECRET_KEY"], m["HUB_S3_PREFIX"] = "s3", "http://minio:9000", "hub", "k", "s", "p/"
	if c, err = Load(env(m)); err != nil || c.Store.S3.Bucket != "hub" || c.Store.S3.Prefix != "design-blobs/" || c.Store.S3.AccessKey != "k" {
		t.Fatalf("the hub's bucket: %v %+v", err, c.Store.S3)
	}
	delete(m, "HUB_STORE_S3_PREFIX")
	if c, err = Load(env(m)); err != nil || c.Store.S3.Prefix != "p/store/" {
		t.Fatalf("the default prefix: %v %+v", err, c.Store.S3)
	}
	for k, v := range map[string]string{"HUB_STORE": "maybe", "HUB_STORE_DATABASE_URL": "mysql://x"} {
		bad := map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040", "HUB_DATA_DIR": data, k: v}
		if _, err := Load(env(bad)); err == nil {
			t.Errorf("%s=%s was taken", k, v)
		}
	}
	m["HUB_STORE"] = "off"
	if c, err = Load(env(m)); err != nil || !c.Store.Off {
		t.Fatalf("off: %v %+v", err, c.Store)
	}
}

func TestDesignImport(t *testing.T) {
	m := map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040", "HUB_DATA_DIR": t.TempDir()}
	if c, err := Load(env(m)); err != nil || c.DesignImport.Mode != "" {
		t.Fatalf("off by default: %v %+v", err, c.DesignImport)
	}
	m["HUB_DESIGN_IMPORT_GATEWAY"] = "apply"
	if _, err := Load(env(m)); err == nil {
		t.Fatal("apply without the gateway's database or a space")
	}
	m["HUB_DESIGN_IMPORT_GATEWAY_URL"] = "postgres://u:p@gateway/llm"
	m["HUB_STORE_LEGACY_SPACE"] = "acme-team"
	m["HUB_DESIGN_IMPORT_TEAMS"] = "acme=ge34, harnessly=ufpe"
	m["HUB_DESIGN_IMPORT_MACHINES"] = "ann@example.org@vm-1=6kj6"
	c, err := Load(env(m))
	if err != nil || c.DesignImport.Mode != "apply" || c.DesignImport.Space != "acme-team" || c.DesignImport.Teams["harnessly"] != "ufpe" ||
		c.DesignImport.Machines["ann@example.org@vm-1"] != "6kj6" {
		t.Fatalf("apply: %v %+v", err, c.DesignImport)
	}
	m["HUB_DESIGN_IMPORT_GATEWAY"] = "yes"
	if _, err := Load(env(m)); err == nil {
		t.Fatal("an unknown mode")
	}
}

func TestTunnelMemory(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_SMTP_URL": "smtp://mail.example.com"}
	c, err := Load(env(base))
	if err != nil || c.TunnelMemory != 256<<20 {
		t.Fatalf("default: %d %v", c.TunnelMemory, err)
	}
	base["HUB_TUNNEL_MEMORY_MB"] = "64"
	if c, err = Load(env(base)); err != nil || c.TunnelMemory != 64<<20 {
		t.Fatalf("set: %d %v", c.TunnelMemory, err)
	}
	for _, v := range []string{"0", "-5", "lots"} {
		base["HUB_TUNNEL_MEMORY_MB"] = v
		if _, err := Load(env(base)); err == nil || !strings.Contains(err.Error(), "HUB_TUNNEL_MEMORY_MB") {
			t.Errorf("%q: %v", v, err)
		}
	}
}

// The cloud edition does not start unless organizations, machine access lists and document access are all on; a
// self-hosted hub starts and says what is off.
func TestCloudNeedsIsolation(t *testing.T) {
	on := map[string]string{"HUB_ORGS": "on", "HUB_ACCESS_ENFORCE": "on", "HUB_RESOURCE_ACCESS": "on"}
	load := func(edition string, change func(m map[string]string)) (*Config, error) {
		m := map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_SMTP_URL": "smtp://mail.example.com",
			"HUB_RUNTIME_DOMAIN": "m.example.net", "HUB_EDITION": edition}
		for k, v := range on {
			m[k] = v
		}
		if change != nil {
			change(m)
		}
		return Load(env(m))
	}
	if c, err := load(EditionCloud, nil); err != nil || len(c.Warnings) != 0 {
		t.Fatalf("the cloud with all three on: %v %v", err, c)
	}
	for name, value := range map[string][]string{
		"HUB_ORGS":            {"", "off"},
		"HUB_ACCESS_ENFORCE":  {"", "off", "warn"},
		"HUB_RESOURCE_ACCESS": {"", "off"},
	} {
		for _, v := range value {
			change := func(m map[string]string) {
				if m[name] = v; v == "" {
					delete(m, name)
				}
			}
			if _, err := load(EditionCloud, change); err == nil || !strings.Contains(err.Error(), "HUB_EDITION=cloud needs "+name+"=on") {
				t.Errorf("the cloud with %s=%q: %v", name, v, err)
			}
			c, err := load(EditionSelfHosted, change)
			if err != nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], name+" is not on") {
				t.Errorf("self-hosted with %s=%q: %v %v", name, v, err, c)
			}
		}
	}
	// a self-hosted hub with none of them starts, with a warning for each
	c, err := load(EditionSelfHosted, func(m map[string]string) {
		for k := range on {
			delete(m, k)
		}
	})
	if err != nil || len(c.Warnings) != 3 {
		t.Errorf("self-hosted with none: %v %v", err, c)
	}
}

func TestQuotas(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040"}
	c, err := Load(env(base))
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxSpaces() != DefaultMaxSpaces || c.MaxOrgs() != DefaultMaxOrgs || c.MaxServers() != DefaultMaxServers {
		t.Errorf("defaults: %+v", c.Quotas)
	}
	var none *Config
	if none.MaxSpaces() != DefaultMaxSpaces || none.MaxOrgs() != DefaultMaxOrgs || none.MaxServers() != DefaultMaxServers {
		t.Error("a hub without a configuration has the defaults")
	}
	base["HUB_MAX_SPACES_PER_PERSON"], base["HUB_MAX_ORGS_PER_PERSON"], base["HUB_MAX_SERVERS_PER_SPACE"] = "3", "4", "5"
	if c, err = Load(env(base)); err != nil || c.MaxSpaces() != 3 || c.MaxOrgs() != 4 || c.MaxServers() != 5 {
		t.Errorf("set: %v %+v", err, c)
	}
	for _, name := range []string{"HUB_MAX_SPACES_PER_PERSON", "HUB_MAX_ORGS_PER_PERSON", "HUB_MAX_SERVERS_PER_SPACE"} {
		for _, v := range []string{"0", "-1", "many"} {
			m := map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040", name: v}
			if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("%s=%q: %v", name, v, err)
			}
		}
	}
}

func TestHistoryDeletedKeep(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_SMTP_URL": "smtp://mail.example.com"}
	for value, want := range map[string]time.Duration{"": 30 * 24 * time.Hour, "7": 7 * 24 * time.Hour, "90": 90 * 24 * time.Hour, "0": 0} {
		vars := map[string]string{"HUB_HISTORY_DELETED_DAYS": value}
		for k, v := range base {
			vars[k] = v
		}
		c, err := Load(env(vars))
		if err != nil || c.HistoryDeletedKeep != want {
			t.Errorf("HUB_HISTORY_DELETED_DAYS=%q: %v %v, want %v", value, c.HistoryDeletedKeep, err, want)
		}
	}
	for _, value := range []string{"-1", "a month", "1.5", "1", "6"} {
		vars := map[string]string{"HUB_HISTORY_DELETED_DAYS": value}
		for k, v := range base {
			vars[k] = v
		}
		if _, err := Load(env(vars)); err == nil {
			t.Errorf("HUB_HISTORY_DELETED_DAYS=%q was taken", value)
		}
	}
}

func TestLoadServicePublicURLs(t *testing.T) {
	c, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://h",
		"HUB_BOARDS_URL": "http://boards:8080/", "HUB_ARTIFACTS_URL": "http://artifacts:8080",
		"HUB_BOARDS_PUBLIC_URL": "https://boards.example.com/"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BoardsURL != "http://boards:8080" || c.BoardsPublicURL != "https://boards.example.com" {
		t.Errorf("boards: %q %q", c.BoardsURL, c.BoardsPublicURL)
	}
	// Without a public address the machines get the hub's own one.
	if c.ArtifactsPublicURL != "http://artifacts:8080" {
		t.Errorf("artifacts public: %q", c.ArtifactsPublicURL)
	}
	if _, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_BOARDS_PUBLIC_URL": "boards.example.com"})); err == nil ||
		!strings.Contains(err.Error(), "HUB_BOARDS_PUBLIC_URL") {
		t.Errorf("a public address without a scheme: %v", err)
	}
}
