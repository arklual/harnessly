package hlcli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/designkit"
)

// harnessly design kit install|list: the UI kits of the design library, kept by version in a folder of this machine
// (RUNTIME_DESIGN_KITS_DIR) and served by the library server at /kit/<id>/<version>/. Both work on the folder itself,
// they do not need the runtime to be up.

const kitHelp = `harnessly design kit install <id> <version> <tarball-or-url> [--force] [--dir DIR] [--header 'Name: value']
harnessly design kit list [--dir DIR]
harnessly design kit export <id> [--version V] [--out FILE] [--with-sources] [--accept-restricted] [--dir DIR]
harnessly design kit import <file-or-url> [--as-id ID] [--as-version V] [--dry-run] [--dir DIR] [--header 'Name: value']
harnessly design kit search <id> <words…> [--theme T] [--version V] [--dir DIR]
harnessly design kit show <id> <node> [--theme T] [--version V] [--dir DIR]
harnessly design kit check <folder> [--render] [--json]
harnessly design kit render <folder> [--shots] [--only ID,ID] [--json]

A UI kit is installed once by version and boards load it from /kit/<id>/<version>/... instead of carrying a copy.

  install   unpack a kit tarball (.tar or .tar.gz made from the kit's browser build folder) into <dir>/<id>/<version>/.
            <tarball-or-url> is a file, an http(s) address, or - for standard input. All or nothing: a half-installed
            version is never served. The same tarball again does nothing; a version holding other content is refused
            (versions are immutable) unless --force. For an address that wants a login send a header, for GitLab's
            package registry --header 'PRIVATE-TOKEN: <token>' (or HARNESSLY_KIT_TOKEN in the environment).
  list      the installed kits and versions.
  export    pack one installed version with everything inside it into <id>-<version>.kit.tar.gz: the upstream format,
            kit.json, _harnessly, fonts, assets, and a manifest (kit-export.json) with the size and sha256 of every
            file. Sources (sources/) go in only with --with-sources. A kit with license-restricted files (a corporate
            font) is exported only after you confirm with --accept-restricted; the files are named and the manifest
            records them. Without --version the newest version is taken.
  import    install a kit from an export. It checks every file against the manifest first and installs nothing on
            any mismatch, a missing required kit or a damaged file. It never overwrites: when the id and version
            are installed with other content it shows the difference and stops; install it as a new version
            (--as-version) or as a copy under another id (--as-id). The same export again does nothing.
            --dry-run checks and reports, and changes nothing.

  search    find layouts, components, icons, colours of a kit's canvas by name, description, purpose, a colour's hex
            or a layout's number: the same index the kit screen's search (⌘K) uses. Prints node, kind, title, place.
  show      one node of the canvas (from search): its description, props, variants, slots, and how to call it.
  init      start a kit in an empty <folder>: --id acme-ui --namespace Acme [--title "Acme UI"] [--source draft|library].
            It writes every file a kit needs — design-system.json, README.md with the Consuming section, tokens.json
            and tokens.css (light and dark), components/bundle.js and bundle.css, a card per component — with a
            provider (Root), a plain component (Button) and a compound one (Card with its parts). It passes check;
            replace the components with yours and check after each change.
  check     check a kit folder before packing it (or a project's ds/<folder>/): its identity, the canvas fields,
            tokens.json's shape, a card and a purpose for every component, the bundle's place and styles. Errors
            make it exit 1. --render also draws every component and layout (see render). The contract:
            harnessly docs read agents/design-kits.
  render    draw every component (its sample and first variant) and every layout of a kit folder in this machine's
            headless Chrome through the pages the kit screen uses, and report script errors, files that do not load
            and components that draw nothing. --shots writes the canvas's pictures (_harnessly/thumbs/…) and their
            "thumb" fields into design-system.json: run it as the last step of a build.

The kits folder is --dir, else $RUNTIME_DESIGN_KITS_DIR, else design-kits in the runtime's data folder (the same
default the runtime serves from).
`

