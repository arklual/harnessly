package docs

import (
	"context"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// operatorWindow is how long repeated openings of one document by the same administrator stay one line.
const operatorWindow = time.Hour

// noteOperator writes into the journal of the document's space that an administrator of the installation who is not
// in that space opened the document: who, which space, which document, nothing of what it says.
func (s *Service) noteOperator(ctx context.Context, p *auth.Principal, slug string, m *meta) error {
	if !p.Admin || m == nil || m.SpaceID == "" {
		return nil
	}
	st := s.db.S()
	role, err := st.Role(ctx, m.SpaceID, p.User.ID)
	if err != nil || role != "" {
		return err
	}
	sp, err := st.SpaceByID(ctx, m.SpaceID)
	if store.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	at := time.Now().UTC()
	_, err = st.AddOperatorRead(ctx, &store.AuditEvent{OrgID: sp.OrgID, SpaceID: sp.ID, ActorID: p.User.ID, ScopeKind: store.ScopeTenant, ScopeID: sp.ID,
		Action: "operator.read", TargetKind: "doc", TargetID: slug, At: at}, at.Add(-operatorWindow))
	return err
}
