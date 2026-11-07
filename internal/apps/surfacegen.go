package apps

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// The files written from the surface (surface.go), by `UPDATE_SURFACE=1 go test ./internal/apps -run TestSurfaceFilesAreCurrent`. Paths are from the repository's root.
const (
	SurfaceTSPath        = "web/app/next/features/app-views/surface.gen.ts"
	SurfaceReferencePath = "docs/apps/reference.md"
	SurfaceSchemaPath    = "docs/apps/app.schema.json"
)

const generatedBy = "Generated from internal/apps/surface.go and contributions.go (UPDATE_SURFACE=1 go test ./internal/apps -run TestSurfaceFilesAreCurrent); do not edit."

// SurfaceFiles are the generated files by path.
func SurfaceFiles() map[string][]byte {
	return map[string][]byte{
		SurfaceTSPath:        SurfaceTS(),
		SurfaceReferencePath: SurfaceReference(),
		SurfaceSchemaPath:    SurfaceSchema(),
	}
}

func tsString(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}

// SurfaceTS is the page's table: the right of every bridge method, the rights, the targets and the contribution kinds.
func SurfaceTS() []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// %s\n\n", generatedBy)
	b.WriteString("/** The right each method of the bridge needs; '' is a method that needs none. */\n")
	b.WriteString("export const METHOD_PERMISSION = {\n")
	for _, m := range surfaceMethods {
		fmt.Fprintf(&b, "  %s: %s,\n", tsString(m.Name), tsString(m.Permission))
	}
	b.WriteString("} as const satisfies Record<string, string>;\n\n")
	list := func(doc, name string, items []string) {
		fmt.Fprintf(&b, "/** %s */\nexport const %s = [\n", doc, name)
		for _, s := range items {
			fmt.Fprintf(&b, "  %s,\n", tsString(s))
		}
		b.WriteString("] as const;\n\n")
	}
	list("The rights an app asks for in `permissions`; files.read, files.write and secrets are written right:<value>.",
		"APP_PERMISSIONS", PermissionNames())
	targets := []string{}
	for _, t := range surfaceTargets {
		targets = append(targets, t.Name)
	}
	list("The places an app works in.", "APP_TARGETS", targets)
	kinds := []string{}
	for _, k := range contributionTable {
		kinds = append(kinds, k.Name)
	}
	sinopia := []string{}
	for _, m := range surfaceMethods {
		if rest, ok := strings.CutPrefix(m.Name, "harnessly/sinopia."); ok {
			sinopia = append(sinopia, rest)
		}
	}
	list("The Sinopia methods (the part after `harnessly/sinopia.`).", "SINOPIA_METHODS", sinopia)
	list("The kinds of contribution an app puts into the host.", "CONTRIBUTION_KINDS", kinds)
	b.WriteString("export type TContributionKind = (typeof CONTRIBUTION_KINDS)[number];\n")
	return b.Bytes()
}

func mdCell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// SurfaceReference is docs/apps/reference.md.
func SurfaceReference() []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Apps: reference\n\n<!-- %s -->\n\n", generatedBy)
	b.WriteString("Everything an app can use, from the one table the manifest check and the bridge read. " +
		"An agent gets the same with `harnessly app catalog` or the host tool `catalog`.\n\n")
	b.WriteString("## Targets\n\n| Target | What it is |\n| --- | --- |\n")
	for _, t := range surfaceTargets {
		fmt.Fprintf(&b, "| `%s` | %s |\n", t.Name, mdCell(t.Doc))
	}
	b.WriteString("\n## Contributions\n\n| Kind | Declared in | Shows in | What it is |\n| --- | --- | --- | --- |\n")
	for _, k := range contributionTable {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s |\n", k.Name, k.Field, "`"+strings.Join(k.Targets, "`, `")+"`", mdCell(k.Doc))
	}
	b.WriteString("\n## Permissions\n\n| Right | What it opens | Methods |\n| --- | --- | --- |\n")
	for _, p := range surfacePermissions {
		name := p.Name
		if p.Param != "" {
			name += ":" + p.Param
		}
		doc := mdCell(p.Doc)
		if p.Unsupported {
			doc += " Refused for now: not supported yet."
		}
		methods := methodsNeeding(p.Name)
		cell := "—"
		if len(methods) > 0 {
			cell = "`" + strings.Join(methods, "`, `") + "`"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", name, doc, cell)
	}
	b.WriteString("\n## Bridge methods\n\n| Method | Right | SDK | What it does |\n| --- | --- | --- | --- |\n")
	for _, m := range surfaceMethods {
		right, sdk := "—", "—"
		if m.Permission != "" {
			right = "`" + m.Permission + "`"
		}
		if m.SDK != "" {
			sdk = "`" + mdCell(m.SDK) + "`"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", m.Name, right, sdk, mdCell(m.Doc))
	}
	b.WriteString("\n## Host tools of the agent\n\nThe server `harnessly-app`, in a chat with apps on.\n\n| Tool | What it does |\n| --- | --- |\n")
	for _, t := range surfaceTools {
		fmt.Fprintf(&b, "| `%s` | %s |\n", t.Name, mdCell(t.Doc))
	}
	return b.Bytes()
}

// SurfaceSchema is a JSON Schema of app.json for editors: the fields of the manifest, the targets and the rights. The
// check (Manifest.Check) is still the judge; the schema only helps an author type the right names.
func SurfaceSchema() []byte {
	props := map[string]any{}
	t := reflect.TypeOf(Manifest{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		props[name] = map[string]any{}
	}
	var targets []any
	for _, tg := range surfaceTargets {
		if !tg.Pattern {
			targets = append(targets, map[string]any{"const": tg.Name, "description": tg.Doc})
		}
	}
	targets = append(targets,
		map[string]any{"const": "sinopia.*", "description": "The editor of every Sinopia document type."},
		map[string]any{"pattern": `^sinopia\.[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)*$`, "description": "The editor of one Sinopia document type."})
	var rights []any
	for _, p := range surfacePermissions {
		if p.Unsupported {
			continue
		}
		if p.Param != "" {
			rights = append(rights, map[string]any{"pattern": "^" + regexp.QuoteMeta(p.Name) + ":.+$", "description": p.Doc})
		} else {
			rights = append(rights, map[string]any{"const": p.Name, "description": p.Doc})
		}
	}
	props["id"] = map[string]any{"type": "string", "pattern": idRe.String(), "description": "<publisher>.<name>"}
	props["version"] = map[string]any{"type": "string", "pattern": semverRe.String()}
	props["trust"] = map[string]any{"enum": []string{TrustDeclarative, TrustSandboxed, TrustTrusted}}
	props["targets"] = map[string]any{"type": "array", "minItems": 1, "uniqueItems": true, "items": map[string]any{"anyOf": targets}}
	props["permissions"] = map[string]any{"type": "array", "uniqueItems": true, "items": map[string]any{"anyOf": rights}}
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$comment":             generatedBy,
		"title":                "harnessly/app.json",
		"type":                 "object",
		"required":             []string{"id", "name", "version", "targets", "permissions"},
		"additionalProperties": false,
		"properties":           props,
	}
	raw, _ := json.MarshalIndent(schema, "", "  ")
	return append(raw, '\n')
}
