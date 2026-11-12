package wire

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestDesignlyCreateProfileContract(t *testing.T) {
	sc := mustSchema(t)
	def := sc.defs["DesignlyCreate"].(map[string]any)
	for _, kind := range []string{"site", "slides", "video"} {
		for _, profile := range []string{"glm", "kimi", "gpt-compatible"} {
			t.Run(kind+"/"+profile, func(t *testing.T) {
				raw := []byte(fmt.Sprintf(`{"title":"Test","brief":"A simple design","kind":%q,"provider":"claude","profile":%q,"model":"vendor/model","effort":null,"designSystem":null}`, kind, profile))
				var c DesignlyCreate
				if err := decodeStrict(raw, &c); err != nil {
					t.Fatal(err)
				}
				if err := c.Validate(); err != nil {
					t.Fatal(err)
				}
				v, err := decodeNumbers(raw)
				if err != nil {
					t.Fatal(err)
				}
				if err := sc.check(def, v, "$"); err != nil {
					t.Fatal(err)
				}
				back, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]any
				if err := json.Unmarshal(back, &fields); err != nil {
					t.Fatal(err)
				}
				if fields["profile"] != profile {
					t.Fatalf("profile lost: %s", back)
				}
			})
		}
	}
}

func TestDesignlyCreateRejectsMalformedProfile(t *testing.T) {
	for _, profile := range []string{"../other", "bad profile", "$(echo bad)"} {
		raw := []byte(fmt.Sprintf(`{"title":"Test","brief":"Test","kind":"site","provider":"claude","profile":%q,"model":"vendor/model","effort":null,"designSystem":null}`, profile))
		var c DesignlyCreate
		if err := decodeStrict(raw, &c); err != nil {
			t.Fatal(err)
		}
		if c.Validate() == nil {
			t.Fatalf("accepted invalid profile %q", profile)
		}
	}
}
