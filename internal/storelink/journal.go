package storelink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// ErrNoJournal is a store that keeps no journal of the move of the machine's data (docs/migrations): the store inside
// the runtime, or a hub of an older version.
var ErrNoJournal = errors.New("the hub keeps no journal of the move")

// ErrNoSidebar is a hub that keeps no sidebar of the machine's person (its history is off, or it is older).
var ErrNoSidebar = errors.New("the hub keeps no sidebar")

// Report tells the hub's journal how the move of this machine's data goes (hubproto.MigrationReport) and returns what
// a person asked of it since. It goes with the store session; a refused session is asked again once.
func (l *Link) Report(ctx context.Context, rep hubproto.MigrationReport) (hubproto.MigrationAnswer, error) {
	var ans hubproto.MigrationAnswer
	return ans, l.post(ctx, "/migration/v1/report", "report", rep, &ans, ErrNoJournal)
}

// Sidebar merges this machine's projects and favorites into its person's sidebar on the hub (hubproto.MigrationSidebar).
func (l *Link) Sidebar(ctx context.Context, side hubproto.MigrationSidebar) (hubproto.MigrationSidebarAnswer, error) {
	var ans hubproto.MigrationSidebarAnswer
	return ans, l.post(ctx, "/migration/v1/sidebar", "sidebar", side, &ans, ErrNoSidebar)
}

// post sends in to the hub's journal API with the store session and decodes its answer into out; a hub without the
// route is none (the error it is).
func (l *Link) post(ctx context.Context, path, what string, in, out any, none error) error {
	if l == nil || l.kind != KindHub {
		return none
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := l.token(ctx, attempt > 0)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.base+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := l.hc.Do(req)
		if err != nil {
			return err
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusUnauthorized && attempt == 0:
			continue
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed:
			return none
		case resp.StatusCode/100 != 2:
			return fmt.Errorf("the hub answered the %s %d: %.200s", what, resp.StatusCode, raw)
		}
		// An older hub may answer an unknown route with its page: that is none either.
		if err := json.Unmarshal(raw, out); err != nil {
			return none
		}
		return nil
	}
	return errors.New("the hub refused this machine's store session")
}
