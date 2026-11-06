package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/sites"
)

const storageUsage = "usage: hub storage migrate [--dry-run] [--remove-source]  (moves the hub's files from its data folder to the bucket of HUB_STORAGE=s3)"

// runStorage is `hub storage migrate`: the one-time move of the files a hub already holds on disk into the bucket.
func runStorage(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "migrate" {
		fmt.Fprintln(stderr, storageUsage)
		return 2
	}
	fs := flag.NewFlagSet("storage migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dry := fs.Bool("dry-run", false, "count what would move and write nothing")
	remove := fs.Bool("remove-source", false, "remove a file from the data folder once the bucket holds it")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		fmt.Fprintln(stderr, storageUsage)
		return 2
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if !cfg.StorageS3() {
		fmt.Fprintln(stderr, "hub storage migrate: set HUB_STORAGE=s3 and HUB_S3_* first: the files move to that bucket")
		return 2
	}
	dst, err := sites.NewS3Blobs(sites.S3Config{Endpoint: cfg.SitesS3.Endpoint, Region: cfg.SitesS3.Region, Bucket: cfg.SitesS3.Bucket,
		Prefix: cfg.SitesS3.Prefix, AccessKey: cfg.SitesS3.AccessKey, SecretKey: cfg.SitesS3.SecretKey})
	if err == nil {
		err = dst.Check(ctx)
	}
	if err != nil {
		fmt.Fprintln(stderr, "hub storage migrate:", err)
		return 1
	}
	rep, err := sites.Migrate(ctx, cfg.SitesDir, dst, *dry, *remove)
	fmt.Fprintf(stdout, "sites          %d files in %s: %d copied (%d bytes), %d already in the bucket", rep.Found, cfg.SitesDir, rep.Copied, rep.Bytes, rep.Present)
	if *remove && !*dry {
		fmt.Fprintf(stdout, ", %d removed from the folder", rep.Removed)
	}
	fmt.Fprintln(stdout)
	for _, h := range rep.Skipped {
		fmt.Fprintf(stdout, "skipped        %s: the content does not match its name\n", h)
	}
	if err != nil {
		fmt.Fprintln(stderr, "hub storage migrate:", err)
		return 1
	}
	if *dry {
		fmt.Fprintln(stdout, "dry run: nothing written")
	}
	return 0
}
