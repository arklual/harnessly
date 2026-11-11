package filesync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A store that refuses contents for their size answers ErrTooBig; other refusals do not.
func TestAPutRefusedForItsSizeIsTooBig(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		big    bool
	}{
		{http.StatusBadRequest, `{"error":"file bigger than 52428800 bytes"}`, true},
		{http.StatusRequestEntityTooLarge, "", true},
		{http.StatusBadRequest, `{"error":"contents do not hash"}`, false},
		{http.StatusServiceUnavailable, "busy", false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, tc.body, tc.status)
		}))
		c := NewClient(srv.URL, "uploads", func(context.Context, bool) (string, error) { return "t", nil }, nil)
		err := c.PutBlob(context.Background(), "abc", []byte("x"))
		srv.Close()
		if err == nil || errors.Is(err, ErrTooBig) != tc.big {
			t.Errorf("%d %q: err = %v, want too big %v", tc.status, tc.body, err, tc.big)
		}
	}
}
