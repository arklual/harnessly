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

### Optional fields: code, presets, status, kind

All of these are optional; a kit without them passes `kit check` as before. Each has its own rule below.

| Where | Field | Meaning |
| --- | --- | --- |
| component | `code` | `{import, export}`: the module a consuming project imports from and the export name. Gives the component readiness level 0. |
| component | `preset` | `{of, props}`: this component is another component with fixed props (`of` is its id, `props` are values it declares). A preset needs only a description; it takes its target's card, sample and readiness. |
| component | `todo` | what an imported draft still lacks (a list of sentences); `kit check` warns while it is not empty. See "Other formats". |
| component | `status` | `stable`, `new` or `deprecated`. |
| component | `replacedBy` | the component that replaces a `deprecated` one. |
| `kit` | `kind` | `app`, `site`, `slides` or `video`. |
| `kit` | `extends` | `<id>@<range>`: the kit this one builds on, e.g. `base-kit@^1.2.0` (`^`, `~`, `>=`, `<`, exact). |
| `kit` | `omitted` | what the kit leaves out on purpose: `tokens`, `examples`, `docs`, `patterns`, `templates`. An omitted part counts as done in the readiness. |
| `kit` | `strict` | `true`: every project on the kit is checked in strict mode (see «Strict mode»). |
| token | `var` | the CSS custom property the token is published as, `--like-this`. |

```json
{ "id": "PrimaryButton", "description": "The main call to action of a form",
  "code": { "import": "@acme/ui", "export": "Button" },
  "preset": { "of": "Button", "props": { "variant": "primary" } }, "status": "stable" }
```

### Readiness

`kit check` ends with a summary of how far each component is built out. It is not a finding and does not change the
exit code; `--levels` lists every component and what it lacks for the next level.

| Level | The component has |
| --- | --- |
| -1 | no `code` yet |
| 0 | `code` |
| 1 | and the theme has `tokens.json` |
| 2 | and a sample (or variants) plus an example in its card; a part needs no sample of its own |
| 3 | and a card with «When not to use» |

The kit's level is the lowest of its components.

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
network, a real chart engine's config) gets no sample; the check warns, the README says why, and the render (`kit render`,
`check --render`) passes over a component with neither a sample nor variants.

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

Optional sections, each its own `##` heading, are read by `kit show`, `kit_component` and the screen's panel; the check
does not require them (the rules below stay as they are): `Examples` (several examples, as `Example`), `Content` (what
the text in it should say), `Accessibility`, `States`, `Status` (a note on maturity). **Used with** is not written: it
is computed from the kit's samples and the layouts' boards. A preset has no card of its own; it reads its target's.
The sections, the answers of the `kit_*` tools and the order an agent reads in are in
[design-kit-mcp.md](design-kit-mcp.md).

The kit's `README.md` ends with **Consuming**: the namespace, the files a board loads in order, the root, and the
**index** — one line per component, `Name — description`, by group, parts indented under their parent.

## A kit on a kit

`kit.extends` (`"acme-ui@^1.0.0"`: a kit id and a version range) makes a kit the **child** of a base kit. The child holds
only its differences; the rest is the base's, read from the newest installed version the range allows. One brand can
then serve slides and application pages: foundation → brand → a kit for the kind of work. A child can be as small as three
files — `design-system.json` (with `extends`), `README.md`, `tokens.json` with the tokens it changes.

```json
{ "kit": { "id": "acme-brand", "version": "1.0.0", "source": "library", "extends": "acme-ui@^1.0.0" } }
```

What the child gets:

| Part | Rule |
| --- | --- |
| Tokens | the base's, a token of the child replacing the base's of the same name; new ones are added. `tokenPrefix` and `root` are the base's unless the child sets its own |
| Components | the base's, a component of the child replacing the base's of its id (a preset too); new ones are added to the group of the same id, else to a group of their own |
| Templates | the base's, a template of the child replacing the base's of its id |
| Not inherited yet | assets (icons, logos, pictures) and the pictures of inherited components and layouts: the kit screen draws inherited components live |

