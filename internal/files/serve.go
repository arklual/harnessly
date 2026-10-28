package files

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// WriteJSON writes a JSON answer as the old runtime did: its content type, ETag and length, 304 to a caller that
// already holds it.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, v Object) {
	body := AppendJSON(nil, v)
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	etag := ETag(body)
	h.Set("ETag", etag)
	if Fresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeBytes is a whole in-memory body (a shown document), with its ETag and 304 like WriteJSON.
func writeBytes(w http.ResponseWriter, r *http.Request, body []byte) {
	etag := ETag(body)
	w.Header().Set("ETag", etag)
	if Fresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// WriteUnauthorized is the old API's refusal of a caller with neither a page session nor the cli-token.
func WriteUnauthorized(w http.ResponseWriter) {
	body := `{"error":"Unauthorized"}`
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, body)
}

// sendFile sends status and length bytes of the file from start; a file that cannot be opened ends the connection
// before anything is sent, as the old runtime's broken stream did.
func sendFile(w http.ResponseWriter, status int, path string, start, length int64) {
	f, err := os.Open(path)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	defer f.Close()
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
	w.WriteHeader(status)
	if length > 0 {
		_, _ = io.Copy(w, io.LimitReader(f, length))
	}
}

// ServePreview answers a GET of the viewer: a file shown in place (a video in ranges), or with download=1 any
// allowed file handed over to be saved. The caller has a page session.
func (a *Access) ServePreview(w http.ResponseWriter, r *http.Request, q Query) {
	w.Header().Set("Cache-Control", "no-store")
	requested, _ := q.String("path")
	if z, _ := q.String("zip"); z == "list" || z == "entry" {
		a.ServeZip(w, r, q)
		return
	}
	if d, _ := q.String("download"); d == "1" {
		t := a.Download(requested)
		if !t.OK {
			WriteJSON(w, r, t.Status, Error(t.Text))
			return
		}
		name := BaseName(t.Real)
		h := w.Header()
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Length", strconv.FormatInt(t.Size, 10))
		h.Set("Content-Disposition", `attachment; filename="`+ASCIIName(name)+`"; filename*=UTF-8''`+EncodeURIComponent(name))
		h.Set("X-Content-Type-Options", "nosniff")
		sendFile(w, http.StatusOK, t.Real, 0, t.Size)
		return
	}
	t := a.Preview(requested)
	if !t.OK {
		WriteJSON(w, r, t.Status, Error(t.Text))
		return
	}
	contentType, video := PreviewType(t.Ext)
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	if video {
		h.Set("Accept-Ranges", "bytes")
		header := r.Header.Get("Range")
		start, end, ok := PreviewRange(header, t.Size)
		if header != "" && !ok {
			h.Set("Content-Range", "bytes */"+strconv.FormatInt(t.Size, 10))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		status := http.StatusOK
		if !ok {
			start, end = 0, t.Size-1
		} else {
			status = http.StatusPartialContent
			h.Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(t.Size, 10))
		}
		h.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		if t.Size == 0 {
			w.WriteHeader(status)
			return
		}
		sendFile(w, status, t.Real, start, end-start+1)
		return
	}
	if t.Ext != "pdf" {
		h.Set("Content-Security-Policy", PreviewCSP)
	}
	body, err := os.ReadFile(t.Real)
	if err != nil {
		// The old runtime failed such a read as a server error, the headers set so far kept.
		h.Set("Content-Type", "text/plain")
		h.Set("Content-Length", "21")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "Internal Server Error")
		return
	}
	writeBytes(w, r, body)
}

// ServeUpload answers a GET of an uploaded attachment: the file the segments name under dir, in ranges for media.
func ServeUpload(w http.ResponseWriter, r *http.Request, dir string, segments []string) {
	file, ok := UploadPath(dir, segments)
	if !ok {
		WriteJSON(w, r, http.StatusForbidden, Error("Path not allowed"))
		return
	}
	mime := UploadType(file)
	if mime == "" {
		WriteJSON(w, r, http.StatusUnsupportedMediaType, Error("Unsupported file type"))
		return
	}
	// Unlike the previous runtime, a link is followed only as far as the uploads folder.
	if real, err := filepath.EvalSymlinks(file); err == nil {
		if root, err := filepath.EvalSymlinks(dir); err != nil || real == root || !within(root, real) {
			WriteJSON(w, r, http.StatusForbidden, Error("Path not allowed"))
			return
		}
	}
	st, err := os.Stat(file)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			WriteJSON(w, r, http.StatusNotFound, Error("Not found"))
			return
		}
		WriteJSON(w, r, http.StatusInternalServerError, Error("Failed to serve upload"))
		return
	}
	if !st.Mode().IsRegular() {
		WriteJSON(w, r, http.StatusNotFound, Error("Not found"))
		return
	}
	size := st.Size()
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("Accept-Ranges", "bytes")
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	start, end, present, satisfiable := UploadRange(r.Header.Get("Range"), size)
	if present && !satisfiable {
		h.Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if present {
		h.Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(size, 10))
		h.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		sendFile(w, http.StatusPartialContent, file, start, end-start+1)
		return
	}
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	sendFile(w, http.StatusOK, file, 0, size)
}

// NodeTempDir is the temporary folder the old runtime counted as one of the viewer's places: TMPDIR, TMP or TEMP,
// else /tmp, without a trailing slash.
func NodeTempDir() string {
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		if v := os.Getenv(k); v != "" {
			if len(v) > 1 {
				v = strings.TrimSuffix(v, "/")
			}
			return v
		}
	}
	return "/tmp"
}
