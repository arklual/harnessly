package hlcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/release"
	"github.com/ly-solutions/harnessly/internal/upgrade"
)

const upgradeHelp = `harnessly upgrade plan|apply|rollback

Install a release as one set: the hub, runtime, agent and command line of the same version. A release is described by
a signed manifest (/dl/release.json on a hub, or a file); plan says what would change and which registered extensions
the new core cannot speak to, and changes nothing. apply replaces the programs together, restarts, checks and puts the
old ones back if the check fails. The data (registration, databases) is never touched. rollback puts back the programs
the last apply set aside as <name>.prev. The hub's own update by /dl/manifest.json is a different thing and unchanged.

  --hub URL          where the release is (default $HARNESSLY_HUB_URL, then $RUNTIME_HUB_URL)
  --manifest FILE    read the manifest from a file instead of the hub (with --sig FILE)
  --key FILE         the pinned public key (default $HARNESSLY_RELEASE_KEY_FILE); a manifest without a valid
                     signature is refused unless --allow-unsigned
  --dir DIR          where harnessly-runtime, harnessly-agent and harnessly are (default: next to this command)
  --hub-bin PATH     also replace the hub binary
  --extensions FILE  the extensions file (default $HUB_EXTENSIONS_FILE): checked against the release's ranges
  --restart-cmd CMD  run by sh after the programs are replaced, and again after a rollback
  --health-url URL   must answer 200 within 30 s after the restart
  --check-cmd CMD    run by sh after the restart; a non-zero exit rolls back
  --force            apply although an extension is incompatible
`

func (c *cli) upgrade(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(c.env.Stdout, upgradeHelp)
		return nil
	}
	sub, args := args[0], args[1:]
	if sub != "plan" && sub != "apply" && sub != "rollback" {
		return usageErr("upgrade: plan, apply or rollback, not %q", sub)
	}
	fs := flag.NewFlagSet("upgrade "+sub, flag.ContinueOnError)
	fs.SetOutput(c.env.Stderr)
	var (
		hub, manifest, sigFile, keyFile, dir, hubBin, extFile, restart, health, check string
		insecure, force                                                               bool
	)
	fs.StringVar(&hub, "hub", "", "")
	fs.StringVar(&manifest, "manifest", "", "")
	fs.StringVar(&sigFile, "sig", "", "")
	fs.StringVar(&keyFile, "key", "", "")
	fs.StringVar(&dir, "dir", "", "")
	fs.StringVar(&hubBin, "hub-bin", "", "")
	fs.StringVar(&extFile, "extensions", "", "")
	fs.StringVar(&restart, "restart-cmd", "", "")
	fs.StringVar(&health, "health-url", "", "")
	fs.StringVar(&check, "check-cmd", "", "")
	fs.BoolVar(&insecure, "allow-unsigned", false, "")
	fs.BoolVar(&force, "force", false, "")
	if err := fs.Parse(args); err != nil {
		return usageErr("upgrade: %v", err)
	}
	if fs.NArg() > 0 {
		return usageErr("upgrade: unexpected %q", fs.Arg(0))
	}
	pick := func(v string, names ...string) string {
		if v != "" {
			return v
		}
		for _, n := range names {
			if x := strings.TrimSpace(c.env.Getenv(n)); x != "" {
				return x
			}
		}
		return ""
	}
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if p, err := filepath.EvalSymlinks(exe); err == nil {
			exe = p
		}
		dir = filepath.Dir(exe)
	}
	targets := []upgrade.Target{
		{Component: release.Runtime, Path: filepath.Join(dir, "harnessly-runtime"), Optional: true},
		{Component: release.Agent, Path: filepath.Join(dir, "harnessly-agent"), Optional: true},
		{Component: release.CLI, Path: filepath.Join(dir, "harnessly"), Optional: true},
	}
	if hubBin != "" {
		targets = append(targets, upgrade.Target{Component: release.Hub, Path: hubBin, Optional: true})
	}
	if sub == "rollback" {
		back, err := upgrade.Rollback(targets)
		if err != nil {
			return err
		}
		for _, p := range back {
			c.out("restored %s\n", p)
		}
		if restart != "" {
			if err := shell(context.Background(), restart); err != nil {
				return fmt.Errorf("restart-cmd: %w", err)
			}
		}
		return nil
	}

	hub = strings.TrimRight(pick(hub, "HARNESSLY_HUB_URL", "RUNTIME_HUB_URL"), "/")
	body, sig, err := readManifest(hub, manifest, sigFile)
	if err != nil {
		return err
	}
	if kf := pick(keyFile, "HARNESSLY_RELEASE_KEY_FILE"); kf != "" {
		raw, err := os.ReadFile(kf)
		if err != nil {
			return err
		}
		pub, err := release.ParsePublicKey(string(raw))
		if err != nil {
			return err
		}
		if err := release.Verify(pub, body, sig); err != nil {
			return err
		}
	} else if !insecure {
		return errors.New("no pinned key: give --key FILE (or $HARNESSLY_RELEASE_KEY_FILE); --allow-unsigned skips the check")
	}
	m, err := release.Parse(body)
	if err != nil {
		return err
	}
	var exts []upgrade.Extension
	if ef := pick(extFile, "HUB_EXTENSIONS_FILE"); ef != "" {
		specs, err := ext.LoadSpecs(ef)
		if err != nil {
			return err
		}
		for _, s := range specs {
			exts = append(exts, upgrade.Extension{Name: s.Name, Protocol: s.Protocol})
		}
	}
	plan, err := upgrade.MakePlan(m, targets, runtime.GOOS, runtime.GOARCH, exts, upgrade.ProbeVersion)
	if err != nil {
		return err
	}
	if sub == "plan" {
		c.printPlan(plan)
		if plan.Blocked() {
			return exitCode(1)
		}
		return nil
	}

	o := upgrade.Options{Force: force, Log: func(s string) { fmt.Fprintln(c.env.Stderr, "upgrade: "+s) }}
	o.Fetch = func(ctx context.Context, key string) (io.ReadCloser, error) {
		if hub == "" {
			return nil, errors.New("apply downloads from a hub: give --hub URL")
		}
		return httpGet(ctx, hub+"/dl/"+key, 10*time.Minute)
	}
	if restart != "" {
		o.Restart = func(ctx context.Context) error { return shell(ctx, restart) }
	}
	o.Verify = func(ctx context.Context, items []upgrade.Item) error {
		for _, it := range items {
			if it.Component != release.Runtime && upgrade.ProbeVersion(upgrade.Target{Component: it.Component, Path: it.Path}) == "" {
				return fmt.Errorf("%s does not answer `version`", it.Path)
			}
		}
		if health != "" {
			if err := waitHealthy(ctx, health, 30*time.Second); err != nil {
				return err
			}
		}
		if check != "" {
			if err := shell(ctx, check); err != nil {
				return fmt.Errorf("check-cmd: %w", err)
			}
		}
		return nil
	}
	c.printPlan(plan)
	res, err := upgrade.Apply(context.Background(), plan, o)
	if res != nil && res.RolledBack {
		fmt.Fprintln(c.env.Stderr, "upgrade: rolled back to the previous programs; the data was not touched")
	}
	if err != nil {
		return err
	}
	if len(res.Replaced) == 0 {
		c.out("nothing to do: already at %s\n", plan.Version)
		return nil
	}
	c.out("upgraded %s to %s (the old ones are kept as <name>.prev; `harnessly upgrade rollback` puts them back)\n", strings.Join(res.Replaced, ", "), plan.Version)
	return nil
}

