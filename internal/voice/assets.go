package voice

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"harnessly/internal/nodejson"
)

// The in-tab Whisper engine's model and ONNX Runtime files, served from the runtime's own origin so the page needs
// neither model host to be reachable. The first request for a file downloads it once, streaming to the page while it
// lands on disk; later requests are read from the disk cache.

// WhisperModel is the only model the page may ask for.
const WhisperModel = "onnx-community/whisper-small"

var (
	modelFile   = regexp.MustCompile(`^(?:[a-z_]+\.(?:json|txt)|onnx/(?:encoder_model|decoder_model_merged)(?:_fp16|_q4|_quantized)?\.onnx(?:_data)?)$`)
	runtimeFile = regexp.MustCompile(`^ort-wasm-simd-threaded\.jsep\.(?:mjs|wasm)$`)
	version     = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

// Asset is one allowed file: where it comes from and where it is kept.
type Asset struct {
	Upstream, CacheKey, ContentType string
}

func contentType(file string) string {
	switch {
	case strings.HasSuffix(file, ".json"):
		return "application/json"
	case strings.HasSuffix(file, ".txt"):
		return "text/plain; charset=utf-8"
	case strings.HasSuffix(file, ".wasm"):
		return "application/wasm"
	case strings.HasSuffix(file, ".mjs"):
		return "text/javascript"
	}
	return "application/octet-stream"
}

// Assets serves the files.
type Assets struct {
	CacheDir string
	// ModelHost and RuntimeHost are the bases of the model files and of the runtime's dist (tests serve their own).
	ModelHost, RuntimeHost string
	Client                 *http.Client
	Log                    func(level, msg string, kv ...any)
}

// Resolve is the file segments name: hf/<org>/<model>/resolve/main/<file> mirrors the model host's layout the page's
// library builds, ort/<library version>/<file> the dist it would otherwise load from a CDN.
func (a *Assets) Resolve(segments []string) (Asset, bool) {
	if len(segments) == 0 {
		return Asset{}, false
	}
	kind, rest := segments[0], segments[1:]
	switch {
	case kind == "hf":
		prefix := WhisperModel + "/resolve/main/"
		joined := strings.Join(rest, "/")
		if !strings.HasPrefix(joined, prefix) {
			return Asset{}, false
		}
		file := joined[len(prefix):]
		if !modelFile.MatchString(file) {
			return Asset{}, false
		}
		return Asset{a.ModelHost + "/" + prefix + file, "hf/" + WhisperModel + "/" + file, contentType(file)}, true
	case kind == "ort" && len(rest) == 2:
		v, file := rest[0], rest[1]
		if !version.MatchString(v) || !runtimeFile.MatchString(file) {
			return Asset{}, false
		}
		return Asset{a.RuntimeHost + "/npm/@huggingface/transformers@" + v + "/dist/" + file, "ort/" + v + "/" + file, contentType(file)}, true
	}
	return Asset{}, false
}

func writeError(w http.ResponseWriter, r *http.Request, status int, text string) {
	status, body, h := nodejson.Answer(r, status, map[string]string{"error": text}, nil)
	for k, vs := range h {
		w.Header()[k] = vs
	}
	if body != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(status)
	if raw, ok := body.(json.RawMessage); ok {
		_, _ = w.Write(raw)
	}
}

// Serve answers GET /api/voice/assets/<segments…>; the caller checked the session or token and the method.
func (a *Assets) Serve(w http.ResponseWriter, r *http.Request, segments []string) {
	asset, ok := a.Resolve(segments)
	if !ok {
		writeError(w, r, http.StatusNotFound, "Unknown voice asset")
		return
	}
	file := filepath.Join(a.CacheDir, asset.CacheKey)
	h := w.Header()
	if st, err := os.Stat(file); err == nil && st.Mode().IsRegular() {
		f, err := os.Open(file)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "file could not be read")
			return
		}
		defer f.Close()
		h.Set("Content-Type", asset.ContentType)
		h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
		h.Set("Cache-Control", "private, max-age=604800, immutable")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f)
		return
	}
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Get(asset.Upstream)
	if err != nil || res.StatusCode < 200 || res.StatusCode > 299 {
		if err != nil && a.Log != nil {
			a.Log("warn", "voice asset download failed", "url", asset.Upstream, "error", err.Error())
		}
		if res != nil {
			res.Body.Close()
		}
		writeError(w, r, http.StatusBadGateway, "Не удалось скачать "+segments[len(segments)-1])
		return
	}
	defer res.Body.Close()
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		writeError(w, r, http.StatusInternalServerError, "file could not be read")
		return
	}
	tmp := file + "." + strconv.Itoa(os.Getpid()) + "." + strconv.FormatInt(time.Now().UnixMilli(), 10) + ".part"
	sink, err := os.Create(tmp)
	if err != nil {
		sink = nil
	}
	// A body the client decompressed is not the length the upstream named.
	length := int64(-1)
	if !res.Uncompressed && res.Header.Get("Content-Encoding") == "" && res.ContentLength >= 0 {
		length = res.ContentLength
	}
	h.Set("Content-Type", asset.ContentType)
	if length >= 0 {
		h.Set("Content-Length", strconv.FormatInt(length, 10))
	}
	h.Set("Cache-Control", "private, max-age=604800, immutable")
	w.WriteHeader(http.StatusOK)
	var written int64
	buf := make([]byte, 64*1024)
	broken := false
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if sink != nil {
				if _, werr := sink.Write(buf[:n]); werr != nil {
					sink.Close()
					_ = os.Remove(tmp)
					sink = nil
				}
			}
			written += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			broken = true
			if a.Log != nil {
				a.Log("warn", "voice asset stream broke", "url", asset.Upstream, "error", err.Error())
			}
			break
		}
	}
	if sink == nil {
		return
	}
	sink.Close()
	if broken || length >= 0 && written != length {
		_ = os.Remove(tmp)
		if broken {
			panic(http.ErrAbortHandler) // the page must not take a cut file for a whole one
		}
		return
	}
	_ = os.Rename(tmp, file)
}