**Files.** A page loads the kits of the chain in order, the outermost base first, the child last: each one's `tokens.css`,
`fonts.css`, `components/bundle.css`, then each one's `components/bundle.js`. So the child's variables and styles overrule
the base's by the cascade, and its bundle runs after the base's. A component the child **replaces** is assigned by its
bundle into the base's namespace (`window.Acme.Button = Button`), so a board written on the base draws the replacement
too; a component the child **adds** lives in its own namespace (`window.AcmeBrand`). The assembled kit tells each
component's namespace (`ns`), and `call` and `sampleJsx` use it.

**Source.** In `kit show`, search and the `kit_*` tools every inherited token and component says where it comes from
(`from`: `acme-ui@1.0.0`), a replacing one says what it replaces (`overrides`); the index marks inherited components
`←acme-ui`. A card of an inherited component is read from the base.

**Which base.** The version is chosen when the child's version is installed or published (or the first time it is read) and
kept in `<kits>/<id>/.<version>.base.json`: a published child shows the same after the base gets a newer release, and
needs that base version installed (`extends.missing` otherwise). A new child version takes the newest base that fits.
`kit export` and `design pack` list the base as a requirement and carry it. A project is pinned to the child; its boards
find the base's files under `ds/<base theme>/`. A draft kit follows the newest base.

**Check.** `kit check` (and `kit_check`) needs the kits folder to find the base (`--dir`, the runtime's by default). It
checks the child as assembled: a preset, part or sample element may name an inherited component. A component that
replaces the base's must keep its contract, so a board written on the base still draws: every prop of the base stays with
its type, an enum keeps all its values, what the child adds is optional (`extends.override`). The chain is at most three
bases deep and has no loop. `kit render` draws the child with its bases when they are installed.

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

One command makes the kit from the package, the same command (or `kit update`) brings it to a new version:

```sh
harnessly design kit add npm:@acme/ui@1.4.0 ./acme-ui [--root ThemeProvider] [--css @acme/ui/theme.css] [--registry URL]
harnessly design kit add shadcn:ui ./shadcn-ui                 # shadcn/ui; shadcn:<folder or address> for another registry
harnessly design kit update ./acme-ui --version 1.5.0          # the newest when --version is not given
```

| Step | What `kit add` does |
| --- | --- |
| Install | `npm install` of the package (plus React 18.3.1 and TypeScript for reading the types) into a build folder in the user cache, with the registry and token of the `.npmrc` in the folder the command runs in (or `--registry`); `--from DIR` takes a folder that already has `node_modules` and installs nothing. A peer dependency npm left out is installed when the bundle needs it. A shadcn registry: every `registry:ui` item and what it names, from the address or a folder (a mirror in a closed network) |
| Bundle | esbuild: every export into `window.<Namespace>`, `react`, `react-dom`, `react/jsx-runtime` and React 19's compiler runtime mapped to the page's React 18; the package's stylesheets (its `style` field and the `.css` of its `exports`, or `--css`) with fonts and pictures inlined, in `@layer kit-<id>`. A shadcn kit: Tailwind compiles the classes the components use, with the registry's style, into `bundle.css`; the kit gets a `Root` (light and dark) |
| Props | TypeScript reads each exported component's props: enums from string unions, booleans, nodes, functions, the doc comments as descriptions. The attributes React's own types add (`className`, `onClick`, `aria-*`) are left out; `children` stays where the component takes it |
| Parts | a `…Provider` is a provider; in a file declaring several components, the ones named after another (`CardHeader` after `Card`) are its parts |
| Tokens | the CSS variables of the package's stylesheets (shadcn: of the registry's colours) under `:root`, `[data-theme]`, `.dark`: `tokens.json` + `tokens.css` (see «Other formats», CSS) |
| Index and cards | `kit.version` = the package's version, `kit.package`, `code` `{import, export}` for every component, a first sample (a value for every required prop; a parent holds its parts), variants from a `variant` prop, a card each (description, Props, Example, In code), `todo` with what a person writes, README.md with «Not in the kit» and Consuming |
| Draw | every component in headless Chrome (`--no-render` skips it): one that throws or draws nothing alone loses its sample, says why in its `todo`, and is listed in the README under «Not in the kit»; the check and `--render` then pass over it |

