package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/docs"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/diag"
	"github.com/ly-solutions/harnessly/internal/hub/migration"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// moveCheck is the «data move» line of `hub doctor` (docs/migrations, HRN-883) and what it prints under it: every
// machine's move in a line, and for the codes that failed, their cause and fix from the page of the move.
type moveCheck struct {
	check
	lines []string
}

// checkMove reads the journal of the machines' move of their data into the store. It reads only: a database the hub
// has not migrated yet has no journal, and that is said, not failed.
func checkMove(ctx context.Context, cfg *config.Config) moveCheck {
	c := moveCheck{check: check{name: "data move", verdict: vSkip}}
	if path := cfg.SQLitePath(); path != "" {
		if _, err := os.Stat(path); err != nil {
			c.detail = "no database yet"
			return c
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := store.Connect(cctx, cfg.DatabaseURL)
	if err != nil {
		c.detail = "the database does not answer (see database)"
		return c
	}
	defer db.Close()
	moves, err := migration.New(db, nil).Summary(cctx)
	if err != nil {
		c.detail = "no journal yet (the hub adds it at start)"
		return c
	}
	return describeMoves(moves)
}

func describeMoves(moves []hubproto.MigrationStatus) moveCheck {
	c := moveCheck{check: check{name: "data move", verdict: vOK}}
	if len(moves) == 0 {
		c.detail = "no machine has moved its data yet"
		return c
	}
	var done, failed int
	codes := map[string]string{} // code → migration
	var order []string
	for _, m := range moves {
		switch {
		case m.Failed > 0:
			failed++
		case m.FinishedAt != "":
			done++
		}
		c.lines = append(c.lines, migration.Line(m))
		for _, f := range m.Failures {
			if _, ok := codes[f.Code]; !ok && f.Code != "" {
				codes[f.Code] = m.Migration
				order = append(order, f.Code)
			}
		}
	}
	c.detail = fmt.Sprintf("%d machines: %d done, %d moving, %d stopped on errors", len(moves), done, len(moves)-done-failed, failed)
	if failed == 0 {
		return c
	}
	c.verdict = vWarn
	c.problem = &diag.Problem{Code: diag.MoveFailed,
		What: fmt.Sprintf("%d machines stopped moving their data on objects with errors; the machines keep those objects", failed),
		Fix:  "on the machine: harnessly migrate status, then retry or skip as the page of the move says for the code"}
	for _, code := range order {
		id := codes[code]
		c.lines = append(c.lines, fmt.Sprintf("%s (%s):", code, migration.DocPath(id)))
		for _, l := range codeSection(id, code) {
			c.lines = append(c.lines, "  "+l)
		}
	}
	return c
}

// codeSection is the cause and fix of an error code as the page of the move this version carries says them.
func codeSection(id, code string) []string {
	raw, err := docs.Migration(id)
	if err != nil {
		return []string{"(this version carries no page " + migration.DocPath(id) + ")"}
	}
	var out []string
	in := false
	for _, l := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(l, "### "+code):
			in = true
			continue
		case in && (strings.HasPrefix(l, "### ") || strings.HasPrefix(l, "## ")):
			return trimBlank(out)
		}
		if in {
			out = append(out, l)
		}
	}
	if !in {
		return []string{"(the page has no section for " + code + ")"}
	}
	return trimBlank(out)
}

func trimBlank(ls []string) []string {
	for len(ls) > 0 && strings.TrimSpace(ls[0]) == "" {
		ls = ls[1:]
	}
	for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
		ls = ls[:len(ls)-1]
	}
	return ls
}
