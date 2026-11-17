package telemetry

import (
	"path/filepath"
	"testing"
)

func TestEnvOff(t *testing.T) {
	for env, want := range map[string]bool{
		"RUNTIME_TELEMETRY=off": true, "HARNESSLY_TELEMETRY=0": true, "HARNESSLY_TELEMETRY=Disabled": true,
		"DO_NOT_TRACK=1": true, "DO_NOT_TRACK=true": true, "DO_NOT_TRACK=0": false, "HARNESSLY_TELEMETRY=on": false, "": false,
	} {
		get := func(string) string { return "" }
		if len(env) > 0 {
			k := env[:indexEq(env)]
			get = func(x string) string {
				if x == k {
					return env[len(k)+1:]
				}
				return ""
			}
		}
		if got, _ := EnvOff(get); got != want {
			t.Errorf("%q: off=%v, want %v", env, got, want)
		}
	}
}

func indexEq(s string) int {
	for i := range s {
		if s[i] == '=' {
			return i
		}
	}
	return len(s)
}

// On by default with a stable random id; off in the file forgets the id; the environment's off wins over the file.
func TestConsent(t *testing.T) {
	envOff := ""
	c := &Consent{Path: filepath.Join(t.TempDir(), "telemetry.json"), Getenv: func(k string) string {
		if k == "HARNESSLY_TELEMETRY" {
			return envOff
		}
		return ""
	}}
	if !c.Allowed() || c.Why() != "" {
		t.Fatal("not on by default")
	}
	id := c.ID()
	if len(id) != 32 || c.ID() != id {
		t.Fatalf("id %q not stable", id)
	}
	if err := c.Set(false); err != nil {
		t.Fatal(err)
	}
	if c.Allowed() || c.Why() != "settings" {
		t.Fatal("off in settings not kept")
	}
	_ = c.Set(true)
	if !c.Allowed() || c.ID() == id {
		t.Fatal("on again should be a new machine")
	}
	envOff = "off"
	if c.Allowed() || c.Why() != "HARNESSLY_TELEMETRY" {
		t.Fatal("environment's off does not win")
	}
}
