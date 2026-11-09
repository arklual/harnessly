package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
)

const designUsage = `usage: hub design import-gateway [--dry-run] [--from postgres://…] [--space slug] [--teams team=<id>,…] [--machines person@example.org@vm-1=<id>,…] [--machine-prefix p-] [--default-machine id]

Moves the designs' access from a cluster's gateway into the hub (HRN-880, docs/hub/design-access.md): each project
once, keeping what the hub has, then hands every person × machine the hub found to the hub. The flags default to
HUB_DESIGN_IMPORT_*; the hub also runs it at start when HUB_DESIGN_IMPORT_GATEWAY=dry-run|apply.
`

// runDesign is `hub design …`.
func runDesign(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "import-gateway" {
		fmt.Fprint(stderr, designUsage)
		return 2
	}
	fs := flag.NewFlagSet("hub design import-gateway", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dry := fs.Bool("dry-run", false, "report what would move and write nothing")
	from := fs.String("from", "", "the gateway's Postgres (HUB_DESIGN_IMPORT_GATEWAY_URL)")
	space := fs.String("space", "", "the space's slug (HUB_DESIGN_IMPORT_SPACE, HUB_STORE_LEGACY_SPACE)")
	teams := fs.String("teams", "", "gateway team=hub team id, comma-separated (HUB_DESIGN_IMPORT_TEAMS)")
	machines := fs.String("machines", "", "person@machine=hub machine id, comma-separated (HUB_DESIGN_IMPORT_MACHINES)")
	fallback := fs.String("default-machine", "", "the machine of a project whose owner has none in the space")
	prefix := fs.String("machine-prefix", "", "what the hub machines' names put before a gateway machine's (HUB_DESIGN_IMPORT_MACHINE_PREFIX)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprint(stderr, designUsage)
		return 2
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	d := cfg.DesignImport
	d.Mode = "apply"
	if *dry {
		d.Mode = "dry-run"
	}
	for _, v := range []struct {
		dst *string
		val string
	}{{&d.URL, *from}, {&d.Space, *space}, {&d.DefaultMachine, *fallback}, {&d.MachinePrefix, *prefix}} {
		if v.val != "" {
			*v.dst = v.val
		}
	}
	if d.Space == "" {
		d.Space = cfg.Store.LegacySpace
	}
	for _, v := range []struct {
		dst *map[string]string
		val string
	}{{&d.Teams, *teams}, {&d.Machines, *machines}} {
		if v.val == "" {
			continue
		}
		m, err := config.ParsePairs(v.val)
		if err != nil {
			fmt.Fprintln(stderr, "hub design:", err)
			return 2
		}
		*v.dst = m
	}
	if d.URL == "" || d.Space == "" {
		fmt.Fprintln(stderr, "hub design: the gateway's database (--from) and the space (--space) are needed")
		return 2
	}
	log := logger(stderr, cfg.LogLevel)
	db, err := openDB(ctx, cfg, log)
	if err != nil {
		fmt.Fprintln(stderr, "hub design:", err)
		return exitCode(err)
	}
	defer db.Close()
	svc := designaccess.New(designaccess.Deps{DB: db, Log: log, Shared: !cfg.Store.Off})
	rep, linked, err := moveDesigns(ctx, svc, d)
	fmt.Fprint(stdout, rep.Summary())
	if err != nil {
		fmt.Fprintln(stderr, "hub design:", err)
		return 1
	}
	fmt.Fprintln(stdout, outcomeLine(rep, linked))
	if rep.Failed() {
		return 1
	}
	return 0
}

func moveDesigns(ctx context.Context, svc *designaccess.Service, d config.DesignImportConfig) (designaccess.ImportReport, int, error) {
	return svc.MoveFromGateway(ctx, d.URL, designaccess.ImportOptions{
		Space: d.Space, Teams: d.Teams, Machines: d.Machines, MachinePrefix: d.MachinePrefix, DefaultMachine: d.DefaultMachine,
		DryRun: d.Mode != "apply", By: "hub-import",
	})
}

func outcomeLine(rep designaccess.ImportReport, linked int) string {
	switch {
	case rep.DryRun:
		return "dry run: nothing written"
	case rep.Failed():
		return "some projects failed: no pair was handed to the hub; fix them and run again"
	default:
		return fmt.Sprintf("%d pairs handed to the hub", linked)
	}
}

// importDesignsAtStart is the release step: HUB_DESIGN_IMPORT_GATEWAY runs the move once the hub is up, again after a
// failure, a few times; a later start runs it again and finds every project moved.
func importDesignsAtStart(ctx context.Context, cfg *config.Config, svc *designaccess.Service, log *slog.Logger) {
	if cfg.DesignImport.Mode == "" || svc == nil {
		return
	}
	for attempt := 1; attempt <= 5; attempt++ {
		rep, linked, err := moveDesigns(ctx, svc, cfg.DesignImport)
		if err == nil && !rep.Failed() {
			log.Info("design access moved from the gateway", "mode", cfg.DesignImport.Mode, "pairs", linked,
				"report", strings.TrimSpace(rep.LogSummary()))
			return
		}
		log.Error("design access not moved from the gateway", "mode", cfg.DesignImport.Mode, "attempt", attempt, "error", err,
			"report", strings.TrimSpace(rep.LogSummary()))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}
