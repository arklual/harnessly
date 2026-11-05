package sitesclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// «Поделиться» finds the site made from its project among the machine's sites, by origin only.
func TestFindByOriginTakesOnlyTheProjectsSite(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sites/{space}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sites": []map[string]any{
			{"name": "hand-made", "address": "https://hand.example.com", "access": "team", "currentVersion": 1},
			{"name": "other", "address": "https://other.example.com", "access": "team", "currentVersion": 2, "origin": "sinopia:p2"},
			{"name": "shop", "address": "https://shop.example.com", "access": "link", "currentVersion": 3, "origin": "sinopia:p1"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &Client{Base: srv.URL, Token: "token-for-the-test"}
	site, err := c.FindByOrigin(context.Background(), "acme", "sinopia:p1")
	if err != nil || site == nil {
		t.Fatalf("site %v, err %v", site, err)
	}
	if site.Name != "shop" || site.Address != "https://shop.example.com" || site.Access != "link" || site.CurrentVersion != 3 {
		t.Fatalf("site %+v", site)
	}
	none, err := c.FindByOrigin(context.Background(), "acme", "sinopia:p9")
	if err != nil || none != nil {
		t.Fatalf("none %v, err %v", none, err)
	}
	if empty, _ := c.FindByOrigin(context.Background(), "acme", ""); empty != nil {
		t.Fatalf("an empty origin matched %+v", empty)
	}
}
