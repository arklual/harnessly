package noticehub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

type plain struct{}

func (plain) ServeMachine(http.ResponseWriter, *http.Request, store.User, store.Machine) bool {
	return false
}

type rich struct{ plain }

func (rich) ServeHome(http.ResponseWriter, *http.Request, store.User, store.Machine, func(context.Context, http.ResponseWriter)) bool {
	return true
}
func (rich) AnchorMachines(context.Context, []string, string) []string { return []string{"m1"} }
func (rich) KeyedFileUser(*http.Request) string                        { return "u1" }

// The proxy reaches the design service through the chain: what the service answers beyond ServeMachine must come
// through it, past a member that has none of it.
func TestChainPassesOnWhatItsMembersAnswer(t *testing.T) {
	c := Chain{plain{}, rich{}}
	r := httptest.NewRequest("GET", "/", nil)
	if !c.ServeHome(httptest.NewRecorder(), r, store.User{}, store.Machine{}, nil) {
		t.Error("home not answered")
	}
	if got := c.AnchorMachines(context.Background(), nil, ""); len(got) != 1 || got[0] != "m1" {
		t.Errorf("anchor: %v", got)
	}
	if got := c.KeyedFileUser(r); got != "u1" {
		t.Errorf("keyed file user: %q", got)
	}
	if (Chain{plain{}}).KeyedFileUser(r) != "" || (Chain{plain{}}).ServeHome(httptest.NewRecorder(), r, store.User{}, store.Machine{}, nil) {
		t.Error("a chain without them answers nothing")
	}
}
