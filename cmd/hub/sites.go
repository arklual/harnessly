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
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/buildinfo"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/sites"
)

// sitesConfig is the configuration of `hub sites`: the sites module on its own, without the hub's accounts, spaces and
// machines. Whoever runs it puts a gateway in front that signs people in (docs/hub/sites.md, «Отдельный сервер»).
type sitesConfig struct {
	Listen        string
	DataDir       string
	DatabaseURL   string
	BlobsDir      string
	S3            sites.S3Config
	Domain        string
	Secure        bool
	AbuseURL      string
	AdminToken    string
	GatewaySecret string
	LoginURL      string
	LogLevel      string
	// StripHeaders are the prefixes of request headers a gateway in front uses for identity; the edge drops them.
	StripHeaders []string
}

func loadSitesConfig(lookup func(string) (string, bool)) (*sitesConfig, error) {
	var problems []string
	bad := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
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
	c := &sitesConfig{
		Listen:        def("SITES_LISTEN", "0.0.0.0:8050"),
		DataDir:       def("SITES_DATA_DIR", "/var/lib/harnessly-sites"),
		Domain:        strings.ToLower(strings.Trim(get("HARNESSLY_SITES_DOMAIN"), ".")),
		AbuseURL:      get("HARNESSLY_SITES_ABUSE_URL"),
		AdminToken:    get("SITES_ADMIN_TOKEN"),
		GatewaySecret: get("SITES_GATEWAY_SECRET"),
		LoginURL:      get("SITES_LOGIN_URL"),
		LogLevel:      strings.ToLower(def("SITES_LOG_LEVEL", "info")),
		StripHeaders:  prefixes(get("SITES_STRIP_HEADER_PREFIXES")),
	}
	c.DatabaseURL = get("SITES_DATABASE_URL")
	if c.DatabaseURL == "" {
		c.DatabaseURL = "sqlite://" + filepath.Join(c.DataDir, "sites.db")
	}
	c.BlobsDir = def("HARNESSLY_SITES_DIR", filepath.Join(c.DataDir, "blobs"))
	c.S3 = sites.S3Config{Endpoint: get("HARNESSLY_SITES_S3_ENDPOINT"), Region: get("HARNESSLY_SITES_S3_REGION"), Bucket: get("HARNESSLY_SITES_S3_BUCKET"),
		Prefix: get("HARNESSLY_SITES_S3_PREFIX"), AccessKey: get("HARNESSLY_SITES_S3_ACCESS_KEY"), SecretKey: get("HARNESSLY_SITES_S3_SECRET_KEY")}
	if c.S3.Endpoint != "" && (c.S3.Bucket == "" || c.S3.AccessKey == "" || c.S3.SecretKey == "") {
		bad("HARNESSLY_SITES_S3_ENDPOINT needs HARNESSLY_SITES_S3_BUCKET, HARNESSLY_SITES_S3_ACCESS_KEY and HARNESSLY_SITES_S3_SECRET_KEY")
	}
	if c.Domain == "" {
		bad("HARNESSLY_SITES_DOMAIN is required: the standalone server serves sites at <site>--<space>.<domain>")
	}
	host := c.Domain
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	c.Secure = !(host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".test"))
	switch strings.ToLower(get("SITES_SECURE")) {
	case "":
	case "on", "true", "1", "yes":
		c.Secure = true
	case "off", "false", "0", "no":
		c.Secure = false
	default:
		bad("SITES_SECURE must be on or off")
	}
	if len(c.GatewaySecret) < 32 {
		bad("SITES_GATEWAY_SECRET is required, at least 32 characters (openssl rand -base64 32): the gateway signs tickets with it")
	}
	if c.AdminToken != "" && len(c.AdminToken) < 24 {
		bad("SITES_ADMIN_TOKEN must be at least 24 characters (openssl rand -base64 24)")
	}
	if c.LoginURL != "" && !strings.HasPrefix(c.LoginURL, "https://") && !strings.HasPrefix(c.LoginURL, "http://") {
		bad("SITES_LOGIN_URL must be an http(s) address")
	}
	if len(problems) > 0 {
		return nil, errors.New("configuration: " + strings.Join(problems, "; "))
	}
	return c, nil
}

func (c *sitesConfig) blobs() (sites.Blobs, error) {
	if c.S3.Endpoint != "" {
		return sites.NewS3Blobs(c.S3)
	}
	return sites.NewFolderBlobs(c.BlobsDir)
}

