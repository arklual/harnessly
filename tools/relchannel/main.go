// Command relchannel decides whether a published release may become the stable one and writes the stable pointer
// (internal/updatecheck, docs/update-channels.md). It reads files and writes a file: fetching the release and putting
// the pointer in place is the promote-stable workflow's.
//
//	relchannel promote -release release.json [-current stable.json] [-window 168h] [-allow-older] [-now RFC3339] -out stable.json
//
// release.json is the releases API's description of the release. The promotion is refused (exit 1, nothing written)
// for a draft, a pre-release, a release marked [withdrawn] in its title or notes, one that has been out for less than
// the window, and one older than the stable release in place (unless -allow-older).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ly-solutions/harnessly/internal/updatecheck"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "promote" {
		fmt.Fprintln(stderr, "relchannel promote -release release.json [-current stable.json] [-window 168h] [-allow-older] -out stable.json")
		return 2
	}
	fs := flag.NewFlagSet("promote", flag.ContinueOnError)
	fs.SetOutput(stderr)
	releaseFile := fs.String("release", "", "the release as the releases API describes it (JSON file)")
	currentFile := fs.String("current", "", "the stable pointer in place; a missing or empty file means there is none yet")
	window := fs.Duration("window", updatecheck.DefaultWindow, "how long the release must have been out")
	allowOlder := fs.Bool("allow-older", false, "let the pointer move back to an older release")
	nowRaw := fs.String("now", "", "the time of the promotion, RFC 3339 (default: now)")
	out := fs.String("out", "", "where the new pointer is written")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *releaseFile == "" || *out == "" {
		fmt.Fprintln(stderr, "relchannel: -release and -out are needed")
		return 2
	}
	if *window <= 0 {
		fmt.Fprintln(stderr, "relchannel: -window must be more than zero")
		return 2
	}
	p := updatecheck.Promotion{Now: time.Now(), Window: *window, AllowOlder: *allowOlder}
	if *nowRaw != "" {
		t, err := time.Parse(time.RFC3339, *nowRaw)
		if err != nil {
			fmt.Fprintf(stderr, "relchannel: -now: %v\n", err)
			return 2
		}
		p.Now = t
	}
	raw, err := os.ReadFile(*releaseFile)
	if err != nil {
		fmt.Fprintf(stderr, "relchannel: %v\n", err)
		return 1
	}
	var rel updatecheck.Candidate
	if err := json.Unmarshal(raw, &rel); err != nil {
		fmt.Fprintf(stderr, "relchannel: %s: %v\n", *releaseFile, err)
		return 1
	}
	if *currentFile != "" {
		cur, err := os.ReadFile(*currentFile)
		switch {
		case errors.Is(err, os.ErrNotExist) || (err == nil && len(cur) == 0):
		case err != nil:
			fmt.Fprintf(stderr, "relchannel: %v\n", err)
			return 1
		default:
			ptr, err := updatecheck.ParsePointer(cur)
			if err != nil {
				fmt.Fprintf(stderr, "relchannel: %s: %v\n", *currentFile, err)
				return 1
			}
			p.Current = &ptr
		}
	}
	ptr, err := updatecheck.Promote(rel, p)
	if err != nil {
		fmt.Fprintf(stderr, "relchannel: %v\n", err)
		return 1
	}
	body, _ := json.MarshalIndent(ptr, "", "  ")
	if err := os.WriteFile(*out, append(body, '\n'), 0o644); err != nil {
		fmt.Fprintf(stderr, "relchannel: %v\n", err)
		return 1
	}
	from := "none"
	if p.Current != nil {
		from = p.Current.Version
	}
	fmt.Fprintf(stdout, "stable: %s -> %s (released %s)\n", from, ptr.Version, ptr.ReleasedAt)
	return 0
}