The run ends with the report: components, parts, providers, tokens; on an update what changed (`+` a component, `-` a
component, `~` its props: `+size, -tone, ~variant: enum(a|b) → enum(a|b|c)`); **what it could not read** (no types, no
stylesheet, no variables, items a registry lacks) and what a person writes; then the check.

**What a later run keeps.** The bundle, `tokens.css`, the kit record, every component's `props` and `code`, the cards'
`Props` and `In code` sections and the README from «Not in the kit» on are rebuilt each time. Everything else is the
person's and stays: descriptions and prop descriptions, keywords, samples and variants (a prop that is gone is taken out
of them, a new required one gets a value — both reported), status, presets, the rest of each card, the README above
«Not in the kit», a token's usage. A component gone from the package leaves the index; its card stays and is reported.
The same version twice writes the same files. `_harnessly/kit-source.json` records the source `kit update` builds from.

After the command: write what each component's `todo` says (what it is for, «When to use», «When not to use»), better
samples, `keywords`; four to six templates; `harnessly design kit render <folder> --shots`; `check --render` until clean.

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
not a part or a provider and has a sample or variants, and every layout, through the kit screen's own pages, and reports script errors, failed
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
| `identity.kind` | error | the kit record's kind, when set, is app, site, slides or video |
| `identity.extends` | error | extends, when set, is <id>@<range> with a valid id and version range |
| `extends.missing` | error | the base kit named by extends is installed |
| `extends.version` | error | an installed version of the base kit satisfies the range |
| `extends.cycle` | error | the chain of bases does not lead back to the kit |
| `extends.depth` | error | the chain of bases is at most three kits long |
| `extends.override` | error | a component that replaces the base's keeps its props: it may add optional props and enum values, not drop, retype or require |
| `identity.strict` | error | the kit record's strict, when set, is true or false |
| `identity.omitted` | error | omitted, when set, lists only tokens, examples, docs, patterns, templates |
| `code.shape` | error | a component's code, when set, is {import, export} with plain strings |
| `preset.shape` | error | a component's preset, when set, is {of, props} |
| `preset.target` | error | a preset's of is another component of the theme, not a preset itself |
| `preset.props` | error | a preset's props are simple values the target component declares |
| `status.value` | error | a component's status, when set, is stable, new or deprecated |
| `status.replaced-by` | error | replacedBy, when set, names another component and goes with status deprecated |
| `token.var` | error | a token's var, when set, is a CSS custom property name --like-this |
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
| `component.todo` | warning | a component imported as a draft lists what a person still writes in todo; the list is empty when it is done |
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

## Other formats

The kit's own format is the only one inside: `design-system.json` v3, `tokens.json` as lists, the cards. Other people's
formats exist at the border, in one command, and nothing else reads them. `tokens.json` stays lists (the check refuses
DTCG there); DTCG is only what `convert` writes and reads.

```sh
harnessly design kit convert <folder> --to dtcg|css|tailwind|tailwind4|design-md [--out FILE]
harnessly design kit convert <folder> --from dtcg|css|design-md|figma <file> [--mode ID] [--css] [--dry-run]
harnessly design kit convert <folder> --from storybook <build folder | index.json | stories.json> [--dry-run]
```

`<folder>` is a theme folder (the one with `design-system.json`). Without `--out` an export goes to standard output and
its report to standard error. An import merges into the kit by name: a new token is added, one with the same name is
replaced, nothing is deleted; components already in the kit are left as they are. `tokens.css` is rewritten only with
`--css` (components read it; the report reminds you when it is stale). `--dry-run` writes nothing.

