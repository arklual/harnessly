package main

import (
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNodeQuery(t *testing.T) {
	got := nodeQuery("provider=claude&&cwd=%2Froot%2Fa+b;c&x&cwd2=%zz&bad=%E2%82")
	want := map[string][]string{"provider": {"claude"}, "cwd": {"/root/a b;c"}, "x": {""}, "cwd2": {"%zz"}, "bad": {"\uFFFD"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q", got)
	}
	if got := nodeQuery("cwd=1&cwd=2")["cwd"]; len(got) != 2 {
		t.Errorf("repeated: %q", got)
	}
}

func TestTaggedAnswers304(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	status, v, h := tagged(r, 200, map[string]string{"a": "<b>"}, nil)
	etag := h.Get("ETag")
	if status != 200 || v == nil || len(etag) != 29 {
		t.Fatalf("%d %v %q", status, v, etag)
	}
	for _, inm := range []string{etag, "W/" + etag, `"x", ` + etag, "*"} {
		r.Header.Set("If-None-Match", inm)
		if status, v, _ := tagged(r, 200, map[string]string{"a": "<b>"}, nil); status != 304 || v != nil {
			t.Errorf("%q: %d", inm, status)
		}
	}
	r.Header.Set("Cache-Control", "no-cache")
	if status, _, _ := tagged(r, 200, map[string]string{"a": "<b>"}, nil); status != 200 {
		t.Errorf("no-cache: %d", status)
	}
}
