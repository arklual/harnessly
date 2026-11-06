package apps

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// withActivation scaffolds a chat app and puts activation.on into its manifest.
func withActivation(t *testing.T, id string, on ...string) string {
	t.Helper()
	dir := scaffold(t, id, "chat")
	p := filepath.Join(dir, ManifestPath)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["activation"] = map[string]any{"on": on}
	out, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAgentCommandsAreNotAnApps(t *testing.T) {
	for _, cmd := range []string{"/clear", "/compact", "/model"} {
		m := &Manifest{Activation: &Activation{On: []string{"command:" + cmd}}}
		found := false
		for _, p := range m.Check() {
			if p.Path == "activation.on[0]" && !p.Warning {
				found = true
			}
		}
		if !found {
			t.Fatalf("an app may take %s", cmd)
		}
	}
}

func TestSecondAppWithTheSameCommandIsRefused(t *testing.T) {
	reg := &Registry{Root: t.TempDir()}
	if _, ps, err := reg.Install(withActivation(t, "acme.one", "command:/tasks"), InstallOptions{HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err, ps)
	}
	_, _, err := reg.Install(withActivation(t, "acme.two", "command:/tasks"), InstallOptions{HostVersion: "0.17.0"})
	if !errors.Is(err, ErrCommandTaken) {
		t.Fatalf("second /tasks: %v", err)
	}
	// The app itself installed again keeps its command.
	if _, _, err := reg.Install(withActivation(t, "acme.one", "command:/tasks"), InstallOptions{HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
}