// designKit is "design kit ..."; args are what follows "kit".
func (c *cli) designKit(args []string) error {
	if len(args) == 0 {
		return usageErr("design kit takes: init|check|render|install|list|export|import|search|show")
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
	case "export":
		return c.kitExport(opt, pos)
	case "import":
		return c.kitImport(opt, pos)
	case "search":
		return c.kitSearch(opt, pos)
	case "show":
		return c.kitShow(opt, pos)
	case "init":
		return c.kitInit(opt, pos)
	case "check":
		return c.kitCheck(opt, pos)
	case "render":
		return c.kitRender(opt, pos)
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

	version, out, asID, asVersion string
	theme                         string
	withSources, acceptRestricted bool
	dryRun                        bool
	json                          bool
	render, shots                 bool
	only                          string
	id, namespace, title, source  string
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
		case "with-sources":
			o.withSources = true
		case "accept-restricted":
			o.acceptRestricted = true
		case "dry-run":
			o.dryRun = true
		case "json":
			o.json = true
		case "render":
			o.render = true
		case "shots":
			o.shots = true
		case "only":
			o.only, err = value()
		case "id":
			o.id, err = value()
		case "namespace":
			o.namespace, err = value()
		case "title":
			o.title, err = value()
		case "source":
			o.source, err = value()
		case "version":
			o.version, err = value()
		case "out", "o":
			o.out, err = value()
		case "as-id":
			o.asID, err = value()
		case "theme":
			o.theme, err = value()
		case "as-version":
			o.asVersion, err = value()
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

func (c *cli) kitExport(o kitOptions, pos []string) error {
	if len(pos) != 1 {
		return usageErr("design kit export needs <id>")
	}
	id := pos[0]
	if !designkit.ValidID(id) {
		return usageErr("%q is not a kit id (lower case letters, digits and dashes)", id)
	}
	version := o.version
	if version == "" {
		for _, k := range designkit.List(o.dir) {
			if k.ID == id && len(k.Versions) > 0 {
				version = k.Versions[0].Version
			}
		}
		if version == "" {
			return fail("%s is not installed in %s", id, o.dir)
		}
	}
	m, err := designkit.BuildManifest(o.dir, id, version, designkit.ExportOptions{WithSources: o.withSources, AcceptRestricted: o.acceptRestricted})
	if errors.Is(err, designkit.ErrRestricted) {
		var b strings.Builder
		for _, r := range m.Restricted {
			b.WriteString("\n  " + r.Path + ": " + r.Reason)
		}
		return fail("%s %s holds files with a restricted license; passing them on may need the holder's permission:%s\nExport again with --accept-restricted to confirm", id, version, b.String())
	}
	if err != nil {
		return fail("%v", err)
	}
	out := o.out
	if out == "" {
		out = id + "-" + version + ".kit.tar.gz"
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(c.env.Cwd, out)
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".kit-export-")
	if err != nil {
		return fail("cannot write %s: %v", out, err)
	}
	defer os.Remove(tmp.Name())
	werr := designkit.WriteArchive(o.dir, m, tmp)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fail("%v", werr)
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return fail("cannot write %s: %v", out, err)
	}
	if c.json {
		raw, _ := json.Marshal(struct {
			File string `json:"file"`
			*designkit.Manifest
		}{out, m})
		c.out("%s\n", raw)
		return nil
	}
	c.out("exported %s %s: %d files, sha256 %s\n  %s\n", id, version, len(m.Files), m.SHA256, out)
	if len(m.Restricted) > 0 {
		c.out("  restricted files included (%d): the manifest records them\n", len(m.Restricted))
	}
	if !m.WithSources {
		c.out("  without sources (--with-sources adds them)\n")
	}
	return nil
}

func (c *cli) kitImport(o kitOptions, pos []string) error {
	if len(pos) != 1 {
		return usageErr("design kit import needs <file-or-url>")
	}
	body, closeBody, err := c.kitSource(pos[0], o.headers)
	if err != nil {
		return err
	}
	defer closeBody()
	res, err := designkit.Import(o.dir, body, designkit.ImportOptions{AsID: o.asID, AsVersion: o.asVersion, DryRun: o.dryRun, Force: o.force})
	var conflict *designkit.ConflictError
	if errors.As(err, &conflict) {
		if c.json {
			raw, _ := json.Marshal(struct {
				Error    string              `json:"error"`
				ID       string              `json:"id"`
				Version  string              `json:"version"`
				Conflict designkit.Diff      `json:"conflict"`
				Manifest *designkit.Manifest `json:"manifest"`
			}{conflict.Error(), conflict.ID, conflict.Version, conflict.Diff, res.Manifest})
			c.out("%s\n", raw)
		}
		var b strings.Builder
		for _, g := range []struct {
			title string
			files []string
		}{{"changed", conflict.Diff.Changed}, {"added by the file", conflict.Diff.Added}, {"missing from the file", conflict.Diff.Removed}} {
			for i, f := range g.files {
				if i == 5 {
					b.WriteString("\n  ... and " + strconv.Itoa(len(g.files)-5) + " more " + g.title)
					break
				}
				b.WriteString("\n  " + g.title + ": " + f)
			}
		}
		return fail("%s %s is already installed with different content; nothing was overwritten%s\nInstall the file as a new version (--as-version X.Y.Z) or as a copy (--as-id other-id)", conflict.ID, conflict.Version, b.String())
	}
	if err != nil {
		return fail("%v", err)
	}
	if c.json {
		raw, _ := json.Marshal(res)
		c.out("%s\n", raw)
		return nil
	}
	for _, w := range res.Warnings {
		c.out("warning: %s\n", w)
	}
	switch {
	case res.Unchanged:
		c.out("%s %s is already installed with this content, nothing changed\n", res.ID, res.Version)
	case o.dryRun:
		c.out("%s %s checks out: %d files, sha256 %s; nothing installed (dry run)\n", res.ID, res.Version, len(res.Manifest.Files), res.Manifest.SHA256)
	default:
		c.out("installed %s %s: %d files, sha256 %s\n  %s\n  served at /kit/%s/%s/\n", res.ID, res.Version, len(res.Manifest.Files), res.Manifest.SHA256,
			filepath.Join(o.dir, res.ID, res.Version), res.ID, res.Version)
	}
	if len(res.Manifest.Restricted) > 0 {
		c.out("  note: %d files have a restricted license (%s); keep them inside the space that may use them\n", len(res.Manifest.Restricted), res.Manifest.Restricted[0].Reason)
	}
	return nil
}

// designLayouts is "design layouts [kit] [--dir DIR]": the slide layouts of the installed kits, with the
// address `design page create ... --layout` takes. It reads the kits folder itself.
func (c *cli) designLayouts(args []string) error {
	o, pos, err := c.kitFlags(args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return usageErr("design layouts takes at most one kit id")
	}
	kit := ""
	if len(pos) == 1 {
		kit = pos[0]
	}
	list := designkit.Layouts(o.dir, kit)
	if c.json {
		raw, _ := json.Marshal(struct {
			Layouts []designkit.Layout `json:"layouts"`
		}{list})
		c.out("%s\n", raw)
		return nil
	}
	if len(list) == 0 {
		c.out("no layouts in the kits installed in %s\n", o.dir)
		return nil
	}
	for _, l := range list {
		c.out("%s\t%s\t%s\t%s\n", l.Address(), l.Mode, l.Purpose, l.Name)
	}
	return nil
}

// kitCheck is "design kit check <folder>": designkit.Check, printed one problem a line, errors first.
func (c *cli) kitCheck(opt kitOptions, pos []string) error {
	if len(pos) != 1 {
		return usageErr("design kit check takes one folder: harnessly design kit check ./dist/kit")
	}
	rep, err := designkit.Check(pos[0])
	if err != nil {
		return fail("%v", err)
	}
	if opt.render && rep.Errors() == 0 {
		items, err := design.RenderKit(pos[0], design.KitRenderOptions{Only: onlySet(opt.only)})
		if err != nil {
			return fail("render: %v", err)
		}
		rep.Problems = append(rep.Problems, renderProblems(items)...)
	} else if opt.render {
		c.out("the render is skipped until the errors above are fixed\n")
	}
	if opt.json {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		c.out("%s\n", raw)
	} else {
		probs := append([]designkit.Problem(nil), rep.Problems...)
		sort.SliceStable(probs, func(i, j int) bool {
			return probs[i].Level == designkit.LevelError && probs[j].Level != designkit.LevelError
		})
		for _, p := range probs {
			c.out("%-7s [%s] %s: %s\n", p.Level, p.Rule, p.File, p.Message)
		}
		c.out("%d themes, %d components: %d errors, %d warnings\n", len(rep.Themes), rep.Components, rep.Errors(), len(rep.Problems)-rep.Errors())
	}
	if rep.Errors() > 0 {
		return fail("the kit has %d errors", rep.Errors())
	}
	return nil
}

// kitRender is "design kit render <folder>": design.RenderKit, one line per component or layout that is not ok.
func (c *cli) kitRender(opt kitOptions, pos []string) error {
	if len(pos) != 1 {
		return usageErr("design kit render takes one folder: harnessly design kit render ./dist/kit --shots")
	}
	items, err := design.RenderKit(pos[0], design.KitRenderOptions{Shots: opt.shots, Only: onlySet(opt.only)})
	if err != nil {
		return fail("%v", err)
	}
	if opt.json {
		raw, _ := json.MarshalIndent(items, "", "  ")
		c.out("%s\n", raw)
	}
	bad, shots := 0, 0
	for _, it := range items {
		if it.Shot != "" {
			shots++
		}
		if it.Status == "ok" {
			continue
		}
		bad++
		if !opt.json {
			c.out("%-6s %s %s\n", it.Status, it.Node, strings.Join(it.Errors, " | "))
		}
	}
	if !opt.json {
		c.out("%d rendered: %d ok, %d not; %d pictures written\n", len(items), len(items)-bad, bad, shots)
	}
	if bad > 0 {
		return fail("%d of %d did not render", bad, len(items))
	}
	return nil
}

func onlySet(list string) map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out[s] = true
		}
	}
	return out
}

