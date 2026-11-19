package outputs

import "testing"

func TestWroteNamesTheBoardOfADesign(t *testing.T) {
	out, ok := Wrote("/srv/design-artifacts/harnessly/project/Регулярные задачи.dc.html", false)
	if !ok || out.Ref != "harnessly" || len(out.Pages) != 1 || out.Pages[0] != "Регулярные задачи.dc.html" {
		t.Fatalf("board: %+v %v", out, ok)
	}
	out, ok = Wrote("/srv/design-artifacts/harnessly/project/lib/hx/tokens.css", false)
	if !ok || out.Ref != "harnessly" || len(out.Pages) != 0 {
		t.Fatalf("not a board: %+v %v", out, ok)
	}
}
