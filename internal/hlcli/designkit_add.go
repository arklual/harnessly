package hlcli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/designkit/kitadd"
)

// kitAdd is "design kit add <source> [<folder>]": a library kit from a package in one command.
func (c *cli) kitAdd(opt kitOptions, pos []string) error {
	if len(pos) < 1 || len(pos) > 2 {
		return usageErr("design kit add npm:<package>[@<version>]|shadcn:<registry> [<folder>]")
	}
	src, err := kitadd.ParseSource(pos[0])
	if err != nil {
		return usageErr("%v", err)
	}
	o := kitadd.Options{Source: src, ID: opt.id, Namespace: opt.namespace, Title: opt.title, Version: opt.version,
		Root: opt.root, React: opt.react, Registry: opt.registry, From: opt.from}
	if len(pos) == 2 {
		o.Folder = pos[1]
	}
	if opt.licenseSecret != "" {
		vals, err := c.secretValues(map[string]string{"LICENSE": opt.licenseSecret}, nil)
		if err != nil {
			return err
		}
		if o.License = vals[opt.licenseSecret]; o.License == "" {
			return fail("the safe has no secret %q (harnessly secret ask %s --why \"PrimeUI license for kit add\")", opt.licenseSecret, opt.licenseSecret)
		}
	}
	return c.kitAddRun(opt, o)
}

// kitUpdate is "design kit update <folder>": the kit made again from a newer version of its package.
func (c *cli) kitUpdate(opt kitOptions, pos []string) error {
	if len(pos) != 1 {
		return usageErr("design kit update <folder> [--version V]")
	}
	src, rec, err := kitadd.ReadSource(pos[0])
	if err != nil {
		return fail("%v", err)
	}
	o := kitadd.Options{Source: src, Folder: pos[0], Root: rec.Root, React: rec.React, CSS: rec.CSS, Registry: rec.Registry, From: opt.from}
	if opt.registry != "" {
		o.Registry = opt.registry
	}
	if src.Kind == "npm" {
		o.Source.Version = opt.version // "" = the newest
	} else {
		o.Version = opt.version
	}
	return c.kitAddRun(opt, o)
}

func (c *cli) kitAddRun(opt kitOptions, o kitadd.Options) error {
	if opt.cssFiles != "" {
		o.CSS = strings.Split(opt.cssFiles, ",")
	}
	o.Cwd = c.env.Cwd
	// A package the team's recipes do not know runs in the sandbox (no home, no tokens, no network but npm's); --sandbox
	// asks for it on any, --no-sandbox turns it off.
	if opt.sandbox && opt.noSandbox {
		return usageErr("--sandbox and --no-sandbox together")
	}
	if o.From == "" && !opt.noSandbox && (opt.sandbox || o.Registry != "" || !kitadd.Verified(o.Source)) {
		switch {
		case kitadd.SandboxAvailable():
			o.Sandbox = true
			for _, h := range strings.Split(os.Getenv("HARNESSLY_KIT_REGISTRIES"), ",") {
				if h = strings.TrimSpace(h); h != "" {
					o.AllowedHosts = append(o.AllowedHosts, h)
				}
			}
		case opt.sandbox:
			return fail("--sandbox: this machine cannot make a sandbox (Landlock is off)")
		default:
			fmt.Fprintln(os.Stderr, "warning: no sandbox on this machine (Landlock is off); the package runs as you")
		}
	}
	o.NoDraft = opt.noDraft
	o.DraftOn = opt.draft
	if o.Folder == "" {
		o.Folder = o.ID
		if o.Folder == "" {
			o.Folder = kitadd.DefaultID(o.Source)
		}
	}
	if !filepath.IsAbs(o.Folder) {
		o.Folder = filepath.Join(c.env.Cwd, o.Folder)
	}
	if o.From != "" && !filepath.IsAbs(o.From) {
		o.From = filepath.Join(c.env.Cwd, o.From)
	}
	if !opt.json {
		o.Log = os.Stderr
	}
	rep, err := kitadd.Add(o)
	if err != nil {
		return fail("%v", err)
	}
	var drawn, alone int
	if !opt.noRender {
		// The tokens a stylesheet cannot give (a theme built in JavaScript, calc() and color-mix()) are read from the kit
		// drawn in the browser, before the components are drawn for the check.
		if pr, perr := design.ProbeKit(o.Folder, design.KitProbeOptions{KitsDir: opt.dir}); perr != nil {
			rep.NotCarried = append(rep.NotCarried, "tokens: the browser probe failed: "+firstOf(perr.Error()))
		} else {
			vars := make([]kitadd.ProbeVar, 0, len(pr.Vars))
			for _, v := range pr.Vars {
				vars = append(vars, kitadd.ProbeVar{Name: v.Name, Value: v.Value, Dark: v.Dark})
			}
			if n, aerr := kitadd.ApplyProbe(o.Folder, o.Title, vars); aerr != nil {
				rep.NotCarried = append(rep.NotCarried, "tokens: "+firstOf(aerr.Error()))
			} else if n > 0 {
				rep.Tokens += n
				kept := rep.NotCarried[:0]
				for _, l := range rep.NotCarried {
					if !strings.HasPrefix(l, "tokens: no CSS custom properties") && !strings.Contains(l, "defines no variables under") {
						kept = append(kept, l)
					}
				}
				rep.NotCarried = kept
			}
		}
		items, rerr := design.RenderKit(o.Folder, design.KitRenderOptions{KitsDir: opt.dir})
		if rerr != nil {
			rep.NotCarried = append(rep.NotCarried, "render: "+rerr.Error()+" (run `kit check --render` where Chrome is)")
		} else {
			failed := map[string]string{}
			for _, it := range items {
				switch it.Status {
				case "ok":
					drawn++
				case "blank":
					failed[it.ID] = "it draws nothing alone"
				default:
					why := "it does not render alone"
					if len(it.Errors) > 0 {
						why = firstOf(it.Errors[0])
					}
					failed[it.ID] = why
				}
			}
			// A component that draws nothing or throws alone gets up to three more samples (opened, with a text, with entries).
			for attempt := 1; attempt <= 3 && len(failed) > 0; attempt++ {
				var ids []string
				for id := range failed {
					ids = append(ids, id)
				}
				changed, terr := kitadd.RetrySamples(o.Folder, ids, attempt)
				if terr != nil || len(changed) == 0 {
					continue
				}
				only := map[string]bool{}
				for _, id := range changed {
					only[id] = true
				}
				again, aerr := design.RenderKit(o.Folder, design.KitRenderOptions{KitsDir: opt.dir, Only: only})
				if aerr != nil {
					break
				}
				for _, it := range again {
					if it.Status == "ok" && only[it.ID] {
						delete(failed, it.ID)
						drawn++
					}
				}
			}
			alone = len(failed)
			ns, root := kitIdentity(o.Folder)
			if err := kitadd.DropSamples(o.Folder, ns, root, failed); err != nil {
				return fail("%v", err)
			}
		}
	}
	chk, err := designkit.CheckIn(o.Folder, opt.dir)
	if err != nil {
		return fail("%v", err)
	}
	if opt.json || c.json {
		raw, _ := json.MarshalIndent(map[string]any{"report": rep, "check": chk, "drawn": drawn, "drawnOnlyInside": alone}, "", "  ")
		c.out("%s\n", raw)
	} else {
		c.printAddReport(rep, chk, drawn, alone, !opt.noRender)
	}
	if chk.Errors() > 0 {
		return fail("the kit has %d errors", chk.Errors())
	}
	return nil
}

