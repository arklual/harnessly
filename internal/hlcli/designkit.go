package hlcli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// harnessly design kit install|list: the UI kits of the design library, kept by version in a folder of this machine
// (RUNTIME_DESIGN_KITS_DIR) and served by the library server at /kit/<id>/<version>/. Both work on the folder itself,
// they do not need the runtime to be up.

const kitHelp = `harnessly design kit install <id> <version> <tarball-or-url> [--force] [--dir DIR] [--header 'Name: value']
harnessly design kit list [--dir DIR]

A UI kit is installed once by version and boards load it from /kit/<id>/<version>/... instead of carrying a copy.

  install   unpack a kit tarball (.tar or .tar.gz made from the kit's browser build folder) into <dir>/<id>/<version>/.
            <tarball-or-url> is a file, an http(s) address, or - for standard input. All or nothing: a half-installed
            version is never served. The same tarball again does nothing; a version holding other content is refused
            (versions are immutable) unless --force. For an address that wants a login send a header, for GitLab's
            package registry --header 'PRIVATE-TOKEN: <token>' (or HARNESSLY_KIT_TOKEN in the environment).
  list      the installed kits and versions.

The kits folder is --dir, else $RUNTIME_DESIGN_KITS_DIR, else design-kits in the runtime's data folder (the same
default the runtime serves from).
`

// designKit is "design kit ..."; args are what follows "kit".
func (c *cli) designKit(args []string) error {
	if len(args) == 0 {
		return usageErr("design kit takes: install|list")
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		c.out("%s", kitHelp)
		return nil
	}
	sub, args := args[0], args[1:]
	opt, pos, err := c.kitFlags(args)
	if err != nil {
		return err
	}
	switch sub {
	case "install":
		return c.kitInstall(opt, pos)
	case "list", "ls":
		if len(pos) != 0 {
			return usageErr("design kit list takes no arguments, got %q", pos[0])
		}
		return c.kitList(opt)
	}
	return usageErr("unknown design kit command %q", sub)
}

type kitOptions struct {
	dir     string
	force   bool
	headers []string
}

func (c *cli) kitFlags(args []string) (kitOptions, []string, error) {
	var o kitOptions
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		value := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", usageErr("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "force", "f":
			o.force = true
		case "dir":
			o.dir, err = value()
		case "header", "H":
			var h string
			if h, err = value(); err == nil {
				o.headers = append(o.headers, h)
			}
		default:
			err = usageErr("unknown option %s", a)
		}
		if err != nil {
			return o, nil, err
		}
	}
	if o.dir == "" {
		o.dir = strings.TrimSpace(c.env.Getenv("RUNTIME_DESIGN_KITS_DIR"))
	}
	if o.dir == "" {
		o.dir = designkit.DefaultDir(c.dataDir())
	}
	if !filepath.IsAbs(o.dir) {
		o.dir = filepath.Join(c.env.Cwd, o.dir)
	}
	if token := strings.TrimSpace(c.env.Getenv("HARNESSLY_KIT_TOKEN")); token != "" {
		o.headers = append(o.headers, "PRIVATE-TOKEN: "+token)
	}
	return o, pos, nil
}

func (c *cli) kitInstall(o kitOptions, pos []string) error {
	if len(pos) != 3 {
		return usageErr("design kit install needs <id> <version> <tarball-or-url>")
	}
	id, version, source := pos[0], pos[1], pos[2]
	if !designkit.ValidID(id) {
		return usageErr("%q is not a kit id (lower case letters, digits and dashes)", id)
	}
	if !designkit.ValidVersion(version) {
		return usageErr("%q is not a version (major.minor.patch, an optional -tag)", version)
	}
	body, closeBody, err := c.kitSource(source, o.headers)
	if err != nil {
		return err
	}
	defer closeBody()
	res, err := designkit.Install(o.dir, id, version, body, designkit.Options{Force: o.force})
	if errors.Is(err, designkit.ErrExists) {
		return fail("%s %s is already installed with different content. Versions do not change: publish a new version, or install this one again with --force to repair it", id, version)
	}
	if err != nil {
		return fail("%v", err)
	}
	if c.json {
		raw, _ := json.Marshal(struct {
			ID  string `json:"id"`
			Dir string `json:"dir"`
			designkit.Result
		}{id, filepath.Join(o.dir, id, version), res})
		c.out("%s\n", raw)
		return nil
	}
	if res.Unchanged {
		c.out("%s %s is already installed with this content, nothing changed\n  %s\n", id, version, filepath.Join(o.dir, id, version))
		return nil
	}
	state := "installed"
	if res.Replaced {
		state = "replaced"
	}
	c.out("%s %s %s: %d files, %.1f MB, sha256 %s\n  %s\n  served at /kit/%s/%s/\n", state, id, version, res.Files,
		float64(res.Bytes)/(1<<20), res.SHA256, filepath.Join(o.dir, id, version), id, version)
	return nil
}

// kitSource opens a tarball: a file, standard input ("-") or an http(s) address.
func (c *cli) kitSource(source string, headers []string) (io.Reader, func(), error) {
	switch {
	case source == "-":
		return c.env.Stdin, func() {}, nil
	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		if _, err := url.Parse(source); err != nil {
			return nil, nil, usageErr("%q is not an address", source)
		}
		req, err := http.NewRequest(http.MethodGet, source, nil)
		if err != nil {
			return nil, nil, fail("%v", err)
		}
		for _, h := range headers {
			name, value, ok := strings.Cut(h, ":")
			if !ok || strings.TrimSpace(name) == "" {
				return nil, nil, usageErr("a header is 'Name: value', got %q", h)
			}
			req.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
		}
		client := c.env.Client
		if client == nil {
			client = &http.Client{Timeout: 5 * time.Minute}
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, fail("download failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, nil, fail("download failed: %s answered %s", req.URL.Host, resp.Status)
		}
		return resp.Body, func() { _ = resp.Body.Close() }, nil
	}
	path := source
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.env.Cwd, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fail("cannot read %s: %v", source, errors.Unwrap(err))
	}
	return f, func() { _ = f.Close() }, nil
}

func (c *cli) kitList(o kitOptions) error {
	kits := designkit.List(o.dir)
	if c.json {
		raw, _ := json.Marshal(struct {
			Dir  string          `json:"dir"`
			Kits []designkit.Kit `json:"kits"`
		}{o.dir, kits})
		c.out("%s\n", raw)
		return nil
	}
	if len(kits) == 0 {
		c.out("no kits installed in %s\n", o.dir)
		return nil
	}
	for _, k := range kits {
		versions := make([]string, len(k.Versions))
		for i, v := range k.Versions {
			versions[i] = v.Version
		}
		c.out("%s\t%s\n", k.ID, strings.Join(versions, ", "))
	}
	return nil
}
