package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

func TestPersonalReviewOwnerConnection(t *testing.T) {
	const origin = "https://git.example.com"
	const address = origin + "/group/sub/repo/-/merge_requests/42"
	t.Setenv("RUNTIME_OWNER_EMAIL", "owner@example.com")
	allowed := true
	subject := "owner@example.com"
	d := deps{Claims: func(*http.Request) map[string]any { return map[string]any{"verifiedOwner": allowed, "sub": subject} }}
	calls := 0
	responseCode := http.StatusOK
	providerOwner := "owner@example.com"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/v1/detect":
			if in["url"] != origin {
				t.Errorf("detection disclosed %v", in)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"baseUrl": origin})
		case "/v1/reviews/get":
			calls++
			conn, _ := in["conn"].(map[string]any)
			if r.Method != "POST" || len(in) != 3 || in["ownerSubject"] != "owner@example.com" || conn["method"] != "none" || conn["baseUrl"] != origin || in["url"] != address {
				t.Errorf("unexpected request %+v", in)
			}
			w.WriteHeader(responseCode)
			_ = json.NewEncoder(w).Encode(map[string]any{"ownerSubject": providerOwner, "view": map[string]any{"kind": "mr", "url": address, "diff": "+line"}, "review": map[string]any{"title": "Change", "canApprove": true}})
		default:
			t.Errorf("unexpected operation %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := ext.NewClient(ext.Config{BaseURL: server.URL, Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	p, err := connections.NewExtProvider(client, ext.Description{Name: "personal-forge", Capabilities: []string{"detect", "merge-request-read"}})
	if err != nil {
		t.Fatal(err)
	}
	cs := connections.New(t.TempDir(), nil, nil)
	read := func(path string, view bool) (any, int) {
		r := httptest.NewRequest("GET", path, nil)
		return readPersonalReview(d, r, personalReviewURL(address+"?tab=changes#diff"), view, cs, []connections.Provider{p})
	}
	out, status := read("/", true)
	if status != 200 || string(out.(map[string]json.RawMessage)["diff"]) != "\"+line\"" {
		t.Fatalf("diff: %v %v", status, out)
	}
	out, status = read("/", false)
	if status != 200 || out.(map[string]any)["review"].(*connections.Review).CanApprove || out.(map[string]any)["connection"].(map[string]any)["personal"] != false {
		t.Fatalf("review: %v %v", status, out)
	}
	for _, code := range []int{403, 404} {
		responseCode = code
		if _, status = read("/", true); status != code {
			t.Fatalf("wanted %v got %v", code, status)
		}
	}
	if _, status = read("/?path=private.txt", true); status != 404 {
		t.Fatal(status)
	}
	subject = "other@example.com"
	if _, status = read("/", true); status != 403 {
		t.Fatal(status)
	}
	subject = "owner@example.com"
	allowed = false
	if _, status = read("/", true); status != 403 {
		t.Fatal(status)
	}
	allowed = true
	t.Setenv("RUNTIME_OWNER_EMAIL", "")
	responseCode = 200
	if _, status = read("/", true); status != 200 {
		t.Fatal(status)
	}
	providerOwner = "old-owner@example.com"
	if _, status = read("/", true); status != 403 {
		t.Fatal(status)
	}
	if calls != 6 {
		t.Fatalf("adapter reads=%d; expected six owner reads", calls)
	}
}

func TestPersonalReviewObjectURLs(t *testing.T) {
	for _, raw := range []string{"http://forge.example/g/r/-/merge_requests/1", "https://user@forge.example/g/r/-/merge_requests/1", "https://forge.example/g/%2e%2e/-/merge_requests/1", "https://forge.example/g/r/-/merge_requests/1/approve", "https://forge.example/g/r/-/commits/42"} {
		if personalReviewURL(raw) != nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
