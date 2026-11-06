package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/envseed"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// runEnvSeed is `hub env-seed --file env.yaml [--dry-run]`: catalog items and environment lists of organizations from a
// file, on the database the hub's configuration names (docs/hub/environment.md, «Machine setup»).
func runEnvSeed(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hub env-seed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "the env-seed file (YAML)")
	dry := fs.Bool("dry-run", false, "print what would change and change nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" || fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: hub env-seed --file env.yaml [--dry-run]  (docs/hub/environment.md)")
		return 2
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	f, err := os.Open(*file)
	if err != nil {
		fmt.Fprintln(stderr, "hub env-seed:", err)
		return 2
	}
	plan, err := envseed.Parse(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(stderr, "hub env-seed:", err)
		return 2
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		fmt.Fprintln(stderr, "hub env-seed: data folder:", err)
		return 1
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(stderr, "hub env-seed:", err)
		return 1
	}
	defer db.Close()
	changes, err := envseed.Apply(ctx, db, plan, *dry, time.Now())
	if err != nil {
		fmt.Fprintln(stderr, "hub env-seed:", err)
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
