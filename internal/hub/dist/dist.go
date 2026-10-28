// Package dist serves what a machine downloads to join the hub: the installer script at /install and the binaries
// under /dl/, with a manifest of their SHA-256 sums. Nothing here needs a login: the binaries are the open-source
// ones, and a machine has no account to sign in with before it is registered.
package dist

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// maxInstaller bounds the installer script the hub is willing to read and serve.
const maxInstaller = 1 << 20

// binaryName is the file names under /dl/: runtime-<os>-<arch> and agent-<os>-<arch>.
var binaryName = regexp.MustCompile(`^(runtime|agent|harnessly)-(linux|darwin)-(amd64|arm64)$`)

// Handler serves the installer and the binaries from a folder.
type Handler struct {
	Dir     string
	HubURL  string
	Version string
	// Image is the machine image of the container profile (HUB_CONTAINER_IMAGE); empty when the hub has none.
	Image string

	mu   sync.Mutex
	sums map[string]cachedSum
}

type cachedSum struct {
	size  int64
	mtime time.Time
	sum   string
}

// New makes the handler. hubURL is the hub's public URL and image the machine image of the container profile, both put
// into the installer; version goes into the manifest.
func New(dir, hubURL, version, image string) *Handler {
	return &Handler{Dir: dir, HubURL: hubURL, Version: version, Image: image, sums: map[string]cachedSum{}}
}

// Mount registers /install and /dl/….
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.Handle("GET /install", httpx.Handler(h.install))
	mux.Handle("GET /dl/manifest.json", httpx.Handler(h.manifest))
	mux.Handle("GET /dl/{name}", httpx.Handler(h.binary))
}

// shellQuote quotes a value for sh in single quotes.
func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }

// install serves install.sh with HARNESSLY_HUB_URL (and HARNESSLY_IMAGE, when the hub has a machine image) set on its
// first lines, after the shebang.
func (h *Handler) install(w http.ResponseWriter, r *http.Request) error {
	f, err := os.Open(filepath.Join(h.Dir, "install.sh"))
	if err != nil {
		return httpx.New(http.StatusNotFound, "not_found", "Установщик не найден на этом хабе")
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, maxInstaller+1))
	if err != nil || len(body) > maxInstaller {
		return httpx.ErrInternal
	}
	script := string(body)
	assign := "HARNESSLY_HUB_URL=" + shellQuote(h.HubURL) + "\n"
	if h.Image != "" {
		assign += "HARNESSLY_IMAGE=" + shellQuote(h.Image) + "\n"
	}
	if strings.HasPrefix(script, "#!") {
		line, rest, _ := strings.Cut(script, "\n")
		script = line + "\n" + assign + rest
	} else {
		script = assign + script
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.WriteString(w, script)
	return nil
}

type manifestFile struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest lists the binaries present in the folder with their sums. A sum is computed once per file and again when
// the file changes.
func (h *Handler) Manifest() (map[string]manifestFile, error) {
	entries, err := os.ReadDir(h.Dir)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]manifestFile{}
	for _, e := range entries {
		name := e.Name()
		if !binaryName.MatchString(name) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		c, ok := h.sums[name]
		if !ok || c.size != info.Size() || !c.mtime.Equal(info.ModTime()) {
			sum, err := sha256File(filepath.Join(h.Dir, name))
			if err != nil {
				continue
			}
			c = cachedSum{size: info.Size(), mtime: info.ModTime(), sum: sum}
			h.sums[name] = c
		}
		out[name] = manifestFile{SHA256: c.sum, Size: c.size}
	}
	return out, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (h *Handler) manifest(w http.ResponseWriter, r *http.Request) error {
	files, err := h.Manifest()
	if errors.Is(err, os.ErrNotExist) {
		return httpx.New(http.StatusNotFound, "not_found", "На этом хабе нет бинарников для машин")
	}
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"version": h.Version, "files": files})
	return nil
}

func (h *Handler) binary(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if !binaryName.MatchString(name) {
		return httpx.ErrNotFound
	}
	f, err := os.Open(filepath.Join(h.Dir, name))
	if err != nil {
		return httpx.ErrNotFound
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return httpx.ErrNotFound
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), f)
	return nil
}
