package machines

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func ownerGitAuthor(ctx context.Context, st store.Store, owner string) (hubproto.GitAuthor, error) {
	var none hubproto.GitAuthor
	if owner == "" {
		return none, nil
	}
	u, err := st.UserByID(ctx, owner)
	if errors.Is(err, store.ErrNotFound) {
		return none, nil
	}
	if err != nil {
		return none, err
	}
	if u.DisabledAt != nil || u.Email == "" {
		return none, nil
	}
	emails, err := st.VerifiedEmails(ctx, owner)
	if err != nil {
		return none, err
	}
	if !slices.Contains(emails, strings.ToLower(u.Email)) {
		return none, nil
	}
	return hubproto.GitAuthor{Name: u.Name, Email: u.Email}, nil
}
