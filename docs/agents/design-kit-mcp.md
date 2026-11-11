# Reading a UI kit with the `kit_*` tools

An agent that draws a page on a UI kit reads the kit through eight MCP tools of the runtime, not by opening the kit's
README and files. They work the same for every kit (library or draft), only read, and answer JSON text. The kit's
format is in [design-kits.md](design-kits.md); this page is what the tools answer. The code is
`internal/design/kittools.go` (the tools) and `internal/designkit/view.go` (the shapes, shared with
`harnessly design kit show`); a test compares the examples below with the real answers on a fixture kit.

## The order

1. `kit_list` — which kits there are (skip it when the project already names its kit).
2. `kit_index {kit}` — the map: groups, one line per component, token counts, templates. About 2 KB for fifty
   components. Choose candidates by it; `kit_search` when the name is not there.
3. `kit_component {kit, name}` — everything about one component. Read **when to use / when not** first: that is how look-alikes
   are told apart. Copy `sampleJsx` or `call` as the start of the markup.
4. `kit_tokens` for the variables (`var`) instead of inventing colours; `kit_templates` and `kit_template` when the page
   is a known kind (a list, a record, a slide).
5. Write the board. Then `kit_check {kit, project}` checks the project in strict mode (only the kit's components, props
   and tokens) and says what to change, line by line. `kit_check {kit}` alone is the kit's own check, for a person who
   edits the kit.

## Common to all

