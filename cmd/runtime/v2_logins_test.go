package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A CLI that signs in is found on the person's PATH and starts with a short environment: none of the runtime's tokens.
func TestLoginEnvironment(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nenv > " + filepath.Join(dir, "env") + "\nprintf 'https://claude.com/cai/oauth/authorize?x=1\\n'\nsleep 5\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RT_CLI_TOKEN", "runtime-secret-0000")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-0000")
	t.Setenv("HTTPS_PROXY", "http://proxy.example:3128")
	svc := v2Logins(dir, func() string { return dir + ":/usr/bin:/bin" }, func(wire.Provider) {})
	st, err := svc.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginSubscription)
	if err != nil || st.Flow == nil || st.Flow.State != wire.LoginAwaiting {
		t.Fatalf("%v %+v", err, st.Flow)
	}
	defer func() { _, _ = svc.CancelLogin(context.Background(), wire.ProviderClaude) }()
	var env string
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end) && env == ""; time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(filepath.Join(dir, "env"))
		env = string(b)
	}
	for _, want := range []string{"HOME=" + dir, "HTTPS_PROXY=http://proxy.example:3128", "BROWSER=/bin/false"} {
		if !strings.Contains(env, want) {
			t.Errorf("no %s", want)
		}
	}
	for _, secret := range []string{"RT_CLI_TOKEN", "runtime-secret", "ANTHROPIC_API_KEY", "sk-test-0000"} {
		if strings.Contains(env, secret) {
			t.Errorf("the CLI got %s", secret)
		}
	}
}
