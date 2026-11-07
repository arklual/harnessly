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
	b.WriteString("/** The class of risk of every right: it orders the install screen, it forbids nothing. */\n")
	b.WriteString("export const PERMISSION_CLASS = {\n")
	for _, p := range surfacePermissions {
		fmt.Fprintf(&b, "  %s: %s,\n", tsString(p.Name), tsString(p.Class))
	}
	b.WriteString("} as const satisfies Record<string, 'critical' | 'high' | 'medium' | 'low'>;\n\n")
	b.WriteString("/** The methods that still work but are deprecated, with what to call instead. */\n")
	b.WriteString("export const DEPRECATED_METHODS: Readonly<Record<string, string>> = {\n")
	for _, m := range surfaceMethods {
		if m.Deprecated != "" {
			fmt.Fprintf(&b, "  %s: %s,\n", tsString(m.Name), tsString(m.Deprecated))
		}
	}
	b.WriteString("};\n\n")
	b.WriteString("/** The rights written with a value (files.read:<mask>, events:<kind>): held when any value is listed. */\n")
	b.WriteString("export const PARAM_PERMISSIONS: ReadonlySet<string> = new Set([")
	first := true
	for _, p := range surfacePermissions {
		if p.Param == "" {
			continue
		}
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString(tsString(p.Name))
	}
	b.WriteString("]);\n\n")
	b.WriteString("/** The versions of the bridge's protocol the host speaks; a view names its own in ui/initialize. */\nexport const SDK_PROTOCOLS = [")
	for i, v := range SDKProtocols {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d", v)
	}
	b.WriteString("] as const;\n\n")
	targets := []string{}
	for _, t := range allTargets() {
		targets = append(targets, t.Name)
	}
	list("The places an app works in; the last ones are aliases of the builtin apps' screens (sinopia.<kind>).", "APP_TARGETS", targets)
	kinds := []string{}
	for _, k := range contributionKinds() {
		kinds = append(kinds, k.Name)
	}
	sinopia := []string{}
	for _, m := range surfaceMethods {
		if rest, ok := strings.CutPrefix(m.Name, "harnessly/sinopia."); ok {
			sinopia = append(sinopia, rest)
		}
	}
	list("The Sinopia methods (the part after `harnessly/sinopia.`).", "SINOPIA_METHODS", sinopia)
	list("The kinds of contribution an app puts into the host: the host's own, then the builtin apps' points.", "CONTRIBUTION_KINDS", kinds)
	b.WriteString("export type TContributionKind = (typeof CONTRIBUTION_KINDS)[number];\n\n")
	b.WriteString("/** The host's own apps (internal/apps/builtin): they run in the page, through the same bridge and rights. */\n")
	b.WriteString("export const BUILTIN_APPS = [\n")
	for _, m := range Builtins() {
		quoted := func(items []string) string {
			parts := make([]string, len(items))
			for i, it := range items {
				parts[i] = tsString(it)
			}
			return "[" + strings.Join(parts, ", ") + "]"
		}
		var points, aliases []string
		for _, p := range m.Points {
			points = append(points, p.Name)
		}
		for _, a := range m.Aliases {
			aliases = append(aliases, a.Name)
		}
		fmt.Fprintf(&b, "  {\n    id: %s,\n    name: %s,\n    version: %s,\n    targets: %s,\n    permissions: %s,\n    points: %s,\n    aliases: %s,\n  },\n",
			tsString(m.ID), tsString(m.Name), tsString(m.Version), quoted(m.Targets), quoted(m.Permissions), quoted(points), quoted(aliases))
	}
	b.WriteString("] as const;\n")
	return b.Bytes()
}

