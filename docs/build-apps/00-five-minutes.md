# From zero to a working view in 5 minutes

You need `harnessly` on this machine and a chat open in Harnessly. Nothing else: the first view needs no build tools.

## 1. Create it

```sh
harnessly app new acme.hello --template pane-chat
cd hello
harnessly app validate . --strict
```

`new` writes a folder that passes `validate` as it is. `--strict` makes warnings fail as well, so it is a good habit from the first
minute. The view is already in the look of Harnessly: it loads `/__hl/hl-ui.css` (the platform's colours and the `.hl-*` classes), so
light and dark follow the pane.

```text
hello/
  harnessly/app.json     # the manifest: id, targets, permissions, the view
  ui/index.html          # the view's page
  ui/main.js             # connects to the host; shows loading and an error on the page
  ui/style.css           # yours
  instructions.md, skills/…   # what the agent gets (can stay as it is)
```

## 2. Install the folder itself and turn it on

```sh
harnessly app dev .
harnessly app enable acme.hello       # in the chat this tab runs
```

`dev` installs the folder, not a copy: an edit reaches the app at once. The view files are read again each time the view is opened.
A changed manifest, `instructions.md` or `.mcp.json` is read by an agent that starts after the edit.

## 3. Open it

The right pane: «+», then the app. Or `harnessly open app:acme.hello`. You see the title, where the view is shown, and a button that
writes into the chat. The page has three states, and `main.js` handles each: a skeleton while it connects, the page itself, and the
error with its code if the host does not answer. Open `ui/index.html` as a plain file, and the error is what you get: there is no host.

If the view stays blank, read the address it was opened at and the console of the pane (the «Отладка» switch on the app's page);
`harnessly app explain acme.hello` says why an app is off.

## 4. Change something

Edit `ui/main.js`: put another text into `show(…)`, reopen the view. Call another method of `hl` (`hl.storage`, `hl.files`; the
list is `harnessly app catalog`) and check the answer with `hl.refused(answer)`: protocol 2 answers a refused call with
`{ error, code, message }` instead of throwing, so one `if` covers «not allowed here», «this host cannot» and «failed».

## Recipe: Vite + React + TypeScript

```sh
harnessly app new acme.board --template vite
cd board
npm install
npm run build        # builds src/ into ui/
harnessly app dev .
harnessly app enable acme.board
```

`npm run dev` rebuilds on every save; reopen the view to see the build. The template is minimal on purpose:

- `index.html` (the source) loads the SDK as a classic script and `src/main.tsx` as a module; `vite build` writes the result into
  **`ui/`**, the folder the view is served from.
- **`base: './'` in `vite.config.ts` is what makes it work.** A view is served with its session in the address
  (`/<session>/<app>/ui/index.html`), so every address in the build must be relative to the page. An address from the root
  (`/assets/app.js`) leaves the session out and the file comes back 401. `validate` warns when it finds one in `ui/`.
- Module scripts, `fetch` of the view's own files and fonts work from host 0.16.198: the files are served with the session in the path
  and answer CORS to the opaque origin of the frame. Before that, only classic scripts loaded.
- Types of `window.harnessly` are in `src/app-sdk.d.ts` (a copy of `/__hl/app-sdk.d.ts`). The package `@harnessly/app-sdk` carries
  the same types and the ES module, for a bundler that imports it: `import { connect } from '@harnessly/app-sdk'`. It is not on npm:
  it lives in [the repository](https://github.com/ly-solutions/harnessly) (`internal/apps/sdk`); `scripts/app-sdk-package.sh` builds it
  into `dist/app-sdk-npm`, and `npm install <path>/dist/app-sdk-npm` adds it to your app.
- Nothing leaves the view without `network` in the manifest (a `fetch` of an outside address is refused, see [Backend](03-backend.md)).
  Only what is in `ui/` is served: keep the source in `src/`, outside.

## Where next

[Views](02-views.md) for cards, a screen of its own, theme tokens, storage; [Debugging](06-debugging.md) for what is picked up
when and every error code; [Backend](03-backend.md) when the view needs a server.
