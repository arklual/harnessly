package docs

import (
	"context"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A document belongs to the space its record names. One whose record names none (it was published before records had
// a space, and nothing told which) belongs where what the hub does know puts it: the space of the machine that
// published it, and when there is none, the spaces of its owner or of the author the documents service names. The team level of a document reaches only the people of those organizations; a share by name and a
// guest link reach whoever they name.

// circle answers, for one signed-in person and one request, whether a document is of their organizations. It keeps
// what it read.
type circle struct {
	s     *Service
	p     *auth.Principal
	mine  map[string]bool
	homes map[string]map[string]bool
	// addrs is the addresses the person has proved; read is whether they were read.
	addrs []string
	read  bool
	// orgs is how many organizations the installation has; 0 until counted, -1 when unknown.
	orgs int
}

func (s *Service) circleOf(p *auth.Principal) *circle {
	return &circle{s: s, p: p, homes: map[string]map[string]bool{}}
}

// proved is the addresses the person has proved: what is addressed to an address reaches only them.
func (c *circle) proved(ctx context.Context) []string {
	if !c.read && c.p != nil {
		c.read = true
		list, err := c.s.db.S().VerifiedEmails(ctx, c.p.User.ID)
		if err != nil {
			c.s.log.Warn("addresses of a person not read", "error", err)
		}
		c.addrs = list
	}
	return c.addrs
}

func (c *circle) hasProved(ctx context.Context, email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, v := range c.proved(ctx) {
		if v == email {
			return true
		}
	}
	return false
}

// orgKey is the organization a space is a tenant of, or the space itself when it has none.
func orgKey(sp store.Space) string {
	if sp.OrgID != "" {
		return sp.OrgID
	}
	return "space:" + sp.ID
}

func (c *circle) ofUser(ctx context.Context, userID string) map[string]bool {
	out := map[string]bool{}
	if userID == "" {
		return out
	}
	spaces, err := c.s.db.S().SpacesByUser(ctx, userID)
	if err != nil {
		c.s.log.Warn("spaces of a person not read", "error", err)
		return out
	}
	for _, sp := range spaces {
		out[orgKey(sp.Space)] = true
	}
	return out
}

// ofOwner is the organizations of the person with this address; none for an address the hub does not know (a board's
// author is often a display name). The owner of a recorded document is the account that published it. typed is for an
// address somebody typed (the author the documents service names): it stands for the one person who has proved it,
// and for nobody while no one has.
func (c *circle) ofOwner(ctx context.Context, email string, typed bool) map[string]bool {
	owner := strings.ToLower(strings.TrimSpace(email))
	if owner == "" {
		return nil
	}
	key := "o:" + owner
	if typed {
		key = "t:" + owner
	}
	if h, ok := c.homes[key]; ok {
		return h
	}
	h := map[string]bool{}
	if typed {
		if id, err := c.s.db.S().UserIDByVerifiedEmail(ctx, owner); err == nil && id != "" {
			h = c.ofUser(ctx, id)
		}
	} else if u, err := c.s.db.S().UserByEmail(ctx, owner); err == nil {
		h = c.ofUser(ctx, u.ID)
	}
	c.homes[key] = h
	return h
}

// home is the organizations a document belongs to. A document the hub has a record of belongs to the machine's, or
// else to its owner's; one it has no record of (m is nil: a board, a document published straight to the documents
// service) to those of the author the service names.
func (c *circle) home(ctx context.Context, m *meta, author string) map[string]bool {
	if m == nil {
		return c.ofOwner(ctx, author, true)
	}
	if m.SpaceID != "" {
		if h, ok := c.homes["s:"+m.SpaceID]; ok {
			return h
		}
		// A space that is gone keeps its key, which nobody is a member of.
		h := map[string]bool{"space:" + m.SpaceID: true}
		if sp, err := c.s.db.S().SpaceByID(ctx, m.SpaceID); err == nil {
			h = map[string]bool{orgKey(sp): true}
		}
		c.homes["s:"+m.SpaceID] = h
		return h
	}
	if m.MachineID != "" {
		if h, ok := c.homes["m:"+m.MachineID]; ok {
			return h
		}
		if mc, err := c.s.db.S().MachineByID(ctx, m.MachineID); err == nil && mc.SpaceID != "" {
			if sp, err := c.s.db.S().SpaceByID(ctx, mc.SpaceID); err == nil {
				h := map[string]bool{orgKey(sp): true}
				c.homes["m:"+m.MachineID] = h
				return h
			}
		}
	}
	return c.ofOwner(ctx, m.OwnerEmail, false)
}

// single reports whether the installation is one organization (or one space without any): there a document that
// nothing ties to an organization can only be that organization's.
func (c *circle) single(ctx context.Context) bool {
	if c.orgs == 0 {
		c.orgs = -1
		var n int
		err := c.s.db.S().Q().QueryRow(ctx, `SELECT COUNT(DISTINCT COALESCE(NULLIF(org_id, ''), 'space:' || id)) FROM spaces`).Scan(&n)
		if err != nil {
			c.s.log.Warn("organizations not counted", "error", err)
		} else if n > 0 {
			c.orgs = n
		}
	}
	return c.orgs == 1
}

// holds reports whether the document is of an organization the person is a member of. A document that nothing ties to
// an organization is everybody's where there is one organization, and nobody's but its author's where there are more.
func (c *circle) holds(ctx context.Context, m *meta, author string) bool {
	// The author the documents service names is an address somebody typed: it is the person's only when they have
	// proved it.
	if m == nil && author != "" && c.hasProved(ctx, author) {
		return true
	}
	if c.mine == nil {
		c.mine = c.ofUser(ctx, c.p.User.ID)
	}
	if len(c.mine) == 0 {
		return false
	}
	home := c.home(ctx, m, author)
	if len(home) == 0 {
		return c.single(ctx)
	}
	for k := range home {
		if c.mine[k] {
			return true
		}
	}
	return false
}

// reportLoose says once, at the start, how many documents of an installation with several organizations nothing ties
// to one: they are closed to everybody but their author and the administrators until somebody shares them by name.
func (s *Service) reportLoose(ctx context.Context) {
	if !s.Status().Documents {
		return
	}
	c := s.circleOf(nil)
	if c.single(ctx) || c.orgs < 0 {
		return
	}
	type item struct{ slug, author string }
	var items []item
	arts, err := s.art.list(ctx)
	if err != nil {
		s.log.Warn("documents not listed at the start", "error", err)
		return
	}
	for _, a := range arts {
		items = append(items, item{a.Slug, a.Author})
	}
	if bs, err := s.boards(ctx); err == nil {
		for _, b := range bs {
			items = append(items, item{b.Slug, b.Author})
		}
	}
	slugs := make([]string, len(items))
	for i, it := range items {
		slugs[i] = it.slug
	}
	metas, _, err := s.metas(ctx, slugs, reachAll())
	if err != nil {
		s.log.Warn("documents not read at the start", "error", err)
		return
	}
	var loose []string
	for _, it := range items {
		var mp *meta
		if m, ok := metas[it.slug]; ok {
			mp = &m
		}
		if len(c.home(ctx, mp, it.author)) == 0 {
			loose = append(loose, it.slug)
		}
	}
	if len(loose) == 0 {
		return
	}
	shown := loose
	if len(shown) > 50 {
		shown = shown[:50]
	}
	s.log.Warn("documents and boards that belong to no organization are closed to everybody but their author and the administrators: share them by name",
		"count", len(loose), "slugs", strings.Join(shown, " "))
}
