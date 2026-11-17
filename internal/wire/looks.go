package wire

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The pictures of the person's own looks (the background of their chats): a page uploads one and keeps its
// hash in the look (the prefs key hx-looks); every device then reads the same picture. The hub keeps them for its
// user in its own folder or bucket, a runtime on its own in its data folder; both serve the same two paths:
//
//	PUT  /api/v2/me/looks          the picture's bytes (png, jpeg, webp, avif; at most MaxLookImage) -> {hash}
//	                               or a live effect's script (starts with LookFxMagic, UTF-8, at most MaxLookFx) -> {hash}
//	GET  /api/v2/me/looks/<hash>   the picture or script, cached for good (a hash names one content)
//
// Only pictures and fx scripts are taken (by their bytes, not by what the request says) and they are served with nosniff
// and a sandbox: neither can ever run as a page. A script runs only where the page puts it: in a sandboxed frame.

// MaxLookImage is the largest picture a look takes; a page shrinks a bigger one before it uploads.
const MaxLookImage = 3 << 20

// LookFxMagic starts every fx script (the same as looks.FxMagic; a test keeps them equal).
const LookFxMagic = "//hx-fx v1\n"

// MaxLookFx is the largest fx script (a live effect's code) a look takes.
const MaxLookFx = 256 << 10

// LookBlobs keeps the pictures by the sha256 of their content (sites.FolderBlobs and sites.S3Blobs fit).
type LookBlobs interface {
	Has(ctx context.Context, hash string) (bool, error)
	Put(ctx context.Context, hash string, r io.Reader, size int64) error
	Open(ctx context.Context, hash string) (io.ReadCloser, int64, error)
}

// LookPath says whether a path under /api/v2 is one of the looks' pictures: "me/looks" (hash empty) or
// "me/looks/<hash>".
func LookPath(v2path string) (hash string, ok bool) {
	p := strings.Trim(v2path, "/")
	if p == "me/looks" {
		return "", true
	}
	rest, ok := strings.CutPrefix(p, "me/looks/")
	if ok && rest == LookFxFrame {
		return rest, true
	}
	if !ok || !validLookHash(rest) {
		return "", false
	}
	return rest, true
}

func validLookHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil && strings.ToLower(h) == h
}

// isFxScript says whether the bytes begin like an fx script (the whole file's UTF-8 is checked on upload).
func isFxScript(head []byte) bool { return bytes.HasPrefix(head, []byte(LookFxMagic)) }

// lookType is the type of the bytes: a picture's, text/plain for an fx script, "" for anything else.
func lookType(head []byte) string {
	if isFxScript(head) {
		return "text/plain; charset=utf-8"
	}
	switch ct := http.DetectContentType(head); ct {
	case "image/png", "image/jpeg", "image/webp":
		return ct
	}
	if len(head) >= 12 && string(head[4:8]) == "ftyp" && (string(head[8:12]) == "avif" || string(head[8:12]) == "avis") {
		return "image/avif"
	}
	return ""
}

// LookFxFrame is the path (under me/looks/) of the document a live effect runs in.
const LookFxFrame = "fx-frame"

// lookFxFrameCSP is the frame's own policy. Served from its own URL, the frame does not inherit the page's policy (a
// srcdoc frame would, and the hub's page allows no inline script). It is a sandbox (an opaque origin: no cookies, no
// storage, no reach into the page) with no network at all; its one script waits for the page to send the effect's
// code and runs it, which is what 'unsafe-eval' allows — inside that sandbox only.
const lookFxFrameCSP = "default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval'; style-src 'unsafe-inline'; " +
	"img-src data: blob:; font-src data:; form-action 'none'; base-uri 'none'; sandbox allow-scripts"

// lookFxFrameHTML is light, as its iframe (.hx-fx): Chrome paints a frame whose colour scheme differs from its
// element's opaque, and a dark page would get a white sheet over the chat.
// lookFxFrameHTML takes the code from its parent only (the hx SDK and the effect, features/looks/fx-frame.ts), once.
const lookFxFrameHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="color-scheme" content="light">` +
	`<style>:root{color-scheme:light}html,body{margin:0;padding:0;background:transparent;overflow:hidden}canvas{display:block}</style>` +
	`</head><body><script>(function(){` +
	`function take(e){if(e.source!==parent||!e.data||e.data.hx!=="code"||typeof e.data.code!=="string")return;` +
	`removeEventListener("message",take);document.documentElement.dataset.layer=e.data.layer==="back"?"back":"front";` +
	`try{(0,eval)(e.data.code)}catch(err){parent.postMessage({hxFx:"err",msg:String(err&&err.stack||err).slice(0,400)},"*")}}` +
	`addEventListener("message",take);parent.postMessage({hxFx:"boot"},"*")})()</script></body></html>`

func serveFxFrame(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", lookFxFrameCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "private, max-age=3600")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, lookFxFrameHTML)
}

// ServeLooks answers the two paths of the looks' pictures from a store, and the live effects' frame.
func ServeLooks(w http.ResponseWriter, r *http.Request, blobs LookBlobs, hash string) {
	if hash == LookFxFrame {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		serveFxFrame(w, r)
		return
	}
	if blobs == nil {
		refuse(w, RefuseNotFound)
		return
	}
	switch {
	case hash == "" && (r.Method == http.MethodPost || r.Method == http.MethodPut):
		putLook(w, r, blobs)
	case hash != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		getLook(w, r, blobs, hash)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func putLook(w http.ResponseWriter, r *http.Request, blobs LookBlobs) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, MaxLookImage+1))
	if err != nil {
		refuse(w, RefuseBadRequest)
		return
	}
	switch {
	case isFxScript(raw):
		if len(raw) > MaxLookFx {
			refuse(w, RefuseAttachmentTooLarge)
			return
		}
		if !utf8.Valid(raw) {
			refuse(w, RefuseAttachmentType)
			return
		}
	case len(raw) > MaxLookImage:
		refuse(w, RefuseAttachmentTooLarge)
		return
	case lookType(raw) == "":
		refuse(w, RefuseAttachmentType)
		return
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	ctx := r.Context()
	if has, err := blobs.Has(ctx, hash); err != nil || !has {
		if err := blobs.Put(ctx, hash, bytes.NewReader(raw), int64(len(raw))); err != nil {
			refuse(w, RefuseInternal)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"hash": hash})
}

func getLook(w http.ResponseWriter, r *http.Request, blobs LookBlobs, hash string) {
	rc, size, err := blobs.Open(r.Context(), hash)
	if err != nil {
		refuse(w, RefuseNotFound)
		return
	}
	defer rc.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(rc, head)
	head = head[:n]
	ct := lookType(head)
	if ct == "" {
		refuse(w, RefuseNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("ETag", `"`+hash+`"`)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(head)
	_, _ = io.Copy(w, rc)
}
