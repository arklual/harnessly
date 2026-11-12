package llmimport

import (
	"context"
	"strings"
	"testing"
)

// the gateway's rules that are on become one hub person's rules; a second run and a rule the owner already
// has change nothing; a dry run writes nothing; an owner the hub does not know, or a machine that is not theirs, is refused.

type fakeRules []Rule

func (f fakeRules) Rules(context.Context) ([]Rule, error) { return f, nil }

func ruleFixture() fakeRules {
	return fakeRules{
		{ID: "r1", Owner: "sergey@example.com", Name: "posthog-new-error-group", Trigger: `{"source":"posthog","filter":{"app":"web"}}`,
			Prompt: "Look at {{event}}", Model: "sonnet", Limits: `{"maxRunsPerDay":3,"quietHoursStartMsk":0,"quietHoursEndMsk":8}`},
		{ID: "r2", Owner: "sergey@example.com", Name: "already-there", Trigger: `{"source":"posthog"}`, Prompt: "gateway text", Model: "sonnet", Limits: `{}`},
	}
}

func automationsHub(t *testing.T) (ctx context.Context, run func(Options) (Report, error), count func(string) int) {
	t.Helper()
	ctx = context.Background()
	db := openHub(t)
	q := db.S().Q()
	for _, s := range []string{
		`INSERT INTO users (id, email, created_at) VALUES ('u-ann', 'ann@example.com', 1), ('u-bob', 'bob@example.com', 1)`,
		`INSERT INTO machines (id, name, hostname, os, arch, unix_user, profile, via, key_pub, enc_pub, agent_version, created_at, info, created_by)
			VALUES ('m-ann', 'ann-vm', 'h', '', '', '', '', '', 'k1', 'e1', '', 1, '', 'u-ann'), ('m-bob', 'bob-vm', 'h', '', '', '', '', '', 'k2', 'e2', '', 1, '', 'u-bob')`,
		`INSERT INTO automation_rules (id, owner_id, name, trigger, prompt_template, model, limits, machine_id, enabled, created_at, updated_at)
			VALUES ('x1', 'u-ann', 'already-there', '{}', 'the hub text', 'sonnet', '{}', NULL, 0, 1, 1)`,
	} {
		if _, err := q.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	run = func(o Options) (Report, error) {
		o.Automations = ruleFixture()
		if o.AutomationsOwner == "" {
			o.AutomationsOwner = "Ann@Example.com"
		}
		o.Persons = []string{"ann@example.com"}
		return Run(ctx, db, Data{}, o)
	}
	count = func(where string) int { return scalar(t, db, `SELECT count(*) FROM automation_rules WHERE `+where) }
	return
}

func TestAutomationsMoveOnceUnderTheOwner(t *testing.T) {
	_, run, count := automationsHub(t)

	rep, err := run(Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if a := rep.Automations; !a.On || a.Read != 2 || a.Added != 1 || a.Kept != 1 {
		t.Fatalf("dry run: %+v", a)
	}
	if n := count(`1 = 1`); n != 1 {
		t.Fatalf("a dry run wrote rules: %d", n)
	}

	rep, err = run(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a := rep.Automations; a.Added != 1 || a.Kept != 1 {
		t.Fatalf("report: %+v", a)
	}
	if n := count(`owner_id = 'u-ann' AND name = 'posthog-new-error-group' AND enabled = 1 AND machine_id IS NULL AND model = 'sonnet'`); n != 1 {
		t.Fatalf("the moved rule: %d", n)
	}
	if n := count(`name = 'already-there' AND prompt_template = 'the hub text' AND enabled = 0`); n != 1 {
		t.Fatal("the owner's own rule was overwritten")
	}
	if s := rep.Summary(); !strings.Contains(s, "posthog-new-error-group (llm owner sergey@example.com) → ann@example.com: added") {
		t.Fatalf("summary: %s", s)
	}

	again, err := run(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a := again.Automations; a.Added != 0 || a.Kept != 2 {
		t.Fatalf("second run: %+v", a)
	}
	if n := count(`1 = 1`); n != 2 {
		t.Fatalf("a second run added rules: %d", n)
	}
}

func TestAutomationsPinToTheOwnersMachine(t *testing.T) {
	_, run, count := automationsHub(t)
	if _, err := run(Options{AutomationsMachine: "m-bob"}); err == nil {
		t.Fatal("a machine of another person was accepted")
	}
	if n := count(`1 = 1`); n != 1 {
		t.Fatalf("a refused run wrote rules: %d", n)
	}
	if _, err := run(Options{AutomationsMachine: "m-ann"}); err != nil {
		t.Fatal(err)
	}
	if n := count(`name = 'posthog-new-error-group' AND machine_id = 'm-ann'`); n != 1 {
		t.Fatalf("the pinned rule: %d", n)
	}
}

func TestAutomationsNeedAKnownOwner(t *testing.T) {
	_, run, count := automationsHub(t)
	if _, err := run(Options{AutomationsOwner: "nobody@example.com"}); err == nil || !strings.Contains(err.Error(), "nobody@example.com") {
		t.Fatalf("an unknown owner: %v", err)
	}
	if n := count(`1 = 1`); n != 1 {
		t.Fatalf("wrote rules for an unknown owner: %d", n)
	}
}
