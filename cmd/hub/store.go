package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/storehub"
)

const storeUsage = `usage: hub store backup --dir <folder>
       hub store gc [--keep N] [--days D] [--grace 24h] [--dry-run]
       hub store migrate (--from <folder> | --from-db <url> --from-dir <folder>) [--rooms a,b] [--into-room r]

The store (docs/hub/store.md) is configured by HUB_STORE_*: these commands open the same database and files the hub does.
`

// runStore is `hub store …`: the store's backup, garbage collection and the copy of another store into this one.
func runStore(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, storeUsage)
		return 2
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("store "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		dir                   = fs.String("dir", "", "the folder the backup is written to")
		keep                  = fs.Int("keep", 20, "gc: the newest N revisions of every file stay")
		days                  = fs.Int("days", 30, "gc: revisions younger than this many days stay")
		grace                 = fs.Duration("grace", 24*time.Hour, "gc: a blob nothing refers to is deleted after this long")
		dry                   = fs.Bool("dry-run", false, "gc: count what would go and delete nothing")
		from, fromDB, fromDir = fs.String("from", "", "migrate: a store's folder (SQLite and files: a runtime's or a hub's)"), fs.String("from-db", "", "migrate: the source database URL"), fs.String("from-dir", "", "migrate: the source files folder")
		rooms, intoRoom       = fs.String("rooms", "", "migrate: only these rooms, comma separated"), fs.String("into-room", "", "migrate: the room the copied room becomes (one room only)")
	)
	switch sub {
	case "backup", "gc", "migrate":
	default:
		fmt.Fprint(stderr, storeUsage)
		return 2
	}
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		fmt.Fprint(stderr, storeUsage)
		return 2
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if cfg.Store.Off {
		fmt.Fprintln(stderr, "hub store: the store is off (HUB_STORE=off)")
		return 2
	}
	meta, blobs, err := storehub.OpenDrivers(ctx, cfg)
	if err != nil {
		fmt.Fprintln(stderr, "hub store:", err)
		return 1
	}
	defer meta.Close()
	defer blobs.Close()
	switch sub {
	case "backup":
		if *dir == "" {
			fmt.Fprint(stderr, storeUsage)
			return 2
		}
		rep, err := filestore.Backup(ctx, meta, blobs, *dir)
		fmt.Fprintf(stdout, "backup         %d rooms into %s: %d blobs copied (%d bytes), %d already there\n", rep.Rooms, *dir, rep.BlobsCopied, rep.BytesCopied, rep.BlobsKept)
		if err != nil {
			fmt.Fprintln(stderr, "hub store backup:", err)
			return 1
		}
		return 0
	case "gc":
		reps, err := filestore.RunGC(ctx, meta, blobs, filestore.Retention{Keep: *keep, Days: *days, Grace: *grace}, *dry)
		for _, r := range reps {
			fmt.Fprintf(stdout, "%-14s %d revisions pruned, %d blobs deleted (%d bytes), %d kept for the grace period\n", r.Room, r.RevisionsPruned, r.BlobsDeleted, r.BytesFreed, r.BlobsKept)
		}
		if err != nil {
			fmt.Fprintln(stderr, "hub store gc:", err)
			return 1
		}
		if *dry {
			fmt.Fprintln(stdout, "dry run: nothing deleted")
		}
		return 0
	}
	return storeMigrate(ctx, meta, blobs, *from, *fromDB, *fromDir, *rooms, *intoRoom, stdout, stderr)
}

func storeMigrate(ctx context.Context, meta filestore.MetaStore, blobs filestore.BlobStore, from, fromDB, fromDir, rooms, intoRoom string, stdout, stderr io.Writer) int {
	if from != "" {
		if fromDB != "" || fromDir != "" {
			fmt.Fprint(stderr, storeUsage)
			return 2
		}
		fromDB, fromDir = "sqlite://"+from, from
	}
	if fromDB == "" || fromDir == "" {
		fmt.Fprint(stderr, storeUsage)
		return 2
	}
	o := filestore.MigrateOptions{ToMeta: meta, ToBlobs: blobs}
	for _, r := range strings.Split(rooms, ",") {
		if r = strings.TrimSpace(r); r != "" {
			o.Rooms = append(o.Rooms, r)
		}
	}
	if intoRoom != "" {
		if len(o.Rooms) != 1 || !filestore.ValidRoom(intoRoom) {
			fmt.Fprintln(stderr, "hub store migrate: --into-room takes one room of --rooms and a valid room name")
			return 2
		}
		o.Into = func(string) string { return intoRoom }
	}
	src, err := filestore.OpenMetaStore(ctx, fromDB)
	if err != nil {
		fmt.Fprintln(stderr, "hub store migrate: the source database:", err)
		return 1
	}
	defer src.Close()
	srcBlobs, err := filestore.NewFileBlobs(fromDir)
	if err != nil {
		fmt.Fprintln(stderr, "hub store migrate: the source files:", err)
		return 1
	}
	defer srcBlobs.Close()
	o.FromMeta, o.FromBlobs = src, srcBlobs
	rep, err := filestore.Migrate(ctx, o)
	fmt.Fprintf(stdout, "migrate        %d rooms: %d revisions, %d files, %d blobs (%d copied, %d bytes), verified %v\n",
		rep.Rooms, rep.History, rep.Files, rep.Blobs, rep.BlobsCopied, rep.BlobBytes, rep.Verified)
	if rep.MissingInSrc > 0 {
		fmt.Fprintf(stdout, "missing        %d blobs a revision refers to were not in the source\n", rep.MissingInSrc)
	}
	if err != nil {
		fmt.Fprintln(stderr, "hub store migrate:", err)
		return 1
	}
	return 0
}

// describeStore is the store's line of check-config.
func describeStore(w io.Writer, cfg *config.Config) {
	st := cfg.Store
	if st.Off {
		fmt.Fprintln(w, "store          off (HUB_STORE=off): runtimes keep their data as before")
		return
	}
	db := "SQLite in " + st.Dir
	if !strings.HasPrefix(st.DatabaseURL, "sqlite://") {
		db = "Postgres"
		if st.PGPrefix != "" {
			db += " (the hub's, tables " + st.PGPrefix + "*)"
		}
	}
	files := "the folder " + st.Dir
	if st.S3.Endpoint != "" {
		files = "s3 " + st.S3.Endpoint + "/" + st.S3.Bucket + "/" + st.S3.Prefix
	}
	fmt.Fprintf(w, "store          %s, files in %s\n", db, files)
	if st.LegacySpace != "" {
		fmt.Fprintf(w, "store legacy   the room design is the library of the space %s\n", st.LegacySpace)
	}
}
