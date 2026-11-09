package hlcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/selfupdate"
)

const fleetHelp = `harnessly fleet stage|activate|rollback|status|delta

Rolls a runtime release out to this machine, the fleet mode of the machine's self-update (RUNTIME_UPDATE_MODE=fleet; off
unless asked for, and the hourly update from the hub is unchanged without it). A release is a folder the runtime runs
from: <root>/releases/<version>-<sha12>, with <root>/current pointing at the active one.

  stage      check the tarball against its sha256 and unpack it next to the running release; nothing that runs changes
  activate   point current at a staged release, restart, wait for the health check; a failed check puts the previous
             release back (exit 1)
  rollback   put back the release that was active before the last activation
  status     the active release and the staged ones (--json)
  delta      cut a delta out of a whole release tarball: only the parts a pin change moves (release/<part>)

  --root DIR        the releases folder (default $RUNTIME_FLEET_ROOT)
  --version X.Y.Z   the release number (stage, activate)
  --tarball FILE    the release or delta tarball (stage, delta); deleted after staging
  --sha256 HEX      its checksum (stage), or the staged release's (activate)
  --delta-base SHA  stage: the tarball is a delta cut against this commit. Exit 3 when the running release is another
                    one: nothing changed, send the whole release
  --out FILE        delta: where to write it; --head COMMIT the commit the tarball was packed from; --parts a,b the
                    parts (default $RUNTIME_FLEET_PARTS, then bin and the release file)
  --json            status as JSON

The services' restart and health check come from RUNTIME_FLEET_RESTART and RUNTIME_FLEET_HEALTH_URL
(RUNTIME_FLEET_KEEP, RUNTIME_FLEET_PARTS, RUNTIME_FLEET_HEALTH_TIMEOUT), see docs/hub/fleet-rollout.md.
`

// exitFleetDelta is the exit code of a delta the running release does not match.
const exitFleetDelta = 3

func (c *cli) fleet(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		c.out("%s", fleetHelp)
		return nil
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "stage", "activate", "rollback", "status", "delta":
	default:
		return usageErr("fleet: stage, activate, rollback, status or delta, not %q", sub)
	}
	fs := flag.NewFlagSet("fleet "+sub, flag.ContinueOnError)
	fs.SetOutput(c.env.Stderr)
	var root, version, tarball, sha, base, out, head, parts string
	var asJSON bool
	fs.StringVar(&root, "root", "", "")
	fs.StringVar(&version, "version", "", "")
	fs.StringVar(&tarball, "tarball", "", "")
	fs.StringVar(&sha, "sha256", "", "")
	fs.StringVar(&base, "delta-base", "", "")
	fs.StringVar(&out, "out", "", "")
	fs.StringVar(&head, "head", "", "")
	fs.StringVar(&parts, "parts", "", "")
	fs.BoolVar(&asJSON, "json", false, "")
	if err := fs.Parse(args); err != nil {
		return usageErr("fleet %s: %v", sub, err)
	}
	if fs.NArg() > 0 {
		return usageErr("fleet %s: unexpected %q", sub, fs.Arg(0))
	}
	getenv := c.env.Getenv
	if root != "" {
		orig := getenv
		getenv = func(k string) string {
			if k == "RUNTIME_FLEET_ROOT" {
				return root
			}
			return orig(k)
		}
	}
	fl := selfupdate.FleetFromEnv(getenv, func(m string) { fmt.Fprintln(c.env.Stdout, m) })
	if sub == "delta" {
		list := selfupdate.DefaultParts
		if fl != nil {
			list = fl.Parts
		}
		if parts != "" {
			list = strings.Split(parts, ",")
		}
		if tarball == "" || out == "" || head == "" {
			return usageErr("fleet delta needs --tarball, --out and --head")
		}
		sum, err := selfupdate.CutDelta(tarball, out, head, list)
		if err != nil {
			return fmt.Errorf("delta: %w", err)
		}
		c.out("delta %s sha256 %s\n", out, sum)
		return nil
	}
	if fl == nil {
		return fail("fleet: no releases folder: pass --root or set RUNTIME_FLEET_ROOT")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	switch sub {
	case "status":
		st := fl.Status()
		if asJSON || c.json { // the global --json is taken before the subcommand sees it
			raw, _ := json.Marshal(st)
			c.out("%s\n", raw)
			return nil
		}
		c.out("root %s\nactive %s\nversion %s\ncommit %s\nstaged %s\n", st.Root, st.Active, st.Version, st.Commit, strings.Join(st.Staged, " "))
		return nil
	case "stage":
		if version == "" || tarball == "" || sha == "" {
			return usageErr("fleet stage needs --version, --tarball and --sha256")
		}
		dir, err := fl.Stage(selfupdate.StageInput{Version: version, Tarball: tarball, SHA256: sha, DeltaBase: base})
		if errors.Is(err, selfupdate.ErrDeltaBase) {
			fmt.Fprintln(c.env.Stderr, "harnessly: "+err.Error()+": nothing changed, send the whole release")
			return exitCode(exitFleetDelta)
		}
		if err != nil {
			return err
		}
		c.out("staged %s in %s\n", version, dir)
		return nil
	case "activate":
		if version == "" || sha == "" {
			return usageErr("fleet activate needs --version and --sha256")
		}
		return fl.Activate(ctx, version, sha)
	default: // rollback
		return fl.Rollback(ctx)
	}
}