**Every run ends with what it did not carry** (`not carried`), and an import with what a person still writes (`to write`)
and the result of `kit check`. Nothing is dropped without a line.

| Format | Direction | Carried | Not carried |
| --- | --- | --- | --- |
| DTCG (W3C Design Tokens) | out and in | colours with `$type`, `$description` = usage, `{a.b}` aliases, a mode other than the first in `$extensions.mode.<mode>`, dimensions (spacing, radius), shadows as `$type: shadow` objects, font families, text styles as `typography`. Kit-only members (`var`, font files, unknown token fields, the colour themes) go to `$extensions.harnessly`, so "ours → DTCG → ours" is lossless | components, cards, templates, assets. Dimensions are strings (`"8px"`), not `{value, unit}`. On import: tokens of types the kit has no family for (`duration`, `number`, `cubicBezier`, `border`, `gradient`), `$ref` pointers, group-level `$extends` |
| CSS variables | out and in | every token as `--<prefix><name>` (its `var` when set); the first mode under `:root`, others under `[data-theme="<mode>"]`; alias `{a}` ⇄ `var(--a)`; usage ⇄ a comment next to the declaration. Import: `:root`, `html`, `@theme`, `[data-theme]`, `.dark`, `@media (prefers-color-scheme: dark)`; every token gets `var` | text styles, font files, components. Import: properties that are not a colour, a length, a shadow or a font family (z-index, durations, `calc()`, `color-mix()`), rules under component selectors; named colours become values only for `white`, `black`, `transparent` |
| Tailwind | out | v3: a preset with `theme.extend` (`colors`, `spacing`, `borderRadius`, `boxShadow`, `fontFamily`, `fontSize`) whose values are `var(--…)`, `darkMode` for the second mode. v4: `@custom-variant`, the variables, and `@theme inline` pointing at them | values per mode (they live in the variables, one class works in every mode); opacity modifiers on colours in v3; components |
| DESIGN.md | out and in | out: one file for an agent that has nothing else: front matter (`name`, `colors`, `typography`, `rounded`, `spacing`), prose (Overview, Colors with every mode, Typography, Layout, Elevation, Shapes) and the kit's components (name, purpose, props, when to use and not). In: the front matter's tokens; usage and other modes from a table with a `Token` column and a `Use` column, or from `**Name (#hex):** text` bullets; the prose that has no field kept in `_harnessly/design-notes.md` | out: shadows in the front matter (the format has no key), samples, variants, the bundle, templates, assets. In: components (front-matter recipes are style recipes without props or code, listed in the notes, never made into kit components; prose is never read for components), `omitted`, tokens from prose alone (no front matter is an error) |
| Storybook build | in | from `index.json` (v5/v4) or `stories.json` (v3): every title is a component (`Group/Name`), stories are variants and the first one is the sample; `argTypes` become props (type, enum values, required, description); from the component manifest (`components.json`): description, props (react-docgen), `import` → `code {import, export}`. A card is written for each | MDX and autodocs text, play functions, decorators, parameters; the stories' code and the components' code (the bundle is built from the package, not from a Storybook); `part`/`provider` marks; «when not to use»; a description when the build has none; props when it has neither `argTypes` nor a manifest |
| Figma variables | in | the JSON of `GET /v1/files/:key/variables/local` (its `meta.variables` is how it is recognised; the file is read from disk, nothing calls Figma): COLOR → colour with a value per Figma mode (the default mode first), aliases between colours, FLOAT named like spacing or radius → `px`, STRING named like a font → font family, `description` → usage | BOOLEAN and other FLOAT/STRING variables, modes of non-colour variables (the first is kept), scopes, code syntax, remote variables, text styles and effects (not variables). A DTCG export made by a Figma plugin goes through `--from dtcg` |

### A Storybook import is a draft

