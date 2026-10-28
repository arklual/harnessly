package agentnet

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvSetGetUnset(t *testing.T) {
	e := NewEnvDir(filepath.Join(t.TempDir(), "agent-env"))
	if err := e.Set(ScopeAll, map[string]string{"GH_TOKEN": "ghp_1", "GITLAB_HOST": "git.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Set(ScopeAll, map[string]string{"GH_TOKEN": "ghp_2"}); err != nil {
		t.Fatal(err)
	}
	if v, ok := e.Get(ScopeAll, "GH_TOKEN"); !ok || v != "ghp_2" {
		t.Fatalf("replaced: %q %v", v, ok)
	}
	if got := e.Names(ScopeAll, nil); strings.Join(got, ",") != "GH_TOKEN,GITLAB_HOST" {
		t.Fatalf("names %v", got)
	}
	st, err := os.Stat(e.File(ScopeAll))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode: %v %v", st, err)
	}
	if st, _ := os.Stat(e.Dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v", st.Mode())
	}
	if err := e.Unset(ScopeAll, "GH_TOKEN", "NOT_THERE"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Get(ScopeAll, "GH_TOKEN"); ok {
		t.Fatal("not removed")
	}
	if err := e.Unset(ScopeAll, "GITLAB_HOST"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.File(ScopeAll)); !os.IsNotExist(err) {
		t.Fatalf("an empty file is left: %v", err)
	}
	if err := e.Unset(ScopeAll, "GITLAB_HOST"); err != nil {
		t.Fatalf("unsetting from nothing: %v", err)
	}
}

func TestEnvRefusals(t *testing.T) {
	e := NewEnvDir(t.TempDir())
	if err := e.Set("everyone", map[string]string{"A": "1"}); err == nil {
		t.Error("an unknown scope")
	}
	if err := e.Set(ScopeAll, map[string]string{"lower": "1"}); err == nil {
		t.Error("a lower-case name")
	}
	if err := e.Set(ScopeAll, map[string]string{"A B": "1"}); err == nil {
		t.Error("a name with a space")
	}
	if err := e.Set(ScopeAll, map[string]string{"A": "a\x00b"}); err == nil {
		t.Error("a NUL in a value")
	}
	if err := e.Unset("everyone", "A"); err == nil {
		t.Error("unset in an unknown scope")
	}
}

// The files are read back by a shell: whatever the value holds, sourcing gives it back exactly.
func TestEnvFileSourcedByShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	values := []string{
		"plain",
		"it's got 'quotes'",
		"''",
		`back\slash and "double" and $DOLLAR and ` + "`tick`",
		"line one\nline two\n",
		"ключ-тест",
		"  spaced  ",
		"a;rm -rf /;b",
		"",
	}
	e := NewEnvDir(t.TempDir())
	for i, want := range values {
		name := "V" + string(rune('A'+i))
		if err := e.Set(ScopeAll, map[string]string{name: want}); err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range values {
		name := "V" + string(rune('A'+i))
		out, err := exec.Command(sh, "-c", "set -a; . "+Quote(e.File(ScopeAll))+"; set +a; printf %s \"$"+name+"\"").Output()
		if err != nil || string(out) != want {
			t.Errorf("%s: shell got %q (%v), want %q", name, out, err, want)
		}
		if got, ok := e.Get(ScopeAll, name); !ok || got != want {
			t.Errorf("%s: Get gave %q %v, want %q", name, got, ok, want)
		}
	}
}

func TestEnvMergedAndNames(t *testing.T) {
	e := NewEnvDir(t.TempDir())
	_ = e.Set(ScopeAll, map[string]string{"GH_TOKEN": "g", "HTTPS_PROXY": "http://a", "NO_PROXY": "localhost"})
	_ = e.Set(ScopeClaude, map[string]string{"ANTHROPIC_API_KEY": "k", "HTTPS_PROXY": "http://b"})
	m := e.Merged(ScopeClaude)
	if m["HTTPS_PROXY"] != "http://b" || m["GH_TOKEN"] != "g" || m["ANTHROPIC_API_KEY"] != "k" {
		t.Fatalf("%v", m)
	}
	if _, ok := e.Merged(ScopeCodex)["ANTHROPIC_API_KEY"]; ok {
		t.Fatal("Claude's key is in Codex's launch")
	}
	secrets := e.Names(ScopeAll, IsProxyName)
	if len(secrets) != 1 || secrets[0] != "GH_TOKEN" {
		t.Fatalf("secrets %v", secrets)
	}
}

func TestParseEnvSkipsJunk(t *testing.T) {
	raw := "# header\nGOOD='1'\nbad line\nlower='2'\nHALF='unclosed\nTAIL=\\'x\\'\nLAST='z'\n"
	got := parseEnv(raw)
	var names []string
	for _, en := range got {
		names = append(names, en.name+"="+en.value)
	}
	// HALF opens a quote that never closes; the lines after it are swallowed until a quote closes, so only what is
	// before it and what the writer would produce survive. The point: no panic, nothing invented.
	if len(names) == 0 || names[0] != "GOOD=1" {
		t.Fatalf("%v", names)
	}
}

func TestCheckName(t *testing.T) {
	good := []string{"GH_TOKEN", "GITLAB_TOKEN", "GITHUB_TOKEN", "MY_SECRET_1_KEY", "DB_PASSWORD", "GITHUB_PAT",
		"SENTRY_DSN", "DB_USER", "NPM_TOKEN", "HF_TOKEN", "PYPI_TOKEN", "_X_TOKEN", "A_KEY"}
	bad := []string{
		// Not a name at all.
		"", "gh_token", "1ABC", "A-B", strings.Repeat("A", 65) + "_KEY",
		// Does not look like a secret or an address.
		"A", "_X", "TOKEN", "DEBUG", "MY_FLAG", "NODE_ENV", "KEYS", "TOKEN_FILE",
		// The old list and prefixes.
		"PATH", "HOME", "SHELL", "USER", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "OPENAI_API_KEY", "HARNESSLY_DATA_DIR", "RUNTIME_PORT", "AGENT_PORT",
		"BASH_ENV", "NODE_OPTIONS", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "CODEX_API_KEY", "BASH_FUNC_x%%",
		// Code in git, a pager, interpreters, TLS stacks and the JVM, even when they end like a secret.
		"GIT_SSH_COMMAND", "GIT_ASKPASS", "GIT_CONFIG_COUNT", "GIT_TOKEN", "GIT_SSH_KEY", "PAGER", "LESS", "LESSOPEN", "LESS_KEY",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "SSL_CERT_KEY", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "AWS_CA_KEY",
		"PYTHONPATH", "PYTHONSTARTUP", "PYTHON_TOKEN", "PERL5OPT", "PERL_URL", "RUBYOPT", "RUBY_HOST", "JAVA_TOOL_OPTIONS",
		"_JAVA_OPTIONS", "GCONV_PATH", "PS4", "BROWSER", "EDITOR", "VISUAL",
		// Proxies by another name.
		"FTP_PROXY", "MY_PROXY", "SOCKS_PROXY", "GITLAB_PROXY",
		// An address is not a secret: what points a tool at another server makes the machine run what that server serves.
		"REGISTRY_URL", "DB_HOST", "GITLAB_HOST", "GH_HOST", "PIP_INDEX_URL", "PIP_EXTRA_INDEX_URL", "NPM_CONFIG_REGISTRY", "UV_INDEX_URL",
		"DOCKER_HOST", "CARGO_REGISTRY_URL", "CONDA_CHANNEL_ALIAS", "GOPROXY", "GOFLAGS", "HF_ENDPOINT", "HF_ENDPOINT_URL",
		// Package managers' own settings, even when they end like a secret.
		"PIP_TOKEN", "PIP_KEY", "UV_PUBLISH_TOKEN", "DOCKER_AUTH_KEY", "NPM_CONFIG__AUTH_TOKEN", "CARGO_REGISTRY_TOKEN", "CARGO_HTTP_USER",
		"CONDA_TOKEN",
	}
	for _, n := range good {
		if err := CheckName(n); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	for _, n := range bad {
		if err := CheckName(n); err == nil {
			t.Errorf("%q accepted", n)
		}
	}
}

func TestCheckRemove(t *testing.T) {
	for _, n := range []string{"GH_TOKEN", "NOT_THERE", "OLD_NAME", "A"} {
		if err := CheckRemove(n); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	for _, n := range []string{"", "lower", "1A", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", strings.Repeat("A", 65)} {
		if err := CheckRemove(n); err == nil {
			t.Errorf("%q accepted", n)
		}
	}
}
