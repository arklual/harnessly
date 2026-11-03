package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const notionTestID = "1429989f-e8ac-4eff-bc8f-57f56486db54"

func notionServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer ntn_x" || r.Header.Get("Notion-Version") != "2022-06-28" {
				w.WriteHeader(401)
				secWrite(w, `{"object":"error","status":401,"code":"unauthorized"}`)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/v1/users/me", guard(func(w http.ResponseWriter, r *http.Request) {
		secWrite(w, `{"object":"user","id":"b1","name":"Harnessly bot","avatar_url":null,"type":"bot","bot":{"workspace_name":"Acme"}}`)
	}))
	mux.HandleFunc("/v1/search", guard(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if r.Method != "POST" || b["query"] != "roadmap" || b["page_size"] != float64(25) {
			w.WriteHeader(400)
			return
		}
		secWrite(w, `{"object":"list","results":[{"object":"page","id":"`+notionTestID+`","last_edited_time":"2026-10-02T10:00:00.000Z","url":"https://www.notion.so/Roadmap-1429989fe8ac4effbc8f57f56486db54","parent":{"type":"workspace"},
		"properties":{"title":{"id":"title","type":"title","title":[{"plain_text":"Road"},{"plain_text":"map"}]}}}],"has_more":false}`)
	}))
	mux.HandleFunc("/v1/pages/"+notionTestID, guard(func(w http.ResponseWriter, r *http.Request) {
		secWrite(w, `{"object":"page","id":"`+notionTestID+`","last_edited_time":"2026-10-02T10:00:00.000Z","url":"https://www.notion.so/Roadmap-1429989fe8ac4effbc8f57f56486db54","properties":{"Name":{"type":"title","title":[{"plain_text":"Roadmap"}]}}}`)
	}))
	mux.HandleFunc("/v1/blocks/"+notionTestID+"/children", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start_cursor") == "c2" {
			secWrite(w, `{"results":[{"id":"b4","type":"to_do","has_children":false,"to_do":{"rich_text":[{"plain_text":"ship"}],"checked":true}},{"id":"b5","type":"divider","divider":{}}],"has_more":false}`)
			return
		}
		secWrite(w, `{"results":[
		{"id":"b1","type":"heading_1","has_children":false,"heading_1":{"rich_text":[{"plain_text":"Plan"}]}},
		{"id":"b2","type":"paragraph","has_children":false,"paragraph":{"rich_text":[{"plain_text":"Two "},{"plain_text":"steps"}]}},
		{"id":"b3","type":"bulleted_list_item","has_children":true,"bulleted_list_item":{"rich_text":[{"plain_text":"first"}]}}],"has_more":true,"next_cursor":"c2"}`)
	}))
	mux.HandleFunc("/v1/blocks/b3/children", guard(func(w http.ResponseWriter, r *http.Request) {
		secWrite(w, `{"results":[{"id":"b6","type":"numbered_list_item","has_children":false,"numbered_list_item":{"rich_text":[{"plain_text":"sub"}]}}],"has_more":false}`)
	}))
	return httptest.NewServer(mux)
}

func TestNotionDetect(t *testing.T) {
	p := &notionProvider{}
	for _, raw := range []string{"https://www.notion.so/acme/Roadmap-1429989fe8ac4effbc8f57f56486db54", "https://acme.notion.site/Doc-1429989fe8ac4effbc8f57f56486db54"} {
		u, _ := url.Parse(raw)
		d, err := p.Detect(context.Background(), nil, u)
		if err != nil || d == nil || d.Kind != Notion || d.APIURL != "https://api.notion.com" || d.Methods[0].TokenURL != "https://www.notion.so/my-integrations" || d.Capabilities[0] != Pages {
			t.Fatalf("%s: %+v %v", raw, d, err)
		}
	}
	u, _ := url.Parse("https://example.com/x")
	if d, _ := p.Detect(context.Background(), nil, u); d != nil {
		t.Fatal("claimed foreign host")
	}
}

func TestNotionClient(t *testing.T) {
	srv := notionServer(t)
	defer srv.Close()
	conn := Conn{Kind: Notion, BaseURL: "https://www.notion.so", APIURL: "https://api.notion.com", Method: "api-key"}
	p := &notionProvider{}
	h := secRewriteHTTP(t, srv)
	cl := p.Open(conn, Credentials{FieldToken: "ntn_x"}, h).(*notionClient)
	ctx := context.Background()
	me, err := cl.Me(ctx)
	if err != nil || me.Name != "Harnessly bot" || me.Login != "Acme" {
		t.Fatalf("%+v %v", me, err)
	}
	if _, err := p.Open(conn, Credentials{FieldToken: "bad"}, h).Me(ctx); !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
	pages, err := cl.SearchPages(ctx, Query{Text: "roadmap"})
	if err != nil || len(pages) != 1 || pages[0].Title != "Roadmap" || pages[0].ID != notionTestID || pages[0].Updated.IsZero() {
		t.Fatalf("%+v %v", pages, err)
	}
	pg, err := cl.Page(ctx, "1429989fe8ac4effbc8f57f56486db54")
	want := "# Plan\nTwo steps\n- first\n  1. sub\n- [x] ship"
	if err != nil || pg.Title != "Roadmap" || pg.Body != want {
		t.Fatalf("%+v %v", pg, err)
	}
	if _, err := cl.Page(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if _, err := cl.Page(ctx, "ffffffffffffffffffffffffffffffff"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestNotionLink(t *testing.T) {
	p := &notionProvider{}
	for raw, want := range map[string]string{
		"https://www.notion.so/acme/Roadmap-1429989fe8ac4effbc8f57f56486db54?pvs=4": notionTestID,
		"https://acme.notion.site/1429989fe8ac4effbc8f57f56486db54":                 notionTestID,
	} {
		u, _ := url.Parse(raw)
		if got, ok := p.Link(Conn{}, u); !ok || got.Kind != "page" || got.Key != want {
			t.Fatalf("%s: %+v %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"https://www.notion.so/acme", "https://example.com/1429989fe8ac4effbc8f57f56486db54", "https://www.notion.so/Roadmap1429989fe8ac4effbc8f57f56486db54x"} {
		u, _ := url.Parse(raw)
		if _, ok := p.Link(Conn{}, u); ok {
			t.Fatalf("matched %s", raw)
		}
	}
}
