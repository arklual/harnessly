package api

import (
	"context"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// orgEvents tells the people watching the tenants of an organization that something in it changed (env.changed).
type orgEvents struct {
	db     *store.DB
	broker *events.Broker
}

func (o orgEvents) Publish(orgID, kind string, data any) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	spaces, err := o.db.S().SpacesByOrg(ctx, orgID)
	if err != nil {
		return
	}
	for _, sp := range spaces {
		o.broker.PublishJSON(sp.ID, kind, data, false)
	}
}
