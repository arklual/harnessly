package design

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type deckIndex struct {
	Kind *string `json:"kind"`
	Deck *struct {
		Video *struct {
			Chapters []DeckChapter `json:"chapters"`
		} `json:"video"`
		PDF bool `json:"pdf"`
	} `json:"deck"`
}

func readDeckIndex(t *testing.T, a *API, token string) deckIndex {
	t.Helper()
	w := publicGet(a, "GET", "p1", token)
	if w.Code != 200 {
		t.Fatalf("index: %d", w.Code)
	}
	var got deckIndex
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPublicIndexOfAnOrdinaryDesignHasNoDeck(t *testing.T) {
	a, _ := scopeAPI(t)
	got := readDeckIndex(t, a, scopeToken(t, nil))
	if got.Kind != nil || got.Deck != nil {
		t.Fatalf("kind %v deck %v", got.Kind, got.Deck)
	}
	if w := publicGet(a, "GET", "p1/__media/video.mp4", scopeToken(t, nil)); w.Code != 404 {
		t.Fatalf("media of a design: %d", w.Code)
	}
}

func TestPublicDeckIndexAndMedia(t *testing.T) {
	a, root := scopeAPI(t)
	writeFile(t, filepath.Join(root, "p1", DeckSettingsFile), `{"v":1}`)
	token := scopeToken(t, nil)

	got := readDeckIndex(t, a, token)
	if got.Kind == nil || *got.Kind != "slides" || got.Deck == nil || got.Deck.Video != nil || got.Deck.PDF {
		t.Fatalf("nothing published yet: %+v", got)
	}
	if w := publicGet(a, "GET", "p1/__media/video.mp4", token); w.Code != 404 {
		t.Fatalf("not published: %d", w.Code)
	}

	writeFile(t, filepath.Join(root, "p1", DeckPublishedDir, "video.mp4"), "0123456789")
	writeFile(t, filepath.Join(root, "p1", DeckPublishedDir, "deck.pdf"), "%PDF-1")
	writeFile(t, filepath.Join(root, "p1", DeckPublishedDir, "chapters.json"), `[{"page":"A","title":"One","t":0},{"page":"","title":"x","t":5},{"page":"B","title":"Two","t":4000}]`)
	writeFile(t, filepath.Join(root, "p1", DeckMediaDir, "rec", "r1", "av.webm"), "secret")

	got = readDeckIndex(t, a, token)
	if got.Deck == nil || got.Deck.Video == nil || !got.Deck.PDF || len(got.Deck.Video.Chapters) != 2 || got.Deck.Video.Chapters[1].T != 4000 {
		t.Fatalf("published: %+v", got.Deck)
	}

	w := publicRequest(a, "GET", "p1/__media/video.mp4", token, http.Header{"Range": {"bytes=2-5"}})
	if w.Code != 206 || w.Body.String() != "2345" || w.Header().Get("Content-Range") != "bytes 2-5/10" || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("range: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	w = publicRequest(a, "GET", "p1/__media/video.mp4", token, http.Header{"Range": {"bytes=50-60"}})
	if w.Code != 416 {
		t.Fatalf("out of range: %d", w.Code)
	}
	if w := publicGet(a, "HEAD", "p1/__media/deck.pdf", token); w.Code != 200 || w.Header().Get("Content-Length") != "6" {
		t.Fatalf("pdf head: %d %v", w.Code, w.Header())
	}
	for _, name := range []string{"av.webm", "chapters.json", "../rec/r1/av.webm", "video.mp4/x"} {
		if w := publicGet(a, "GET", "p1/__media/"+name, token); w.Code != 404 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := publicGet(a, "GET", "p1/.media/published/video.mp4", token); w.Code != 404 {
		t.Errorf("hidden path: %d", w.Code)
	}
}

func TestPublicDeckMediaHonoursTheScope(t *testing.T) {
	a, root := scopeAPI(t)
	writeFile(t, filepath.Join(root, "p1", DeckSettingsFile), `{"v":1}`)
	writeFile(t, filepath.Join(root, "p1", DeckPublishedDir, "video.mp4"), "0123456789")
	for name, claims := range map[string]map[string]any{
		"some pages": {"pages": []any{"One.dc.html"}},
		"no pages":   {"pages": []any{}},
	} {
		token := scopeToken(t, claims)
		got := readDeckIndex(t, a, token)
		if got.Kind == nil || got.Deck != nil {
			t.Errorf("%s: kind %v deck %v", name, got.Kind, got.Deck)
		}
		if w := publicGet(a, "GET", "p1/__media/video.mp4", token); w.Code != 404 {
			t.Errorf("%s: media %d", name, w.Code)
		}
	}
	if w := publicGet(a, "GET", "p1/__media/video.mp4", scopeToken(t, nil)); w.Code != 200 || !strings.HasPrefix(w.Body.String(), "0123") {
		t.Errorf("whole link: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p1/__media/video.mp4", "forged.token.x"); w.Code != 401 {
		t.Errorf("forged: %d", w.Code)
	}
}
