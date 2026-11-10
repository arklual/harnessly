package designkit

// Rule is one thing `harnessly design kit check` looks at. The registry is the only list of them: the check names
// a problem by its rule, and the docs page agents/design-kits lists every rule — a test fails when the two differ, so
// a rule the docs do not explain cannot exist, and the docs cannot promise a rule the check does not run.
type Rule struct {
	ID    string
	Level string
	// What the rule wants, in one line: the docs table shows it.
	Want string
}

// Rules are the static rules (check.go) and the render rules (`kit check --render`, `kit render`).
var Rules = []Rule{
	{"kitjson.json", LevelError, "kit.json is valid JSON"},
	{"kitjson.theme", LevelError, "each theme in kit.json has a plain id and a folder inside the kit with its design-system.json"},
	{"kitjson.empty", LevelError, "kit.json lists at least one theme (a one-theme kit needs no kit.json)"},
	{"index.missing", LevelError, "every theme has design-system.json"},
	{"index.json", LevelError, "design-system.json is valid JSON"},
	{"index.title", LevelWarning, "the theme has a title"},
	{"index.namespace", LevelError, "namespace is a JavaScript name: boards write <Namespace>.<Component>"},
	{"index.section", LevelWarning, "sections are only foundation, components, templates, assets"},
	{"index.empty", LevelWarning, "the theme has groups, assetGroups or templates"},
	{"identity.missing", LevelError, "one member of design-system.json is the kit record {id, version, source}"},
	{"identity.id", LevelError, "the kit id is lower case letters, digits and -"},
	{"identity.version", LevelError, "the version is major.minor.patch"},
	{"identity.source", LevelWarning, "source is library or draft"},
	{"identity.kind", LevelError, "the kit record's kind, when set, is app, site, slides or video"},
	{"identity.extends", LevelError, "extends, when set, is <id>@<range> with a valid id and version range"},
	{"identity.omitted", LevelError, "omitted, when set, lists only tokens, examples, docs, patterns, templates"},
	{"code.shape", LevelError, "a component's code, when set, is {import, export} with plain strings"},
	{"preset.shape", LevelError, "a component's preset, when set, is {of, props}"},
	{"preset.target", LevelError, "a preset's of is another component of the theme, not a preset itself"},
	{"preset.props", LevelError, "a preset's props are simple values the target component declares"},
	{"status.value", LevelError, "a component's status, when set, is stable, new or deprecated"},
	{"status.replaced-by", LevelError, "replacedBy, when set, names another component and goes with status deprecated"},
	{"token.var", LevelError, "a token's var, when set, is a CSS custom property name --like-this"},
	{"readme.missing", LevelError, "the theme has README.md"},
	{"readme.consuming", LevelWarning, "README.md has a Consuming section"},
	{"groups.names", LevelError, "groups are component groups, not a claude.ai list of names"},
	{"groups.shape", LevelError, "groups is [{id, title, components}]"},
	{"groups.id", LevelError, "every group has an id"},
	{"component.id", LevelError, "a component id is its export name in the namespace"},
	{"component.twice", LevelError, "a component is listed once"},
	{"component.description", LevelError, "every component has a description: what it is for"},
	{"component.description-short", LevelWarning, "a description has five words or more"},
	{"component.description-same", LevelWarning, "no two components share a description"},
	{"component.thumb", LevelError, "a component's thumb is a file of the kit"},
	{"component.preview", LevelError, "a component's preview, when set, is a file of the kit"},
	{"component.dotted", LevelWarning, "a component drawn live has a one-word id (a dotted one is a part)"},
	{"component.no-sample", LevelWarning, "a component that is not a part has a sample or variants"},
	{"prop.name", LevelError, "every prop has a name"},
	{"prop.type", LevelError, "a prop's type is one of enum, boolean, number, string, color, token, node, function, object, array"},
	{"prop.enum", LevelError, "an enum prop lists its values"},
	{"prop.description", LevelWarning, "a prop says what it changes on screen"},
	{"variant.id", LevelWarning, "every variant has an id"},
	{"sample.undeclared", LevelError, "a sample, a variant or an element sets only props its component declares"},
	{"sample.not-passed", LevelWarning, "a sample sets no prop the live preview never passes (handlers, styles, links, dashed names), unless it is required"},
	{"sample.enum", LevelError, "a sample sets an enum prop only to one of its values"},
	{"sample.required", LevelError, "every required prop has a value in the sample or the first variant"},
	{"sample.depth", LevelError, "elements nest at most 12 deep (lists and objects between them do not count)"},
	{"element.function", LevelError, `a function value is {"$fn": ""} (does nothing) or {"$fn": "<field>"} (returns its argument's field)`},
	{"element.id", LevelError, `an element's "$c" is a component id`},
	{"element.unknown", LevelError, "an element names a component of the kit"},
	{"part.shape", LevelError, `part is "provider", the parent's id or {"of": parent}`},
	{"part.self", LevelError, "a component is not a part of itself"},
	{"part.parent", LevelError, "a part's parent is a component of the kit"},
	{"part.nested", LevelError, "a part's parent is not itself a part"},
	{"part.unused", LevelWarning, "the parent's sample or variants use the part, so the part has a picture"},
	{"keywords.shape", LevelError, "keywords is a list of strings"},
	{"card.missing", LevelError, "every component has components/<Comp>/README.md"},
	{"card.summary", LevelError, "a card starts with a sentence about what the component is for"},
	{"card.summary-differs", LevelWarning, "the card's first sentence is the component's description"},
	{"card.when-not", LevelWarning, "a card has «When not to use»"},
	{"card.example", LevelWarning, "a card has an <x-import component-from-global-scope=…> example"},
	{"assets.shape", LevelError, "assetGroups is {id: {title, kind, items: [{name, file}]}}"},
	{"assets.items", LevelError, "an asset group lists its items"},
	{"assets.kind", LevelWarning, "an asset group's kind is icon, image or logo"},
	{"assets.files", LevelError, "every asset has a name and a file of the kit"},
	{"templates.shape", LevelError, "templates is a list of templates"},
	{"templates.id", LevelError, "every template has an id"},
	{"templates.empty", LevelError, "every template has layouts"},
	{"layout.id", LevelError, "every layout has an id"},
	{"layout.purpose", LevelWarning, "every layout says what page it is for"},
	{"layout.file", LevelError, "a layout's board and thumb are files of the kit"},
	{"layout.thumb", LevelWarning, "every layout has a thumb"},
	{"tokens.missing", LevelError, "the theme has tokens.json"},
	{"tokens.json", LevelError, "tokens.json is valid JSON"},
	{"tokens.dtcg", LevelError, "tokens.json is lists, not a W3C/DTCG name→$value map"},
	{"tokens.family", LevelError, `each family is {"tokens": [{name, value, usage}]}`},
	{"tokens.name", LevelError, "a token name is letters, digits, _ . -"},
	{"tokens.duplicate", LevelError, "a token name is used once across families"},
	{"tokens.usage", LevelWarning, "every token says what it is for"},
	{"tokens.font", LevelError, "a font file in type.fonts is a file of the kit"},
	{"tokens.color-shape", LevelError, "a colour value is a string or {mode: value}"},
	{"tokens.mode", LevelWarning, "a colour has a value in every mode"},
	{"tokens.color-value", LevelError, "a colour is hex, rgb(), hsl() or oklch() — no var(), color-mix() or names"},
	{"bundle.at-root", LevelError, "the bundle is components/bundle.js, not bundle.js"},
	{"bundle.missing", LevelError, "a kit with components has components/bundle.js"},
	{"bundle.script-tag", LevelError, "the bundle contains no </script"},
	{"bundle.namespace", LevelWarning, "the bundle assigns window.<Namespace>"},
	{"bundle.own-react", LevelError, "the bundle takes React from window.React, it does not carry its own"},
	{"bundle.module", LevelError, "the bundle is one classic script, not a module"},
	{"css.missing", LevelWarning, "a kit with components has components/bundle.css"},
	{"css.global", LevelError, "rules on html, body, * sit inside @layer kit-<id>"},
	{"render.error", LevelError, "a component or a layout draws without an exception, a console error or a failed request (--render)"},
	{"render.blank", LevelError, "a component or a layout draws something visible (--render; a provider or a part is marked as one)"},
}

var rulesByID = func() map[string]Rule {
	m := make(map[string]Rule, len(Rules))
	for _, r := range Rules {
		m[r.ID] = r
	}
	return m
}()

// RuleOf finds a rule by id.
func RuleOf(id string) (Rule, bool) {
	r, ok := rulesByID[id]
	return r, ok
}
