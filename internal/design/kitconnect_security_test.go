package design

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The button «Подключить UI-кит» runs npm on the machine for what a request names: who may ask, which
// registry, which package.

func TestConnectNeedsTheOwner(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", otherV2)
	l := accessLibrary(t, root)
	a := pagesAPI(l)
	for _, role := range []string{"view", "edit"} {
		s := snapshot(map[string]Grant{"p1": {Role: role, Owner: "k@x", OwnerName: "Олег"}}, map[string]string{"p1": "none"})
		s.At = s.At.Add(time.Duration(len(role)) * time.Second)
		if err := l.SetAccess(s); err != nil {
			t.Fatal(err)
		}
		ans := pagePost(a, `{"action":"connect","project":"p1","source":"npm:antd"}`)
		if ans.Status != 403 {
			t.Errorf("%s may connect a package: %d", role, ans.Status)
		}
	}
}

func TestConnectRefusesRegistryOutsideTheList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HARNESSLY_KIT_REGISTRIES", "npm.corp.example.com, localhost")
	for _, bad := range []string{"https://evil.example.org/", "http://registry.npmjs.org/", "https://user:pw@registry.npmjs.org/", "file:///etc", "evil", "https://registry.npmjs.org.evil.example.org/"} {
		if err := checkKitRegistry(bad); err == nil {
			t.Errorf("registry %q was accepted", bad)
		}
	}
	for _, ok := range []string{"", "https://registry.npmjs.org/", "https://npm.corp.example.com/repo/", "http://localhost:4873/"} {
		if err := checkKitRegistry(ok); err != nil {
			t.Errorf("registry %q refused: %v", ok, err)
		}
	}
}

func TestRegistriesOfTheMachineNpmrcAreAllowed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HARNESSLY_KIT_REGISTRIES", "")
	if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte("registry=https://mirror.example.com/npm/\n@acme:registry=https://acme.example.net/\n//acme.example.net/:_authToken=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{"https://mirror.example.com/npm/", "https://acme.example.net/"} {
		if err := checkKitRegistry(ok); err != nil {
			t.Errorf("%q from .npmrc refused: %v", ok, err)
		}
	}
	if err := checkKitRegistry("https://other.example.com/"); err == nil {
		t.Error("a registry that is not in .npmrc was accepted")
	}
}

func TestConnectNeedsARealProject(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", mineV2)
	l := accessLibrary(t, root)
	writeFile(t, filepath.Join(l.MetaDir("p1"), MetaProjectFile), `{"title":"Plan"}`)
	for _, id := range []string{"", "../p1", "nope", "p1/../p1"} {
		if _, err := l.ConnectKitPackage(id, "npm:antd", ""); err == nil || !strings.Contains(err.Error(), "проекта нет") && !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("project %q: %v", id, err)
		}
	}
	a := pagesAPI(l)
	if ans := pagePost(a, `{"action":"connect","project":"../x","source":"npm:antd"}`); ans.Status == 200 {
		t.Errorf("a made-up project connected: %d", ans.Status)
	}
}

func TestConnectRefusesWhatIsNotAPlainPackage(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", mineV2)
	l := accessLibrary(t, root)
	writeFile(t, filepath.Join(l.MetaDir("p1"), MetaProjectFile), `{"title":"Plan"}`)
	for _, src := range []string{"npm:git+https://example.com/x.git", "npm:file:../x", "npm:../x", "npm:https://example.com/x.tgz", "npm:@scope/../x", "npm:antd@git+https://example.com/x", "npm:antd@file:../x", "npm:-g", "git+https://example.com/x.git", "npm:A B"} {
		if _, err := l.ConnectKitPackage("p1", src, ""); err == nil {
			t.Errorf("source %q was taken", src)
		}
	}
	if _, err := l.ConnectKitPackage("p1", "npm:antd", "https://evil.example.org/"); err == nil || !strings.Contains(err.Error(), "evil.example.org") {
		t.Errorf("a registry outside the list: %v", err)
	}
	if _, err := l.ConnectKitPackage("p1", "shadcn:/etc", ""); err == nil {
		t.Error("a shadcn registry that is a folder was taken")
	}
}
