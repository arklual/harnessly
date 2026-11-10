# Design kits for Sinopia

A **design kit** is a design system packed so that Sinopia boards can use its real components, its tokens and its
assets, and so that the kit screen (tree, canvas, panel, search) can show it. This page is the contract: what a kit
folder holds, what each field means, how a kit is made, checked, installed and read. The code that reads it is
`internal/designkit/` (format, check, install, canvas, search) and `internal/design/kit_*.go` (serving, live
previews, rendering). The checks below are generated from the same registry the check runs (`designkit.Rules`); a test
fails when this page and the code differ, so when something here looks wrong, say so — do not work around it.

The agent's side is the `design-kit` skill: the order of commands. This page is the data.

## Two kinds of kit, one format

| | Library kit | Draft kit |
| --- | --- | --- |
| What for | prototype pages that become production code | try an idea before any library exists |
| Source of truth | a React package (`@acme/ui`): its exports, types, tokens | the kit folder itself |
| Components | the package's own, built into one browser script | small components written with `React.createElement`, no build |
| Props | from the package's types; the card names the real import | written by hand, named the way real libraries name them |
| Tokens | the package's real CSS variables, resolved to values | a small set: colours in light and dark, type, spacing, radius |
| Where it lives | built and published by the package's CI, by version | in the project as `ds/<folder>/`; installed by version when shared |
| Changes | a new package version, then a new kit version; a version never changes | edited in place, checked after every change |

Both use the same files, so an agent, a board, the kit screen and the search never need to know which kind they read.

## Start from a valid kit

```sh
harnessly design kit init <folder> --id acme-ui --namespace Acme --title "Acme UI" --source draft   # or library
harnessly design kit check <folder> --render
```

