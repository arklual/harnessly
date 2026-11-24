package designaccess

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/jpeg"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStaticGuestReadsOnlyThePublishedPages(t *testing.T) {
	r := newSpaceRig(t, true)
	ctx := context.Background()
	s := r.svc
	s.staticGuests = true
	project := "two-pages"
	if _, err := s.registerCreated(ctx, r.owner, project, r.ownerU.Email); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	marked := append(buf.Bytes(), []byte("unpublished suffix")...)
	img, err := cleanStaticPage(marked)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := base64.StdEncoding.DecodeString(img); bytes.Contains(raw, []byte("unpublished suffix")) {
		t.Fatal("extra bytes kept")
	}
	if _, err = cleanStaticPage([]byte("<svg></svg>")); err == nil {
		t.Fatal("active content accepted")
	}
	pages := []staticPage{{"one", "<img src=private>", img}, {"two", "Second", img}}
	var token, id string
	err = s.write(ctx, r.owner, func(tx *txn) error {
		row, value, e := tx.createShare(project, r.ownerU.Email, shareInput{Static: true, Rights: roleView, Version: "pinned", Rev: strings.Repeat("a", 40), TTL: ttl{step: time.Hour}, Pages: []string{"one", "two"}})
		if e != nil {
			return e
		}
		token, id = value, row.ID
		return tx.saveStatic(row, pages)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !isStaticToken(token) || shareTokenRe.MatchString(token) {
		t.Fatal("dynamic and static token domains overlap")
	}
	check := func(method, path string, want int) string {
		t.Helper()
		w := httptest.NewRecorder()
		s.StaticGuest(w, httptest.NewRequest(method, path, nil))
		if w.Code != want {
			t.Fatalf("%s %s got %d want %d", method, path, w.Code, want)
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("cacheable")
		}
		return w.Body.String()
	}
	path := StaticPrefix + token
	body := check("GET", path, 200)
	if !strings.Contains(body, "data:image/jpeg;base64,") || !strings.Contains(body, "?page=1") || strings.Contains(body, "<img src=private>") {
		t.Fatal("unsafe or incomplete page")
	}
	check("GET", path+"?page=1", 200)
	if body := check("HEAD", path, 200); body != "" {
		t.Fatal("HEAD returned body")
	}
	check("GET", path+"?page=2", 404)
	check("GET", path+"/files", 404)
	check("POST", path, 404)
	var next string
	if err = s.write(ctx, r.owner, func(tx *txn) error { var e error; next, e = tx.reissueShare(project, r.ownerU.Email, id); return e }); err != nil {
		t.Fatal(err)
	}
	check("GET", path, 404)
	if !isStaticToken(next) {
		t.Fatal("rotation lost static scope")
	}
	path = StaticPrefix + next
	check("GET", path, 200)
	if err = s.write(ctx, r.owner, func(tx *txn) error { return tx.revokeShare(project, r.ownerU.Email, id) }); err != nil {
		t.Fatal(err)
	}
	check("GET", path+"?page=1", 404)
}
