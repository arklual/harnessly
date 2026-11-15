// Package webpkg keeps the machines' page the hub hands out (/dl/web.json, /dl/web.tar.gz) in step with the newest page
// a release published: the generic package web/<version> of the project's package registry, which only the
// pipeline of a protected tag may write (tools/ci/publish-web.sh).
//
// Nothing is taken on trust from the registry's listing. A page is taken only when every file of its package was
// written by a pipeline of the tag v<version>, at the tag's commit, and the archive matches both the registry's SHA-256
// and the SHA256SUMS the same pipeline wrote; the archive is downloaded next to the served one and put in place with a
// rename. Without HUB_WEB_PACKAGE_URL nothing runs and /dl/web.json answers 404: the machines keep the page compiled
// into their runtime.
package webpkg

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// PackageName is the generic package of the page.
	PackageName = "web"
	// Archive is the page: a tar.gz of web/dist.
	Archive = "web.tar.gz"
	// Sums is the SHA256SUMS file the release writes next to the archive.
	Sums = "SHA256SUMS"
	// Pointer is the file in the hub's folder that names the page it serves.
	Pointer = "web.json"
	// Every is the time between two looks at the registry.
	Every = 5 * time.Minute

	maxArchive = 256 << 20
	maxSmall   = 4 << 20
)

var version = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Page is web.json: the page the hub serves now.
type Page struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	// File is the archive's name in the folder (web-<version>.tar.gz).
	File string `json:"file"`
	// Pipeline is the tag's pipeline that wrote the package.
	Pipeline int64 `json:"pipeline"`
}

// Source is the project's API in the registry (https://host/api/v4/projects/<id>) and a token that may read it.
type Source struct {
	API    string
	Token  string
	Client *http.Client
}

func (s Source) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{
		Timeout: 10 * time.Minute, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
		CheckRedirect: keepTokenHome,
	}
}

// keepTokenHome drops the registry's token on a redirect to another host (a download may be sent to object storage).
func keepTokenHome(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del("PRIVATE-TOKEN")
	}
	return nil
}

func (s Source) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.API, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	if s.Token != "" {
		req.Header.Set("PRIVATE-TOKEN", s.Token)
	}
	res, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("%s: %s", path, res.Status)
	}
	return res, nil
}

