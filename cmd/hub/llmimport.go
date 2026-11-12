package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/llmimport"
)

const llmImportUsage = `usage: hub import-llm [--apply] [--from postgres://…] [--persons a@example.org,…] [--everyone] [--history] [--voice-everyone] [--machines vm-name=<hub machine id>,…] [--features f1,f2] [--automations --automations-owner <email> [--automations-machine <hub machine id>]]

Moves what the old gateway's database holds into the hub (HRN-1166, docs/hub/llm-import.md): the rollout switches of
the named people and the access log of the shared documents, then checks the people, documents, share links and
conversations the hub already holds against llm's. llm is only read. Without --apply nothing is written: the report
says what would move. With --automations the rules of llm that are switched on become the rules of --automations-owner (a person of the hub),
running on --automations-machine or, without it, on any connected machine of theirs (HRN-1039). The flags default to HUB_LLM_IMPORT_URL, _PERSONS, _MACHINES and _FEATURES.
`

// runImportLLM is `hub import-llm`.
func runImportLLM(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	env := func(k string) string { v, _ := lookup(k); return v }
	fs := flag.NewFlagSet("hub import-llm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	apply := fs.Bool("apply", false, "write to the hub (default: a dry run)")
	from := fs.String("from", env("HUB_LLM_IMPORT_URL"), "llm's Postgres (HUB_LLM_IMPORT_URL)")
	persons := fs.String("persons", env("HUB_LLM_IMPORT_PERSONS"), "the people whose switches move (HUB_LLM_IMPORT_PERSONS)")
	everyone := fs.Bool("everyone", false, "move the switches of every person and of anyone")
	machines := fs.String("machines", env("HUB_LLM_IMPORT_MACHINES"), "llm machine name=hub machine id, comma-separated (HUB_LLM_IMPORT_MACHINES)")
	history := fs.Bool("history", env("HUB_LLM_IMPORT_HISTORY") == "1", "also move the chats' transcripts, sidebars, offline prompts and the documents the hub lacks (HUB_LLM_IMPORT_HISTORY=1)")
	evalToo := fs.Bool("eval", env("HUB_LLM_IMPORT_EVAL") == "1", "with --history, also move the evaluation of the harness: the catalog, the schedules, the finished batches and runs (HUB_LLM_IMPORT_EVAL=1)")
	voiceAll := fs.Bool("voice-everyone", env("HUB_LLM_IMPORT_VOICE_EVERYONE") == "1", "move the voice takes and weekly answers of every person (HUB_LLM_IMPORT_VOICE_EVERYONE=1)")
	features := fs.String("features", env("HUB_LLM_IMPORT_FEATURES"), "the switches to move (default: the ones the hub's paths read)")
	automations := fs.Bool("automations", env("HUB_LLM_IMPORT_AUTOMATIONS") == "1", "also move the automation rules of llm that are switched on (HUB_LLM_IMPORT_AUTOMATIONS=1)")
	autoOwner := fs.String("automations-owner", env("HUB_LLM_IMPORT_AUTOMATIONS_OWNER"), "the hub person who gets the rules (HUB_LLM_IMPORT_AUTOMATIONS_OWNER)")
	autoMachine := fs.String("automations-machine", env("HUB_LLM_IMPORT_AUTOMATIONS_MACHINE"), "the hub machine id that runs them; empty: any connected machine of the owner (HUB_LLM_IMPORT_AUTOMATIONS_MACHINE)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 || *from == "" {
		fmt.Fprint(stderr, llmImportUsage)
		return 2
	}
	opt := llmimport.Options{DryRun: !*apply, Everyone: *everyone, VoiceEveryone: *voiceAll, By: "llm-import"}
	opt.Persons = splitList(*persons)
	opt.Features = splitList(*features)
	if len(opt.Persons) == 0 && !opt.Everyone {
		fmt.Fprintln(stderr, "hub import-llm: say for whom the switches move: --persons or --everyone")
		return 2
	}
	if *automations && strings.TrimSpace(*autoOwner) == "" {
		fmt.Fprintln(stderr, "hub import-llm: say whose the rules become: --automations-owner <email of a hub person>")
		return 2
	}
	opt.AutomationsOwner, opt.AutomationsMachine = *autoOwner, *autoMachine
	if *machines != "" {
		m, err := config.ParsePairs(*machines)
		if err != nil {
			fmt.Fprintln(stderr, "hub import-llm:", err)
			return 2
		}
		opt.Machines = m
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	log := logger(stderr, cfg.LogLevel)
	db, err := openDB(ctx, cfg, log)
	if err != nil {
		fmt.Fprintln(stderr, "hub import-llm:", err)
		return exitCode(err)
	}
	defer db.Close()
	src, err := llmimport.Open(ctx, *from)
	if err != nil {
		fmt.Fprintln(stderr, "hub import-llm:", err)
		return 1
	}
	defer src.Close()
	data, err := src.Read(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "hub import-llm:", err)
		return 1
	}
	if *history {
		opt.History = src
		if *evalToo {
			opt.Eval = src
		}
	} else if *evalToo {
		fmt.Fprintln(stderr, "hub import-llm: --eval goes with --history (it learns the hub machines from the chats)")
		return 2
	}
	if *automations {
		opt.Automations = src
	}
	rep, err := llmimport.Run(ctx, db, data, opt)
	fmt.Fprint(stdout, rep.Summary())
	if err != nil {
		fmt.Fprintln(stderr, "hub import-llm:", err)
		return 1
	}
	return 0
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
