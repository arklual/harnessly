package hlcli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultInstallScript = "https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh"

// install checks this machine and installs harnessly on it with the installer script: it finds the script
// (a file, the offline folder, the mirror or the public address), asks it to check the environment first and passes
// every other flag on to it.
func (c *cli) install(args []string) error {
	var script string
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--script" {
			if i+1 >= len(args) {
				return usageErr("install: --script needs a file")
			}
			script = args[i+1]
			i++
			continue
		}
		if v, ok := strings.CutPrefix(a, "--script="); ok {
			script = v
			continue
		}
		rest = append(rest, a)
	}
	opts := installOpts(rest)
	if script == "" {
		script = c.env.Getenv("HARNESSLY_INSTALL_SCRIPT")
	}
	if script == "" && opts["offline"] == "" {
		opts["offline"] = c.env.Getenv("HARNESSLY_OFFLINE")
	}
	if script == "" && opts["offline"] != "" {
		script = filepath.Join(opts["offline"], "install.sh")
	}
	if script == "" {
		tmp, err := os.MkdirTemp("", "harnessly-install-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		script = filepath.Join(tmp, "install.sh")
		if err := c.fetchInstaller(c.installerURL(opts), opts, script); err != nil {
			return err
		}
	}
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("install: the installer script %s is not there: %w", script, err)
	}
	env := os.Environ()
	if c.env.Environ != nil {
		env = c.env.Environ()
	}
	env = append(env, "HARNESSLY_PREFLIGHT=1")
	cmd := exec.CommandContext(context.Background(), "sh", append([]string{script}, rest...)...) //nolint:gosec // the script is the one the person named or fetched
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.env.Stdin, c.env.Stdout, c.env.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	return nil
}

// installOpts reads the few options that decide where the script comes from; the script reads all of them itself.
func installOpts(args []string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := strings.TrimPrefix(args[i], "--")
		if a == args[i] {
			continue
		}
		if k, v, ok := strings.Cut(a, "="); ok {
			out[k] = v
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			out[a] = args[i+1]
			i++
		}
	}
	return out
}

func (c *cli) installerURL(opts map[string]string) string {
	mirror := opts["mirror"]
	if mirror == "" {
		mirror = c.env.Getenv("HARNESSLY_MIRROR")
	}
	if mirror != "" {
		return strings.TrimRight(mirror, "/") + "/install.sh"
	}
	return defaultInstallScript
}

// fetchInstaller downloads the script, with the company's CA and proxy when they were given.
func (c *cli) fetchInstaller(url string, opts map[string]string, to string) error {
	client := c.env.Client
	if client == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert // the default transport is one
		ca := opts["ca-file"]
		if ca == "" {
			ca = c.env.Getenv("HARNESSLY_CA_FILE")
		}
		if ca != "" {
			pem, err := os.ReadFile(ca) //nolint:gosec // the person's own CA file
			if err != nil {
				return fmt.Errorf("install: reading the CA file: %w", err)
			}
			pool, _ := x509.SystemCertPool()
			if pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pem) {
				return fmt.Errorf("install: %s holds no PEM certificate", ca)
			}
			tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		}
		if p := opts["proxy"]; p != "" {
			if u, err := http.NewRequest(http.MethodGet, p, nil); err == nil {
				tr.Proxy = http.ProxyURL(u.URL)
			}
		}
		client = &http.Client{Transport: tr, Timeout: 60 * time.Second}
	}
	resp, err := client.Get(url) //nolint:noctx // a short one-off download
	if err != nil {
		return fmt.Errorf("install: fetching %s: %w (give --ca-file for a company CA, --proxy, --mirror, or --offline DIR / --script FILE)", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("install: fetching %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return os.WriteFile(to, body, 0o600)
}
