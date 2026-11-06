package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/diag"
	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/sites"
)

// Verdicts of one check.
const (
	vOK   = "ok"
	vWarn = "warn"
	vFail = "FAIL"
	vSkip = "skip"
)

// check is one line of `hub doctor`: what was looked at, the verdict, and for a warning or a failure the problem with its code.
type check struct {
	name    string
	verdict string
	detail  string
	problem *diag.Problem
}

// runDoctor is `hub doctor`: it reads the configuration like the hub does and asks what the hub depends on whether it is
// well: the database and its migrations, the S3 bucket of the sites, the OpenID providers, the public address, the proxy
// in front of the hub, the clock, and the extension adapters (docs/hub/extensions.md). Every warning and failure carries a
// code, what to do and a link into docs/hub/doctor.md. `--offline` skips what needs the public address to answer.
//
// It exits 0 when nothing failed (warnings do not fail it), 1 when a check failed, 2 when the configuration cannot be read.
func runDoctor(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	offline := false
	for _, a := range args {
		if a != "--offline" {
			fmt.Fprintf(stderr, "hub doctor takes no arguments except --offline, got %q\n", a)
			return 2
		}
		offline = true
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		fmt.Fprint(stderr, diag.Describe(configProblem(err)))
		return 2
	}
	fmt.Fprintln(stdout, "configuration  ok")

	checks := []check{checkAddress(cfg), checkDatabase(ctx, cfg), checkS3(ctx, cfg), checkOIDC(ctx, cfg), checkProxy(cfg)}
	if offline {
		checks = append(checks, check{name: "public address", verdict: vSkip, detail: "--offline"}, check{name: "clock", verdict: vSkip, detail: "--offline"})
	} else {
		reach, clock := checkPublic(ctx, cfg)
		checks = append(checks, reach, clock)
	}
	failed := false
	for _, c := range checks {
		printCheck(stdout, c)
		failed = failed || c.verdict == vFail
	}

	code, extFailed, err := doctorExtensions(ctx, cfg, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if failed || extFailed {
		return 1
	}
	return code
}

func printCheck(w io.Writer, c check) {
	fmt.Fprintf(w, "%-14s %s", c.name, c.verdict)
	if c.detail != "" {
		fmt.Fprintf(w, "  %s", c.detail)
	}
	fmt.Fprintln(w)
	if c.problem != nil {
		fmt.Fprintf(w, "               %s: %s\n", c.problem.Code, c.problem.Error())
		if c.problem.Fix != "" {
			fmt.Fprintf(w, "               what to do: %s\n", c.problem.Fix)
		}
		fmt.Fprintf(w, "               docs: %s\n", c.problem.Link())
	}
}

// configProblem gives a configuration error its code: the address variables have their own.
func configProblem(err error) *diag.Problem {
	if p := new(diag.Problem); errors.As(err, &p) {
		return p
	}
	msg := err.Error()
	if strings.Contains(msg, "HUB_PUBLIC_URL") || strings.Contains(msg, "HUB_AGENT_URL") {
		return &diag.Problem{Code: diag.AddressInvalid, What: msg, Fix: "set it to http(s)://host[:port][/path] as people open the hub, e.g. https://hub.example.com/harnessly"}
	}
	return &diag.Problem{Code: diag.ConfigInvalid, What: msg, Fix: "fix the variable the message names; `hub check-config` shows the result without starting the hub"}
}

func checkAddress(cfg *config.Config) check {
	c := check{name: "address", verdict: vOK, detail: cfg.PublicURL}
	if cfg.BasePath != "" && !cfg.SubdomainMode() {
		c.verdict = vWarn
		c.problem = &diag.Problem{Code: diag.BaseNoRuntime,
			What: "HUB_PUBLIC_URL has a path (" + cfg.BasePath + ") and there is no HUB_RUNTIME_DOMAIN: the pages of machines (/m/<id>) do not work under a path",
			Fix:  "set HUB_RUNTIME_DOMAIN to a domain with a wildcard certificate that reaches the hub, or serve the hub at the root of its host"}
	}
	return c
}

func checkDatabase(ctx context.Context, cfg *config.Config) check {
	c := check{name: "database", verdict: vOK, detail: dbKind(cfg)}
	if path := cfg.SQLitePath(); path != "" {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			c.detail += ", no file yet (made at the first start)"
			return c
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := store.Connect(cctx, cfg.DatabaseURL)
	if err != nil {
		c.verdict = vFail
		c.problem = &diag.Problem{Code: diag.DBUnreachable, What: err.Error(),
			Fix: "check HUB_DATABASE_URL (host, port, user, password, database name) and that the database accepts connections from this machine"}
		return c
	}
	defer db.Close()
	pending, unknown, err := db.PendingMigrations(cctx)
	switch {
	case err != nil:
		c.verdict = vFail
		c.problem = &diag.Problem{Code: diag.DBUnreachable, What: "reading the migrations: " + err.Error(), Fix: "the user of HUB_DATABASE_URL must be able to read schema_migrations"}
	case len(unknown) > 0:
		c.verdict = vFail
		c.problem = &diag.Problem{Code: diag.MigrationsNewer, What: fmt.Sprintf("the database has migrations %v this version does not know", unknown),
			Fix: "run the hub version that wrote them (a rollback is not possible after a migration); do not start this one on it"}
	case len(pending) > 0:
		c.verdict = vWarn
		c.detail += fmt.Sprintf(", %d migrations to apply", len(pending))
		c.problem = &diag.Problem{Code: diag.MigrationsLeft, What: fmt.Sprintf("%d migrations are not applied yet (last: %s)", len(pending), pending[len(pending)-1]),
			Fix: "nothing: the hub applies them at start; make a backup first if the database matters"}
	default:
		c.detail += ", migrations up to date"
	}
	return c
}

func checkS3(ctx context.Context, cfg *config.Config) check {
	c := check{name: "sites storage", verdict: vSkip, detail: "a folder (HARNESSLY_SITES_S3_ENDPOINT is not set)"}
	if cfg.SitesS3.Endpoint == "" {
		return c
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s3, err := sites.NewS3Blobs(sites.S3Config{Endpoint: cfg.SitesS3.Endpoint, Region: cfg.SitesS3.Region, Bucket: cfg.SitesS3.Bucket,
		Prefix: cfg.SitesS3.Prefix, AccessKey: cfg.SitesS3.AccessKey, SecretKey: cfg.SitesS3.SecretKey})
	if err == nil {
		err = s3.Check(cctx)
	}
	c.detail = cfg.SitesS3.Endpoint + "/" + cfg.SitesS3.Bucket
	if err != nil {
		c.verdict = vFail
		c.problem = &diag.Problem{Code: diag.S3Unreachable, What: err.Error(),
			Fix: "check HARNESSLY_SITES_S3_ENDPOINT, _BUCKET and the keys: the bucket must exist and the keys must read and write it"}
		return c
	}
	c.verdict = vOK
	return c
}

func checkOIDC(ctx context.Context, cfg *config.Config) check {
	if len(cfg.OIDC) == 0 {
		return check{name: "OIDC", verdict: vSkip, detail: "no provider (HUB_OIDC_*)"}
	}
	c := check{name: "OIDC", verdict: vOK}
	var names []string
	for _, p := range cfg.OIDC {
		names = append(names, p.ID)
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		req, _ := http.NewRequestWithContext(cctx, http.MethodGet, strings.TrimRight(p.Issuer, "/")+"/.well-known/openid-configuration", nil)
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			var doc struct {
				Issuer string `json:"issuer"`
			}
			if res.StatusCode != http.StatusOK {
				err = fmt.Errorf("answered %s", res.Status)
			} else if e := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); e != nil || doc.Issuer == "" {
				err = errors.New("the answer is not an OpenID configuration")
			}
			res.Body.Close()
		}
		cancel()
		if err != nil && c.problem == nil {
			c.verdict = vFail
			c.problem = &diag.Problem{Code: diag.OIDCUnreachable, What: fmt.Sprintf("provider %s (%s): %v", p.ID, p.Issuer, err),
				Fix: "the issuer must be the address whose /.well-known/openid-configuration answers from this machine"}
		}
	}
	c.detail = strings.Join(names, ", ")
	return c
}

func checkProxy(cfg *config.Config) check {
	c := check{name: "proxy", verdict: vOK, detail: fmt.Sprintf("%d trusted", len(cfg.TrustedProxies))}
	if cfg.Secure && len(cfg.TrustedProxies) == 0 {
		// The hub itself speaks plain http: an https address means something in front terminates TLS, and the hub then sees
		// that thing's address for every person unless it is told whom to believe about X-Forwarded-For.
		c.verdict = vWarn
		c.problem = &diag.Problem{Code: diag.ProxyNoTrusted, What: "the address is https but HUB_TRUSTED_PROXIES is empty: every person looks to the hub like the proxy, so limits and the audit log lose the real address",
			Fix: "set HUB_TRUSTED_PROXIES to the addresses of the proxy (e.g. 127.0.0.1/32) and make it send X-Forwarded-For and X-Forwarded-Proto"}
	}
	return c
}

// checkPublic asks the public address for /healthz as a browser behind the proxy would, and compares the clock with the Date it answers with.
func checkPublic(ctx context.Context, cfg *config.Config) (reach, clock check) {
	reach = check{name: "public address", verdict: vOK, detail: cfg.PublicURL + "/healthz"}
	clock = check{name: "clock", verdict: vSkip, detail: "needs the public address"}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, cfg.PublicURL+"/healthz", nil)
	fail := func(what, fix string) {
		reach.verdict = vFail
		reach.problem = &diag.Problem{Code: diag.AddressUnreach, What: what, Fix: fix}
	}
	res, err := client.Do(req)
	if err != nil {
		fail(err.Error(), "from this machine the address must lead to the hub: check DNS, the certificate and that the proxy is up (from inside a container the public name may not resolve: use --offline)")
		return
	}
	defer res.Body.Close()
	var body struct {
		OK bool `json:"ok"`
	}
	switch {
	case res.StatusCode >= 300 && res.StatusCode < 400:
		fail("answered "+res.Status+" to "+res.Header.Get("Location"), "a proxy that redirects /healthz is likely missing a trailing slash in its location (nginx: location /harnessly/ { proxy_pass ...; }) or forces another scheme")
	case res.StatusCode != http.StatusOK:
		fail("answered "+res.Status, "the proxy does not pass this path to the hub: see docs/hub/proxy.md for the location block")
	case json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body) != nil || !body.OK:
		fail("answered 200, but it is not the hub's answer", "the address leads to another server or to the hub's page for a wrong path: check the location and proxy_pass of the proxy")
	}
	if d, err := http.ParseTime(res.Header.Get("Date")); err == nil {
		skew := time.Since(d)
		clock = check{name: "clock", verdict: vOK, detail: fmt.Sprintf("%s apart from the proxy's Date", skew.Round(time.Second).Abs())}
		if skew > 30*time.Second || skew < -30*time.Second {
			clock.verdict = vWarn
			clock.problem = &diag.Problem{Code: diag.ClockSkew, What: fmt.Sprintf("this machine's clock differs by %s from the server that answered", skew.Round(time.Second)),
				Fix: "run NTP (chrony or systemd-timesyncd) on the hub and the proxy: sign-in codes, tokens and machine links are signed with the time"}
		}
	}
	return
}

