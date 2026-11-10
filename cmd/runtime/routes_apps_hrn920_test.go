package main

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// HRN-920: the log route takes since; the check route answers for an app that waits for consent or is off without a
// browser, says plainly when there is none, and 404s an app that is not installed.
func TestAppLogsSinceAndCheckRoutes(t *testing.T) {
	call, _ := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := examplePkg(t)
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	first := `{"id":"example.app","view":"main","lines":[{"level":"error","text":"first"}]}`
	if code, got := call("POST", platformAPI+"/apps/view-log", first, page); code != http.StatusOK {
		t.Fatalf("report: %d %s", code, got)
	}
	time.Sleep(15 * time.Millisecond)
	mid := time.Now().UnixMilli()
	time.Sleep(15 * time.Millisecond)
	second := `{"id":"example.app","view":"main","lines":[{"level":"error","text":"second"}]}`
	if code, got := call("POST", platformAPI+"/apps/view-log", second, page); code != http.StatusOK {
		t.Fatalf("report: %d %s", code, got)
	}
	code, got := call("GET", "/api/cli/apps/logs?id=example.app&since="+strconv.FormatInt(mid, 10), "", cli)
	if code != http.StatusOK || strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("since: %d %s", code, got)
	}

	// No browser in the test rig: the check says so, with a status the CLI turns into words.
	if code, got := call("POST", "/api/cli/apps/check", `{"id":"example.app"}`, cli); code != http.StatusServiceUnavailable || !strings.Contains(got, "no-browser") {
		t.Fatalf("check without a browser: %d %s", code, got)
	}
	if code, _ := call("POST", "/api/cli/apps/check", `{"id":"example.none"}`, cli); code != http.StatusNotFound {
		t.Fatalf("an app that is not installed: %d", code)
	}
	if code, _ := call("POST", "/api/cli/apps/check", `{"id":"example.app"}`, page); code != http.StatusForbidden {
		t.Fatalf("the check is the CLI's: %d", code)
	}
	// An app that waits for the person's consent is answered with no browser: what it waits for and where.
	waiting := examplePkg(t, "storage")
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+waiting+`","dev":true}`, cli); code != http.StatusOK {
		t.Fatalf("install waiting: %d %s", code, body)
	}
	if code, got := call("POST", "/api/cli/apps/check", `{"id":"example.app"}`, cli); code != http.StatusOK || !strings.Contains(got, `"state":"waiting"`) {
		t.Fatalf("check of a waiting app: %d %s", code, got)
	}
}
