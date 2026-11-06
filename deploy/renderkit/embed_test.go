package renderkit

import (
	"os"
	"regexp"
	"testing"
)

func TestPinsComplete(t *testing.T) {
	p := Pins()
	for _, k := range []string{"RENDER_KIT_REV", "RENDER_KIT_DEBIAN", "RENDER_KIT_CHROMIUM", "RENDER_KIT_FFMPEG", "RENDER_KIT_FONTS_LIBERATION", "RENDER_KIT_FONTS_EMOJI"} {
		if p[k] == "" {
			t.Errorf("%s is not pinned", k)
		}
	}
	if Major(p["RENDER_KIT_CHROMIUM"]) < 1 || Major(p["RENDER_KIT_FFMPEG"]) < 1 {
		t.Errorf("majors do not parse: %v", p)
	}
}

func TestMajor(t *testing.T) {
	for in, want := range map[string]int{"156.*": 156, "7:5.1.*": 5, "Google Chrome 154": -1, "": -1} {
		if got := Major(in); got != want {
			t.Errorf("Major(%q)=%d want %d", in, got, want)
		}
	}
}

// install.sh is served alone, so it carries the pins itself: they must be the file's.
func TestInstallScriptCarriesThePins(t *testing.T) {
	raw, err := os.ReadFile("../install/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range Pins() {
		re := regexp.MustCompile(`(?m)^\s*` + k + `='?` + regexp.QuoteMeta(v) + `'?\s*$`)
		if !re.Match(raw) {
			t.Errorf("install.sh must carry %s=%s", k, v)
		}
	}
}

// The image installs from the same file.
func TestDockerfileUsesThePins(t *testing.T) {
	raw, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`deploy/renderkit/versions\.env`).Match(raw) {
		t.Error("Dockerfile must install the render kit from deploy/renderkit/versions.env")
	}
}