// doctorExtensions prints the extension adapters' state. It returns the exit code of that part (2: the file cannot be read), whether an adapter is not healthy.
func doctorExtensions(ctx context.Context, cfg *config.Config, stdout io.Writer) (int, bool, error) {
	if !cfg.Extensions.On {
		fmt.Fprintln(stdout, "extensions     off (HUB_EXTENSIONS=on registers the adapters of HUB_EXTENSIONS_FILE)")
		return 0, false, nil
	}
	specs, err := ext.LoadSpecs(cfg.Extensions.File)
	if err != nil {
		return 0, false, err
	}
	reg, err := ext.NewRegistry(specs, ext.Config{Attempts: 1, Timeout: 5 * time.Second})
	if err != nil {
		return 0, false, err
	}
	reg.Check(ctx)
	sts := reg.Statuses()
	fmt.Fprintf(stdout, "extensions     %d registered\n", len(sts))
	fmt.Fprint(stdout, ext.Summary(sts))
	for _, s := range sts {
		if s.Health != ext.HealthOK {
			fmt.Fprintf(stdout, "               %s: an adapter is not healthy\n               docs: %s\n", diag.ExtensionDown, diag.Link(diag.ExtensionDown))
			return 1, true, nil
		}
	}
	return 0, false, nil
}