func firstOf(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

func kitIdentity(folder string) (ns, root string) {
	raw, err := os.ReadFile(filepath.Join(folder, designkit.ManifestFile))
	if err != nil {
		return "", ""
	}
	var d struct {
		Namespace string `json:"namespace"`
		Kit       struct {
			Root string `json:"root"`
		} `json:"kit"`
	}
	_ = json.Unmarshal(raw, &d)
	return d.Namespace, d.Kit.Root
}

func (c *cli) printAddReport(rep *kitadd.Report, chk *designkit.CheckReport, drawn, alone int, rendered bool) {
	what := "made"
	if rep.Previous != "" {
		what = "updated " + rep.Previous + " →"
	}
	c.out("%s %s %s from %s in %s\n", what, rep.Package, rep.Version, rep.Source, rep.Folder)
	c.out("  %d components, %d parts, %d providers, %d tokens\n", rep.Components, rep.Parts, rep.Providers, rep.Tokens)
	if rendered {
		c.out("  drawn alone: %d; drawn only inside an application (no sample, listed in README.md): %d\n", drawn, alone)
	}
	if rep.Previous != "" {
		if len(rep.Added)+len(rep.Removed)+len(rep.Changed) == 0 {
			c.out("changes: none in components and props\n")
		} else {
			c.out("changes:\n")
		}
		for _, id := range rep.Added {
			c.out("  + %s\n", id)
		}
		for _, id := range rep.Removed {
			c.out("  - %s\n", id)
		}
		for _, ch := range rep.Changed {
			var parts []string
			for _, p := range ch.Added {
				parts = append(parts, "+"+p)
			}
			for _, p := range ch.Removed {
				parts = append(parts, "-"+p)
			}
			for _, p := range ch.Changed {
				parts = append(parts, "~"+p)
			}
			c.out("  ~ %s: %s\n", ch.Component, strings.Join(parts, ", "))
		}
	}
	if len(rep.NotCarried) > 0 {
		c.out("not read automatically:\n")
		for _, l := range rep.NotCarried {
			c.out("  - %s\n", l)
		}
	}
	if len(rep.ToWrite) > 0 {
		c.out("to write:\n")
		for _, l := range rep.ToWrite {
			c.out("  - %s\n", l)
		}
	}
	c.out("check: %d errors, %d warnings", chk.Errors(), len(chk.Problems)-chk.Errors())
	if chk.Errors() > 0 {
		c.out(" — see harnessly design kit check %s", rep.Folder)
	}
	c.out("\n")
	c.readiness(chk, false)
}
