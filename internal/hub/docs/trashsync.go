package docs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// trashSyncTimeout bounds one telling of the documents service, so a route does not hang on a slow service.
const trashSyncTimeout = 10 * time.Second

// tellTrash tells the documents service a document went into the trash or out of it. When the service does not
// confirm, the record stays waiting and the leader's sweep sends the current state again (syncTrash).
// It answers whether the service confirmed.
func (s *Service) tellTrash(ctx context.Context, slug string, trashed bool, by string, stamp int64) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), trashSyncTimeout)
	defer cancel()
	if err := s.sendTrash(ctx, slug, trashed, by); err != nil {
		s.log.Warn("documents service did not confirm the trash, the sweep sends it again", "slug", slug, "trashed", trashed, "error", err)
		return false
	}
	s.trashSynced(ctx, slug, trashed, stamp)
	return true
}

func (s *Service) trashSynced(ctx context.Context, slug string, trashed bool, stamp int64) {
	in := 0
	if trashed {
		in = 1
	}
	if _, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET trash_sync = NULL
		WHERE slug = ? AND trash_sync = ? AND CASE WHEN trashed_at IS NULL THEN 0 ELSE 1 END = ?`, slug, stamp, in); err != nil {
		s.log.Warn("trash confirmation not recorded", "slug", slug, "error", err)
	}
}

// sendTrash posts the trash state of a document to the documents service and checks the state it answers. A document
// the service no longer has needs no telling.
func (s *Service) sendTrash(ctx context.Context, slug string, trashed bool, by string) error {
	if s.art == nil {
		return errors.New("no documents service")
	}
	body := map[string]any{"trashed": trashed}
	if by != "" {
		body["by"] = by
	}
	var out struct {
		Trashed bool `json:"trashed"`
	}
	err := s.art.do(ctx, http.MethodPost, "/api/v1/artifacts/"+url.PathEscape(slug)+"/trash", body, &out)
	var up *upstreamError
	if errors.As(err, &up) && up.Status == http.StatusNotFound {
		// A service without the trash route answers 404 too: only a document it no longer has needs no telling.
		if _, _, gone := s.art.get(ctx, slug); isStatus(gone, http.StatusNotFound) {
			s.log.Warn("documents service has no such document, its trash needs no telling", "slug", slug, "trashed", trashed)
			return nil
		}
		return fmt.Errorf("documents service has no trash route: %w", err)
	}
	if err != nil {
		return err
	}
	if out.Trashed != trashed {
		return fmt.Errorf("documents service answered trashed=%v", out.Trashed)
	}
	return nil
}

// syncTrash sends the documents service the trash state of every document still waiting for its confirmation, at
// start and every minute on the leader, and says in the log what it could not send.
func (s *Service) syncTrash(ctx context.Context) {
	if s.art == nil {
		return
	}
	rows, err := s.db.S().Q().Query(ctx, `SELECT slug, trash_sync, trashed_at, COALESCE(trashed_by, '') FROM doc_meta
		WHERE trash_sync IS NOT NULL AND COALESCE(kind, '') <> ? ORDER BY trash_sync LIMIT 50`, kindBoard)
	if err != nil {
		s.log.Warn("trash waiting for the documents service not read", "error", err)
		return
	}
	type waiting struct {
		slug    string
		stamp   int64
		trashed bool
		by      string
	}
	var all []waiting
	for rows.Next() {
		var w waiting
		var at sql.NullInt64
		if err := rows.Scan(&w.slug, &w.stamp, &at, &w.by); err != nil {
			_ = rows.Close()
			s.log.Warn("trash waiting for the documents service not read", "error", err)
			return
		}
		w.trashed = at.Valid && at.Int64 != 0
		all = append(all, w)
	}
	_ = rows.Close()
	for _, w := range all {
		if ctx.Err() != nil {
			return
		}
		one, cancel := context.WithTimeout(ctx, trashSyncTimeout)
		err := s.sendTrash(one, w.slug, w.trashed, w.by)
		if err == nil {
			s.trashSynced(one, w.slug, w.trashed, w.stamp)
		}
		cancel()
		if err != nil {
			s.log.Warn("documents service still does not confirm the trash", "slug", w.slug, "trashed", w.trashed,
				"waiting", time.Since(time.UnixMilli(w.stamp)).Round(time.Second).String(), "error", err)
		}
	}
}
