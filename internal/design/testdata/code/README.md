# The proof of `harnessly design code`

A neutral kit and the package it stands for, a project made of it, and an application to export into:

- `package/acme-ui/` — the npm package `@acme/ui`: `components.js` (the components, written once), `index.js`,
  `index.d.ts`, `styles.css`. The kit's `components/bundle.js` is `components.js` wrapped as a classic script
  (`TestDesignCodeProof` writes it), so the mockup and the application draw the same code.
- `kit/` — the kit `acme-ui` 1.0.0 over the package: index, tokens (`accent` is the library's `--acme-color-accent`,
  so the export has a variable to rewrite), the preset `PrimaryButton`.
- `project/` — the design project `events` (strict mode on): one page module, its fixtures, a local component.
- `app/` — the application: `sinopia.export.json`, a Vite-free entry (`src/main.tsx`) that draws the first route.

`go test ./internal/design -run TestDesignCode` checks the strict gate, the files, the second export and the conflicts.
`DESIGN_CODE_PROOF=<dir> go test ./internal/design -run TestDesignCodeProof` leaves a built mockup and the exported
application in `<dir>`; `node web/scripts/design-code-proof.mjs <dir>` then runs the types, the build and the
pixel comparison.
