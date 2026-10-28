package sitesclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func claimServer(t *testing.T, getStatus int, origin string, postStatus int) (*Client, *int) {
	t.Helper()
	posts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sites/{space}/{site}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(getStatus)
		_ = json.NewEncoder(w).Encode(map[string]string{"origin": origin, "error": "x"})
	})
	mux.HandleFunc("POST /api/sites/{space}", func(w http.ResponseWriter, r *http.Request) {
		posts++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(postStatus)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "x"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, Token: "token-for-the-test"}, &posts
}

func TestClaimTakesOnlyItsOwnSites(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name       string
		get        int
		origin     string
		post       int
		want       error
		wantCreate bool
	}{
		{"same origin", 200, "sinopia:p1", 201, nil, false},
		{"another origin", 200, "sinopia:p2", 201, ErrNameTaken, false},
		{"no origin: a site of a person", 200, "", 201, ErrNameTaken, false},
		{"somebody else's", 403, "", 201, ErrNameTaken, false},
		{"free", 404, "", 201, nil, true},
		{"taken while creating", 404, "", 409, ErrNameTaken, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl, posts := claimServer(t, c.get, c.origin, c.post)
			err := cl.Claim(ctx, "acme", "deck", "team", "sinopia:p1")
			if !errors.Is(err, c.want) {
				t.Fatalf("err %v, want %v", err, c.want)
			}
			if (*posts > 0) != c.wantCreate {
				t.Fatalf("creates: %d", *posts)
			}
		})
	}
}
