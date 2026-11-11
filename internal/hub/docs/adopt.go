package docs

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Placing documents in a space by hand. A document or a board that nothing ties to an organization (the ones the
// start of the hub counts: no record with a space, no machine, no author the hub can tell) stays closed to everybody
// but its author and the administrators. An administrator of the installation places such documents in a space by
// listing their authors. The hub places nothing on its own guess: an author's name is something somebody typed, so
// that it equals the account address of a member proves nothing, and is only said in the report as a hint. The
// answer without apply is the same report, with nothing written.

type adoptRequest struct {
	SpaceID string `json:"spaceId"`
	// Authors are authors as the documents and boards services name them (an address, a display name, "" for a
	// document that names nobody): their documents are placed, on the administrator's word.
	Authors []string `json:"authors"`
	Apply   bool     `json:"apply"`
}

type adoptedDoc struct {
	Slug   string `json:"slug"`
	Kind   string `json:"kind"`
	Author string `json:"author"`
}

type leftAuthor struct {
	Author string `json:"author"`
	Count  int    `json:"count"`
	// Member says the author goes by the account address of a member of the space. The address is not proved to be
	// theirs: it is a hint for the administrator, who lists the author to place the documents.
	Member bool `json:"member"`
}

type adoptReport struct {
	SpaceID string `json:"spaceId"`
	// Applied says the documents were placed; false is a dry run.
	Applied bool         `json:"applied"`
	Adopted []adoptedDoc `json:"adopted"`
	// Left is the documents that still belong to no organization, counted by the author they name.
	Left      []leftAuthor `json:"left"`
	LeftCount int          `json:"leftCount"`
}

func (s *Service) adoptRoute(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	if !p.Admin {
		return httpx.ErrNotFound
	}
	var req adoptRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.SpaceID) == "" {
		return httpx.Invalid("spaceId", "the space the documents are placed in")
	}
	rep, err := s.adopt(r.Context(), strings.TrimSpace(req.SpaceID), req.Authors, req.Apply)
	if err != nil {
		return err
	}
	if rep.Applied && s.auth != nil {
		s.auth.Audit(r.Context(), p.User.ID, "docs_adopted", r, rep.SpaceID+" "+strings.Join(slugsOf(rep.Adopted, 40), " "))
	}
	httpx.WriteJSON(w, http.StatusOK, rep)
	return nil
}

func slugsOf(list []adoptedDoc, max int) []string {
	out := make([]string, 0, len(list))
	for _, d := range list {
		if len(out) == max {
			break
		}
		out = append(out, d.Slug)
	}
	return out
}

// adopt reports which documents that belong to no organization the listed authors made, and with apply places them
// in the space. A placed document gets a record in the space; one the hub had no record of keeps what it gave before to the
// people of its organization (they may read it) and still has no owner. A record that is already in a space is never
// moved.
func (s *Service) adopt(ctx context.Context, spaceID string, authors []string, apply bool) (adoptReport, error) {
	rep := adoptReport{SpaceID: spaceID, Adopted: []adoptedDoc{}, Left: []leftAuthor{}}
	st := s.db.S()
	if _, err := st.SpaceByID(ctx, spaceID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return rep, httpx.ErrNotFound
		}
		return rep, err
	}
	members, err := st.Members(ctx, spaceID)
	if err != nil {
		return rep, err
	}
	norm := func(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
	member, listed := map[string]bool{}, map[string]bool{}
	for _, m := range members {
		member[norm(m.Email)] = true
	}
	for _, a := range authors {
		listed[norm(a)] = true
	}

	type item struct{ slug, kind, title, author string }
	var items []item
	arts, err := s.art.list(ctx)
	if err != nil {
		return rep, err
	}
	for _, a := range arts {
		items = append(items, item{a.Slug, "artifact", a.Title, a.Author})
	}
	bs, err := s.boards(ctx)
	if err != nil {
		return rep, err
	}
	for _, b := range bs {
		items = append(items, item{b.Slug, "board", b.Title, b.Author})
	}
	slugs := make([]string, len(items))
	for i, it := range items {
		slugs[i] = it.slug
	}
	metas, _, err := s.metas(ctx, slugs, reachAll())
	if err != nil {
		return rep, err
	}
	c := s.circleOf(nil)
	left := map[string]int{}
	var take []item
	for _, it := range items {
		var mp *meta
		if m, ok := metas[it.slug]; ok {
			mp = &m
		}
		if len(c.home(ctx, mp, it.author)) > 0 {
			continue
		}
		who := norm(it.author)
		if mp != nil && mp.OwnerEmail != "" {
			who = norm(mp.OwnerEmail)
		}
		if !listed[who] && !listed[norm(it.author)] {
			left[who]++
			rep.LeftCount++
			continue
		}
		take = append(take, it)
		rep.Adopted = append(rep.Adopted, adoptedDoc{Slug: it.slug, Kind: it.kind, Author: it.author})
	}
	for a, n := range left {
		rep.Left = append(rep.Left, leftAuthor{Author: a, Count: n, Member: a != "" && member[a]})
	}
	sort.Slice(rep.Left, func(i, j int) bool {
		if rep.Left[i].Count != rep.Left[j].Count {
			return rep.Left[i].Count > rep.Left[j].Count
		}
		return rep.Left[i].Author < rep.Left[j].Author
	})
	if !apply || len(take) == 0 {
		return rep, nil
	}
	now := s.now().UnixMilli()
	err = s.db.Tx(ctx, func(tx store.Store) error {
		q := tx.Q()
		for _, it := range take {
			if _, err := q.Exec(ctx, `INSERT INTO doc_meta (slug, space_id, title, team_level, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (slug) DO UPDATE SET space_id = excluded.space_id, updated_at = excluded.updated_at
				WHERE doc_meta.space_id IS NULL`, it.slug, spaceID, it.title, levelView, now, now); err != nil {
				return err
			}
			if err := resaccess.AdoptSpace(ctx, q, resaccess.Ref{Kind: resaccess.KindDoc, ID: it.slug, Space: spaceID}); err != nil {
				return err
			}
		}
		return nil
	})
	rep.Applied = err == nil
	return rep, err
}
