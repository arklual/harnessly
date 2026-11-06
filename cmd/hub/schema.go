package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// runSchema is `hub schema snapshot`: it migrates an empty database and writes the schema the migrations produce to
// internal/hub/store/schema/<dialect>.json. The tests and the start-up check compare databases with these files.
func runSchema(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "snapshot" {
		fmt.Fprintln(stderr, "usage: hub schema snapshot [--dir internal/hub/store/schema] [--postgres postgres://…]")
		return 2
	}
	fs := flag.NewFlagSet("hub schema snapshot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "internal/hub/store/schema", "folder for the snapshot files")
	pg := fs.String("postgres", "", "URL of a Postgres to take the Postgres snapshot from (a throwaway schema is made and dropped); empty: write the SQLite one only")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	tmp, err := os.MkdirTemp("", "hubschema")
	if err != nil {
		fmt.Fprintln(stderr, "hub schema:", err)
		return 1
	}
	defer os.RemoveAll(tmp)
	targets := map[string]string{"sqlite.json": "sqlite://" + filepath.Join(tmp, "snap.db")}
	if *pg != "" {
		targets["postgres.json"] = *pg
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(stderr, "hub schema:", err)
		return 1
	}
	for name, u := range targets {
		s, err := store.SnapshotSchema(ctx, u)
		if err != nil {
			fmt.Fprintln(stderr, "hub schema:", name+":", err)
			return 1
		}
		b, err := store.MarshalSchema(s)
		if err != nil {
			fmt.Fprintln(stderr, "hub schema:", err)
			return 1
		}
		if err := os.WriteFile(filepath.Join(*dir, name), b, 0o644); err != nil {
			fmt.Fprintln(stderr, "hub schema:", err)
			return 1
		}
		fmt.Fprintln(stdout, "wrote", filepath.Join(*dir, name))
	}
	return 0
}
