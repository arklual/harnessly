package designview

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
	"github.com/ly-solutions/harnessly/internal/hub/storehub"
)

// Hub is the view over the hub's store as the hub's API takes it (api.DesignView): the spaces' design rooms, put into
// folders under <data>/design-view. The API does not import this package (the runtime's design code it runs imports the
// agent, whose tests import the API); the hub's main sets it.
func Hub(st *storehub.Store, dataDir string, app fs.FS, log *slog.Logger) designaccess.Viewer {
	v := New(Options{
		Rooms: func(ctx context.Context, spaceID string) (Room, error) { return st.DesignRoom(ctx, spaceID) },
		Dir:   filepath.Join(dataDir, "design-view"), GuestApp: app, Log: log,
	})
	if v == nil {
		return nil
	}
	return v
}
