package hlcli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/fleetdriver"
)

const fleetRolloutHelp = `harnessly fleet rollout --fleet FILE --version X.Y.Z --tarball FILE [flags]

Rolls one runtime release out to the machines of a fleet file: uploads it, stages it on every machine (nothing that runs
changes), then activates the machines wave by wave and checks each. When a machine fails (it has put itself back), the
machines already moved go back with "fleet rollback" and the command exits 1. Machines that already run the version are
left alone, so a cut-short rollout can be run again. Off unless RUNTIME_ROLLOUT_DRIVER=on.

  --fleet FILE         the fleet file (default $RUNTIME_ROLLOUT_FLEET), see docs/hub/fleet-rollout.md
  --version X.Y.Z      the release number
  --tarball FILE       the whole release
  --delta FILE         optional: a delta cut with "fleet delta"; --delta-base COMMIT is the commit it was cut against
  --only a,b           only these machines (names from the fleet file)
  --stage-jobs N       machines staged at once (4)
  --activate-jobs N    machines of a wave activated at once (the whole wave)
  --soak 5m            pause after a wave that passed, before the next
  --force              activate now, whatever the machines' window says (RUNTIME_FLEET_MIN_GAP)
  --dry-run            say what would go where; change nothing
  --ssh "CMD"          the command that runs a command on a machine as "CMD <target> <command>" (default: the
                       fleet file's "ssh", then $RUNTIME_ROLLOUT_SSH, then ssh)
`

// fleetRollout is `harnessly fleet rollout`.
func (c *cli) fleetRollout(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		c.out("%s", fleetRolloutHelp)
		return nil
	}
	if c.env.Getenv("RUNTIME_ROLLOUT_DRIVER") != "on" {
		return fail("fleet rollout is off: set RUNTIME_ROLLOUT_DRIVER=on")
	}
	fs := flag.NewFlagSet("fleet rollout", flag.ContinueOnError)
	fs.SetOutput(c.env.Stderr)
	var fleetFile, version, tarball, delta, deltaBase, only, sshCmd string
	var stageJobs, activateJobs int
	var soak time.Duration
	var force, dry, asJSON bool
	fs.StringVar(&fleetFile, "fleet", c.env.Getenv("RUNTIME_ROLLOUT_FLEET"), "")
	fs.StringVar(&version, "version", "", "")
	fs.StringVar(&tarball, "tarball", "", "")
	fs.StringVar(&delta, "delta", "", "")
	fs.StringVar(&deltaBase, "delta-base", "", "")
	fs.StringVar(&only, "only", "", "")
	fs.StringVar(&sshCmd, "ssh", "", "")
	fs.IntVar(&stageJobs, "stage-jobs", 0, "")
	fs.IntVar(&activateJobs, "activate-jobs", 0, "")
	fs.DurationVar(&soak, "soak", 0, "")
	fs.BoolVar(&force, "force", false, "")
	fs.BoolVar(&dry, "dry-run", false, "")
	fs.BoolVar(&asJSON, "json", false, "")
	if err := fs.Parse(args); err != nil {
		return usageErr("fleet rollout: %v", err)
	}
	if fs.NArg() > 0 {
		return usageErr("fleet rollout: unexpected %q", fs.Arg(0))
	}
	if fleetFile == "" || version == "" || tarball == "" {
		return usageErr("fleet rollout needs --fleet, --version and --tarball")
	}
	fl, err := fleetdriver.Load(fleetFile)
	if err != nil {
		return fail("%v", err)
	}
	cmd := strings.Fields(sshCmd)
	if len(cmd) == 0 {
		cmd = fl.SSH
	}
	if len(cmd) == 0 {
		cmd = strings.Fields(c.env.Getenv("RUNTIME_ROLLOUT_SSH"))
	}
	if len(cmd) == 0 {
		cmd = []string{"ssh"}
	}
	p := &fleetdriver.Plan{
		Fleet: fl, Runner: fleetdriver.ExecRunner{Cmd: cmd, Stderr: c.env.Stderr},
		Version: version, Tarball: tarball, Delta: delta, DeltaBase: deltaBase,
		Force: force, StageJobs: stageJobs, ActivateJobs: activateJobs, Soak: soak, DryRun: dry,
		Log: func(f string, a ...any) { fmt.Fprintf(c.env.Stdout, f+"\n", a...) },
	}
	if only != "" {
		p.Only = strings.Split(only, ",")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	res, err := p.Run(ctx)
	if asJSON || c.json {
		raw, _ := json.Marshal(res)
		c.out("%s\n", raw)
	}
	return err
}
