package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		"no public url":      {map[string]string{}, "HUB_PUBLIC_URL is required"},
		"public url path":    {map[string]string{"HUB_PUBLIC_URL": "https://h.example.com/app"}, "without a path"},
		"public url scheme":  {map[string]string{"HUB_PUBLIC_URL": "ftp://h.example.com"}, "without a path"},
		"edition":            {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_EDITION": "saas"}, "HUB_EDITION"},
		"signup":             {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_SIGNUP": "closed"}, "HUB_SIGNUP"},
		"database":           {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_DATABASE_URL": "mysql://x"}, "HUB_DATABASE_URL"},
		"half github":        {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_GITHUB_CLIENT_ID": "x"}, "go together"},
		"half yandex":        {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_YANDEX_CLIENT_SECRET": "x"}, "go together"},
		"oidc incomplete":    {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_OIDC_PROVIDERS": "kc"}, "HUB_OIDC_KC_ISSUER"},
		"oidc id":            {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_OIDC_PROVIDERS": "my-kc"}, "[a-z0-9]+"},
		"proxies":            {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_TRUSTED_PROXIES": "banana"}, "HUB_TRUSTED_PROXIES"},
		"smtp":               {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_SMTP_URL": "http://mail"}, "HUB_SMTP_URL"},
		"runtime domain":     {map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_RUNTIME_DOMAIN": "hub.example.com"}, "differ"},
		"runtime domain bad": {map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_RUNTIME_DOMAIN": "not a domain"}, "HUB_RUNTIME_DOMAIN"},
		"missing file":       {map[string]string{"HUB_PUBLIC_URL": "http://h", "HUB_SMTP_URL_FILE": "/nonexistent/x"}, "HUB_SMTP_URL_FILE"},
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
		m := map[string]string{"HUB_PUBLIC_URL": hub, "HUB_RUNTIME_DOMAIN": runtime, "HUB_SMTP_URL": "smtp://mail.example.com"}
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
