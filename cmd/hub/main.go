// Command hub is the harnessly hub: the server people sign in to, which keeps their spaces and machines and carries a
// browser to a machine's runtime (docs/hub/architecture.md). Everything is configured by HUB_* variables
// (docs/hub/deploy.md).
//
//	hub                run the hub
//	hub check-config   read the configuration and print what it switches on, without starting anything
//	hub healthcheck    ask a running hub on this host whether it is up (for container health checks)
//	hub sites          run the sites module on its own (docs/hub/sites.md)
//	hub version
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/buildinfo"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/mail"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/web"
)

const shutdownGrace = 20 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr))
}

// run is main without the process around it: the arguments, the environment and the output are passed in, and the
// exit code is returned.
func run(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "version", "-v", "--version":
		fmt.Fprintln(stdout, buildinfo.Version)
		return 0
	case "healthcheck":
		return healthcheck(ctx, lookup, stdout, stderr)
	case "check-config":
		cfg, err := config.Load(lookup)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		describe(stdout, cfg)
		return 0
	case "serve":
		if len(args) > 0 {
			fmt.Fprintf(stderr, "hub serve takes no arguments, got %q\n", args[0])
			return 2
		}
		cfg, err := config.Load(lookup)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if err := serve(ctx, cfg, nil, stderr); err != nil {
			fmt.Fprintln(stderr, "hub:", err)
			return 1
		}
		return 0
	case "sites":
		return runSites(ctx, args, lookup, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "hub: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

const usage = `usage: hub [serve | check-config | healthcheck | sites | version]

The hub is configured by HUB_* environment variables; see docs/hub/deploy.md.
`

// logger writes to w at the configured level.
func logger(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: l}))
}

// serve runs the hub until ctx ends. ready, when set, gets the address it listens on once it does.
func serve(ctx context.Context, cfg *config.Config, ready chan<- string, logOut io.Writer) error {
	log := logger(logOut, cfg.LogLevel)
	slog.SetDefault(log)

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("data folder: %w", err)
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	var mailer mail.Mailer
	if cfg.SMTPURL != "" {
		if mailer, err = mail.NewSMTP(cfg.SMTPURL, cfg.MailFrom); err != nil {
			return err
		}
	} else {
		// config.Load refuses this already; a configuration built another way must not get a hub whose log is a mailbox.
		if cfg.MailLogRefused() {
			return errors.New("HUB_SMTP_URL is required on an https hub and in the cloud edition (or HUB_MAIL_LOG=allow): sign-in codes and invitation links must not be written to the log")
		}
		mailer = mail.Log{Logger: log}
	}
	hub, err := api.New(api.Options{Config: cfg, DB: db, Mailer: mailer, Log: log, UI: web.HubDist()})
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           hub.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		// No read or write timeout: event streams, agent links and tunnelled WebSockets live for hours.
		IdleTimeout: 2 * time.Minute,
		ErrorLog:    slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	bg, stopBackground := context.WithCancel(ctx)
	defer stopBackground()
	go hub.Run(bg)

	log.Info("hub started", "version", buildinfo.Version, "listen", ln.Addr().String(), "publicUrl", cfg.PublicURL,
		"edition", cfg.Edition, "database", dbKind(cfg), "mail", mailer.Mode(), "runtime", runtimeMode(cfg))
	for _, w := range cfg.Warnings {
		log.Error(w)
	}
	if !cfg.SubdomainMode() {
		log.Warn("no HUB_RUNTIME_DOMAIN: a machine's page runs on the hub's own origin, with the signed-in person's session, so connect only machines you trust as much as the hub (docs/hub/deploy.md)")
	}
	if mailer.Mode() == "log" {
		if cfg.Secure || cfg.Edition == config.EditionCloud {
			log.Warn("HUB_MAIL_LOG=allow and no HUB_SMTP_URL: every sign-in code and invitation link is written to this log, so whoever can read it can sign in as anyone; set HUB_SMTP_URL")
		} else {
			log.Warn("no HUB_SMTP_URL: sign-in codes and invitation links are written to this log (fine for one host on plain http, not for a shared hub)")
		}
	}
	if ready != nil {
		ready <- ln.Addr().String()
	}

	failed := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}

	log.Info("hub stopping")
	stopBackground()
	// Event streams and agent links never end by themselves: close them first, or Shutdown waits for its grace to run out.
	hub.Shutdown()
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func dbKind(cfg *config.Config) string {
	if cfg.IsPostgres() {
		return "postgres"
	}
	return "sqlite"
}

