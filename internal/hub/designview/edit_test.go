package designview

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
)

// The hub takes an editor's save (designaccess.EditRoutes) only for a route the view can run, and runs every route it
// knows.
func TestEditRoutesAreTheHubsRoutes(t *testing.T) {
	for route := range EditRoutes {
		if !designaccess.EditRoutes[route] {
			t.Errorf("designaccess.EditRoutes lacks %q", route)
		}
	}
	for route := range designaccess.EditRoutes {
		if EditRoutes[route] == nil {
			t.Errorf("designview.EditRoutes lacks %q", route)
		}
	}
}
