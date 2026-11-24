package design

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func rasterPageKey(path string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(path))) }

// pinnedRaster is called once by the publisher on the author's machine. The
// short-lived server renders the extracted revision, including its own assets.
func (a *API) pinnedRaster(w http.ResponseWriter, r *http.Request, lib *Library, project string, board Board) {
	bin := a.L.findChrome()
	if bin == "" {
		publicText(w, 503, "Unavailable")
		return
	}
	dir, err := os.MkdirTemp("", "harnessly-snapshot-")
	if err != nil {
		publicText(w, 503, "Unavailable")
		return
	}
	defer os.RemoveAll(dir)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		publicText(w, 503, "Unavailable")
		return
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { lib.serveLibrary(w, r, port) }), ReadHeaderTimeout: 5 * time.Second}
	defer srv.Close()
	go func() { _ = srv.Serve(ln) }()
	target := filepath.Join(dir, "page.jpg")
	slots := a.L.thumbs()
	slots.takeSlot(dir, true)
	defer slots.freeSlot()
	if r.Context().Err() != nil || !a.L.drawThumbAt(project, board, target, "http://127.0.0.1:"+strconv.Itoa(port), bin, true) {
		publicText(w, 503, "Unavailable")
		return
	}
	f, err := os.Open(target)
	if err != nil {
		publicText(w, 503, "Unavailable")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 || st.Size() > 2<<20 {
		publicText(w, 413, "Page too large")
		return
	}
	w.Header().Set("X-Design-Static-Page", rasterPageKey(board.Path))
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, f)
}
