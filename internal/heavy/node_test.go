package heavy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nodeTree is a git tree with a web/ package: its scripts and a pnpm lockfile.
func nodeTree(t *testing.T) (top, web string) {
	t.Helper()
	top = t.TempDir()
	web = filepath.Join(top, "web")
	for _, d := range []string{filepath.Join(top, ".git"), filepath.Join(web, "src")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pkg := `{"scripts": {
		"test": "vitest run",
		"typecheck": "tsc -p tsconfig.app.json --noEmit",
		"lint": "eslint . && prettier --check src",
		"lint:fix": "eslint --fix .",
		"check": "pnpm typecheck && pnpm run lint",
		"build": "tsc -b && vite build",
		"e2e": "playwright test",
		"piped": "vitest run | tee out.txt",
		"env": "NODE_ENV=test cross-env TZ=UTC vitest run"
	}}`
	for p, s := range map[string]string{filepath.Join(web, "package.json"): pkg, filepath.Join(web, "pnpm-lock.yaml"): "lockfileVersion: '9.0'\n"} {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return top, web
}

func TestNodeChecksGoRemoteAndCallsThatWriteStay(t *testing.T) {
	top, web := nodeTree(t)
	for _, c := range []struct {
		call       string
		want, back bool
	}{
		{"npx tsc --noEmit -p .", true, false},
		{"npx tsc -p tsconfig.app.json --noEmit", true, false},
		{"npx vitest run app/hub", true, false},
		{"npx vitest run src/x.test.ts -t name", true, false},
		{"npx eslint src/a.ts", true, false},
		{"npm exec vitest run app/hub-api", true, false},
		{"pnpm exec tsc --noEmit", true, false},
		{"pnpm vitest run", true, false},
		{"pnpm test", true, false},
		{"npm test -- src/x.test.ts", true, false},
		{"pnpm typecheck", true, false},
		{"pnpm run lint", true, false},
		{"pnpm check", true, false},
		{"npm run env", true, false},
		{"npx tsc", true, true},
		{"npx tsc -b", true, true},
		{"npx tsc --noEmit=false", true, true},
		{"npx vitest run -u", false, false},
		{"npx vitest run --coverage", false, false},
		{"npx vitest run --browser.enabled", false, false},
		{"npx eslint --fix src", false, false},
		{"npx -p typescript tsc --noEmit", false, false},
		{"npx playwright test", false, false},
		{"npm exec playwright test -c playwright.hub.config.ts", false, false},
		{"pnpm run lint:fix", false, false},
		{"pnpm build", true, true},
		{"pnpm e2e", false, false},
		{"pnpm piped", false, false},
		{"pnpm -r test", false, false},
		{"pnpm --filter web test", false, false},
		{"npm --prefix web test", false, false},
		{"pnpm dlx vitest run", false, false},
		{"pnpm missing", false, false},
		{"npx vite build", true, true},
		{"pnpm exec next build", true, true},
		{"npx vite", false, false},
		{"npx vite build --watch", false, false},
		{"npx tsc -b --watch", false, false},
		{"npm run build", true, true},
	} {
		w := strings.Fields(c.call)
		deps, back, got := nodeFits(w[0], w[1:], filepath.Join(web, "src"), top)
		if got != c.want || back != c.back {
			t.Errorf("%s: remote %v back %v, want %v %v", c.call, got, back, c.want, c.back)
		}
		if got && deps != web {
			t.Errorf("%s: deps from %q, want %q", c.call, deps, web)
		}
	}
	if _, _, ok := nodeFits("npx", []string{"vitest", "run"}, top, top); ok {
		t.Error("a call above the lockfile went remote")
	}
}

func TestANodeCheckInstallsOnceThenRunsOnTheRemoteMachine(t *testing.T) {
	f, tree, _ := remoteFixture(t)
	web := filepath.Join(tree, "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	for p, s := range map[string]string{
		filepath.Join(web, "package.json"):   `{"scripts": {"test": "vitest run"}}`,
		filepath.Join(web, "pnpm-lock.yaml"): "lockfileVersion: '9.0'\n",
		// pnpm install puts a vitest into node_modules/.bin; npx is not used, the .bin comes first on PATH.
		filepath.Join(f.real, "pnpm"): "#!/bin/sh\necho \"pnpm $*\" >>\"$HOME/installs\"\nmkdir -p node_modules/.bin\n" +
			"printf '#!/bin/sh\\necho \"vitest dir=$PWD args=$*\"\\n' >node_modules/.bin/vitest\nchmod +x node_modules/.bin/vitest\n",
		filepath.Join(f.real, "npx"): "#!/bin/sh\nexec \"$@\"\n",
	} {
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The remote root is under the remote home, as by default: the wrapper starts there.
	home := t.TempDir()
	f.env["HOME"] = home
	f.env[envRemoteRoot] = "cache"
	remote := filepath.Join(home, "cache")
	f.wd = web
	for i := 0; i < 2; i++ {
		if code := f.run("npx", "vitest", "run", "src"); code != 0 {
			t.Fatalf("run %d: exit %d: %s", i, code, f.stderr.String())
		}
		if want := "vitest dir=" + filepath.Join(remote, web) + " args=run src"; !strings.Contains(f.stdout.String(), want) {
			t.Fatalf("run %d: stdout %q, want %q", i, f.stdout.String(), want)
		}
	}
	b, _ := os.ReadFile(filepath.Join(home, "installs"))
	if n := strings.Count(string(b), "pnpm install --frozen-lockfile"); n != 1 {
		t.Errorf("installs %q, want one", b)
	}

	// A lockfile that does not install runs the call here, and is not tried again until it changes.
	if err := os.WriteFile(filepath.Join(web, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.1'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.real, "pnpm"), []byte("#!/bin/sh\necho \"pnpm $*\" >>\"$HOME/installs\"\necho ERR_PNPM_FETCH_401 >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		f.run("npx", "vitest", "run")
		if !strings.Contains(f.stderr.String(), "node_modules did not install there") {
			t.Fatalf("failed install %d: stderr %q", i, f.stderr.String())
		}
	}
	b, _ = os.ReadFile(filepath.Join(home, "installs"))
	if n := strings.Count(string(b), "pnpm install"); n != 2 {
		t.Errorf("installs %q, want the failed lockfile tried once", b)
	}
}

func TestANodeBuildRunsThereAndItsOutputComesBack(t *testing.T) {
	f, tree, remote := remoteFixture(t)
	web := filepath.Join(tree, "web")
	if err := os.MkdirAll(filepath.Join(web, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, s := range map[string]string{
		filepath.Join(web, "package.json"):   `{"scripts": {"build": "tsc -b"}}`,
		filepath.Join(web, "pnpm-lock.yaml"): "lockfileVersion: '9.0'\n",
		filepath.Join(web, "src", "a.ts"):    "export const a = 1\n",
		// The installed tsc builds src into dist.
		filepath.Join(f.real, "pnpm"): "#!/bin/sh\nmkdir -p node_modules/.bin\n" +
			"printf '#!/bin/sh\\nmkdir -p dist && echo built-there >dist/a.js\\n' >node_modules/.bin/tsc\nchmod +x node_modules/.bin/tsc\n",
		filepath.Join(f.real, "npx"): "#!/bin/sh\nexec \"$@\"\n",
	} {
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.env["HOME"] = t.TempDir()
	f.wd = web
	if code := f.run("npx", "tsc", "-b"); code != 0 {
		t.Fatalf("exit %d: %s", code, f.stderr.String())
	}
	if _, err := os.Stat(filepath.Join(remote, web, "dist", "a.js")); err != nil {
		t.Fatalf("the build did not run there: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(web, "dist", "a.js")); err != nil || string(b) != "built-there\n" {
		t.Errorf("dist/a.js here %q %v, want the remote build's output", b, err)
	}
	if _, err := os.Stat(filepath.Join(web, "node_modules")); !os.IsNotExist(err) {
		t.Errorf("the remote node_modules came back: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree, ".git")); err != nil {
		t.Errorf(".git here is gone: %v", err)
	}

	// Output that does not come back fails the call, though the build passed there.
	f.env["FAKE_RSYNC_FAIL_BACK"] = "1"
	if code := f.run("npx", "tsc", "-b"); code != 1 || !strings.Contains(f.stderr.String(), "its output did not come back") {
		t.Errorf("exit %d, stderr %q: want 1 and why", code, f.stderr.String())
	}
}
