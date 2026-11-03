package design

import (
	"regexp"
	"strings"
	"testing"
)

// The living-art kit: the library serves the rig and the art packs, and the art has what the guide promises.

func TestMotionKitServesTheLivingArt(t *testing.T) {
	kit := motionKit()
	for _, name := range []string{"rig.js", "art/art.js", "art/heroes.js", "art/props.js", "art/backdrops.js"} {
		f := kit[name]
		if f == nil || len(f.raw) == 0 {
			t.Fatalf("%s is missing from the kit", name)
		}
		if f.typ != "text/javascript; charset=utf-8" {
			t.Errorf("%s is served as %q", name, f.typ)
		}
	}
	count := func(file, prefix string) int {
		return len(regexp.MustCompile(`define\('`+prefix).FindAll(kit[file].raw, -1))
	}
	if n := count("art/heroes.js", "hero-"); n < 3 {
		t.Errorf("heroes: %d", n)
	}
	if n := count("art/props.js", "prop-"); n < 10 {
		t.Errorf("things: %d, want at least 10", n)
	}
	if n := count("art/backdrops.js", "bg-"); n < 3 {
		t.Errorf("backdrops: %d, want at least 3", n)
	}
}

// Every hero has the parts the rig moves, so one script of actions plays on any of them. A side's part is built from a
// template in the drawing (data-part="arm' + side), so both spellings count.
func TestHeroesHaveTheRigsParts(t *testing.T) {
	raw := string(motionKit()["art/heroes.js"].raw)
	for _, hero := range []string{"hero-person", "hero-round", "hero-robot"} {
		i := strings.Index(raw, "define('"+hero+"'")
		if i < 0 {
			t.Fatalf("no %s", hero)
		}
		body := raw[i:]
		if j := strings.Index(body[10:], "define('hero-"); j >= 0 {
			body = body[:10+j]
		}
		// Arms and legs are built by functions above the definition; the definition calls them.
		for _, part := range []string{"root", "body", "head", "face", "eyeL", "eyeR", "pupilL", "pupilR", "browL", "browR", "mouth", "shadow"} {
			if !strings.Contains(body, `data-part="`+part+`"`) && !strings.Contains(body, `' + mouth(`) {
				t.Errorf("%s has no part %s", hero, part)
			}
		}
		for _, call := range []string{"Arm('L')", "Arm('R')", "Leg('L')", "Leg('R')", "Foot('L')", "Foot('R')"} {
			if strings.Contains(body, call) {
				goto limbs
			}
		}
		t.Errorf("%s has no arms and legs", hero)
	limbs:
	}
	for _, part := range []string{`data-part="arm' + side`, `data-part="leg' + side`, `data-part="fore' + side`} {
		if !strings.Contains(raw, part) {
			t.Errorf("no template for %s", part)
		}
	}
}

func TestEveryScriptOfTheKitIsGSAPOnly(t *testing.T) {
	for name, f := range motionKit() {
		if !strings.HasPrefix(name, "art/") && name != "rig.js" && name != "tk.js" {
			continue
		}
		src := string(f.raw)
		for _, banned := range []string{"Math.random", "requestAnimationFrame", "gsap.ticker", "Date.now", "performance.now", "http://", "https://"} {
			// comments may name them; code may not.
			for _, line := range strings.Split(src, "\n") {
				code := line
				if k := strings.Index(code, "//"); k >= 0 {
					code = code[:k]
				}
				if strings.Contains(code, banned) && !strings.Contains(code, "www.w3.org") {
					t.Errorf("%s uses %s: %s", name, banned, strings.TrimSpace(line))
				}
			}
		}
	}
}