Every answer has `"schema": "kit-view/1"` (the shapes' version; a change that removes or renames a field is a new
version, a new optional field is not). Tools that read a kit take `kit` (its id), and optionally `version` (the newest
installed one by default; `draft` for the kit's draft) and `theme` (the first by default). A refusal is an error result
(`isError`) starting `refused:` with the reason and what to try (`kit_list`, `kit_search`).

What a call may read is what the person may open: an installed version, as the kit store lists it; the draft only when
its project is open to the person. A version the store does not list is not there for the tools either.

A kit that extends another (see «A kit on a kit» in design-kits.md) answers as assembled: every inherited token,
component and template has `from` (`acme-ui@1.0.0`), a replacing component `overrides`, and an inherited component's
`call` uses the base's namespace. The index marks inherited components `←acme-ui`.

## `kit_list`

No arguments. `draft: true` marks a kit whose draft the person may read; `canvas: false` a kit of boards only (older),
which the other tools refuse.

```json kit_list
{"schema":"kit-view/1","kits":[{"id":"acme-ui","title":"Acme UI","versions":["1.0.0"],"themes":["default"],"canvas":true}]}
```

## `kit_index {kit, version?, theme?}`

`components` are the groups; an item is `Name — description`. After the name: `[new]`, `[deprecated → X]`,
`[provider]`, `+N` (it has N parts: pieces such as `CardHeader`, listed by `kit_component`). The descriptions are cut to
keep the answer near 2 KB for fifty components (a small kit keeps them whole; a big one may show names only). `tokens`
counts by family; `templates` counts layouts.

```json kit_index
{"schema":"kit-view/1","kit":"acme-ui","version":"1.0.0","theme":"default","title":"Acme UI","namespace":"Acme","root":"Root","components":[{"id":"layout","items":["Root [provider] — Sets the theme of everything below it; wrap the page once.","Card +2 — A raised block that groups a record's header and body on a page."]},{"id":"actions","items":["Button — The main way to act on a page: submits a form or starts a flow.","PrimaryButton — The page's one main call to action, a Button in its primary weight."]}],"tokens":{"color":2,"radius":1,"spacing":1,"type":1,"total":5},"templates":[{"id":"pages","layouts":1}],"next":"kit_component {kit, name}"}
```

## `kit_component {kit, name, version?, theme?}`

| Field | What |
| --- | --- |
| `id`, `name`, `group`, `description`, `keywords` | identity and what it is for |
| `status`, `replacedBy` | `stable`, `new`, `deprecated` (and what replaces it) |
| `part` | for a piece of a compound component its parent's id, `provider` for a provider |
| `props`, `required`, `optional` | all props (`name`, `type`, `values`, `required`, `description`) and their names split by need |
| `variants` | named prop sets |
| `parts` | pieces of a compound component: `id`, `description`, `inSample` (its sample draws it) |
| `sample`, `sampleJsx`, `call` | the sample's props, as JSX, as board markup (`<x-import …>`) |
| `code` | `{import, export}`: what a consuming project imports |
| `preset` | for a preset, `{of, props}`; it answers with its target's props and card |
| `presets` | components that are this one with fixed props |
| `card` | the card (`components/<Comp>/README.md`) cut into sections: `found`, `summary`, `whenToUse`, `whenNotToUse`, `examples`, `inCode`, `content`, `accessibility`, `states`, `status` |
| `usedWith` | computed: `components` whose samples hold this one or are held by its sample, `layouts` (`template/layout`) whose boards use it |

Empty optional fields are left out. `harnessly design kit show <kit> component:<id>` prints the same object as `value`.

```json kit_component
{"schema":"kit-view/1","kit":"acme-ui","version":"1.0.0","theme":"default","id":"Button","name":"Button","group":"actions","description":"The main way to act on a page: submits a form or starts a flow.","keywords":["submit","cta"],"status":"stable","props":[{"name":"variant","type":"enum","values":["primary","secondary"],"description":"visual weight; primary once per area"},{"name":"disabled","type":"boolean","description":"greys it out and ignores clicks"},{"name":"children","type":"node","required":true,"description":"the label"}],"required":["children"],"optional":["variant","disabled"],"variants":[{"id":"secondary","name":"Secondary","props":{"variant":"secondary","children":"Cancel"}}],"sample":{"children":"Save","variant":"primary"},"sampleJsx":"<Button variant=\"primary\">Save</Button>","call":"<x-import component-from-global-scope=\"Acme.Button\" children=\"Save\" variant=\"primary\"></x-import>","code":{"import":"@acme/ui","export":"Button"},"presets":[{"id":"PrimaryButton","props":{"variant":"primary"}}],"card":{"found":true,"summary":"The main way to act on a page: submits a form or starts a flow.","whenToUse":["One primary action per area.","A second action takes `variant=\"secondary\"`."],"whenNotToUse":["To go to another page: use a link."],"examples":["<x-import component-from-global-scope=\"Acme.Button\" variant=\"primary\">Save</x-import>"],"inCode":"import { Button } from '@acme/ui';","content":["Verb first: «Save», not «Saving»."],"accessibility":["A label is required; an icon alone needs `aria-label`."],"states":["disabled greys it out and ignores clicks."],"status":"Stable since 1.0."},"usedWith":{"components":[],"layouts":["pages/record-page"]}}
```

### The card

A component's card is what `whenToUse`, `whenNotToUse`, `examples` and the rest are cut from. Its headings (any case):

| Heading | Required | Read as |
| --- | --- | --- |
| (the first paragraph) | yes | `summary`; the component's description, word for word (`card.summary`, `card.summary-differs`) |
| `## When to use` | no | `whenToUse` |
| `## When not to use` | recommended | `whenNotToUse`; names what to take instead (`card.when-not`) |
| `## Props` | no | not read: the props are the manifest's |
| `## Example` / `## Examples` | recommended | `examples`: one per blank-line paragraph or fenced block, as board markup (`card.example`) |
| `## In code` | library kits | `inCode` |
| `## Content` | no | `content`: what the text in it should say |
| `## Accessibility` | no | `accessibility` |
| `## States` | no | `states` |
| `## Status` | no | `status`: a note on maturity |
| `## Used with` | never written | computed into `usedWith`; a heading in the card is ignored |

"Required" is what `kit check` reports (`card.missing`, `card.summary`, `card.example`, `card.when-not`: the registry in
design-kits.md decides, this table follows it). A preset has no card; it takes its target's.

## `kit_search {kit, query, version?, theme?}`

The kit screen's own search (names, keywords, descriptions, prop names, layout purposes, token names and usage), at most 20
results, best first. `ref` is what the next tool takes: a component's id for `kit_component`, a layout's id for
`kit_template`.

