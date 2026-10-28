package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitKey(t *testing.T) {
	home := t.TempDir()
	first, err := EnsureGitKey(home, "box.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "ssh-ed25519 ") || !strings.HasSuffix(first, " harnessly@box.example.com") {
		t.Fatalf("%q", first)
	}
	priv := filepath.Join(home, ".ssh", "harnessly_ed25519")
	for path, want := range map[string]os.FileMode{filepath.Join(home, ".ssh"): 0o700, priv: 0o600, priv + ".pub": 0o644, filepath.Join(home, ".ssh", "config"): 0o600} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want {
			t.Errorf("%s is %v, want %v", path, st.Mode().Perm(), want)
		}
	}
	// Once made, it stays: the same answer, the same files, one block in the config.
	rawBefore, _ := os.ReadFile(priv)
	second, err := EnsureGitKey(home, "other-name")
	if err != nil {
		t.Fatal(err)
	}
	rawAfter, _ := os.ReadFile(priv)
	if second != first || string(rawBefore) != string(rawAfter) {
		t.Errorf("the key changed: %q -> %q", first, second)
	}
	cfg, _ := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if strings.Count(string(cfg), "IdentityFile") != 1 || !strings.Contains(string(cfg), "Host github.com gitlab.com bitbucket.org codeberg.org\n  IdentityFile ~/.ssh/harnessly_ed25519\n") || strings.Contains(string(cfg), "Host *") {
		t.Errorf("config %q", cfg)
	}
	// The public half read back out of the private file is the one told.
	pub, err := publicOfOpenSSH(rawBefore)
	if err != nil || authorizedLine(pub, "harnessly@box.example.com") != first {
		t.Errorf("%v %q", err, authorizedLine(pub, "x"))
	}
	// A lost .pub is made again from the private key.
	if err := os.Remove(priv + ".pub"); err != nil {
		t.Fatal(err)
	}
	third, err := EnsureGitKey(home, "renamed")
	if err != nil || !strings.HasPrefix(third, "ssh-ed25519 ") || strings.Fields(third)[1] != strings.Fields(first)[1] {
		t.Errorf("%v %q", err, third)
	}
	if got, _ := os.ReadFile(priv + ".pub"); !strings.HasPrefix(string(got), "ssh-ed25519 ") {
		t.Errorf("pub %q", got)
	}
}

// ssh-keygen, where there is one, reads the key as its own and agrees on the public half.
func TestGitKeyIsReadBySSHKeygen(t *testing.T) {
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("no ssh-keygen")
	}
	home := t.TempDir()
	line, err := EnsureGitKey(home, "h")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(keygen, "-y", "-f", filepath.Join(home, ".ssh", "harnessly_ed25519")).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	if got := strings.Fields(string(out)); len(got) < 2 || got[0] != "ssh-ed25519" || got[1] != strings.Fields(line)[1] {
		t.Errorf("ssh-keygen says %q, the agent %q", out, line)
	}
}

func TestSSHConfigKeepsTheOwnersFile(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(home, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	own := "Host github.com\n  User git\n  IdentityFile ~/.ssh/work"
	if err := os.WriteFile(cfgPath, []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureGitKey(home, "h"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(cfgPath)
	if !strings.HasPrefix(string(got), own+"\n\n# >>> harnessly >>>\nHost github.com ") || !strings.HasSuffix(string(got), "# <<< harnessly <<<\n") {
		t.Errorf("%q", got)
	}
}

func TestGitKeyRefusesWhatItCannotRead(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	priv := filepath.Join(home, ".ssh", "harnessly_ed25519")
	if err := os.WriteFile(priv, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureGitKey(home, "h"); err == nil {
		t.Fatal("a key file that is not one was taken")
	}
	if got, _ := os.ReadFile(priv); string(got) != "not a key" {
		t.Error("the file was overwritten")
	}
}

func TestOpenSSHRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	got, err := publicOfOpenSSH(marshalOpenSSH(priv, "c"))
	if err != nil || !pub.Equal(got) {
		t.Fatalf("%v", err)
	}
	if _, err := publicOfOpenSSH([]byte("garbage")); err == nil {
		t.Error("garbage read as a key")
	}
}

func TestSSHConfigBlockOfAnEarlierVersionIsUpdatedInPlace(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(home, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	old := "Host a\n  User x\n\n# >>> harnessly >>>\nHost *\n  IdentityFile ~/.ssh/harnessly_ed25519\n# <<< harnessly <<<\n\nHost b\n  User y\n"
	if err := os.WriteFile(cfgPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureGitKey(home, "h"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(cfgPath)
	want := "Host a\n  User x\n\n" + sshBlock() + "\nHost b\n  User y\n"
	if string(got) != want {
		t.Errorf("%q", got)
	}
}

// The agent takes away what it made and nothing else.
func TestRemoveGitKeyTakesOnlyWhatWasMade(t *testing.T) {
	t.Run("everything was made here", func(t *testing.T) {
		home, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
		if _, err := EnsureGitKeyIn(home, data, "h"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(data, gitKeyRecord)); err != nil {
			t.Fatal("no record", err)
		}
		if _, err := RemoveGitKey(home, data); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(home, ".ssh")); !os.IsNotExist(err) {
			t.Errorf("~/.ssh is left: %v", err)
		}
		if _, err := os.Stat(filepath.Join(data, gitKeyRecord)); !os.IsNotExist(err) {
			t.Error("the record is left")
		}
	})
	t.Run("the person's files stay", func(t *testing.T) {
		home, data := t.TempDir(), t.TempDir()
		dir := filepath.Join(home, ".ssh")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		own := "Host github.com\n  User git"
		if err := os.WriteFile(filepath.Join(dir, "config"), []byte(own), 0o600); err != nil {
			t.Fatal(err)
		}
		// A key of that name that was there before: not made here, so not removed.
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		keyPath := filepath.Join(dir, gitKeyName)
		if err := os.WriteFile(keyPath, marshalOpenSSH(priv, "mine"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := EnsureGitKeyIn(home, data, "h"); err != nil {
			t.Fatal(err)
		}
		if _, err := RemoveGitKey(home, data); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(keyPath); err != nil {
			t.Errorf("a key that was there before was removed: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "config")); string(got) != own+"\n" && string(got) != own {
			t.Errorf("config %q", got)
		}
		// The .pub was missing, so the agent made it, and it goes.
		if _, err := os.Stat(keyPath + ".pub"); !os.IsNotExist(err) {
			t.Errorf("the .pub the agent made is left: %v", err)
		}
	})
}
