package gate

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSameAnswer(t *testing.T) {
	if ok, _ := sameAnswer(200, []byte(`{"a":1,"b":[1,2]}`), 200, []byte(`{"b":[1,2.0],"a":1}`)); !ok {
		t.Fatal("key order and number form do not matter")
	}
	if ok, why := sameAnswer(200, []byte(`{"a":1}`), 404, []byte(`{"a":1}`)); ok || why != "status 200 vs 404" {
		t.Fatal(why)
	}
	if ok, _ := sameAnswer(200, []byte(`{"t":1,"x":true}`), 200, []byte(`{"t":2,"x":true}`), "t"); !ok {
		t.Fatal("a volatile field is compared by presence")
	}
	if _, why := sameAnswer(200, []byte(`{"a":{"b":[1,{"c":"x"}]},"z":1}`), 200, []byte(`{"z":1,"a":{"b":[1,{"c":"y"}]}}`)); why != `json at $.a.b[1].c: "x" vs "y"` {
		t.Fatal(why)
	}
	if _, why := sameAnswer(200, []byte(`{"a":[1],"b":1}`), 200, []byte(`{"a":[1,2]}`)); why != "json at $.a: 1 vs 2 elements" {
		t.Fatal(why)
	}
	if _, why := sameAnswer(200, []byte(`{"a":1}`), 200, []byte(`{"a":1,"b":2}`)); why != "json at $.b: only go" {
		t.Fatal(why)
	}
	if ok, _ := sameAnswer(200, []byte(`{"t":1}`), 200, []byte(`{"u":1}`), "t"); ok {
		t.Fatal("a volatile field must be in both")
	}
	old := []byte(`{"machines":[{"id":"a","uptimeSec":5,"limits":{"claude":{"fiveHour":{"forecast":{"x":1},"usedPct":3}},"codex":null}}]}`)
	cur := []byte(`{"machines":[{"id":"a","uptimeSec":9,"limits":{"claude":{"fiveHour":{"forecast":{"x":2},"usedPct":3}},"codex":null}}]}`)
	if ok, why := sameAnswer(200, old, 200, cur, "machines.*.uptimeSec", "machines.*.limits.*.fiveHour.forecast"); !ok {
		t.Fatal("paths into the answer are volatile too: " + why)
	}
	if ok, _ := sameAnswer(200, old, 200, []byte(strings.Replace(string(cur), `"usedPct":3`, `"usedPct":4`, 1)), "machines.*.uptimeSec",
		"machines.*.limits.*.fiveHour.forecast"); ok {
		t.Fatal("the rest is compared")
	}
	if ok, _ := sameAnswer(200, []byte(`{"c":{"at":1,"n":2},"l":null}`), 200, []byte(`{"c":{"at":5,"n":2},"l":null}`), "c.at"); !ok {
		t.Fatal("a nested volatile field is compared by presence")
	}
	if ok, _ := sameAnswer(200, []byte(`{"c":{"at":1,"n":2}}`), 200, []byte(`{"c":{"at":1,"n":3}}`), "c.at"); ok {
		t.Fatal("the rest of a nested object is compared")
	}
	if ok, _ := sameAnswer(200, []byte(`{"c":null}`), 200, []byte(`{"c":null}`), "c.at"); !ok {
		t.Fatal("a nested volatile field under null")
	}
	if ok, _ := sameAnswer(500, []byte("Internal Server Error"), 500, encode(Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")})); !ok {
		t.Fatal("raw bodies compare as bytes")
	}
}

func TestSameShapeAnswer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{`{"state":"running","n":1,"list":[{"x":1}]}`, `{"n":2,"state":"success","list":[{"x":3},{"x":4}]}`, true},
		{`{"assignee":null,"title":"a"}`, `{"assignee":{"name":"b"},"title":"c"}`, true},
		{`{"title":"a"}`, `{"title":"a","extra":1}`, false},
		{`{"title":"a"}`, `{"title":1}`, false},
		{`{"list":[{"x":1}]}`, `{"list":[{"y":1}]}`, false},
		{`{"list":[]}`, `{"list":{}}`, false},
	} {
		if ok, why := sameShapeAnswer(200, []byte(c.a), 200, []byte(c.b)); ok != c.same {
			t.Errorf("%s vs %s: %v %s", c.a, c.b, ok, why)
		}
	}
	if ok, _ := sameShapeAnswer(200, []byte(`{}`), 404, []byte(`{}`)); ok {
		t.Error("the status is compared")
	}
}

func TestWriteRaw(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, 500, Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}, nil)
	if w.Code != 500 || w.Header().Get("Content-Type") != "text/plain" || w.Body.String() != "Internal Server Error" {
		t.Fatalf("%d %q %q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	w = httptest.NewRecorder()
	writeJSON(w, 200, map[string]any{"valid": true}, nil)
	if w.Header().Get("Content-Type") != "application/json; charset=utf-8" || w.Body.String() != `{"valid":true}` {
		t.Fatalf("%q", w.Body.String())
	}
}

func TestQueryReachesTheOldRuntimeAsWritten(t *testing.T) {
	var got string
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.URL.RawQuery }))
	defer old.Close()
	f, err := New(Config{Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(f)
	defer front.Close()
	res, err := http.Get(front.URL + "/api/x?directory=/a;b&y=%zz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got != "directory=/a;b&y=%zz" {
		t.Fatalf("query %q", got)
	}
}