`init` writes every file below with a provider (`Root`, light and dark), a plain component (`Button`) and a compound
one (`Card` with its parts `CardHeader`, `CardBody`, drawn by elements in Card's sample). It passes the check with no
problem. Replace its components with yours one by one and run the check after each change: a problem then has one
cause.

## The folder

A kit version is one folder. Its base is Claude Design's **Design System** format; harnessly adds the kit's identity,
the canvas fields, `kit.json` for several themes, and `_harnessly/` for what only harnessly reads.

```
<kit folder>/                      one theme: everything at the root; several: kit.json + one folder per theme
  design-system.json               the index: identity, namespace, canvas fields                 REQUIRED
  README.md                        what the kit is, rules, the Consuming section, the index       REQUIRED
  tokens.json                      tokens as lists (Claude Design's shape)                        REQUIRED
  tokens.css                       the same tokens as CSS variables, light and dark
  fonts.css, fonts/<file>          @font-face and the font files
  components/bundle.js             ONE classic script that sets window.<Namespace>                REQUIRED with components
  components/bundle.css            the components' styles, inside @layer kit-<id>
  components/<Comp>/README.md      one card per component (see "Component cards")               REQUIRED per component
  assets/<group>/<file>            icons, logos, pictures
  _harnessly/thumbs/components/    component pictures (`kit render --shots` writes them)
  _harnessly/thumbs/layouts/       layout pictures (the same)
  _harnessly/layouts/<id>.dc.html  page templates as boards
  kit.json                         only for a kit with several themes
```

Paths are relative, without `..` or a leading `/`. A board and the live preview load, in this order, `tokens.css`,
`fonts.css`, `components/bundle.css`, `components/bundle.js` from the theme folder — nothing else, and only from
there: a script at the root (`bundle.js`) is not loaded. `components/<Comp>/preview.html` (Claude Design's preview) may
stay in a kit; harnessly does not read it.

### Several themes: `kit.json`

```json
{ "title": "Acme UI",
  "themes": [ { "id": "light", "folder": "themes/light", "namespace": "Acme", "title": "Light" },
              { "id": "brand", "folder": "themes/brand", "namespace": "AcmeBrand", "title": "Brand" } ] }
```

Each `folder` is a full theme folder. Without `kit.json` the version root is the only theme, served under the kit's
id. A theme is another look (a second brand); light and dark of one look are **modes** of one theme's tokens.

## `design-system.json`

```json
{
  "v": 3,
  "title": "Acme UI",
  "namespace": "Acme",
  "libraries": [],
  "kit": { "id": "acme-ui", "version": "1.4.0", "source": "library",
           "package": "@acme/ui", "root": "Root", "cssLayer": "kit-acme-ui", "tokenPrefix": "--acme-" },
  "sections": { "foundation": { "title": "Foundation" }, "components": { "title": "Components" },
                "templates": { "title": "Page templates" }, "assets": { "title": "Assets" } },
  "groups": [],
  "assetGroups": {},
  "templates": []
}
```

| Field | Meaning |
| --- | --- |
| `v` | `3`. |
| `title` | the kit's name on the screen. |
| `namespace` | the global the bundle sets: `window.Acme`; boards write `Acme.Button`. A JS identifier. |
| `libraries` | Claude Design's list of what previews load; `[]` for a bundle that takes React from the page. |
| `kit` | the kit's record. `id` (lower case, digits, `-`) and `version` (`major.minor.patch`) are what install checks: an archive whose record says another id or version is refused. `source`: `library` or `draft`. `package`: the npm name. `root`: the provider every board wraps its content in, and the live preview wraps each component in. `cssLayer`, `tokenPrefix`: see "Bundle rules". |
| `sections` | titles of the tree's sections; ids and order fixed: `foundation`, `components`, `templates`, `assets`. |
| `groups` | the components, by group (below). |
| `assetGroups` | icons, logos, pictures (below). |
| `templates` | page templates (below). |

**Name clash with Claude Design.** In a claude.ai design system's own index `groups` is a list of asset group *names*
(`["Logos", "Icons"]`) and `assetGroups` keeps uploads by blob id. In a kit both are the canvas shapes below; an index
copied from claude.ai must be rewritten.

### `groups`: components

```json
{ "groups": [
  { "id": "overlays", "title": "Overlays",
    "components": [
      { "id": "Tooltip", "name": "Tooltip",
        "description": "A short hint shown on hover or focus over the element it explains.",
        "keywords": ["hint", "help text", "подсказка"],
        "props": [
          { "name": "content", "type": "string", "required": true, "description": "the hint, one line" },
          { "name": "side", "type": "enum", "values": ["top", "bottom"], "description": "where it opens" },
          { "name": "open", "type": "boolean", "description": "shown without hover (previews use it)" },
          { "name": "children", "type": "node", "required": true, "description": "the element it explains" }
        ],
        "variants": [ { "id": "top", "name": "Top", "props": { "side": "top" } } ],
        "sample": { "content": "Tickets sold in the last 7 days", "open": true,
                    "children": { "$c": "Button", "props": { "variant": "secondary" }, "children": "Sold" } },
        "thumb": "_harnessly/thumbs/components/Tooltip.png" },
      { "id": "DialogBody", "name": "DialogBody", "part": "Dialog",
        "description": "The scrolling content of a Dialog between its header and its footer.",
        "props": [ { "name": "children", "type": "node", "required": true, "description": "the content" } ] },
      { "id": "TooltipProvider", "name": "TooltipProvider", "part": "provider",
        "description": "Sets the delay of every tooltip below it; wrap the page once.",
        "props": [ { "name": "delay", "type": "number", "description": "milliseconds before a tooltip opens" } ] }
    ] } ] }
```

| Field | Meaning |
| --- | --- |
| `id` | the export's name in the namespace: a board writes `<x-import component-from-global-scope="Acme.Tooltip">`. Dotted ids (`Field.TextInput`) work on boards; the live preview draws one-word ids only. |
| `name` | the title on the screen. |
| `description` | **what it is for and when to take it**, one sentence. The search, the panel and an agent choosing a component read it. «A tooltip» is not a description. |
| `keywords` | words a person would search by that the description does not say (synonyms, the other language, the task: «confirm deletion»). |
| `props[]` | `name`, `type` (`enum`, `boolean`, `number`, `string`, `color`, `token`, `node`, `function`, `object`, `array`), `values` for an enum, `required`, `description` (what it changes on screen). The panel draws a field per prop. |
| `variants[]` | named sets of props the panel offers. |
| `sample` | props that draw it with sensible content. |
| `part` | `"provider"`: it only configures what is under it — no picture, listed under Providers. The parent's id (or `{"of": "Dialog"}`): a piece of a compound component — shown in the parent's panel, drawn inside the parent's sample, never alone. |
| `thumb` | its picture on the canvas; `kit render --shots` writes the file and this field. |

Every component a board may use is listed. Hooks, functions and contexts are not components; the README names them
under «not in the kit».

### How the live preview and the pictures draw a component

The kit screen's live preview (`/kitv/<id>/<version>/<theme>/_play.dc.html?c=<Comp>&p=<props>`) and
`harnessly design kit render` draw the same page, so a picture that renders is the preview that renders:

1. It loads `tokens.css`, `fonts.css`, `components/bundle.css`, `components/bundle.js` and wraps the component in
   `kit.root`.
2. It passes the **sample with the first variant over it** (the panel: the sample with the chosen props over it).
3. It passes only props the component **declares**, of a **plain name** — never handlers (`on…`), `style`, `class`,
   `href`, `src`, `action`, `key`, `ref` or dashed names (`aria-label`): the props come from an address, so they can
   only be data. A required prop of that kind (a link's `href`) may stay in the sample for the card; the picture draws
   without it.
4. A value that is another component of the kit is an **element**:
   `{"$c": "Button", "props": {"variant": "secondary"}, "children": "Sold"}`. `children` is a value, an element or a
   list of them; elements nest up to 12 deep (lists and objects between them do not count); the element's props follow
   rule 3. This is how a tooltip gets its trigger, a dialog its parts, a button its icon
   (`{"$c": "Icon", "props": {"name": "plus"}}`).
5. A value that must be a function is `{"$fn": ""}` (does nothing) or `{"$fn": "id"}` (returns its argument's `id`):
   enough for a required callback or accessor (a table's `rowKey`, a list's `getLabel`). A render prop that must return
   elements cannot be written; such a component gets no sample.
6. An overlay (dialog, popover, menu) draws only when open: give it a controlled `open` (or `defaultOpen`) in the
   sample, and the trigger as an element.

A compound component's sample is where its parts get their picture: Dialog's sample opens it with `DialogHeader`,
`DialogBody`, `DialogFooter` inside. A component that cannot draw outside an application (it needs a router, a
network, a real chart engine's config) gets no sample; the check warns, and the README says why.

### `assetGroups`: icons, logos, pictures

```json
{ "assetGroups": {
  "icons": { "title": "Icons", "kind": "icon", "component": "Icon", "prop": "name", "order": 1,
             "items": [ { "name": "calendar", "file": "assets/icons/calendar.svg" } ] },
  "logos": { "title": "Logos", "kind": "logo", "order": 2,
             "items": [ { "name": "acme", "file": "assets/logos/acme.svg", "dark": "assets/logos/acme-white.svg" } ] } } }
```

`kind`: `icon`, `image` or `logo`. `component` and `prop` say how a board draws an item
(`<x-import component-from-global-scope="Acme.Icon" name="calendar">`). Large groups are fine: the screen packs them
into atlases when the version is installed.

### `templates`: page templates

```json
{ "templates": [
  { "id": "pages", "title": "Pages", "kind": "page", "aspect": [16, 10],
    "description": "Starting points for admin pages; copy one and fill it.",
    "sections": [
      { "id": "lists", "title": "Lists",
        "layouts": [
          { "id": "list-with-filters", "name": "List with filters",
            "purpose": "A table of records with search, filters and a primary action above it.",
            "board": "_harnessly/layouts/list-with-filters.dc.html",
            "thumb": "_harnessly/thumbs/layouts/list-with-filters.png",
            "slots": [ { "key": "title", "kind": "text", "sample": "Events" } ] } ] } ] } ] }
```

A layout is a whole page made of the kit's components, as a board (next section). At
`_harnessly/layouts/<id>.dc.html` a project can take it by its address `<kit>/<theme>/<layout>`. `purpose` is what
the search and an agent read to pick it; `thumb` is written by `kit render --shots`; `slots` are what gets filled.

## A board on the kit

A layout's board, and every project board built on the kit, is HTML the board engine (React 18.3.1) renders:

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<script src="./support.js"></script>
<link rel="stylesheet" href="ds/acme-ui/tokens.css">
<link rel="stylesheet" href="ds/acme-ui/components/bundle.css">
<script src="ds/acme-ui/components/bundle.js"></script>
</head>
<body>
<x-dc>
<helmet><style>html,body{margin:0;background:var(--acme-surface-0)}</style></helmet>
<main class="board">
<x-import component-from-global-scope="Acme.Root" theme="light">
  <x-import component-from-global-scope="Acme.PageHeader" title="Events" action="{{create}}"></x-import>
  <x-import component-from-global-scope="Acme.Table" rows="{{rows}}" dense="{{true}}"></x-import>
</x-import>
</main>
</x-dc>
<script type="text/x-dc" data-dc-script data-props='{}'>
class Component extends DCLogic {
  renderVals() {
    const { Button, Icon } = window.Acme;
    const h = React.createElement;
    return {
      create: h(Button, { variant: 'primary' }, h(Icon, { name: 'plus' }), 'Create event'),
      rows: [ { id: 1, name: 'Spring concert', sold: 412 } ],
    };
  }
}
</script>
</body>
</html>
```

- Files from the theme folder under `ds/<theme>/` (a project copies the theme there; a kit's own layout board writes
  the same paths).
- A string prop is an attribute; camelCase props may be written kebab-case (`sub-tone` → `subTone`). Numbers and
  booleans in `{{ }}`: `{{4}}`, `{{true}}`.
- Objects, arrays, elements and functions come from `renderVals()` by name: `rows="{{rows}}"`. On a board any prop
  works, handlers too — the limits of the live preview (rule 3 above) are the preview's, not the board's.
- Everything inside `kit.root`. Dark mode: the root's prop (`theme="dark"`) or the tokens' dark selector — the kit's
  README says which.

## `tokens.json` and `tokens.css`

`tokens.json` is Claude Design's shape: every family a **list**, never a name→value map.

```json
{ "name": "Acme UI", "version": 1,
  "color": { "themes": [ { "id": "light", "name": "Light" }, { "id": "dark", "name": "Dark" } ],
             "tokens": [ { "name": "surface-1", "value": { "light": "#ffffff", "dark": "#14161a" }, "usage": "Cards and inputs." } ] },
  "type": { "fonts": [ { "family": "Acme Sans", "file": "fonts/AcmeSans-Regular.woff2", "weight": "400" } ],
            "families": { "sans": "\"Acme Sans\", system-ui, sans-serif" },
            "groups": [ { "name": "Text", "family": "sans",
                          "styles": [ { "name": "body", "fontSize": "15px", "lineHeight": "22px", "fontWeight": 400 } ] } ] },
  "spacing": { "tokens": [ { "name": "space-4", "value": "16px", "usage": "Card padding." } ] },
  "radius":  { "tokens": [ { "name": "radius-m", "value": "8px", "usage": "Buttons, inputs." } ] },
  "shadow":  { "tokens": [ { "name": "shadow-1", "value": "0 1px 2px rgba(0,0,0,.08)", "usage": "Raised cards." } ] } }
```

- Names `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}`, each once across all families but `type`.
- Colours: hex, `rgb()`, `hsl()`, `oklch()`, or `"{other-token}"`. `var()`, `color-mix()` and named colours are dropped
  by the readers: resolve them at build time (a library kit reads the computed value in a browser).
- A value per mode for every colour; the first mode is the default.
- `usage` on every token: an agent chooses a token by it.
- A library kit takes values from the package's real CSS and never invents one; a token the source lacks is named in
  the README.

`tokens.css` carries the same tokens as CSS variables (`--<prefix><name>`): light under `:root`, dark under
`[data-theme="dark"]` (or the selector the package uses — the README says which). Components read the variables, so a
kit whose `tokens.css` is missing draws without colours.

## Component cards: what a consuming agent reads

An agent building a page reads the kit's `README.md` (its index), then the card of each component it considers
(`components/<Comp>/README.md`), or asks the search (`harnessly design kit search`). Nothing else tells it what a
component is for, so a kit is only as usable as its cards.

```markdown
# Button

The main way to act on a page: submits a form, starts a flow.       ← the description, word for word

## When to use
- One primary action per area; the second action takes `variant="secondary"`.
## When not to use
- Going to another page: `Link`. An action in a table row: `IconButton`.
## Props
| Prop | Type | Default | What |
| `variant` | `primary` · `secondary` · `ghost` | `primary` | visual weight |
## Example
<x-import component-from-global-scope="Acme.Button" variant="primary">Save</x-import>
## In code
import { Button } from '@acme/ui';                                   ← library kits
```

- «When not to use» names what to take instead: that is how an agent tells look-alikes apart (`Select` vs `Combobox`,
  `Dialog` vs `Drawer`, `Toast` vs `Alert`).
- The example is board markup, as a board writes it.
- A part's card names its parent; the parent's card shows the whole compound.

The kit's `README.md` ends with **Consuming**: the namespace, the files a board loads in order, the root, and the
**index** — one line per component, `Name — description`, by group, parts indented under their parent.

## Bundle rules

- `components/bundle.js` is one classic script (IIFE) that assigns `window.<Namespace>`: no `import`, no network, no
  literal `</script`. It takes `React` and `ReactDOM` from `window`; the board engine supplies **React 18.3.1**. A
  bundle with its own React breaks on the first hook.
- A library kit bundles the package's exports with esbuild, `react`, `react-dom`, `react/jsx-runtime` mapped to the
  page's globals; a package on React 19 is built for 18 (no `use`, no React 19 only APIs at the top level).
- A draft kit writes components with `React.createElement` and needs no build.
- Styles live in `@layer kit-<id>`. Rules on `html`, `body` or `*` outside it restyle every board.
- A kit that needs a provider (theme, portals, config) names it in `kit.root` and marks it `"part": "provider"`.

## Making a kit

### A library kit

1. **Inventory** the package: exports, families, the CSS that defines its tokens, fonts, icons. Everything ends up in
   the kit or in the README's «not in the kit» with a reason.
2. `harnessly design kit init <folder> --source library`, then a build script in the package's repository
   (`npm run build:kit`) that writes the folder: the bundle, `tokens.json` + `tokens.css`, `design-system.json` from
   the TypeScript types (react-docgen-typescript or the compiler API) and hand-written samples, cards from the
   package's docs.
3. Compound components: the parent's sample holds its parts as elements; each part says `"part": "<Parent>"`.
   Providers say `"part": "provider"`.
4. Templates: four to six whole pages as boards.
5. `harnessly design kit check <folder> --render` until clean, then `harnessly design kit render <folder> --shots` as
   the build's last step. The package's CI runs the build on a release tag, so a kit version equals a package version.

### A draft kit

1. Agree the look in words: palette, type, density, radius.
2. `harnessly design kit init ds/<folder> --source draft` inside the project.
3. Replace the tokens, then write 10–15 components (Button, Input, Select, Checkbox, Switch, Card, List, Tabs, Dialog,
   Badge, Avatar, Alert, Table): names and props the way real libraries name them, so the draft maps onto a library
   later.
4. `kit check --render` after every change.

## Checking a kit

```sh
harnessly design kit check <folder>              # the files: identity, canvas fields, tokens, cards, bundle
harnessly design kit check <folder> --render     # and draws every component and layout in headless Chrome
harnessly design kit render <folder> [--only Button,Card] [--shots] [--json]
```

`check` reads the folder the way the kit screen and install do. `--render` (and `render`) draws every component that is
not a part or a provider, and every layout, through the kit screen's own pages, and reports script errors, failed
requests and pictures with nothing visible. Errors exit 1: do not pack. Each problem names its rule:

| Rule | Level | What it wants |
| --- | --- | --- |
| `kitjson.json` | error | kit.json is valid JSON |
| `kitjson.theme` | error | each theme in kit.json has a plain id and a folder inside the kit with its design-system.json |
| `kitjson.empty` | error | kit.json lists at least one theme (a one-theme kit needs no kit.json) |
| `index.missing` | error | every theme has design-system.json |
| `index.json` | error | design-system.json is valid JSON |
| `index.title` | warning | the theme has a title |
| `index.namespace` | error | namespace is a JavaScript name: boards write <Namespace>.<Component> |
| `index.section` | warning | sections are only foundation, components, templates, assets |
| `index.empty` | warning | the theme has groups, assetGroups or templates |
| `identity.missing` | error | one member of design-system.json is the kit record {id, version, source} |
| `identity.id` | error | the kit id is lower case letters, digits and - |
| `identity.version` | error | the version is major.minor.patch |
| `identity.source` | warning | source is library or draft |
| `readme.missing` | error | the theme has README.md |
| `readme.consuming` | warning | README.md has a Consuming section |
| `groups.names` | error | groups are component groups, not a claude.ai list of names |
| `groups.shape` | error | groups is [{id, title, components}] |
| `groups.id` | error | every group has an id |
| `component.id` | error | a component id is its export name in the namespace |
| `component.twice` | error | a component is listed once |
| `component.description` | error | every component has a description: what it is for |
| `component.description-short` | warning | a description has five words or more |
| `component.description-same` | warning | no two components share a description |
| `component.thumb` | error | a component's thumb is a file of the kit |
| `component.preview` | error | a component's preview, when set, is a file of the kit |
| `component.dotted` | warning | a component drawn live has a one-word id (a dotted one is a part) |
| `component.no-sample` | warning | a component that is not a part has a sample or variants |
| `prop.name` | error | every prop has a name |
| `prop.type` | error | a prop's type is one of enum, boolean, number, string, color, token, node, function, object, array |
| `prop.enum` | error | an enum prop lists its values |
| `prop.description` | warning | a prop says what it changes on screen |
| `variant.id` | warning | every variant has an id |
| `sample.undeclared` | error | a sample, a variant or an element sets only props its component declares |
| `sample.not-passed` | warning | a sample sets no prop the live preview never passes (handlers, styles, links, dashed names), unless it is required |
| `sample.enum` | error | a sample sets an enum prop only to one of its values |
| `sample.required` | error | every required prop has a value in the sample or the first variant |
| `sample.depth` | error | elements nest at most 12 deep (lists and objects between them do not count) |
| `element.function` | error | a function value is {"$fn": ""} (does nothing) or {"$fn": "<field>"} (returns its argument's field) |
| `element.id` | error | an element's "$c" is a component id |
| `element.unknown` | error | an element names a component of the kit |
| `part.shape` | error | part is "provider", the parent's id or {"of": parent} |
| `part.self` | error | a component is not a part of itself |
| `part.parent` | error | a part's parent is a component of the kit |
| `part.nested` | error | a part's parent is not itself a part |
| `part.unused` | warning | the parent's sample or variants use the part, so the part has a picture |
| `keywords.shape` | error | keywords is a list of strings |
| `card.missing` | error | every component has components/<Comp>/README.md |
| `card.summary` | error | a card starts with a sentence about what the component is for |
| `card.summary-differs` | warning | the card's first sentence is the component's description |
| `card.when-not` | warning | a card has «When not to use» |
| `card.example` | warning | a card has an <x-import component-from-global-scope=…> example |
| `assets.shape` | error | assetGroups is {id: {title, kind, items: [{name, file}]}} |
| `assets.items` | error | an asset group lists its items |
| `assets.kind` | warning | an asset group's kind is icon, image or logo |
| `assets.files` | error | every asset has a name and a file of the kit |
| `templates.shape` | error | templates is a list of templates |
| `templates.id` | error | every template has an id |
| `templates.empty` | error | every template has layouts |
| `layout.id` | error | every layout has an id |
| `layout.purpose` | warning | every layout says what page it is for |
| `layout.file` | error | a layout's board and thumb are files of the kit |
| `layout.thumb` | warning | every layout has a thumb |
| `tokens.missing` | error | the theme has tokens.json |
| `tokens.json` | error | tokens.json is valid JSON |
| `tokens.dtcg` | error | tokens.json is lists, not a W3C/DTCG name→$value map |
| `tokens.family` | error | each family is {"tokens": [{name, value, usage}]} |
| `tokens.name` | error | a token name is letters, digits, _ . - |
| `tokens.duplicate` | error | a token name is used once across families |
| `tokens.usage` | warning | every token says what it is for |
| `tokens.font` | error | a font file in type.fonts is a file of the kit |
| `tokens.color-shape` | error | a colour value is a string or {mode: value} |
| `tokens.mode` | warning | a colour has a value in every mode |
| `tokens.color-value` | error | a colour is hex, rgb(), hsl() or oklch() — no var(), color-mix() or names |
| `bundle.at-root` | error | the bundle is components/bundle.js, not bundle.js |
| `bundle.missing` | error | a kit with components has components/bundle.js |
| `bundle.script-tag` | error | the bundle contains no </script |
| `bundle.namespace` | warning | the bundle assigns window.<Namespace> |
| `bundle.own-react` | error | the bundle takes React from window.React, it does not carry its own |
| `bundle.module` | error | the bundle is one classic script, not a module |
| `css.missing` | warning | a kit with components has components/bundle.css |
| `css.global` | error | rules on html, body, * sit inside @layer kit-<id> |
| `render.error` | error | a component or a layout draws without an exception, a console error or a failed request (--render) |
| `render.blank` | error | a component or a layout draws something visible (--render; a provider or a part is marked as one) |

## Installing, using, finding

```sh
tar -czf acme-ui-1.4.0.tar.gz -C <folder> .
harnessly design kit install acme-ui 1.4.0 acme-ui-1.4.0.tar.gz    # or an https address, --header for a login
harnessly design kit list
harnessly design kit search acme-ui confirm deletion               # the kit screen's search
harnessly design kit show acme-ui component:ConfirmDialog          # description, props, variants, how to call it
```

A version is installed once and never changes; a fix is a new version. A board loads it from
`/kit/<id>/<version>/<file>`; a layout taken by its address copies its theme folder into the project as `ds/<theme>/`.

The search (the screen's ⌘K and `kit search`) reads a component's name, description, keywords, prop names and prop
descriptions, a layout's name and purpose, a token's name, value and usage. It splits the query into words, cuts
Russian and English endings off (`удаления` finds `удаление`, `bookings` finds `booking`), finds a node when at least
half of the words match, and ranks by how many matched, the title first. A word the kit never uses finds nothing — put
it in `keywords`.

## Not done yet

- Projects do not yet record their kits (`meta.kits` is kept but not read): a board names the kit's files itself
  (HRN-407).
- No command draws a project's board on a kit outside Sinopia, and none turns a package's CSS into `tokens.json`: a
  library kit's build does it (HRN-967).
- No `harnessly design code` (board → TSX) yet.