func runtimeMode(cfg *config.Config) string {
	if cfg.SubdomainMode() {
		return "subdomain " + cfg.RuntimeDomain
	}
	return "path"
}

// describe prints what a configuration switches on. It never prints a secret.
func describe(w io.Writer, cfg *config.Config) {
	on := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	fmt.Fprintf(w, "public URL     %s\n", cfg.PublicURL)
	fmt.Fprintf(w, "listen         %s\n", cfg.Listen)
	fmt.Fprintf(w, "edition        %s, sign-up %s\n", cfg.Edition, cfg.Signup)
	fmt.Fprintf(w, "database       %s\n", dbKind(cfg))
	if cfg.SMTPURL != "" {
		fmt.Fprintln(w, "email code     mailed over SMTP")
	} else {
		fmt.Fprintln(w, "email code     written to the hub log (no HUB_SMTP_URL; accepted by HUB_MAIL_LOG=allow or plain http)")
	}
	fmt.Fprintf(w, "passkeys       %s (RP ID %s)\n", on(cfg.Passkey), cfg.RPID)
	fmt.Fprintf(w, "GitHub         %s\n", on(cfg.GitHubOn()))
	fmt.Fprintf(w, "Yandex ID      %s\n", on(cfg.YandexOn()))
	ids := make([]string, 0, len(cfg.OIDC))
	for _, p := range cfg.OIDC {
		ids = append(ids, p.ID)
	}
	if len(ids) == 0 {
		ids = append(ids, "none")
	}
	fmt.Fprintf(w, "OIDC           %s\n", strings.Join(ids, ", "))
	if cfg.SubdomainMode() {
		fmt.Fprintf(w, "runtime origin %s\n", runtimeMode(cfg))
	} else {
		fmt.Fprintf(w, "runtime origin path (a machine's page runs on the hub's origin: machines are trusted as much as the hub)\n")
	}
	for _, warning := range cfg.Warnings {
		fmt.Fprintf(w, "WARNING        %s\n", warning)
	}
	describeSites(w, cfg)
	fmt.Fprintf(w, "SSH install    %s\n", on(cfg.SSH))
	fmt.Fprintf(w, "container      %s\n", on(cfg.ContainerImage != ""))
	fmt.Fprintf(w, "local machine  %s\n", on(cfg.LocalCodeFile != ""))
	fmt.Fprintf(w, "binaries       %s\n", cfg.DistDir)
}

// healthcheck asks the hub that listens on HUB_LISTEN (on this host) for /healthz. It needs no other setting, so a
// container's health check works with whatever the hub was given.
func healthcheck(ctx context.Context, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	listen := "0.0.0.0:8040"
	if v, ok := lookup("HUB_LISTEN"); ok && strings.TrimSpace(v) != "" {
		listen = strings.TrimSpace(v)
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		fmt.Fprintln(stderr, "HUB_LISTEN:", err)
		return 2
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/healthz", nil)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintln(stderr, "healthz answered", res.Status)
		return 1
	}
	fmt.Fprintln(stdout, "ok")
	return 0
}

// describeSites says by which scheme published sites will be reached.
func describeSites(w io.Writer, cfg *config.Config) {
	files := "folder " + cfg.SitesDir
	if cfg.SitesS3.Endpoint != "" {
		files = "s3 " + cfg.SitesS3.Endpoint + "/" + cfg.SitesS3.Bucket
	}
	switch cfg.SitesMode() {
	case "subdomain":
		fmt.Fprintf(w, "sites          subdomain: <site>--<space>.%s, files in %s (the wildcard *.%s must reach the hub, with a certificate)\n", cfg.SitesDomain, files, cfg.SitesDomain)
	case "path":
		fmt.Fprintf(w, "sites          path: %s/sites/<site>--<space>/, static only, sandboxed without allow-same-origin, files in %s (set HARNESSLY_SITES_DOMAIN for an address per site)\n", cfg.PublicURL, files)
	default:
		if cfg.SitesOff {
			fmt.Fprintln(w, "sites          off (HARNESSLY_SITES=off)")
		} else {
			fmt.Fprintln(w, "sites          off (the cloud edition publishes sites only on a domain of their own: set HARNESSLY_SITES_DOMAIN)")
		}
	}
}
