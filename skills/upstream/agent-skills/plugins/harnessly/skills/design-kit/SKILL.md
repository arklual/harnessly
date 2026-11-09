---
name: design-kit
description: Make, change, check or install a design kit for Sinopia — a design system whose real components, tokens and assets boards build pages from and the kit screen shows. Two kinds, one format - a library kit built from a React package (real props, real tokens, real imports, for prototypes that become production code) and a draft kit written by hand inside a project for a quick prototype. Use when the person asks to make a design system, UI kit or kit for Sinopia, to turn a component library into a kit, to make a draft kit for a prototype, when you see a design-system.json, a kit.json or a `harnessly design kit` command, or when a kit does not show, check or render.
---

# Making a design kit for Sinopia

The contract — every file, every field, how the preview draws a component, every rule the check runs — is one page.
Read it first, whole; this skill is the order of work:

```sh
harnessly docs read agents/design-kits
```

The page is tested against the code (its rule table is the check's registry, its JSON examples parse). If something
on it still looks wrong, say so in your answer instead of working around it.

## Which kind

Ask only if the request does not tell you.

- **Library kit** — there is a React package and pages built on the kit should become code that imports from it. The
  kit is built by a script in the package's repository; its CI publishes it by the package's version.
- **Draft kit** — no library, or a quick look to try. The kit lives in the project as `ds/<folder>/`.

## The loop

Start valid, stay valid, one change at a time:

```sh
harnessly design kit init <folder> --id <id> --namespace <Ns> --title "<Title>" --source draft|library
harnessly design kit check <folder> --render      # after every change: files + every component drawn in Chrome
harnessly design kit render <folder> --only Comp   # one component while you fix it
```

`init` writes a kit that passes: a provider (`Root`, light and dark), `Button`, and `Card` with its parts — copy their
shapes. Each problem the check prints names its rule (`[sample.required]`); the docs' table says what it wants.

## A library kit

1. **Inventory** the package (exports, families, token CSS, fonts, icons) into `kit-inventory.md` next to the build:
   every line ends in the kit or in the README's «not in the kit» with a reason.
2. **Build script** (`npm run build:kit`) writing the folder: the bundle (esbuild, React from `window`, built for
   React 18), `tokens.json` with values resolved in a browser + `tokens.css`, `design-system.json` from the types,
   cards from the package's docs.
3. **Samples by hand** for every component: realistic content of the product, required props filled, overlays open
   with their trigger as an element, compound parents holding their parts as elements. Parts `"part": "<Parent>"`,
   providers `"part": "provider"`. Search words the description lacks go into `keywords`.
4. **Templates**: four to six whole pages as boards (the docs' «A board on the kit»).
5. Last step of the build: `harnessly design kit render <folder> --shots` (pictures and `thumb` fields), then
   `check --render` must be clean.

## A draft kit

1. Agree the look in two or three sentences, or take it from the request.
2. `init` into `ds/<folder>/`, replace the tokens, then 10–15 components with `React.createElement` named and propped
   the way real libraries do (`variant`, `size`, `disabled`, `onChange`).
3. `check --render` after every component.

## Cards: write them for the agent that comes after you

The agent building pages knows only the README's index and the cards. Each card: the description as its first
sentence (what it is for, not what it is), When to use, **When not to use naming what to take instead**, Props,
Example as board markup, the real import for a library kit. Then test yourself from the index alone: «a field to pick
one city of 300»? «confirm deleting an event»? If two lines read alike, rewrite them; try the same words in
`harnessly design kit search <id> …` after installing.

## Install and look

```sh
tar -czf <id>-<version>.tar.gz -C <folder> .
harnessly design kit install <id> <version> <id>-<version>.tar.gz
harnessly design kit search <id> <words a person would type>
```

Open the kit screen in Sinopia: every component a picture, parts in their parent's panel, providers apart. A version is
immutable — a fix is a new version.

## Traps the check cannot see

- A sample that renders but shows nothing useful (an empty table, a closed dialog) passes: look at the pictures.
- Descriptions copied from the package's docs often say what a component *is*; rewrite them to what it is *for*.
- A component that cannot draw outside an application (router, network) gets no sample and a line in the README —
  do not fake one that breaks.
