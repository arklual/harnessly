package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// A page shown by path runs on an opaque origin, where localStorage throws. The bridge gives it one of its own, kept
// here: read once, synchronously, before the page's scripts run (GET), written back in batches (POST, a beacon on the
// way out). One file per person and target, so a dev server's page finds its storage again after a reload or a new
// open, and another person's never.

const (
	previewStoragePath = "/__tk/storage"
	// previewStorageMax is what a browser gives an origin's localStorage.
	previewStorageMax = 5 << 20
)

var previewStorageMu sync.Mutex

func previewStorageFile(dir string, sess *previewSession) string {
	sum := sha256.Sum256([]byte(sess.sub + "\x00" + sess.target.Kind + "\x00" + strconv.Itoa(sess.target.Port) + "\x00" + sess.target.Origin))
	return filepath.Join(dir, hex.EncodeToString(sum[:16])+".json")
}

func servePreviewStorage(w http.ResponseWriter, r *http.Request, dir string, sess *previewSession) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Type", "application/json")
	pathSlotHeaders(h)
	file := previewStorageFile(dir, sess)
	switch r.Method {
	case http.MethodGet:
		previewStorageMu.Lock()
		raw, err := os.ReadFile(file)
		previewStorageMu.Unlock()
		if err != nil || !json.Valid(raw) {
			raw = []byte("{}")
		}
		_, _ = w.Write(raw)
	case http.MethodPost:
		raw, err := io.ReadAll(io.LimitReader(r.Body, previewStorageMax+1))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(raw) > previewStorageMax {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var items map[string]string
		if json.Unmarshal(raw, &items) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		clean, _ := json.Marshal(items)
		previewStorageMu.Lock()
		defer previewStorageMu.Unlock()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		tmp := file + ".tmp"
		if os.WriteFile(tmp, clean, 0o600) != nil || os.Rename(tmp, file) != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
