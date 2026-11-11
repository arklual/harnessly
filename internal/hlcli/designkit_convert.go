package hlcli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/designkit/formats"
)

// harnessly design kit convert: a kit's tokens and component drafts to and from other people's formats (DTCG, CSS
// variables, Tailwind, DESIGN.md, Storybook, Figma variables). The kit's own format is the only one inside; the
// others are read and written here, at the border (internal/designkit/formats), and every run says what it did not carry.

var convertTo = map[string]bool{"dtcg": true, "css": true, "tailwind": true, "tailwind3": true, "tailwind4": true, "design-md": true}
var convertFrom = map[string]bool{"dtcg": true, "css": true, "design-md": true, "storybook": true, "figma": true}

func (c *cli) kitConvert(o kitOptions, pos []string) error {
	if (o.to == "") == (o.from == "") {
		return usageErr("design kit convert <folder> --to dtcg|css|tailwind|tailwind4|design-md [--out FILE]   or   --from dtcg|css|design-md|storybook|figma <source>")
	}
	if o.to != "" {
		if len(pos) != 1 {
			return usageErr("design kit convert <folder> --to %s takes one folder", o.to)
		}
		if !convertTo[o.to] {
			return usageErr("--to %q: one of dtcg, css, tailwind (v3), tailwind4, design-md", o.to)
		}
		return c.kitConvertTo(o, pos[0])
	}
	if len(pos) != 2 {
		return usageErr("design kit convert <folder> --from %s <source>: the kit folder, then the file (storybook: the build folder or its index.json)", o.from)
	}
	if !convertFrom[o.from] {
		return usageErr("--from %q: one of dtcg, css, design-md, storybook, figma", o.from)
	}
	return c.kitConvertFrom(o, pos[0], pos[1])
}

func (c *cli) kitConvertTo(o kitOptions, folder string) error {
	kit, err := formats.OpenKit(c.abs(folder))
	if err != nil {
		return fail("%v", err)
	}
	out, rep, err := kit.Export(o.to)
	if err != nil {
		return fail("%v", err)
	}
	if o.out != "" && o.out != "-" {
		target := c.abs(o.out)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fail("%v", err)
		}
		if err := os.WriteFile(target, out, 0o644); err != nil {
			return fail("%v", err)
		}
		rep.Wrote = append(rep.Wrote, o.out)
		return c.convertReport(o, rep, nil)
	}
	c.out("%s", out)
	if !strings.HasSuffix(string(out), "\n") {
		c.out("\n")
	}
	fmt.Fprint(c.errOut(), reportText(o, rep))
	return nil
}

func (c *cli) kitConvertFrom(o kitOptions, folder, source string) error {
	kit, err := formats.OpenKit(c.abs(folder))
	if err != nil {
		return fail("%v", err)
	}
	src := source
	if !filepath.IsAbs(src) {
		src = filepath.Join(c.env.Cwd, src)
	}
	rep, err := kit.Import(o.from, src, formats.ImportOptions{Mode: o.mode, DryRun: o.dryRun, CSS: o.css})
	if err != nil {
		return fail("%v", err)
	}
	check, cerr := designkit.Check(kit.Dir)
	return c.convertReport(o, rep, pickCheck(check, cerr))
}

func (c *cli) abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.env.Cwd, p)
}

func pickCheck(r *designkit.CheckReport, err error) *designkit.CheckReport {
	if err != nil {
		return nil
	}
	return r
}

func reportText(o kitOptions, rep *formats.Report) string {
	if o.json {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		return string(raw) + "\n"
	}
	return rep.Lines()
}

// convertReport prints what the conversion did and, after an import, what `kit check` still says about the kit.
func (c *cli) convertReport(o kitOptions, rep *formats.Report, check *designkit.CheckReport) error {
	if o.json || c.json {
		payload := map[string]any{"report": rep}
		if check != nil {
			payload["check"] = map[string]any{"errors": check.Errors(), "problems": check.Problems}
		}
		raw, _ := json.MarshalIndent(payload, "", "  ")
		c.out("%s\n", raw)
		return nil
	}
	c.out("%s", rep.Lines())
	if check == nil {
		return nil
	}
	probs := append([]designkit.Problem(nil), check.Problems...)
	sort.SliceStable(probs, func(i, j int) bool {
		return probs[i].Level == designkit.LevelError && probs[j].Level != designkit.LevelError
	})
	if len(probs) == 0 {
		c.out("kit check: clean\n")
		return nil
	}
	c.out("kit check: %d errors, %d warnings", check.Errors(), len(probs)-check.Errors())
	if check.Errors() > 0 {
		c.out(" — write these before the kit is fit to pack:")
	}
	c.out("\n")
	shown := 0
	for _, p := range probs {
		if shown >= 20 {
			c.out("  … and %d more: harnessly design kit check <folder>\n", len(probs)-shown)
			break
		}
		c.out("  %-7s [%s] %s: %s\n", p.Level, p.Rule, p.File, p.Message)
		shown++
	}
	return nil
}
