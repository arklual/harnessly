package hlcli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Publishing a site: `harnessly site deploy ./dist --name shop`. The command talks to the hub (or the standalone sites
// server) directly, not to the runtime on this machine: it hashes the folder, tells the server the manifest, uploads
// only the files the server does not have, and commits. The server switches the site to the new version in one step.

const siteHelp = `harnessly site - publish a folder as a site

  harnessly site deploy <dir> [--name NAME] [--space SPACE] [--token T] [--url URL] [--allow-secrets] [--no-current]
        Upload <dir> and print the site's address. Only files the server does not have yet are sent.
        --token   a deploy token of the site (hsite_…), or an administrator's token with --space and --name
                  (HARNESSLY_SITE_TOKEN)
        --url     the hub or sites server (HARNESSLY_SITES_URL)
        --name    the site's name; with a deploy token it is already known. A missing site is created for an
                  administrator's token.
        --allow-secrets   publish even when a file looks like a secret (.env, a private key): off by default
        --no-current      add a version but keep the site on its current one
        .git and node_modules are not uploaded.
  harnessly site list     [--space SPACE] [--token T] [--url URL]
  harnessly site versions [--name NAME] [--space SPACE] [--token T] [--url URL]
  harnessly site rollback <version> [--name NAME] [--space SPACE] [--token T] [--url URL]
        Make an earlier version the current one (a second or two).
`

func init() { commandHelp["site"] = siteHelp }

// siteConn is where and as whom the command talks.
type siteConn struct {
	c      *cli
	base   string
	token  string
	space  string
	name   string
	client *http.Client
}

type siteFlags struct {
	dir          string
	version      string
	name, space  string
	token, url   string
	allowSecrets bool
	noCurrent    bool
}

func parseSiteFlags(args []string) (siteFlags, error) {
	var f siteFlags
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := func() (string, error) {
			if i+1 >= len(args) {
				return "", usageErr("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch {
		case a == "--name" || a == "-n":
			f.name, err = val()
		case a == "--space":
			f.space, err = val()
		case a == "--token":
			f.token, err = val()
		case a == "--url":
			f.url, err = val()
		case a == "--allow-secrets":
			f.allowSecrets = true
		case a == "--no-current":
			f.noCurrent = true
		case strings.HasPrefix(a, "-"):
			return f, usageErr("unknown flag %s", a)
		case f.dir == "":
			f.dir = a
		default:
			return f, usageErr("unexpected argument %q", a)
		}
		if err != nil {
			return f, err
		}
	}
	return f, nil
}

func (c *cli) site(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(c.env.Stdout, siteHelp)
		return nil
	}
	sub, args := args[0], args[1:]
	f, err := parseSiteFlags(args)
	if err != nil {
		return err
	}
	switch sub {
	case "deploy":
		if f.dir == "" {
			return usageErr("site deploy needs a folder")
		}
		return c.siteDeploy(f)
	case "list", "ls":
		return c.siteList(f)
	case "versions":
		return c.siteVersions(f)
	case "rollback":
		if f.dir == "" {
			return usageErr("site rollback needs a version number")
		}
		f.version, f.dir = f.dir, ""
		return c.siteRollback(f)
	}
	return usageErr("unknown site command %q", sub)
}

func (c *cli) siteConnect(f siteFlags) (*siteConn, error) {
	base := strings.TrimRight(firstNonEmpty(f.url, c.env.Getenv("HARNESSLY_SITES_URL")), "/")
	if base == "" {
		return nil, fail("which server? Pass --url https://<hub> or set HARNESSLY_SITES_URL")
	}
	token := firstNonEmpty(f.token, c.env.Getenv("HARNESSLY_SITE_TOKEN"))
	if token == "" {
		return nil, fail("no token: pass --token or set HARNESSLY_SITE_TOKEN (a deploy token is made in the site's settings)")
	}
	cl := c.env.Client
	if cl == nil {
		cl = &http.Client{Timeout: 10 * time.Minute}
	}
	sc := &siteConn{c: c, base: base, token: token, space: f.space, name: f.name, client: cl}
	if strings.HasPrefix(token, "hsite_") {
		var info struct{ Space, Site string }
		if err := sc.json("GET", "/api/sites-token", nil, &info); err != nil {
			return nil, userize(err)
		}
		if f.name != "" && f.name != info.Site {
			return nil, fail("this token is for the site %q, not %q", info.Site, f.name)
		}
		if f.space != "" && f.space != info.Space {
			return nil, fail("this token is for the space %q, not %q", info.Space, f.space)
		}
		sc.space, sc.name = info.Space, info.Site
	}
	return sc, nil
}

// userize makes a server's refusal the message of the command.
func userize(err error) error {
	var se *siteError
	if errors.As(err, &se) {
		return fail("%s", se.Error())
	}
	return err
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// siteError turns an API error into words for the terminal.
type siteError struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *siteError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.Code
	}
	if fs, ok := e.Details["findings"].([]any); ok && len(fs) > 0 {
		var b strings.Builder
		b.WriteString(msg + ":")
		for _, f := range fs {
			if m, ok := f.(map[string]any); ok {
				fmt.Fprintf(&b, "\n  %v — %v", m["path"], m["rule"])
			}
		}
		b.WriteString("\nRemove these files, or pass --allow-secrets if they are not secrets.")
		return b.String()
	}
	if e.Status == http.StatusUnauthorized {
		return msg + " (the token is wrong, revoked, or for another site)"
	}
	return fmt.Sprintf("%s (HTTP %d)", msg, e.Status)
}

