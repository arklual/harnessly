package hlcli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// harnessly app catalog: the places an app works in, the kinds of contribution, the rights and the bridge methods
// they open, from the same table the manifest check reads (internal/apps/surface.go). An agent writing an app asks
// here instead of guessing names from the docs.
func (c *cli) appCatalog(args []string) error {
	section := ""
	if len(args) > 1 {
		return usageErr("app catalog takes one section: targets, contributions, permissions, methods or tools")
	}
	if len(args) == 1 {
		section = args[0]
	}
	cat := apps.SurfaceCatalog()
	if c.json {
		var v any = cat
		switch section {
		case "":
		case "targets":
			v = cat.Targets
		case "contributions":
			v = cat.Contributions
		case "permissions":
			v = cat.Permissions
		case "methods":
			v = cat.Methods
		case "tools":
			v = cat.Tools
		default:
			return usageErr("no section %q: targets, contributions, permissions, methods or tools", section)
		}
		raw, _ := json.MarshalIndent(v, "", "  ")
		c.out("%s\n", raw)
		return nil
	}
	show := func(name string) bool { return section == "" || section == name }
	known := false
	if show("targets") {
		known = true
		c.out("Targets (manifest `targets`):\n")
		for _, t := range cat.Targets {
			c.out("  %-16s %s\n", t.Name, t.Doc)
		}
		c.out("\n")
	}
	if show("contributions") {
		known = true
		c.out("Contributions:\n")
		for _, k := range cat.Contributions {
			c.out("  %-18s %-20s in %s: %s\n", k.Name, k.Field, strings.Join(k.Targets, ", "), k.Doc)
		}
		c.out("\n")
	}
	if show("permissions") {
		known = true
		c.out("Permissions (manifest `permissions`):\n")
		for _, p := range cat.Permissions {
			name := p.Name
			if p.Param != "" {
				name += ":" + p.Param
			}
			doc := p.Doc
			if p.Unsupported {
				doc += " (refused: not supported yet)"
			}
			c.out("  %-22s %s\n", name, doc)
		}
		c.out("\n")
	}
	if show("methods") {
		known = true
		c.out("Bridge methods (right needed, SDK call):\n")
		for _, m := range cat.Methods {
			need := m.Permission
			if need == "" {
				need = "-"
			}
			c.out("  %-32s %-16s %s\n", m.Name, need, m.SDK)
		}
		c.out("\n")
	}
	if show("tools") {
		known = true
		c.out("Host tools of the agent (server harnessly-app):\n")
		for _, t := range cat.Tools {
			c.out("  %-14s %s\n", t.Name, t.Doc)
		}
	}
	if !known {
		return usageErr("no section %q: targets, contributions, permissions, methods or tools", section)
	}
	return nil
}

// harnessly app sdk DIR: the files a view the app's own server hosts serves itself (delivery mode remote): the page
// loads bridge-shim.js first, then imports app-sdk.js. The host hands the view its nonce in the address's fragment.
func (c *cli) appSDK(args []string) error {
	if len(args) != 1 {
		return usageErr("app sdk needs a folder: harnessly app sdk ./public/hl")
	}
	dir := c.appDir(args[0])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail("%s: %v", dir, err)
	}
	files := apps.SDKFiles()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o644); err != nil {
			return fail("%s: %v", name, err)
		}
	}
	if c.json {
		raw, _ := json.Marshal(map[string]any{"dir": dir, "files": names})
		c.out("%s\n", raw)
		return nil
	}
	c.out("Wrote %s into %s.\nIn the view's page: <script src=\"bridge-shim.js\"></script> first, then\n  import { connect } from './app-sdk.js'; const hl = await connect();\n", strings.Join(names, ", "), dir)
	return nil
}