Storybook knows what a component is called, what its stories are and, with `argTypes` or the manifest, its props. It
does not know what the component is for. So the import writes a **draft**: the record in `design-system.json`, a card
(title, description if there is one, props, an example, the story names) and a `todo` list on the component:

```jsonc
{ "id": "TextField", "name": "TextField", "description": "",
  "todo": ["description: what it is for and when to take it (Storybook has no text for it)",
           "sample: no story carries args for declared props, so there is no sample or variants",
           "code: {import, export} (the component manifest has the import statement; this build has none)",
           "card: «When to use» and «When not to use» (names what to take instead)"] }
```

`kit check` reports each draft with a `todo` as a warning (`component.todo`); an empty description stays the error it
always was (`component.description`), so a draft is not fit to pack until a person has written it. Write what the list
says, delete the `todo` field, check again. To get props and a sample, give the import a build that has them:
`storybook extract` writes `stories.json` with `args` and `argTypes`; a plain `index.json` only names the stories.

### Modes from two files

A light file and a dark file of the same tokens (what several Figma plugins write) are imported one after the other:
`--from dtcg light.json`, then `--from dtcg dark.json --mode dark`. The second sets the dark value of the tokens that
have the same name and adds the new ones.

## Installing, using, finding

```sh
tar -czf acme-ui-1.4.0.tar.gz -C <folder> .
harnessly design kit install acme-ui 1.4.0 acme-ui-1.4.0.tar.gz    # or an https address, --header for a login
harnessly design kit list
harnessly design kit search acme-ui confirm deletion               # the kit screen's search
harnessly design kit show acme-ui component:ConfirmDialog          # description, props, variants, card, how to call it (what kit_component answers)
```

