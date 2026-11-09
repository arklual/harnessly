package design

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// A link to the kit's view carries the props in its address: none of them may become a script's way in.
func TestPlayBoardWritesNoHandlerFromTheAddress(t *testing.T) {
	c := &designkit.Canvas{Namespace: "Kit", Groups: []designkit.ComponentGroup{{ID: "g", Components: []designkit.Component{{
		ID: "Button", Name: "Button",
		Props: []designkit.Prop{{Name: "label"}, {Name: "size"}, {Name: "onClick"}, {Name: "from"}, {Name: "sc-camel-onclick"}},
	}}}}}
	q := url.Values{}
	q.Set("c", "Button")
	q.Set("p", `{"label":"ok","onfocus":"alert(1)","OnClick":"alert(2)","autofocus":"","tabindex":"0","style":"x","href":"javascript:alert(3)","size":2,"from":"https://evil/x.js","sc-camel-onclick":"alert(4)","undeclared":"x"}`)
	page, ok := playBoard(c, "", t.TempDir(), q)
	if !ok {
		t.Fatal("no board")
	}
	low := strings.ToLower(page)
	for _, bad := range []string{"onfocus", "onclick", "autofocus", "tabindex", ` style="x"`, "javascript:", "evil", "sc-camel", "undeclared"} {
		if strings.Contains(low, bad) {
			t.Fatalf("the board writes %q:\n%s", bad, page)
		}
	}
	if !strings.Contains(page, ` label="ok"`) || !strings.Contains(page, ` size="{{size}}"`) {
		t.Fatalf("the board lost its props:\n%s", page)
	}
}
