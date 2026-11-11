package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var flagName = regexp.MustCompile(`^(HUB|HARNESSLY)_[A-Z0-9_]+$`)

// namesInSource are the variable names the package's own files spell out (the registry and the tests aside).
func namesInSource(t *testing.T) map[string]bool {
	t.Helper()
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "flags.go" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(lit.Value); err == nil && flagName.MatchString(s) {
				names[s] = true
			}
			return true
		})
	}
	return names
}

func TestEveryVariableIsInTheRegistryAndBack(t *testing.T) {
	source := namesInSource(t)
	if len(source) < 100 {
		t.Fatalf("only %d names found in the source: the scan is broken", len(source))
	}
	registry := map[string]bool{}
	for _, f := range Flags {
		if registry[f.Name] {
			t.Errorf("%s is in the registry twice", f.Name)
		}
		registry[f.Name] = true
		if !source[f.Name] {
			t.Errorf("%s is in the registry and nothing reads it: remove the row", f.Name)
		}
	}
	for name := range source {
		if !registry[name] {
			t.Errorf("%s is read and is not in the registry: add a row with its owner to flags.go", name)
		}
	}
}

func TestRegistryRows(t *testing.T) {
	sorted := sort.SliceIsSorted(Flags, func(i, j int) bool {
		if Flags[i].Owner != Flags[j].Owner {
			return Flags[i].Owner < Flags[j].Owner
		}
		return Flags[i].Name < Flags[j].Name
	})
	if !sorted {
		t.Error("the registry is not sorted by owner and name")
	}
	for _, f := range Flags {
		if !flagName.MatchString(f.Name) {
			t.Errorf("%q is not a variable name", f.Name)
		}
		if !regexp.MustCompile(`^[a-z]+$`).MatchString(f.Owner) {
			t.Errorf("%s has no owner", f.Name)
		}
		_, err := time.Parse(time.DateOnly, f.RemoveAfter)
		switch {
		case f.Temporary && f.RemoveAfter == "":
			t.Errorf("%s is a temporary switch without a removal date", f.Name)
		case f.Temporary && err != nil:
			t.Errorf("%s: the removal date %q is not YYYY-MM-DD", f.Name, f.RemoveAfter)
		case !f.Temporary && f.RemoveAfter != "":
			t.Errorf("%s has a removal date and is not marked temporary", f.Name)
		}
	}
}

// The switches that keep one organization from another were rolled out by flag: they stay marked temporary.
func TestRolloutSwitchesCarryADate(t *testing.T) {
	byName := map[string]Flag{}
	for _, f := range Flags {
		byName[f.Name] = f
	}
	for _, name := range []string{"HUB_ORGS", "HUB_ACCESS_ENFORCE", "HUB_RESOURCE_ACCESS"} {
		if f, ok := byName[name]; !ok || !f.Temporary {
			t.Errorf("%s is not marked temporary", name)
		}
	}
}

// What Load asks its environment for is all in the registry, the names it builds (a provider's, the safe's) included.
func TestLoadAsksOnlyForRegisteredVariables(t *testing.T) {
	envs := []map[string]string{
		{"HUB_PUBLIC_URL": "http://localhost:8040"},
		{
			"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_OIDC_PROVIDERS": "acme", "HUB_SECRETS_VAULT_ADDR": "https://vault.example.com",
			"HUB_STORAGE": "s3", "HARNESSLY_SITES": "s3", "HUB_STORE_S3_BUCKET": "b", "HUB_REPLICAS": "on", "HUB_DESIGN_IMPORT_GATEWAY": "dry-run",
			"HUB_MR_SOURCE_GITLAB_URL": "https://git.example.com", "HUB_PROVIDER_URL": "https://provider.example.com", "HUB_APP_TOKEN": "on",
			"HUB_APP_TOKEN_EXCHANGE_URL": "https://sts.example.com", "HUB_AUTOMATIONS": "on", "HUB_EXTENSIONS": "x",
		},
	}
	asked := map[string]bool{}
	for _, m := range envs {
		_, _ = Load(func(name string) (string, bool) {
			asked[name] = true
			v, ok := m[name]
			return v, ok
		})
	}
	if len(asked) < 100 {
		t.Fatalf("Load asked for %d variables only: the recording is broken", len(asked))
	}
	for name := range asked {
		if !KnownFlag(name) {
			t.Errorf("Load asks for %s and the registry does not name it", name)
		}
	}
}

func TestKnownFlag(t *testing.T) {
	for name, want := range map[string]bool{
		"HUB_ORGS": true, "HUB_SECRET_KEY_FILE": true, "HUB_OIDC_ACME_ISSUER": true, "HUB_OIDC_": false,
		"HUB_NO_SUCH": false, "PATH": false,
	} {
		if KnownFlag(name) != want {
			t.Errorf("KnownFlag(%s) = %v", name, !want)
		}
	}
}

func TestOverdueFlags(t *testing.T) {
	if got := OverdueFlags(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); len(got) != 0 {
		t.Errorf("overdue before any date: %v", got)
	}
	got := OverdueFlags(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	temporary := 0
	for _, f := range Flags {
		if f.Temporary {
			temporary++
		}
	}
	if temporary == 0 || len(got) != temporary {
		t.Errorf("overdue in 2100: %d of %d", len(got), temporary)
	}
}

func TestOrgsShadowIsRefusedByName(t *testing.T) {
	_, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040", "HUB_ORGS": "shadow"}))
	if err == nil || !strings.Contains(err.Error(), "HUB_ORGS=shadow is no longer a value") {
		t.Fatalf("HUB_ORGS=shadow: %v", err)
	}
	for _, v := range []string{"on", "off", "ON"} {
		if _, err := Load(env(map[string]string{"HUB_PUBLIC_URL": "http://localhost:8040", "HUB_ORGS": v})); err != nil {
			t.Errorf("HUB_ORGS=%s: %v", v, err)
		}
	}
}