func (s Source) getJSON(ctx context.Context, path string, v any) error {
	res, err := s.get(ctx, path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(io.LimitReader(res.Body, maxSmall)).Decode(v)
}

type pkg struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type pipeline struct {
	ID  int64  `json:"id"`
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type pkgFile struct {
	ID        int64      `json:"id"`
	Name      string     `json:"file_name"`
	Size      int64      `json:"size"`
	SHA256    string     `json:"file_sha256"`
	Pipelines []pipeline `json:"pipelines"`
}

// latest is the newest published page: the package named exactly "web" (the registry matches names in part) with a
// release version, created last.
func (s Source) latest(ctx context.Context) (pkg, error) {
	var list []pkg
	q := url.Values{
		"package_type": {"generic"}, "package_name": {PackageName}, "status": {"default"},
		"order_by": {"created_at"}, "sort": {"desc"}, "per_page": {"50"},
	}
	if err := s.getJSON(ctx, "/packages?"+q.Encode(), &list); err != nil {
		return pkg{}, err
	}
	for _, p := range list {
		if p.Name == PackageName && version.MatchString(p.Version) {
			return p, nil
		}
	}
	return pkg{}, errors.New("no page published yet")
}

// verified checks where the package came from and returns its archive's entry and the tag's pipeline.
func (s Source) verified(ctx context.Context, p pkg) (pkgFile, int64, error) {
	var files []pkgFile
	if err := s.getJSON(ctx, fmt.Sprintf("/packages/%d/package_files?per_page=100", p.ID), &files); err != nil {
		return pkgFile{}, 0, err
	}
	var tag struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	ref := "v" + p.Version
	if err := s.getJSON(ctx, "/repository/tags/"+url.PathEscape(ref), &tag); err != nil {
		return pkgFile{}, 0, fmt.Errorf("tag %s: %w", ref, err)
	}
	archive, by, err := checkFiles(files, ref, tag.Commit.ID)
	if err != nil {
		return pkgFile{}, 0, err
	}
	// A branch may be named like the tag: each pipeline must be the tag's own.
	seen := map[int64]bool{}
	for _, f := range files {
		for _, pl := range f.Pipelines {
			if seen[pl.ID] {
				continue
			}
			seen[pl.ID] = true
			var full struct {
				Tag bool   `json:"tag"`
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			}
			if err := s.getJSON(ctx, fmt.Sprintf("/pipelines/%d", pl.ID), &full); err != nil {
				return pkgFile{}, 0, fmt.Errorf("pipeline %d: %w", pl.ID, err)
			}
			if !full.Tag || full.Ref != ref || full.SHA != tag.Commit.ID {
				return pkgFile{}, 0, fmt.Errorf("pipeline %d that wrote the page is not a pipeline of tag %s", pl.ID, ref)
			}
		}
	}
	return archive, by, nil
}

// checkFiles is the provenance rule: every file of the package was written only by pipelines of ref at commit sha,
// and the package holds the archive and its sums. The archive's newest entry is the one served.
func checkFiles(files []pkgFile, ref, sha string) (pkgFile, int64, error) {
	if sha == "" {
		return pkgFile{}, 0, fmt.Errorf("tag %s has no commit", ref)
	}
	var archive pkgFile
	var by int64
	sums := false
	for _, f := range files {
		if len(f.Pipelines) == 0 {
			return pkgFile{}, 0, fmt.Errorf("%s (file %d) was not written by a pipeline", f.Name, f.ID)
		}
		for _, pl := range f.Pipelines {
			if pl.Ref != ref || pl.SHA != sha {
				return pkgFile{}, 0, fmt.Errorf("%s (file %d) was written by pipeline %d of %s, not of tag %s", f.Name, f.ID, pl.ID, pl.Ref, ref)
			}
			by = max(by, pl.ID)
		}
		switch f.Name {
		case Sums:
			sums = true
		case Archive:
			if f.ID > archive.ID {
				archive = f
			}
		}
	}
	if archive.ID == 0 || !sums {
		return pkgFile{}, 0, fmt.Errorf("the package of %s lacks %s or %s", ref, Archive, Sums)
	}
	return archive, by, nil
}

// sumOf reads the archive's line of a SHA256SUMS file.
func sumOf(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == Archive {
			return strings.ToLower(f[0]), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s names no %s", Sums, Archive)
}

// Current is the page the folder serves; the zero Page when there is none.
func Current(dir string) (Page, error) {
	var p Page
	raw, err := os.ReadFile(filepath.Join(dir, Pointer))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return Page{}, err
	}
	if !version.MatchString(p.Version) || p.File != archiveName(p.Version) {
		return Page{}, fmt.Errorf("%s: not a page", Pointer)
	}
	return p, nil
}

func archiveName(v string) string { return "web-" + v + ".tar.gz" }

// Sync brings the folder to the newest published page. changed says whether it took a new one.
func (s Source) Sync(ctx context.Context, dir string) (Page, bool, error) {
	cur, _ := Current(dir)
	p, err := s.latest(ctx)
	if err != nil {
		return cur, false, err
	}
	if p.Version == cur.Version {
		if _, err := os.Stat(filepath.Join(dir, cur.File)); err == nil {
			return cur, false, nil
		}
	}
	archive, by, err := s.verified(ctx, p)
	if err != nil {
		return cur, false, err
	}
	base := "/packages/generic/" + PackageName + "/" + url.PathEscape(p.Version) + "/"
	res, err := s.get(ctx, base+Sums)
	if err != nil {
		return cur, false, err
	}
	want, err := sumOf(io.LimitReader(res.Body, maxSmall))
	res.Body.Close()
	if err != nil {
		return cur, false, err
	}
	if want != strings.ToLower(archive.SHA256) {
		return cur, false, fmt.Errorf("%s of %s does not match the registry's sum", Sums, p.Version)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return cur, false, err
	}
	next := Page{Version: p.Version, SHA256: want, File: archiveName(p.Version), Pipeline: by}
	if next.Size, err = s.download(ctx, base+Archive, dir, next.File, want); err != nil {
		return cur, false, err
	}
	if err := writeAtomic(dir, Pointer, mustJSON(next)); err != nil {
		return cur, false, err
	}
	prune(dir, next.File)
	return next, true, nil
}

// download writes the archive under name when its SHA-256 is want.
func (s Source) download(ctx context.Context, path, dir, name, want string) (int64, error) {
	res, err := s.get(ctx, path)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	tmp, err := os.CreateTemp(dir, ".web-*.tar.gz")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(res.Body, maxArchive+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	if n > maxArchive {
		return 0, fmt.Errorf("%s is larger than %d bytes", Archive, maxArchive)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return 0, fmt.Errorf("%s: SHA-256 %s, the release says %s", Archive, got, want)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return 0, err
	}
	return n, os.Rename(tmp.Name(), filepath.Join(dir, name))
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return append(b, '\n')
}

func writeAtomic(dir, name string, body []byte) error {
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
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// prune removes the archives of older pages.
func prune(dir, keep string) {
	entries, _ := os.ReadDir(dir)
	var old []string
	for _, e := range entries {
		n := e.Name()
		if n != keep && strings.HasPrefix(n, "web-") && strings.HasSuffix(n, ".tar.gz") {
			old = append(old, n)
		}
	}
	sort.Strings(old)
	for _, n := range old {
		_ = os.Remove(filepath.Join(dir, n))
	}
}

// Run syncs the folder now and then every Every until ctx ends; failures are logged and the served page stays.
func (s Source) Run(ctx context.Context, dir string, log *slog.Logger) {
	for {
		p, changed, err := s.Sync(ctx, dir)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Warn("web package: the page was not updated", "error", err, "serving", p.Version)
		case changed:
			log.Info("web package: serving a new page to the machines", "version", p.Version, "pipeline", p.Pipeline)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(Every):
		}
	}
}