```json kit_search
{"schema":"kit-view/1","kit":"acme-ui","version":"1.0.0","theme":"default","query":"submit","results":[{"node":"component:Button","kind":"component","title":"Button","path":"Actions","ref":"Button"}]}
```

## `kit_tokens {kit, group?, version?, theme?}`

Tokens by family (`color`, `type`, then the kit's own: `radius`, `spacing`…). `var` is the CSS variable: the token's
own `var`, else the kit's `tokenPrefix` and the name (how `tokens.css` names them). A colour has `values` by mode
(`value` when it has one).

```json kit_tokens
{"schema":"kit-view/1","kit":"acme-ui","version":"1.0.0","theme":"default","prefix":"--acme-","modes":["light","dark"],"groups":{"color":[{"name":"surface-1","var":"--acme-surface-1","values":{"dark":"#14161a","light":"#ffffff"},"usage":"Page background."}],"spacing":[{"name":"space-2","var":"--acme-space-2","value":"8px","usage":"Gap between controls."}]}}
```

## `kit_templates {kit, template?, version?, theme?}` and `kit_template {kit, id, version?, theme?}`

`kit_templates` lists page or slide templates with each layout's `purpose`: choose by it. `kit_template` is one layout:
the `slots` to fill, the `address` a project takes it by (when the theme ships its board), `board` (the file) and
`source` (its HTML, up to 64 KB; a bigger board gives `boardBytes` instead).

```json kit_templates
{"schema":"kit-view/1","kit":"acme-ui","version":"1.0.0","theme":"default","templates":[{"id":"pages","title":"Pages","kind":"page","aspect":[16,10],"description":"Starting points for admin pages; copy one and fill it.","layouts":[{"id":"record-page","name":"Record page","section":"Lists","purpose":"A page for one record: a card with its title and its main action."}]}]}
```

```json kit_template
{"schema":"kit-view/1","kit":"acme-ui","version":"1.0.0","theme":"default","template":"pages","id":"record-page","name":"Record page","kind":"page","aspect":[16,10],"purpose":"A page for one record: a card with its title and its main action.","slots":[{"key":"title","kind":"text","sample":"Spring concert"}],"board":"_harnessly/layouts/record-page.dc.html","source":"<!doctype html>…"}
```

## `kit_check {kit, version?}`

`harnessly design kit check` on the installed version, without the render. `problems` are errors first, at most 40
(`truncated`); each names its rule from the table in design-kits.md. `level` is the kit's readiness (-1 … 3), `levels`
the number of components per level, `behind` the components below the top level with what each lacks.

```json kit_check
{"schema":"kit-view/1","kit":"acme-ui","version":"1.0.0","ok":true,"themes":["default"],"components":7,"errors":0,"warnings":1,"problems":[{"rule":"layout.thumb","level":"warning","file":"design-system.json","message":"pages/record-page has no thumb"}],"level":-1,"next":"level 0: code {import, export}","levels":{"-1":4,"0":0,"3":3},"behind":[{"name":"Root","level":-1,"next":"level 0: code {import, export}"}]}
```

## `kit_check {kit, project, boards?}`

The strict check of a design project on the kit (`harnessly design check <project> --strict`; the rules are «Strict
mode» in design-kits.md). `kit` is the kit the project is on; `boards` narrows it to pages or boards, comma separated.
The answer is not a `kit-view` shape: `checked` are the files read, `findings` one per problem, each with `file`, `line`,
`col`, `rule`, `element`, `what` and `fix` (what to put instead). An empty `findings` is a project made of the kit.

```text
{"project":"events","kit":"acme-ui","version":"1.0.0","checked":["pages/home.tsx"],"findings":[{"rule":"strict.prop","file":"pages/home.tsx","line":6,"col":15,"element":"Button","what":"Button has no prop \"size\"","fix":"Button declares: variant, children"}]}
```

## Not here

Adding a kit (`kit add`), changing one, and who may read or edit it are other work: these tools read what is installed and
open to the person. Resources (`kit://<id>@<version>/component/<Comp>`) are not served yet.
