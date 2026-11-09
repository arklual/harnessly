package storehub

import (
	"context"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// AppsViewer is the hub's serving of harnessly apps from the store (internal/hub/appsview). The hub's API
// takes it through a hook its main sets, because the apps import the agent, whose tests import the API.
type AppsViewer interface {
	// ServeHTTP answers an app's guest link (/apps/guest/<token>/...): the token is the whole authority.
	http.Handler
	// ServeMember answers /apps/space/<space>/<app>/... for a person of the space the API checked, named as the store
	// names them (PersonName).
	ServeMember(w http.ResponseWriter, r *http.Request, spaceID, person string)
	// SpaceApps is the apps installed for a space.
	SpaceApps(ctx context.Context, spaceID string) ([]hubproto.SpaceApp, error)
}