// renderProblems turns what did not render into check problems: a script error or a missing file is an error; a
// component that draws nothing standalone is an error too unless the kit marks it a part or a provider.
func renderProblems(items []design.KitRenderItem) []designkit.Problem {
	var out []designkit.Problem
	for _, it := range items {
		switch it.Status {
		case "ok":
		case "blank":
			out = append(out, designkit.Problem{Rule: "render.blank", Level: designkit.LevelError, File: it.Theme + " " + it.Node,
				Message: "draws nothing with its sample and first variant: give it a sample that shows it, or mark it \"part\" (of its parent) or \"provider\""})
		default:
			out = append(out, designkit.Problem{Rule: "render.error", Level: designkit.LevelError, File: it.Theme + " " + it.Node,
				Message: "does not render: " + strings.Join(it.Errors, " | ")})
		}
	}
	return out
}

// kitInit is "design kit init <folder>": a new kit that passes the check, to replace piece by piece.
func (c *cli) kitInit(opt kitOptions, pos []string) error {
	if len(pos) != 1 || opt.id == "" || opt.namespace == "" {
		return usageErr("design kit init <folder> --id <id> --namespace <Namespace> [--title <title>] [--source draft|library]")
	}
	source := opt.source
	if source == "" {
		source = "draft"
	}
	wrote, err := designkit.Init(pos[0], designkit.InitOptions{ID: opt.id, Namespace: opt.namespace, Title: opt.title, Source: source})
	if err != nil {
		return err
	}
	for _, f := range wrote {
		c.out("%s\n", filepath.Join(pos[0], f))
	}
	c.out("\nA %s kit %s in %s. Next: harnessly docs read agents/design-kits; replace the components; harnessly design kit check %s --render\n", source, opt.id, pos[0], pos[0])
	return nil
}
