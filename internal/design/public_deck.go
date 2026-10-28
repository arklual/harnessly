package design

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// What a guest link shows of a deck: the index says the project is a deck and what the owner published
// (a recording with its chapters, a PDF), and `__media/<file>` serves that published file with Range, so a player
// can seek. Only the two published files are served, never the recordings or the exports behind them.

const (
	// PublicMediaDir is the first segment of a public path that is a published media file.
	PublicMediaDir = "__media"
	// PublicMediaLimit is the largest published file the public route sends.
	PublicMediaLimit = 2 << 30
	// publicKindSlides is what the index says of a deck.
	publicKindSlides  = "slides"
	publicChaptersMax = 64 << 10
)

// publicMedia is the published files a guest may read, by the name in the path, and their types.
var publicMedia = map[string]string{
	"video.mp4": "video/mp4",
	"deck.pdf":  "application/pdf",
}

// publicVideo is the published recording as the guest page needs it.
type publicVideo struct {
	Chapters []DeckChapter `json:"chapters"`
}

// publicDeck is the published part of a deck: the recording (null when there is none) and whether a PDF is there.
type publicDeck struct {
	Video *publicVideo `json:"video"`
	PDF   bool         `json:"pdf"`
}

// publicKind is the index's `kind`: a project that keeps deck settings is a deck. Everything else is null, which the
// guest page reads as an ordinary design.
func (a *API) publicKind(lib *Library, project string) *string {
	if lib.IsDeck(project) {
		k := publicKindSlides
		return &k
	}
	return nil
}

// publicDeckOf is the index's `deck`. A link on a snapshot or on some of the pages gets none: the published files
// are those of the whole latest deck, so they are not what such a link shows.
func (a *API) publicDeckOf(lib *Library, project string, scope publicScope, kind *string) *publicDeck {
	if kind == nil || scope.rev != "" || scope.limited {
		return nil
	}
	d := &publicDeck{}
	if f, _, ok := a.openMedia(lib, project, "video.mp4"); ok {
		f.Close()
		d.Video = &publicVideo{Chapters: a.publicChapters(lib, project)}
	}
	if f, _, ok := a.openMedia(lib, project, "deck.pdf"); ok {
		f.Close()
		d.PDF = true
	}
	return d
}

// publicChapters reads chapters.json; a missing or broken file is a video without chapters.
func (a *API) publicChapters(lib *Library, project string) []DeckChapter {
	out := []DeckChapter{}
	f, st, ok := openRegular(filepath.Join(lib.ArtifactDir(project), filepath.FromSlash(DeckPublishedDir), "chapters.json"))
	if !ok {
		return out
	}
	defer f.Close()
	if st.Size() > publicChaptersMax {
		return out
	}
	raw, err := readPublicFile(f, publicChaptersMax)
	if err != nil {
		return out
	}
	var read []DeckChapter
	if json.Unmarshal(raw, &read) != nil {
		return out
	}
	for _, c := range read {
		if c.Page != "" && c.T >= 0 {
			out = append(out, c)
		}
	}
	return out
}

// openMedia opens one published file through a confined root, as openPublic does for the project's files.
func (a *API) openMedia(lib *Library, project, name string) (*os.File, os.FileInfo, bool) {
	library := lib.Root()
	if library == "" {
		return nil, nil, false
	}
	root, err := os.OpenRoot(library)
	if err != nil {
		return nil, nil, false
	}
	defer root.Close()
	f, err := root.Open(filepath.Join(project, filepath.FromSlash(DeckPublishedDir), name))
	if err != nil {
		return nil, nil, false
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > PublicMediaLimit {
		f.Close()
		return nil, nil, false
	}
	return f, st, true
}

// publicMediaServe answers `__media/<file>`: the published video or PDF, with Range. A link on a snapshot or on some
// of the pages has no deck, so it has no media either.
func (a *API) publicMediaServe(w http.ResponseWriter, r *http.Request, lib *Library, project string, scope publicScope, segments []string) {
	typ, known := "", false
	if len(segments) == 1 {
		typ, known = publicMedia[segments[0]]
	}
	if !known || scope.rev != "" || scope.limited {
		publicText(w, http.StatusNotFound, "Not found")
		return
	}
	f, st, ok := a.openMedia(lib, project, segments[0])
	if !ok {
		publicText(w, http.StatusNotFound, "Not found")
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Cache-Control", "private, no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, segments[0], st.ModTime().Truncate(time.Second), f)
}
