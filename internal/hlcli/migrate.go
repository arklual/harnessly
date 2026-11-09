package hlcli

import (
	"encoding/json"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const migrateHelp = `harnessly migrate status [--json]
harnessly migrate retry [KIND/OBJECT...]
harnessly migrate skip KIND/OBJECT...
harnessly migrate verify [--json]

The move of this machine's own data into the hub (docs/migrations/0001-data-to-hub.md): under a hub with a store, the
runtime moves by itself the pictures of its chats (uploads), the files its agents handed over (files) and the handoff
notes (handoff), checks the hub has each one under its sha256, and keeps a journal here and in the hub. Nothing on the
machine is deleted.

  status    how far it is: objects in the hub of all, waiting, failed (each with its code and error), and whether the
            hub keeps the journal too. --json prints the runtime's answer as it is: the brief of «Продолжить с агентом».
  retry     try the failed objects again (all of them without arguments). Fix the cause first: the page of the move
            says it for every code (harnessly docs read self-host/0001-data-to-hub).
  skip      leave the named objects on the machine only: the move can be over without them. They still open while
            the machine is on.
  verify    ask the hub again for every object the journal calls moved; one it has lost is moved again.

KIND/OBJECT is as status prints it: uploads/up_…, files/<sha256>, handoff/<path>.
`

// harnessly migrate: the move of the machine's data into the hub (the runtime's /api/cli/migrate).
func (c *cli) migrate(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		c.out("%s", migrateHelp)
		return nil
	}
	act, rest := args[0], args[1:]
	switch act {
	case "status":
		if len(rest) > 0 {
			return usageErr("migrate status takes only --json, got %q", rest[0])
		}
		raw, err := c.request("GET", "/api/cli/migrate", nil, nil)
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		return c.printMove(raw)
	case "retry", "skip":
		keys, err := moveKeys(rest)
		if err != nil {
			return err
		}
		if act == "skip" && len(keys) == 0 {
			return usageErr("migrate skip needs the objects to leave on the machine: KIND/OBJECT as status prints them")
		}
		raw, err := c.request("POST", "/api/cli/migrate/"+act, nil, map[string]any{"objects": keys})
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var ans struct {
			Marked int             `json:"marked"`
			Status json.RawMessage `json:"status"`
		}
		if err := json.Unmarshal(raw, &ans); err != nil {
			return fail("the runtime answered something else: %v", err)
		}
		if act == "retry" {
			c.out("%d objects to try again: the move goes on now\n", ans.Marked)
		} else {
			c.out("%d objects left on the machine only\n", ans.Marked)
		}
		return c.printMove(ans.Status)
	case "verify":
		if len(rest) > 0 {
			return usageErr("migrate verify takes only --json, got %q", rest[0])
		}
		raw, err := c.request("POST", "/api/cli/migrate/verify", nil, map[string]any{})
		if err != nil {
			return err
		}
		if c.json {
			c.printRaw(raw)
			return nil
		}
		var ans struct {
			Verify struct {
				Checked int                     `json:"checked"`
				Missing []hubproto.MigrationKey `json:"missing"`
			} `json:"verify"`
		}
		if err := json.Unmarshal(raw, &ans); err != nil {
			return fail("the runtime answered something else: %v", err)
		}
		c.out("%d objects asked of the hub, %d missing\n", ans.Verify.Checked, len(ans.Verify.Missing))
		for _, k := range ans.Verify.Missing {
			c.out("  %s/%s: moved again\n", k.Kind, k.Object)
		}
		if len(ans.Verify.Missing) > 0 {
			return fail("the hub had lost %d objects; they are being moved again", len(ans.Verify.Missing))
		}
		return nil
	}
	return usageErr("unknown migrate command %q (there are: status, retry, skip, verify)", act)
}

func moveKeys(args []string) ([]hubproto.MigrationKey, error) {
	keys := make([]hubproto.MigrationKey, 0, len(args))
	for _, a := range args {
		kind, object, ok := strings.Cut(a, "/")
		if !ok || kind == "" || object == "" {
			return nil, usageErr("%q is not KIND/OBJECT (uploads/up_…, files/<sha256>, handoff/<path>)", a)
		}
		keys = append(keys, hubproto.MigrationKey{Kind: kind, Object: object})
	}
	return keys, nil
}

// moveStatus is the runtime's answer of GET /api/cli/migrate.
type moveStatus struct {
	hubproto.MigrationStatus
	Hub       string `json:"hub"`
	Running   bool   `json:"running"`
	KeptUntil string `json:"keptUntil"`
	Off       string `json:"off"`
}

func (c *cli) printMove(raw []byte) error {
	var st moveStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return fail("the runtime answered something else: %v", err)
	}
	if st.Off != "" {
		c.out("%s\n", st.Off)
		return nil
	}
	state := "moving"
	switch {
	case st.MigrationDone():
		state = "done"
	case st.Failed > 0 && st.Pending == 0:
		state = "stopped on errors"
	}
	c.out("move %s: %d of %d objects in the hub, %d waiting, %d failed, %d left on the machine: %s\n",
		st.Migration, st.Verified, st.Total, st.Pending, st.Failed, st.Skipped, state)
	switch st.Hub {
	case "on":
		c.out("the hub keeps the journal too\n")
	case "":
	default:
		c.out("hub: %s\n", st.Hub)
	}
	if len(st.Failures) > 0 {
		c.out("failed:\n")
		for _, f := range st.Failures {
			c.out("  %s/%s  %s  %s (%d attempts)\n", f.Kind, f.Object, f.Code, f.Error, f.Attempts)
		}
		c.out("fix the cause, then: harnessly migrate retry (or skip KIND/OBJECT)\n")
	}
	if st.KeptUntil != "" {
		c.out("the local copies stay: %s\n", st.KeptUntil)
	}
	c.out("page: %s (harnessly docs read self-host/%s)\n", st.Doc, st.Migration)
	if st.Failed > 0 {
		return fail("%d objects failed", st.Failed)
	}
	return nil
}