// serveSites runs the standalone sites server until ctx ends. ready, when set, gets the address it listens on.
func serveSites(ctx context.Context, cfg *sitesConfig, ready chan<- string, logOut io.Writer) error {
	log := logger(logOut, cfg.LogLevel)
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("data folder: %w", err)
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	blobs, err := cfg.blobs()
	if err != nil {
		return err
	}
	if s3, ok := blobs.(*sites.S3Blobs); ok {
		if err := s3.Check(ctx); err != nil {
			return err
		}
	}
	key := []byte(cfg.GatewaySecret)
	auth := &sites.GatewayAuthority{AdminToken: cfg.AdminToken, LoginURL: cfg.LoginURL, HMACTickets: sites.NewHMACTickets(key, nil)}
	svc, err := sites.New(sites.Deps{DB: db, Blobs: blobs, Auth: auth, Log: log,
		Config: sites.Config{Domain: cfg.Domain, Secure: cfg.Secure, Key: key, AbuseURL: cfg.AbuseURL, StripHeaderPrefixes: cfg.StripHeaders}})
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "version": buildinfo.Version})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		cctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if db.Ping(cctx) != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	svc.Mount(mux)
	edge := svc.Edge()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if svc.IsSiteHost(r.Host) {
			edge.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)}
	bg, stopBackground := context.WithCancel(ctx)
	defer stopBackground()
	go svc.Run(bg)
	log.Info("sites started", "version", buildinfo.Version, "listen", ln.Addr().String(), "domain", cfg.Domain,
		"blobs", map[bool]string{true: "s3", false: "folder"}[cfg.S3.Endpoint != ""], "management", cfg.AdminToken != "")
	if cfg.AdminToken == "" {
		log.Warn("no SITES_ADMIN_TOKEN: sites cannot be created or deployed to over HTTP")
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
	log.Info("sites stopping")
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// runSites is `hub sites [serve|check-config|ticket]`.
func runSites(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	sub := "serve"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	cfg, err := loadSitesConfig(lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	switch sub {
	case "serve":
		if err := serveSites(ctx, cfg, nil, stderr); err != nil {
			fmt.Fprintln(stderr, "hub sites:", err)
			return 1
		}
		return 0
	case "check-config":
		storeKind := "folder " + cfg.BlobsDir
		if cfg.S3.Endpoint != "" {
			storeKind = "s3 " + cfg.S3.Endpoint + "/" + cfg.S3.Bucket
		}
		fmt.Fprintf(stdout, "listen         %s\n", cfg.Listen)
		fmt.Fprintf(stdout, "sites domain   %s (%s, one address per site: <site>--<space>.%s)\n", cfg.Domain, map[bool]string{true: "https", false: "http"}[cfg.Secure], cfg.Domain)
		fmt.Fprintf(stdout, "database       %s\n", strings.SplitN(cfg.DatabaseURL, ":", 2)[0])
		fmt.Fprintf(stdout, "files          %s\n", storeKind)
		fmt.Fprintf(stdout, "management     %s\n", map[bool]string{true: "by SITES_ADMIN_TOKEN", false: "off (no SITES_ADMIN_TOKEN)"}[cfg.AdminToken != ""])
		fmt.Fprintf(stdout, "viewers        tickets signed by the gateway (login entry: %s)\n", map[bool]string{true: cfg.LoginURL, false: "not set"}[cfg.LoginURL != ""])
		return 0
	case "ticket":
		// hub sites ticket <host> [user]: a ticket the gateway would make, for a smoke test.
		if len(args) < 1 {
			fmt.Fprintln(stderr, "usage: hub sites ticket <site host> [user id]")
			return 2
		}
		user := "tester"
		if len(args) > 1 {
			user = args[1]
		}
		fmt.Fprintln(stdout, sites.NewHMACTickets([]byte(cfg.GatewaySecret), nil).Issue(user, args[0]))
		return 0
	}
	fmt.Fprintf(stderr, "hub sites: unknown command %q (serve, check-config, ticket)\n", sub)
	return 2
}

// prefixes reads a comma-separated list of request header prefixes, lower-cased; empty gives the edge's default.
func prefixes(list string) []string {
	var out []string
	for _, p := range strings.Split(list, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
