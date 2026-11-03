package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/seed"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// runSeed is `hub seed --file seed.yaml [--dry-run]`: organizations, tenants, teams, people and roles from a file, on
// the database the hub's configuration names (docs/hub/orgs.md, «Seeding»).
func runSeed(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hub seed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "the seed file (YAML)")
	dry := fs.Bool("dry-run", false, "print what would change and change nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" || fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: hub seed --file seed.yaml [--dry-run]  (orgs: and offboard:, docs/hub/orgs.md)")
		return 2
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	f, err := os.Open(*file)
	if err != nil {
		fmt.Fprintln(stderr, "hub seed:", err)
		return 2
	}
	plan, err := seed.Parse(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(stderr, "hub seed:", err)
		return 2
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		fmt.Fprintln(stderr, "hub seed: data folder:", err)
		return 1
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(stderr, "hub seed:", err)
		return 1
	}
	defer db.Close()
	changes, err := seed.Apply(ctx, db, plan, *dry, time.Now())
	if err != nil {
		fmt.Fprintln(stderr, "hub seed:", err)
		return 1
	}
	if len(changes) == 0 {
		fmt.Fprintln(stdout, "изменений нет")
		return 0
	}
	for _, c := range changes {
		fmt.Fprintln(stdout, c)
	}
	if *dry {
		fmt.Fprintf(stdout, "dry run: %d changes, nothing written\n", len(changes))
	} else {
		fmt.Fprintf(stdout, "%d changes written\n", len(changes))
	}
	return 0
}