A version is installed once and never changes; a fix is a new version. Every install ends with `kit.sealed.json` in the
version's folder: the list of its files with sizes and sha256. A version counts as installed only with that seal and
all its files in place, and a file the seal does not list is not part of the version (see docs/design-pack.md, "The seal
of a version"). The seal is not part of the kit you pack and has no rule: do not write it by hand. A board loads it from
`/kit/<id>/<version>/<file>`; a layout taken by its address copies its theme folder into the project as `ds/<theme>/`.

The search (the screen's ⌘K and `kit search`) reads a component's name, description, keywords, prop names and prop
descriptions, a layout's name and purpose, a token's name, value and usage. It splits the query into words, cuts
Russian and English endings off (`удаления` finds `удаление`, `bookings` finds `booking`), finds a node when at least
half of the words match, and ranks by how many matched, the title first. A word the kit never uses finds nothing — put
it in `keywords`.

## A page as a module

A board can also be written as `pages/<name>.tsx`: a React module that imports the kit's components by `kit.package` (and
the subpaths its components' `code.import` name), takes its data from `pages/<name>.fixtures.ts` and exports the page as
default. The library builds `<name>.dc.html` from it (`harnessly design page build <project>`), a board that is closed to
direct edits; the same `.tsx` is what goes into the product. The rules (what may be imported, fixtures, the check, going
back) are in [design-pages.md](../design-pages.md#pages-written-as-modules-hrn-1012). A component an agent wants on a
page must have `code` in the kit's index, or the page cannot import it.

A board written in the old format becomes such a page with `harnessly design migrate --to tsx <project>`: rules, no
model, and only when the page draws the board's picture pixel for pixel; `--to board` takes it back byte for byte
([migrations/0002-boards-to-tsx.md](../migrations/0002-boards-to-tsx.md)). A migrated page imports nothing from a kit
yet: its `<x-import>`s become `<Global __global="Kit.Button">` (the component found on `window`, as the board found it).

## Into the product's code

`harnessly design code <project> --to <repository>` turns a strict project's page modules into the application's code,
by rules over files and without a model: a kit import is written from the components' `code`, a preset becomes its
target with the preset's props, a token's semantic variable (`var(--acme-accent)`) becomes the library's own (its
`var`, `var(--acme-color-accent)`), and the page's fixtures become the view's props. A kit whose tokens name a `var`
keeps the semantic names as aliases in its `tokens.css` (`--acme-accent: var(--acme-color-accent)`), so the mockup draws
them; the application loads only the package's CSS. The repository's `sinopia.export.json`, the checks and what is
never touched are in [design-pages.md](../design-pages.md#into-the-products-code-hrn-1015).

## Strict mode

A mockup on a kit is meant to become the product's code one to one, so it must be made of the kit and nothing else.
The strict check says where it is not:

```sh
harnessly design check <project> --strict [--board home] [--json]   # local: no runtime, no browser; exit 1 on a finding
```

It is on without the flag when the project says so (`meta/project.json` `"strict": true`) or its kit does (`kit.strict`);
then `harnessly design check <project>` runs it before drawing the boards. Agents have it as `kit_check {kit, project}`
(see [design-kit-mcp.md](design-kit-mcp.md)). It reads the project's files under `project/`:

| File | Read as |
| --- | --- |
| `pages/<name>.tsx` | a page: only the kit's components (imported from `kit.package` and the subpaths in `code.import`), React's own (`Fragment`) and local components imported from the project's files |
| any other `.tsx`, `.jsx` | a local component: a block of the project's own. Plain HTML is allowed in it; its values are still tokens and the kit's components in it are still checked |
| `*.dc.html` written by hand | a board: `<x-import component-from-global-scope="<Namespace>.<Component>">` of the kit or a local component file (`from="components/…"`); `<style>`, `style=""` and the strings of `renderVals()` for values. A board built from a page module is checked through its source |
| `*.fixtures.*` | data, not read |

Each finding is one line an agent can act on: `file:line:col rule — what is wrong. Fix: what to put instead` (the
component and its import, the token with the same value, the props the component declares, the closest name):

```text
pages/home.tsx:6:15 strict.prop — Button has no prop "size". Fix: Button declares: variant, children
pages/home.tsx:9:31 strict.value — the colour #2563eb is not a token. Fix: use var(--acme-accent) (token accent)
```

| Rule | Level | What it wants |
| --- | --- | --- |
| `strict.element` | error | every element is a component of the kit, or a local component of the project in a file of its own |
| `strict.value` | error | colours, lengths and CSS variables are the kit's tokens, and a token prop gets a token |
| `strict.prop` | error | a component gets only the props it declares, an enum prop only one of its values |
| `strict.deprecated` | error | no component the kit marks deprecated |

What counts:

- **Element.** Plain HTML (`<div>`), a component of another package, one the kit does not export, or one written inside
  the page are not the kit's. A block the kit lacks is a local component in its own file (`components/<Name>.tsx`),
  imported by the page: it goes into the product as a file of its own.
- **Value.** A colour (hex, `rgb()`, `hsl()`, `oklch()`…), a length other than 0 (`12px`, `1rem`, a number given to a
  size property of a `style` object), or `var(--x)` of a variable that is not a token. A prop of type `color` or `token`
  takes a token's name (or its variable).
- **Prop.** Any attribute the component does not declare (`style` and `className` included), except `key`, `ref`,
  `children`, `aria-*` and `data-*`; an enum prop set to a value it does not list. A preset takes its target's props.
- **Deprecated.** A component with `status: "deprecated"`; the fix names `replacedBy` and its import.

The check is rules over text, without a model. It reads JSX the way TSX does (an element where an expression starts)
and does not run the code, so props built at run time (a spread `{...props}`, values computed in `renderVals()`) are not
checked.

## Not done yet

- Projects do not yet record their kits (`meta.kits` is kept but not read): a board names the kit's files itself
 .
- No command draws a project's board on a kit outside Sinopia.
- `kit add` reads token values as the stylesheet writes them: a variable defined by `calc()` or `color-mix()` of others
  is left out, not resolved in a browser. Packages styled by CSS-in-JS have no variables to read (their theme object is
  not read yet), and Angular packages are not built.
