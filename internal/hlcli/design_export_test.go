package hlcli

import (
	"strings"
	"testing"
)

func TestDesignExportAsksForThePageAndPrintsItsFile(t *testing.T) {
	var last seen
	env := previewServer(t, `{"job":{"id":"j1","state":"done"},"path":"/tmp/harnessly-delivered/export-j1/Оплата.html"}`, &last)
	r := runWith(t, t.TempDir(), env, "", "design", "export", "shop", "Оплата", "--no-deliver")
	if r.code != 0 || !strings.Contains(r.out, "/tmp/harnessly-delivered/export-j1/Оплата.html") {
		t.Fatalf("%+v", r)
	}
	if last.method != "POST" || last.path != "/api/cli/design/export" {
		t.Fatalf("sent %+v", last)
	}
	if b := last.body; b["format"] != "html" || b["page"] != "Оплата" || b["project"] != "shop" {
		t.Fatalf("body %v", last.body)
	}
	r = runWith(t, t.TempDir(), env, "", "design", "export", "shop", "--zip", "--no-deliver")
	if b := last.body; r.code != 0 || b["format"] != "zip" {
		t.Fatalf("%+v %v", r, last.body)
	}
	// A page with --zip, or html without a page, is a mistake.
	if r := runWith(t, t.TempDir(), env, "", "design", "export", "shop"); r.code == 0 {
		t.Fatal("no page took")
	}
}
