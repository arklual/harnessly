package api_test

import (
	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/appsview"
	"github.com/ly-solutions/harnessly/internal/hub/designview"
)

// The hub's main sets the store's view of design projects; the API's tests do the same.
func init() {
	api.DesignView = designview.Hub
	api.AppsView = appsview.Hub
}