// do runs a request; the body is JSON, bytes or nothing. A status that is not 2xx is a *siteError.
func (s *siteConn) do(method, path string, body any, ctype string) (int, []byte, error) {
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case io.Reader:
		rd = b
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(raw)
		ctype = "application/json"
	}
	req, err := http.NewRequest(method, s.base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return 0, nil, fail("cannot reach %s: %v", s.base, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode/100 != 2 {
		e := &siteError{Status: res.StatusCode}
		var b struct {
			Error   string
			Message string
			Details map[string]any
		}
		if json.Unmarshal(raw, &b) == nil {
			e.Code, e.Message, e.Details = b.Error, b.Message, b.Details
		}
		return res.StatusCode, raw, e
	}
	return res.StatusCode, raw, nil
}

func (s *siteConn) json(method, path string, body, out any) error {
	_, raw, err := s.do(method, path, body, "")
	if err != nil {
		return err
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (s *siteConn) need(what string) error {
	if s.space == "" {
		return fail("which space? Pass --space")
	}
	if what == "name" && s.name == "" {
		return fail("which site? Pass --name")
	}
	return nil
}

type localFile struct {
	path string // slash separated, relative
	abs  string
	hash string
	size int64
}

// skipDir are the folders that are never part of a site.
var skipDir = map[string]bool{".git": true, "node_modules": true}

// scanDir hashes the regular files under dir.
func scanDir(dir string) ([]localFile, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, fail("%v", err)
	}
	if !st.IsDir() {
		return nil, fail("%s is not a folder", dir)
	}
	var files []localFile
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && skipDir[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // links, sockets: not published
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		h := sha256.New()
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		n, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		files = append(files, localFile{path: filepath.ToSlash(rel), abs: p, hash: hex.EncodeToString(h.Sum(nil)), size: n})
		return nil
	})
	if err != nil {
		return nil, fail("%v", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

func (c *cli) siteDeploy(f siteFlags) error {
	sc, err := c.siteConnect(f)
	if err != nil {
		return err
	}
	if err := sc.need("name"); err != nil {
		return err
	}
	files, err := scanDir(f.dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fail("%s has no files to publish", f.dir)
	}
	manifest := map[string]map[string]any{}
	byHash := map[string]localFile{}
	var total int64
	for _, lf := range files {
		manifest[lf.path] = map[string]any{"hash": lf.hash, "size": lf.size}
		byHash[lf.hash] = lf
		total += lf.size
	}
	base := "/api/sites/" + url.PathEscape(sc.space) + "/" + url.PathEscape(sc.name)
	req := map[string]any{"files": manifest, "source": "cli", "allowSecrets": f.allowSecrets}

	var begun struct {
		ID      string   `json:"id"`
		Missing []string `json:"missing"`
	}
	err = sc.json("POST", base+"/deploys", req, &begun)
	// A site that is not there yet is made for whoever may make sites (an administrator's token).
	if se := (*siteError)(nil); errors.As(err, &se) && se.Status == http.StatusNotFound && !strings.HasPrefix(sc.token, "hsite_") {
		if cerr := sc.json("POST", "/api/sites/"+url.PathEscape(sc.space), map[string]any{"name": sc.name}, nil); cerr != nil {
			return userize(cerr)
		}
		c.errf("created the site %s\n", sc.name)
		err = sc.json("POST", base+"/deploys", req, &begun)
	}
	if err != nil {
		return userize(err)
	}
	c.errf("%d files, %.1f MB; %d to upload\n", len(files), float64(total)/1e6, len(begun.Missing))

	if err := sc.upload(base+"/deploys/"+begun.ID, begun.Missing, byHash); err != nil {
		return userize(err)
	}
	var done struct {
		Version int    `json:"version"`
		URL     string `json:"url"`
	}
	if err := sc.json("POST", base+"/deploys/"+begun.ID+"/commit", map[string]any{"current": !f.noCurrent}, &done); err != nil {
		return userize(err)
	}
	if c.json {
		c.out("%s\n", mustJSON(done))
		return nil
	}
	if f.noCurrent {
		c.errf("version %d added; the site stays on its current version\n", done.Version)
	}
	c.out("%s\n", done.URL)
	return nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (c *cli) errf(format string, a ...any) { fmt.Fprintf(c.env.Stderr, format, a...) }

// upload sends the missing files, eight at a time.
func (s *siteConn) upload(deploy string, missing []string, byHash map[string]localFile) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	sem := make(chan struct{}, 8)
	for _, h := range missing {
		lf, ok := byHash[h]
		if !ok {
			return fail("the server asked for a file we did not offer (%s)", h)
		}
		mu.Lock()
		stop := first != nil
		mu.Unlock()
		if stop {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			err := s.putFile(deploy, lf)
			if err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return first
}

func (s *siteConn) putFile(deploy string, lf localFile) error {
	f, err := os.Open(lf.abs)
	if err != nil {
		return fail("%v", err)
	}
	defer f.Close()
	_, _, err = s.do("PUT", deploy+"/blobs/"+lf.hash, f, "application/octet-stream")
	if err != nil {
		var se *siteError
		if errors.As(err, &se) {
			return fail("%s: %s", lf.path, se.Error())
		}
		return err
	}
	return nil
}

func (c *cli) siteList(f siteFlags) error {
	sc, err := c.siteConnect(f)
	if err != nil {
		return err
	}
	if err := sc.need(""); err != nil {
		return err
	}
	var out struct {
		Sites []struct {
			Name, Address, Access, UpdatedAt, UpdatedBy string
			CurrentVersion                              int
		}
	}
	if err := sc.json("GET", "/api/sites/"+url.PathEscape(sc.space), nil, &out); err != nil {
		return userize(err)
	}
	if c.json {
		c.out("%s\n", mustJSON(out))
		return nil
	}
	for _, s := range out.Sites {
		c.out("%s\t%s\tv%d\t%s\t%s\n", s.Name, s.Address, s.CurrentVersion, s.UpdatedAt, s.UpdatedBy)
	}
	return nil
}

func (c *cli) siteVersions(f siteFlags) error {
	sc, err := c.siteConnect(f)
	if err != nil {
		return err
	}
	if err := sc.need("name"); err != nil {
		return err
	}
	var out struct {
		Versions []struct {
			Version, FileCount int
			URL, CreatedAt     string
			CreatedBy          string
			Current            bool
		}
	}
	if err := sc.json("GET", "/api/sites/"+url.PathEscape(sc.space)+"/"+url.PathEscape(sc.name), nil, &out); err != nil {
		return userize(err)
	}
	if c.json {
		c.out("%s\n", mustJSON(out))
		return nil
	}
	for _, v := range out.Versions {
		mark := " "
		if v.Current {
			mark = "*"
		}
		c.out("%s v%d\t%d files\t%s\t%s\t%s\n", mark, v.Version, v.FileCount, v.CreatedAt, v.CreatedBy, v.URL)
	}
	return nil
}

func (c *cli) siteRollback(f siteFlags) error {
	sc, err := c.siteConnect(f)
	if err != nil {
		return err
	}
	if err := sc.need("name"); err != nil {
		return err
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimPrefix(f.version, "v"), "%d", &n); err != nil || n < 1 {
		return usageErr("%q is not a version number", f.version)
	}
	var out struct{ Address string }
	if err := sc.json("POST", "/api/sites/"+url.PathEscape(sc.space)+"/"+url.PathEscape(sc.name)+"/current", map[string]any{"version": n}, &out); err != nil {
		return userize(err)
	}
	c.out("%s\n", out.Address)
	return nil
}
