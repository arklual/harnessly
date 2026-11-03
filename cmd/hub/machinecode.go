package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// machineCodeTTL is how long a code from the command can be used, as one from the page (machines.CodeTTL).
const machineCodeTTL = 30 * time.Minute

// maxActiveMachineCodes bounds the unused codes of a space, as on the page (machines.maxActiveCodes).
const maxActiveMachineCodes = 20

// runMachineCode is `hub machine-code --space <slug> --owner <email> --profile <p>`: a registration code made by the
// installation's operator for a machine that already runs (a closed cluster's agent that the hub should see,
// AGENT_HUB_OBSERVE_FILE). The machine becomes the owner's, as if they had taken the code on the page. The code goes to
// stdout alone, so it can be kept in a file; it is used once and lasts 30 minutes.
func runMachineCode(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hub machine-code", flag.ContinueOnError)
	fs.SetOutput(stderr)
	space := fs.String("space", "", "the tenant (space) slug the machine joins")
	owner := fs.String("owner", "", "the email of the person whose machine it is; an owner of the space")
	profile := fs.String("profile", string(hubproto.ProfileFull), "the machine's isolation profile: dir, container or full")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *space == "" || *owner == "" || fs.NArg() > 0 || !hubproto.Profile(*profile).Valid() {
		fmt.Fprintln(stderr, "usage: hub machine-code --space <slug> --owner <email> [--profile dir|container|full]")
		return 2
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		fmt.Fprintln(stderr, "hub machine-code: data folder:", err)
		return 1
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(stderr, "hub machine-code:", err)
		return 1
	}
	defer db.Close()
	st := db.S()
	sp, err := st.SpaceBySlug(ctx, *space)
	if errors.Is(err, sql.ErrNoRows) {
		fmt.Fprintf(stderr, "hub machine-code: no space %q\n", *space)
		return 1
	} else if err != nil {
		fmt.Fprintln(stderr, "hub machine-code:", err)
		return 1
	}
	u, err := st.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(*owner)))
	if errors.Is(err, sql.ErrNoRows) {
		fmt.Fprintf(stderr, "hub machine-code: no person %s; they sign in once first, or the seed makes them\n", *owner)
		return 1
	} else if err != nil {
		fmt.Fprintln(stderr, "hub machine-code:", err)
		return 1
	}
	if role, err := st.Role(ctx, sp.ID, u.ID); err != nil {
		fmt.Fprintln(stderr, "hub machine-code:", err)
		return 1
	} else if role != store.RoleOwner {
		fmt.Fprintf(stderr, "hub machine-code: %s is not an owner of %s (role %q)\n", *owner, *space, role)
		return 1
	}
	if hubproto.Profile(*profile) == hubproto.ProfileContainer && cfg.ContainerImage == "" {
		fmt.Fprintln(stderr, "hub machine-code: the container profile needs HUB_CONTAINER_IMAGE")
		return 1
	}
	now := time.Now().UTC()
	if n, err := st.CountActiveRegCodes(ctx, sp.ID, now); err != nil {
		fmt.Fprintln(stderr, "hub machine-code:", err)
		return 1
	} else if n >= maxActiveMachineCodes {
		fmt.Fprintf(stderr, "hub machine-code: %s already has %d unused codes; wait 30 minutes\n", *space, n)
		return 1
	}
	code := "hx_" + store.RandomBase32(15)
	expires := now.Add(machineCodeTTL)
	if err := st.CreateRegCode(ctx, store.RegCode{CodeHash: store.HashToken(code), SpaceID: sp.ID, CreatedBy: u.ID, Profile: *profile, ExpiresAt: expires}); err != nil {
		fmt.Fprintln(stderr, "hub machine-code:", err)
		return 1
	}
	fmt.Fprintln(stdout, code)
	fmt.Fprintf(stderr, "a %s code for %s in %s, until %s\n", *profile, *owner, *space, expires.Format(time.RFC3339))
	return 0
}