func mdCell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// SurfaceReference is docs/apps/reference.md.
func SurfaceReference() []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Apps: reference\n\n<!-- %s -->\n\n", generatedBy)
	b.WriteString("Everything an app can use, from the one table the manifest check and the bridge read. " +
		"An agent gets the same with `harnessly app catalog` or the host tool `catalog`.\n\n")
	owned := func(doc, owner, what string) string {
		if owner == "" {
			return mdCell(doc)
		}
		return mdCell(doc) + " " + what + " of `" + owner + "`."
	}
	b.WriteString("## Targets\n\n| Target | What it is |\n| --- | --- |\n")
	for _, t := range allTargets() {
		fmt.Fprintf(&b, "| `%s` | %s |\n", t.Name, owned(t.Doc, t.Owner, "An alias of the screen"))
	}
	b.WriteString("\n## Contributions\n\nThe host's own kinds, then the points the builtin apps declare.\n\n" +
		"| Kind | Declared in | Shows in | What it is |\n| --- | --- | --- | --- |\n")
	for _, k := range contributionKinds() {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s |\n", k.Name, k.Field, "`"+strings.Join(k.Targets, "`, `")+"`", owned(k.Doc, k.Owner, "A point"))
	}
	b.WriteString("\n## Builtin apps\n\nApps of the host's own: they run in the host's page and reach the host through the same bridge, " +
		"with rights from the same list. Their points take contributions from any app.\n\n| App | Rights | Points | Aliases |\n| --- | --- | --- | --- |\n")
	for _, m := range Builtins() {
		var points, aliases []string
		for _, p := range m.Points {
			points = append(points, p.Name)
		}
		for _, a := range m.Aliases {
			aliases = append(aliases, a.Name)
		}
		fmt.Fprintf(&b, "| `%s` (%s) | `%s` | `%s` | `%s` |\n", m.ID, mdCell(m.Name), strings.Join(m.Permissions, "`, `"),
			strings.Join(points, "`, `"), strings.Join(aliases, "`, `"))
	}
	b.WriteString("\n## Permissions\n\nEvery right has a class of risk. The class forbids nothing: it orders the install screen, from critical down, and a " +
		"high or critical right is shown with what it can do to the person and journals what it does.\n\n" +
		"| Right | Class | What it opens | Methods |\n| --- | --- | --- | --- |\n")
	for _, p := range surfacePermissions {
		name := p.Name
		if p.Param != "" && p.Alone.Ru != "" {
			name += "[:" + p.Param + "]"
		} else if p.Param != "" {
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
		class := p.Class
		if p.ParamClass != "" {
			class += ", " + p.ParamClass + " with " + p.Param
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", name, class, doc, cell)
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
		doc := mdCell(m.Doc)
		if m.Deprecated != "" {
			doc += " Deprecated: works, use " + mdCell(m.Deprecated) + "."
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", m.Name, right, sdk, doc)
	}
	b.WriteString("\n## Host tools of the agent\n\nThe server `harnessly-app`, in a chat with apps on. The last ones are the builtin apps' backend tools.\n\n| Tool | What it does |\n| --- | --- |\n")
	for _, t := range allTools() {
		fmt.Fprintf(&b, "| `%s` | %s |\n", t.Name, owned(t.Doc, t.Owner, "Of"))
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
		// The fields of the host's own apps are not an author's.
		if name == "delivery" || name == "points" || name == "aliases" {
			continue
		}
		props[name] = map[string]any{}
	}
	var targets []any
	for _, tg := range surfaceTargets {
		if !tg.Pattern {
			targets = append(targets, map[string]any{"const": tg.Name, "description": tg.Doc})
		}
	}
	for _, m := range Builtins() {
		for _, a := range m.Aliases {
			prefix := a.Name[:strings.LastIndex(a.Name, ".")]
			if strings.HasSuffix(a.Name, ".*") {
				targets = append(targets, map[string]any{"const": a.Name, "description": a.Doc})
			} else {
				targets = append(targets, map[string]any{"pattern": "^" + regexp.QuoteMeta(prefix) + `\.[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)*$`, "description": a.Doc})
			}
		}
	}
	var rights []any
	for _, p := range surfacePermissions {
		if p.Unsupported {
			continue
		}
		if p.Param != "" {
			rights = append(rights, map[string]any{"pattern": "^" + regexp.QuoteMeta(p.Name) + ":.+$", "description": p.Doc})
			if p.Alone.Ru != "" {
				rights = append(rights, map[string]any{"const": p.Name, "description": p.Doc})
			}
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