func (c *cli) printPlan(p *upgrade.Plan) {
	if c.json {
		b, _ := json.MarshalIndent(p, "", "  ")
		c.out("%s\n", b)
		return
	}
	c.out("release %s\n", p.Version)
	for _, it := range p.Items {
		have := it.Have
		if have == "" {
			have = "?"
		}
		c.out("  %-8s %-8s %s -> %s  %s\n", it.Action, it.Component, have, it.Want, it.Path)
	}
	for _, e := range p.Extensions {
		c.out("  extension %-20s %-12s %s\n", e.Name, e.Verdict, e.Detail)
	}
	if len(p.Extensions) == 0 {
		c.out("  extensions: none registered (give --extensions FILE to check them)\n")
	}
	add := "reversible by putting the old programs back"
	if !p.Data.Additive {
		add = "NOT additive: back up the data first, the old programs may not read it after the migrations"
	}
	c.out("  data: schema %d, %s\n", p.Data.SchemaVersion, add)
	for _, b := range p.Blocking {
		c.out("  BLOCKED: %s\n", b)
	}
}

func readManifest(hub, file, sigFile string) (body, sig []byte, err error) {
	if file != "" {
		if body, err = os.ReadFile(file); err != nil {
			return nil, nil, err
		}
		if sigFile == "" {
			sigFile = file + ".sig"
		}
		sig, err = os.ReadFile(sigFile)
		if errors.Is(err, os.ErrNotExist) {
			sig, err = nil, nil
		}
		return body, sig, err
	}
	if hub == "" {
		return nil, nil, errors.New("give --hub URL (or $HARNESSLY_HUB_URL) or --manifest FILE")
	}
	ctx := context.Background()
	for _, g := range []struct {
		path string
		dst  *[]byte
		need bool
	}{{"/dl/release.json", &body, true}, {"/dl/release.json.sig", &sig, false}} {
		rc, err := httpGet(ctx, hub+g.path, 20*time.Second)
		if err != nil {
			if g.need {
				return nil, nil, err
			}
			continue
		}
		*g.dst, err = io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return nil, nil, err
		}
	}
	return body, sig, nil
}

func httpGet(ctx context.Context, url string, wait time.Duration) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		cancel()
		return nil, fmt.Errorf("%s: HTTP %d", url, res.StatusCode)
	}
	return cancelOnClose{res.Body, cancel}, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error { err := c.ReadCloser.Close(); c.cancel(); return err }

func waitHealthy(ctx context.Context, url string, total time.Duration) error {
	deadline := time.Now().Add(total)
	var last error
	for {
		rc, err := httpGet(ctx, url, 5*time.Second)
		if err == nil {
			rc.Close()
			return nil
		}
		last = err
		if time.Now().After(deadline) {
			return fmt.Errorf("health-url: %w", last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func shell(ctx context.Context, cmd string) error {
	out, err := exec.CommandContext(ctx, "sh", "-c", cmd).CombinedOutput()
	if err != nil {
		t := strings.TrimSpace(string(out))
		if len(t) > 400 {
			t = t[len(t)-400:]
		}
		return fmt.Errorf("%w: %s", err, t)
	}
	return nil
}
