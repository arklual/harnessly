package nodejson

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type sample struct {
	A   string   `json:"a"`
	N   *string  `json:"n"`
	T   float64  `json:"t"`
	Arr []string `json:"arr"`
	U   string   `json:"u"`
}

func TestMarshalLikeJSONStringify(t *testing.T) {
	v := sample{A: "<b>&\u2028x\u2029\u0001\b\f\n\t\"\\", T: 1.7e12, Arr: []string{}, U: "é"}
	// Written by node: JSON.stringify of the same object, and the dispatcher's etag of it.
	want := "{\"a\":\"<b>&\u2028x\u2029\\u0001\\b\\f\\n\\t\\\"\\\\\",\"n\":null,\"t\":1700000000000,\"arr\":[],\"u\":\"é\"}"
	got := Marshal(v)
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if tag := ETag(got); tag != `"l2Fo63fJW_-FdUSwsyhzGZoAErM"` {
		t.Fatalf("etag %s", tag)
	}
}

func TestEscapedSeparatorTextStays(t *testing.T) {
	got := string(Marshal(map[string]string{"k": `\u2028`}))
	if got != `{"k":"\\u2028"}` {
		t.Fatalf("%s", got)
	}
}

func TestAnswerConditional(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	status, v, h := Answer(r, 404, map[string]string{"error": "x"}, nil)
	if status != 404 || string(v.(json.RawMessage)) != `{"error":"x"}` || h.Get("ETag") == "" {
		t.Fatalf("%d %v %v", status, v, h)
	}
	for _, c := range []struct {
		inm, cc string
		fresh   bool
	}{
		{h.Get("ETag"), "", true},
		{`W/` + h.Get("ETag"), "", true},
		{`"other", ` + h.Get("ETag"), "", true},
		{"*", "", true},
		{h.Get("ETag"), "no-cache", false},
		{h.Get("ETag"), "max-age=0, no-cache", false},
		{`"other"`, "", false},
	} {
		r := httptest.NewRequest("GET", "/x", nil)
		r.Header.Set("If-None-Match", c.inm)
		if c.cc != "" {
			r.Header.Set("Cache-Control", c.cc)
		}
		status, v, _ := Answer(r, 404, map[string]string{"error": "x"}, http.Header{"Cache-Control": {"no-store"}})
		if (status == 304) != c.fresh || (status == 304) != (v == nil) {
			t.Errorf("%+v: %d %v", c, status, v)
		}
	}
}
