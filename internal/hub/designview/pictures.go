package designview

import (
	"context"
	"io"
	"strings"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// The pictures the store keeps apart from the design's revisions: the cover of a project (covers, `<id>.jpg`)
// and the gallery picture of each board (thumbs, `<id>/<key>.jpg`). Each is one file read by its path; the covers are
// also listed whole, one per project, for the library's cards.

// PictureRoom is a collection of pictures as the hub reads it (filestore.Store).
type PictureRoom interface {
	Current(ctx context.Context, path string) (*filestore.Entry, error)
	Changes(ctx context.Context, since int64, limit int) (filestore.Changes, error)
	OpenBlob(ctx context.Context, sha string) (io.ReadCloser, error)
}

// PictureRooms opens a space's collection of pictures.
type PictureRooms func(ctx context.Context, spaceID string) (PictureRoom, error)

// picture is the file at path as the store has it now, at most limit bytes, with its sha; nil when there is none.
func picture(ctx context.Context, rooms PictureRooms, spaceID, path string, limit int64) ([]byte, string, error) {
	if rooms == nil || spaceID == "" || strings.ContainsAny(spaceID, "/\\.") {
		return nil, "", nil
	}
	room, err := rooms(ctx, spaceID)
	if err != nil {
		return nil, "", err
	}
	e, err := room.Current(ctx, path)
	if err != nil || e == nil || e.Deleted || e.Size > limit || !filestore.ValidSha(e.Sha) {
		return nil, "", err
	}
	rc, err := room.OpenBlob(ctx, e.Sha)
	if err != nil {
		return nil, "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit))
	return b, e.Sha, err
}

// Cover is a project's cover picture as the store has it, at most limit bytes; nil when there is none.
func (v *View) Cover(ctx context.Context, spaceID, project string, limit int64) ([]byte, error) {
	if !(filestore.Covers{}).Synced(filestore.CoverPath(project)) {
		return nil, nil
	}
	b, _, err := picture(ctx, v.o.Covers, spaceID, filestore.CoverPath(project), limit)
	return b, err
}

// Thumb is the picture of a board as the store has it, at most limit bytes, with its sha; nil when there is none.
func (v *View) Thumb(ctx context.Context, spaceID, project, boardPath string, limit int64) ([]byte, string, error) {
	path := filestore.ThumbPath(project, boardPath)
	if !(filestore.Thumbs{}).Synced(path) {
		return nil, "", nil
	}
	return picture(ctx, v.o.Thumbs, spaceID, path, limit)
}

// coverPage is how many entries of the covers collection one read of its listing takes.
const coverPage = 1000

// CoverVersions are the projects of a space that have a cover in the store, by project id, each with the cover's sha.
func (v *View) CoverVersions(ctx context.Context, spaceID string) (map[string]string, error) {
	if v.o.Covers == nil || spaceID == "" || strings.ContainsAny(spaceID, "/\\.") {
		return nil, nil
	}
	room, err := v.o.Covers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for since := int64(0); ; {
		ch, err := room.Changes(ctx, since, coverPage)
		if err != nil {
			return nil, err
		}
		for _, e := range ch.Entries {
			id, ok := filestore.CoverID(e.Path)
			switch {
			case !ok:
			case e.Deleted || !filestore.ValidSha(e.Sha):
				delete(out, id)
			default:
				out[id] = e.Sha
			}
			since = max(since, e.Rev)
		}
		if !ch.More || len(ch.Entries) == 0 {
			return out, nil
		}
	}
}
