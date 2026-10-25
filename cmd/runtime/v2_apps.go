package main

import (
	"context"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The Apps screen of the page (/api/v2/apps) is read by the services slice: the tracker and GitLab, with the person's
// own tokens, the ones the task card and the merge request pane use.

// v2Apps is wire.AppsSource over the services of this runtime.
type v2Apps struct{ d deps }

var _ wire.AppsSource = v2Apps{}

func (a v2Apps) Feed(ctx context.Context) (wire.AppsFeed, error) {
	h, _ := handlersFor(a.d)
	return h.S.Apps(ctx), nil
}
