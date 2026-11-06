package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestSafeBackendIsTheFileByDefault(t *testing.T) {
	dir := t.TempDir()
	local := secrets.Open(filepath.Join(dir, "secrets"), filepath.Join(dir, "key"))
	for _, mode := range []string{"", "local", " LOCAL "} {
		s, info := newSafeBackend(envOf(map[string]string{"RUNTIME_SECRETS_MODE": mode}), local, dir, dir)
		if s.Primary != nil || info.Mode != safeModeLocal || info.Error != "" {
			t.Fatalf("mode %q: %+v %+v", mode, s, info)
		}
	}
}

func TestSafeBackendWithBadSettingsRefusesAndKeepsNothing(t *testing.T) {
	dir := t.TempDir()
	local := secrets.Open(filepath.Join(dir, "secrets"), filepath.Join(dir, "key"))
	for name, env := range map[string]map[string]string{
		"unknown mode": {"RUNTIME_SECRETS_MODE": "s3"},
		"no command":   {"RUNTIME_SECRETS_MODE": "exec"},
		"no such cmd":  {"RUNTIME_SECRETS_MODE": "exec", "RUNTIME_SECRETS_EXEC": "no-such-provider-xyz --store"},
		"exec prefix":  {"RUNTIME_SECRETS_MODE": "exec", "RUNTIME_SECRETS_EXEC": "/bin/true", "RUNTIME_SECRETS_EXEC_PREFIX": "a/../b"},
		"no address":   {"RUNTIME_SECRETS_MODE": "vault", "RUNTIME_SECRETS_VAULT_TOKEN_FILE": "/x"},
		"no token":     {"RUNTIME_SECRETS_MODE": "vault", "RUNTIME_SECRETS_VAULT_ADDR": "https://openbao.example.com"},
		"bad auth":     {"RUNTIME_SECRETS_MODE": "vault", "RUNTIME_SECRETS_VAULT_ADDR": "https://openbao.example.com", "RUNTIME_SECRETS_VAULT_AUTH": "ldap"},
		"approle half": {"RUNTIME_SECRETS_MODE": "vault", "RUNTIME_SECRETS_VAULT_ADDR": "https://openbao.example.com", "RUNTIME_SECRETS_VAULT_AUTH": "approle", "RUNTIME_SECRETS_VAULT_ROLE_ID": "r"},
		"bad version":  {"RUNTIME_SECRETS_MODE": "vault", "RUNTIME_SECRETS_VAULT_ADDR": "https://openbao.example.com", "RUNTIME_SECRETS_VAULT_TOKEN_FILE": "/x", "RUNTIME_SECRETS_VAULT_KV_VERSION": "three"},
		"bad prefix":   {"RUNTIME_SECRETS_MODE": "vault", "RUNTIME_SECRETS_VAULT_ADDR": "https://openbao.example.com", "RUNTIME_SECRETS_VAULT_TOKEN_FILE": "/x", "RUNTIME_SECRETS_VAULT_PREFIX": "a/../b"},
	} {
		s, info := newSafeBackend(envOf(env), local, dir, dir)
		if info.Error == "" || s.Primary == nil {
			t.Errorf("%s: no error: %+v", name, info)
			continue
		}
		if _, _, err := s.Put("gh-token", "value-1"); secrets.ReasonOf(err) != secrets.ReasonUnavailable {
			t.Errorf("%s: put: %v", name, err)
		}
		if local.Has("gh-token") {
			t.Fatalf("%s: the value landed in the file", name)
		}
	}
}

func TestSafeBackendReadsTheServerSettings(t *testing.T) {
	dir := t.TempDir()
	tok := filepath.Join(dir, "token")
	if err := os.WriteFile(tok, []byte("t"), 0o600); err != nil {
		t.Fatal(err)
	}
	local := secrets.Open(filepath.Join(dir, "secrets"), filepath.Join(dir, "key"))
	s, info := newSafeBackend(envOf(map[string]string{
		"RUNTIME_SECRETS_MODE":             "vault",
		"RUNTIME_SECRETS_VAULT_ADDR":       "https://openbao.example.com:8200/",
		"RUNTIME_SECRETS_VAULT_TOKEN_FILE": tok,
		"RUNTIME_SECRETS_VAULT_PREFIX":     "/harnessly/users/alice/",
		"RUNTIME_SECRETS_REF_PREFIXES":     " shared/ci/ , ,team/",
	}), local, dir, dir)
	if info.Error != "" || s.Primary == nil || s.Refs == nil || s.Where != secrets.WhereVault {
		t.Fatalf("%+v %+v", s, info)
	}
	if info.Auth != "token-file" || !info.Refs || info.Mount != "secret" || strings.HasSuffix(info.Addr, "/") {
		t.Fatalf("info: %+v", info)
	}
	remote := s.Primary.(*secrets.Remote)
	if strings.Join(remote.RefPrefixes, ",") != "shared/ci,team" {
		t.Fatalf("prefixes: %v", remote.RefPrefixes)
	}
}

func TestSafeBackendHubModeGoesThroughTheAgent(t *testing.T) {
	dir := t.TempDir()
	local := secrets.Open(filepath.Join(dir, "secrets"), filepath.Join(dir, "key"))
	s, info := newSafeBackend(envOf(map[string]string{"RUNTIME_SECRETS_MODE": "hub"}), local, dir, t.TempDir())
	if info.Error != "" || info.Mode != safeModeHub || !info.Refs || s.Primary == nil || s.Where != secrets.WhereVault {
		t.Fatalf("%+v %+v", s, info)
	}
	// No machine agent here: the safe refuses as unavailable and keeps nothing on the machine.
	if _, _, err := s.Put("gh-token", "value-1"); secrets.ReasonOf(err) != secrets.ReasonUnavailable {
		t.Fatalf("put: %v", err)
	}
	if local.Has("gh-token") {
		t.Fatal("the value landed in the file")
	}
}

func TestSafeBackendExecModeNamesOnlyTheCommand(t *testing.T) {
	dir := t.TempDir()
	local := secrets.Open(filepath.Join(dir, "secrets"), filepath.Join(dir, "key"))
	s, info := newSafeBackend(envOf(map[string]string{
		"RUNTIME_SECRETS_MODE":         "exec",
		"RUNTIME_SECRETS_EXEC":         "/bin/false --token-file /etc/provider/token",
		"RUNTIME_SECRETS_REF_PREFIXES": "shared",
	}), local, dir, dir)
	if info.Error != "" || s.Primary == nil || info.Addr != "false" || info.Prefix != "harnessly" || !info.Refs {
		t.Fatalf("%+v %+v", s, info)
	}
	// A command that fails is a refusal, and the value is not kept on the machine instead.
	if _, _, err := s.Put("gh-token", "value-1"); err == nil || local.Has("gh-token") {
		t.Fatalf("put: %v", err)
	}
}
