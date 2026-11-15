package webrelease

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// A machine takes the page its hub hands out (/dl/web.json, /dl/web.tar.gz: the release's page whose origin the
// hub checked, internal/hub/webpkg) into its data folder, one folder per version, and serves it from there at its next
// start, through the same guard as RUNTIME_WEB_DIR. The archive must match the SHA-256 the hub names, and a page that
// calls a route this runtime does not have is not taken at all: the machine keeps the page it serves.

const (
	hubPointer    = "current.json"
	maxHubArchive = 256 << 20
	maxHubPage    = 512 << 20
	maxHubFiles   = 20000
)

var pageVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// HubPage is what /dl/web.json says.
type HubPage struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// Hub keeps the hub's page in Dir.
type Hub struct {
	URL    string
	Dir    string
	Client *http.Client
}

func (h Hub) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
}

// Current is the folder of the page taken last ("" when none was).
func (h Hub) Current() string {
	raw, err := os.ReadFile(filepath.Join(h.Dir, hubPointer))
	if err != nil {
		return ""
	}
	var p HubPage
	if json.Unmarshal(raw, &p) != nil || !pageVersion.MatchString(p.Version) {
		return ""
	}
	dir := filepath.Join(h.Dir, p.Version)
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return ""
	}
	return dir
}

func (h Hub) get(ctx context.Context, p string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(h.URL, "/")+p, nil)
	if err != nil {
		return nil, err
	}
	res, err := h.client().Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("%s: %s", p, res.Status)
	}
	return res, nil
}

// ErrNoHubPage: the hub hands out no page (it is off there); nothing to do.
var ErrNoHubPage = errors.New("the hub hands out no page")

// Sync takes the hub's page when it is new and this runtime answers its calls (own: the contract of the page compiled
// into this runtime; nil skips the check, for a runtime without one). serving is the folder this runtime serves now,
// which is kept. changed says whether a new page was taken.
func (h Hub) Sync(ctx context.Context, own *Manifest, serving string) (string, bool, error) {
	res, err := h.get(ctx, "/dl/web.json")
	if err != nil {
		if strings.HasSuffix(err.Error(), "404 Not Found") {
			return "", false, ErrNoHubPage
		}
		return "", false, err
	}
	var p HubPage
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&p)
	res.Body.Close()
	if err != nil {
		return "", false, fmt.Errorf("web.json: %w", err)
	}
	if !pageVersion.MatchString(p.Version) || len(p.SHA256) != 64 {
		return "", false, fmt.Errorf("web.json names no page: %q", p.Version)
	}
	prev := h.Current()
	if prev != "" && filepath.Base(prev) == p.Version {
		return p.Version, false, nil
	}
	if err := os.MkdirAll(h.Dir, 0o755); err != nil {
		return "", false, err
	}
	tmp, err := os.MkdirTemp(h.Dir, ".page-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(tmp)
	if err := h.fetch(ctx, p, tmp); err != nil {
		return "", false, err
	}
	page, err := Read(os.DirFS(tmp))
	if err != nil {
		return "", false, fmt.Errorf("page %s: %w", p.Version, err)
	}
	if own != nil {
		if missing := Missing(page, *own); len(missing) > 0 {
			return "", false, fmt.Errorf("page %s calls %d routes this runtime does not have (first %s)", p.Version, len(missing), missing[0])
		}
	}
	dst := filepath.Join(h.Dir, p.Version)
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return "", false, err
	}
	body, _ := json.Marshal(p)
	if err := writeFileAtomic(h.Dir, hubPointer, body); err != nil {
		return "", false, err
	}
	h.prune(p.Version, filepath.Base(prev), filepath.Base(serving))
	return p.Version, true, nil
}

// fetch downloads the archive, checks its SHA-256 and unpacks it into dir.
func (h Hub) fetch(ctx context.Context, p HubPage, dir string) error {
	res, err := h.get(ctx, "/dl/web.tar.gz")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	f, err := os.CreateTemp(h.Dir, ".web-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(res.Body, maxHubArchive+1))
	if err != nil {
		return err
	}
	if n > maxHubArchive {
		return errors.New("web.tar.gz is too large")
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != strings.ToLower(p.SHA256) {
		return fmt.Errorf("web.tar.gz: SHA-256 %s, the hub says %s", got, p.SHA256)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return untar(f, dir)
}

// untar unpacks plain folders and files only, each inside dir.
func untar(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	files := 0
	for {
		hd, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := path.Clean(strings.TrimPrefix(hd.Name, "./"))
		if name == "." {
			continue
		}
		if !fs.ValidPath(name) {
			return fmt.Errorf("web.tar.gz: %q is outside the page", hd.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch hd.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if files++; files > maxHubFiles {
				return errors.New("web.tar.gz holds too many files")
			}
			if total += hd.Size; total > maxHubPage {
				return errors.New("web.tar.gz unpacks too large")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, io.LimitReader(tr, hd.Size))
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("web.tar.gz: %q is neither a file nor a folder", hd.Name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return errors.New("web.tar.gz holds no index.html")
	}
	return nil
}

func writeFileAtomic(dir, name string, body []byte) error {
	tmp, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// prune removes the pages other than keep: the new one, the one taken before it and the one served now, which this
// runtime serves until its next start.
func (h Hub) prune(keep ...string) {
	entries, _ := os.ReadDir(h.Dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && pageVersion.MatchString(n) && !slices.Contains(keep, n) {
			_ = os.RemoveAll(filepath.Join(h.Dir, n))
		}
	}
}
