package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const envSeedOrg = "orgs:\n  - slug: alpha\n    name: Альфа\n    admins: [anna@example.com]\n    tenants:\n      - slug: alpha-main\n        name: Альфа\n        owners: [anna@example.com]\n        teams:\n          - slug: general\n            name: Общая\n            members: [anna@example.com]\n"

const envSeedFile = `orgs:
  - org: alpha
    items:
      - kind: repo
        name: Монорепо
        source: {url: "https://git.example.org/a/mono.git", dir: "~/code/mono"}
      - kind: access
        name: GitLab
        source: {service: gitlab, host: git.example.org}
      - kind: script
        name: Установка
        hint: ставит зависимости
        source: {from: repo, repo: Монорепо, path: scripts/setup.sh}
      - kind: script
        name: Скрипт команды
        source: {from: text, body: "echo team"}
        owner: {layer: team, tenant: alpha-main, team: Общая}
    entries:
      - {layer: org, item: GitLab, pinned: true}
      - {layer: org, item: Монорепо}
      - {layer: org, item: Установка}
      - {layer: team, tenant: alpha-main, team: Общая, item: Скрипт команды}
`

func TestEnvSeedCommand(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	e := env(map[string]string{"HUB_PUBLIC_URL": "http://127.0.0.1:8080", "HUB_DATA_DIR": filepath.Join(dir, "data")})
	do := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, e, &out, &errOut)
		return code, out.String(), errOut.String()
	}
	if code, _, errOut := do("seed", "--file", write("org.yaml", envSeedOrg)); code != 0 {
		t.Fatalf("seed: %d %s", code, errOut)
	}
	file := write("env.yaml", envSeedFile)
	if code, _, errOut := do("env-seed"); code != 2 || !strings.Contains(errOut, "usage: hub env-seed") {
		t.Errorf("no file: %d %q", code, errOut)
	}
	count := func() int {
		cfg, _ := config.Load(e)
		db, err := store.Open(context.Background(), cfg.DatabaseURL)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		o, _ := db.S().OrgBySlug(context.Background(), "alpha")
		items, _ := db.S().CatalogItems(context.Background(), o.ID, true)
		return len(items)
	}
	if code, out, errOut := do("env-seed", "--file", file, "--dry-run"); code != 0 || !strings.Contains(out, "+ script «Установка»") || !strings.Contains(out, "nothing written") || count() != 0 {
		t.Errorf("dry run: %d %q %q (items %d)", code, out, errOut, count())
	}
	if code, out, errOut := do("env-seed", "--file", file); code != 0 || !strings.Contains(out, "changes written") || !strings.Contains(out, "+ entry script «Установка» in org") || count() != 4 {
		t.Errorf("run: %d %q %q", code, out, errOut)
	}
	for _, args := range [][]string{{"env-seed", "--file", file}, {"env-seed", "--file", file, "--dry-run"}} {
		if code, out, errOut := do(args...); code != 0 || strings.TrimSpace(out) != "изменений нет" {
			t.Errorf("%v again: %d %q %q", args, code, out, errOut)
		}
	}
	// Each change is in the journal and the audit trail.
	cfg, _ := config.Load(e)
	db, err := store.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	var audits, changes int
	_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_events WHERE action IN ('catalog.add', 'env.entry.add')`).Scan(&audits)
	_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM org_changes WHERE kind = 'env'`).Scan(&changes)
	db.Close()
	if audits != 8 || changes != 8 {
		t.Errorf("audit %d, changes %d, want 8 and 8", audits, changes)
	}
	// A changed hint is an update of the item only.
	changed := write("env2.yaml", strings.Replace(envSeedFile, "ставит зависимости", "ставит всё", 1))
	if code, out, _ := do("env-seed", "--file", changed); code != 0 || !strings.Contains(out, "~ script «Установка» (hint)") || strings.Count(out, "\n") != 2 {
		t.Errorf("update: %d %q", code, out)
	}
	// The same rules as the API: a bad source, a script of a team in the org layer, an unknown organization.
	for name, body := range map[string]string{
		"brew":    strings.Replace(envSeedFile, `"echo team"`, `"brew install jq"`, 1),
		"layer":   envSeedFile + "      - {layer: org, item: Скрипт команды}\n",
		"org":     strings.Replace(envSeedFile, "org: alpha", "org: nope", 1),
		"tenant":  strings.Replace(envSeedFile, "tenant: alpha-main, team", "tenant: nope, team", 1),
		"secrets": strings.Replace(envSeedFile, `source: {from: text, body: "echo team"}`, `source: {from: text, body: "echo team", secrets: [npm-token]}`, 1),
		"access":  strings.Replace(envSeedFile, "    entries:", "      - kind: access\n        name: Команды\n        source: {service: github}\n        owner: {layer: team, tenant: alpha-main, team: Общая}\n    entries:", 1),
	} {
		if code, _, errOut := do("env-seed", "--file", write(name+".yaml", body)); code != 1 {
			t.Errorf("%s: %d %q", name, code, errOut)
		}
	}
}
