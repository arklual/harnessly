package config

import (
	"strings"
	"testing"
)

func TestDesignShareAliasesAndDocs(t *testing.T) {
	base := func(extra map[string]string) map[string]string {
		m := map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_SMTP_URL": "smtp://mail.example.com", "HUB_DESIGN_SHARE_URL": "https://share.example.com"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	c, err := Load(env(base(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.DesignShareAliases) != 0 || c.DesignShareDocs {
		t.Errorf("both are off by default: %v %v", c.DesignShareAliases, c.DesignShareDocs)
	}
	c, err = Load(env(base(map[string]string{"HUB_DESIGN_SHARE_ALIASES": "https://old.example.com/, https://older.example.com", "HUB_DESIGN_SHARE_DOCS": "on"})))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.DesignShareAliases) != 2 || c.DesignShareAliases[0] != "https://old.example.com" || !c.DesignShareDocs {
		t.Errorf("aliases: %v docs: %v", c.DesignShareAliases, c.DesignShareDocs)
	}
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"alias is the hub":        {base(map[string]string{"HUB_DESIGN_SHARE_ALIASES": "https://hub.example.com"}), "HUB_DESIGN_SHARE_ALIASES"},
		"alias repeats the share": {base(map[string]string{"HUB_DESIGN_SHARE_ALIASES": "https://share.example.com"}), "HUB_DESIGN_SHARE_ALIASES"},
		"alias is not an origin":  {base(map[string]string{"HUB_DESIGN_SHARE_ALIASES": "https://old.example.com/x"}), "HUB_DESIGN_SHARE_ALIASES"},
		"alias is the apps host":  {base(map[string]string{"HUB_DESIGN_SHARE_ALIASES": "https://apps.example.com", "HUB_APPS_GUEST_URL": "https://apps.example.com"}), "HUB_APPS_GUEST_URL"},
		"docs value":              {base(map[string]string{"HUB_DESIGN_SHARE_DOCS": "maybe"}), "HUB_DESIGN_SHARE_DOCS"},
		"aliases without share":   {map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_SMTP_URL": "smtp://mail.example.com", "HUB_DESIGN_SHARE_ALIASES": "https://old.example.com"}, "HUB_DESIGN_SHARE_ALIASES"},
		"docs without share":      {map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_SMTP_URL": "smtp://mail.example.com", "HUB_DESIGN_SHARE_DOCS": "on"}, "HUB_DESIGN_SHARE_DOCS"},
	} {
		if _, err := Load(env(tc.env)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want mention of %s", name, err, tc.want)
		}
	}
}
