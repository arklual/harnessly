package webrelease

import (
	"errors"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestServesFollowsTheRoutesCheck(t *testing.T) {
	cases := []struct {
		route, call []string
		want        bool
	}{
		{[]string{"api", "apps"}, []string{"api", "apps"}, true},
		{[]string{"api", "apps"}, []string{"api", "apps", "*", "files"}, true}, // a prefix serves what is under it
		{[]string{"api", "apps", "{id}"}, []string{"api", "apps", "x"}, true},
		{[]string{"api", "apps", "*", "files"}, []string{"api", "apps"}, false}, // a longer route does not serve a short call
		{[]string{"api", "apps", "x", "files"}, []string{"api", "apps", "**"}, true},
		{[]string{"api", "link-meta"}, []string{"api", "apps"}, false},
	}
	for _, c := range cases {
		if got := Serves(c.route, c.call); got != c.want {
			t.Errorf("Serves(%v, %v) = %v, want %v", c.route, c.call, got, c.want)
		}
	}
}

func TestMissingNamesTheCallsANewerPageMakes(t *testing.T) {
	runtime := Manifest{Routes: [][]string{{"api", "apps"}, {"api", "platform", "chats"}}}
	page := Manifest{Calls: [][]string{{"api", "apps", "*"}, {"api", "link-meta"}, {"api", "platform", "chats"}}}
	if got := Missing(page, runtime); !reflect.DeepEqual(got, []string{"/api/link-meta"}) {
		t.Fatalf("missing = %v", got)
	}
}

func TestRead(t *testing.T) {
	if _, err := Read(fstest.MapFS{"index.html": {}}); !errors.Is(err, ErrNone) {
		t.Fatalf("no manifest: %v", err)
	}
	if _, err := Read(nil); !errors.Is(err, ErrNone) {
		t.Fatalf("nil page: %v", err)
	}
	if _, err := Read(fstest.MapFS{File: {Data: []byte(`{"schema":2}`)}}); err == nil {
		t.Fatal("an unknown schema is accepted")
	}
	m, err := Read(fstest.MapFS{File: {Data: []byte(`{"schema":1,"version":"w1","calls":[["api","x"]],"routes":[]}`)}})
	if err != nil || m.Version != "w1" || len(m.Calls) != 1 {
		t.Fatalf("read %+v, %v", m, err)
	}
}
