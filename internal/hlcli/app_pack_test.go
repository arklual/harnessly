package hlcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

func runApp(t *testing.T, env map[string]string, cwd string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(append([]string{"app"}, args...), Env{Getenv: func(k string) string { return env[k] }, Home: t.TempDir(), Cwd: cwd,
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
	return code, out.String(), errOut.String()
}

func TestAppKeyPackAndSign(t *testing.T) {
	work := t.TempDir()
	if code, _, e := runApp(t, nil, work, "new", "acme.demo", "--template", "pane-chat", "--dir", filepath.Join(work, "demo")); code != 0 {
		t.Fatalf("new: %d %s", code, e)
	}
	// A key: the public one is printed, the private one only goes into the file (0600).
	keyFile := filepath.Join(work, "k")
	code, out, e := runApp(t, nil, work, "key", "--out", keyFile)
	if code != 0 || !strings.Contains(out, "public key") {
		t.Fatalf("key: %d %s %s", code, out, e)
	}
	priv, _ := os.ReadFile(keyFile)
	if info, _ := os.Stat(keyFile); info.Mode().Perm() != 0o600 || strings.Contains(out, strings.TrimSpace(string(priv))) {
		t.Fatal("the private key is in the file only, 0600")
	}
	// Without a key in the environment --sign says where it lives.
	if code, _, e := runApp(t, nil, work, "pack", filepath.Join(work, "demo"), "--sign"); code == 0 || !strings.Contains(e, "secret run --env HLAPP_SIGNING_KEY=") {
		t.Fatalf("no key: %d %s", code, e)
	}
	// Unsigned pack: a file and no signature.
	plain := filepath.Join(work, "plain.hlapp")
	if code, _, e := runApp(t, nil, work, "pack", filepath.Join(work, "demo"), "-o", plain); code != 0 {
		t.Fatalf("pack: %d %s", code, e)
	}
	if _, err := os.Stat(plain + apps.SigSuffix); err == nil {
		t.Fatal("no signature without --sign")
	}
	// Signed: the signature verifies against the archive and names the publisher.
	signed := filepath.Join(work, "signed.hlapp")
	env := map[string]string{signingKeyEnv: strings.TrimSpace(string(priv))}
	if code, out, e := runApp(t, env, work, "pack", filepath.Join(work, "demo"), "-o", signed, "--sign", "--publisher", "Банк"); code != 0 || !strings.Contains(out, "signed by Банк") {
		t.Fatalf("sign: %d %s %s", code, out, e)
	}
	raw, _ := os.ReadFile(signed)
	sraw, _ := os.ReadFile(signed + apps.SigSuffix)
	sig, err := apps.ParseSignature(sraw)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := apps.PublicOf(strings.TrimSpace(string(priv)))
	if v, err := apps.VerifyArchive(raw, sig, nil); err != nil || v.Publisher != "Банк" || sig.Key != pub {
		t.Fatalf("verify: %+v %v", v, err)
	}
}
