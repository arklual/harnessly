package orgs

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// FailTenantMade makes the next tenants fail inside their transaction, after the space is made and bound.
func FailTenantMade(t *testing.T, err error) {
	was := tenantMade
	tenantMade = func(context.Context, store.Store, store.Space) error { return err }
	t.Cleanup(func() { tenantMade = was })
}
