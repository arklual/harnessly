# An existing site: the agent drives your admin panel

You have an admin panel already (Angular, React, anything) and want the agent of the chat to work in it: read what a
form holds, fill it, press «Save». Not by looking at the screen and clicking: the panel tells the agent its state and
its actions through short hooks in its own code, and the agent reads and writes the **model of the form** with one
structured call. Every check that holds for a person holds for the agent, because it writes through the same model and
saves with the same function.

Three primitives, all on `window.harnessly`, all doing nothing outside Harnessly:

| Primitive | The author gives | The agent gets |
| --- | --- | --- |
| `state(name, options)` | description, schema, `get`, `set` | the value in the context of its turn and the tool `<name>_set` |
| `action(name, options)` | description, schema of the arguments, `run`, `mutates` | the tool `<name>` |
| `screen(options)` | title, route, prompt | «the person is on screen X» and what to do there |

## The package

The panel is shown as a view at its own address. The package is a manifest and instructions, nothing else:

```json
{
  "id": "acme.admin",
  "name": "Acme admin",
  "version": "1.0.0",
  "targets": ["pane"],
  "permissions": ["tools.page", "context.publish"],
  "network": { "frames": ["admin.acme.example"] },
  "views": [{ "id": "main", "title": "Admin", "entry": "https://admin.acme.example/", "targets": ["pane"] }]
}
```

The panel must let Harnessly frame it (`Content-Security-Policy: frame-ancestors` with the address of your Harnessly).
Its sign-in cookie is a third-party one inside a frame: check the sign-in in Safari before anything else.

## The loader

One inline line in the `<head>` of the panel's shell, before the framework's scripts:

```html
<script>(function(w,d,b,h){var m=w.parent!==w&&/(?:^#|&)hl-nonce=([\w-]+)/.exec(location.hash),q=m?[]:null,f=function(k){return function(){var c={k:k,a:arguments};q&&q.push(c);return{unregister:function(){c.off=1;c.h&&c.h.unregister()},refresh:function(){c.h&&c.h.refresh()},clear:function(){c.off=1;c.h&&c.h.clear()}}}};w.harnessly={q:q,hosts:h,nonce:m&&m[1],state:f('state'),action:f('action'),screen:f('screen')};if(m){var s=d.createElement('script');s.src=b+'/loader.js';d.head.appendChild(s)}})(window,document,'https://HARNESSLY_VIEWS/__hl/v1',['https://HARNESSLY_HOST']);</script>
```

What it does:

- **Outside Harnessly** (the page is not in a frame, or its address has no `#hl-nonce=…`, which the host adds when it
  opens the view) it loads nothing. `window.harnessly` is a stand-in: `state`, `action` and `screen` answer handles
  whose methods do nothing. The panel's code never asks where it runs, and an ordinary user downloads not a byte.
- **Inside** it loads `loader.js` from the folder the line names; the loader loads `bridge-shim.js` and
  `app-sdk.classic.js` from the same folder, replays what the panel called meanwhile and connects. Calls made before the
  SDK is there are kept in their order; a handle dropped meanwhile is not replayed.

Replace `https://HARNESSLY_VIEWS/__hl/v1` with the folder the files are in. Two ways:

- **Your own copy** (works everywhere): `harnessly app sdk ./public/hl` writes `loader.js`, `bridge-shim.js`,
  `app-sdk.classic.js`, `loader-inline.js` and the types; the line names `https://admin.acme.example/hl`. Run it again
  when you update Harnessly.
- **The view server of your Harnessly**: it serves the same files at `/__hl/v1/`. The `v1` is the major version of the
  SDK's contract: an update of Harnessly keeps `v1` working for a panel that names it.

The address is written by you in the line, never taken from the page's address: the fragment only says «inside», it
cannot say where to load a script from. Allow the folder in the panel's `script-src` if it has a CSP.

**Whom the panel trusts.** Replace `https://HARNESSLY_HOST` at the end of the line with the origin of your Harnessly's
page; there may be several. The fragment in the address is not a proof: any page that frames the panel can write it.
So the loader lets a handshake through only from the origins you named, and a foreign page reads no state and calls no
tool in your user's session. Without the list the loader loads nothing. Keep `frame-ancestors` too: it is the second
line.

The nonce is taken out of the address at once. A full reload of the frame has no nonce, so the page is «outside» until
the view is opened again; navigation inside a single-page app keeps the connection.

## State

```js
const handle = window.harnessly.state('event', {
  description: 'The form of a new event.',
  schema: { type: 'object', properties: { title: { type: 'string' }, price: { type: 'number', minimum: 0 } } },
  get: () => form.getRawValue(),
  set: (patch) => { form.patchValue(patch); return errorsOf(form); },
  subscribe: (changed) => form.valueChanges.subscribe(changed),
  rules: [{ id: 'title-case', text: 'The title is not in capitals.', examples: { pass: ['Jazz night'], fail: ['JAZZ NIGHT'] } }],
  check: (value) => (value.discount > 30 ? [{ field: 'discount', level: 'warn', message: 'The discount is above the usual; tell the person.' }] : []),
});
// when the component goes:
handle.unregister();
```

- **The tool `event_set` takes a patch**: only the fields to change. The SDK calls `set(patch)`, then `get()`, and
  answers the value as it is now with every issue at once.
- **`set` answers the form's issues**: an array, `{ issues }`, or a throw. All of them, not the first.
- **`check(value)`** is rules in code, for the agent's calls only; its issues come with `source: "rule"`. It runs after
  `set`: the value is in the form already, the person sees it.
- **`rules`** are rules in words. The SDK puts them in the context and in the tool's description, so the agent knows
  them before it calls. Nothing judges by them yet.
- **The value republishes itself**: with `subscribe` 300 ms after the last change, without it after every `set` and on
  `handle.refresh()`. The agent sees it on its next turn.
- No confirmation: a state only fills the form. What changes data is an action.

## Action

```js
window.harnessly.action('event_save', {
  description: 'Save the event.',
  mutates: true,
  run: async () => {
    const saved = await api.saveEvent(form.getRawValue());   // the same function the button calls
    return { id: saved.id };
  },
});
```

- **`mutates`** is true unless you say `false`, and a mutating action asks the person before every call
  (`confirm: true`). `confirm: false` takes the question off for one action; `mutates: false` is for an action that
  only reads or opens something.
- **`ok: true` only when `run` finished.** A throw or an issue of level `error` is `ok: false`. A thrown error becomes
  an issue with `source: "server"`; throw `{ issues: [...] }` or answer `{ issues }` to say `form` and name fields.
- `run` may answer `{ result, issues }` to pass warnings on with a result.
- **`check(args)`** runs before `run`; an error stops the action, `run` is not called.

## Screen

```js
const screen = window.harnessly.screen({ title: 'New event', route: '/events/new', prompt: 'An event is created here; ask the date and the venue first.' });
// leaving the screen:
screen.clear();
```

The last call stands; `clear()` of a screen that was replaced does nothing. A change of the screen gives the agent no
turn: the context changes quietly.

## The answer and the issues

Every tool of a state or an action answers one shape:

```json
{
  "ok": false,
  "value": { "title": "CONCERT", "price": -5 },
  "issues": [
    { "source": "form", "field": "price", "level": "error", "message": "The price cannot be negative" },
    { "source": "rule", "field": "title", "level": "error", "message": "The title is not in capitals", "rule": "title-case" },
    { "source": "rule", "field": "discount", "level": "warn", "message": "The discount is above the usual; tell the person" }
  ]
}
```

| Field | What |
| --- | --- |
| `ok` | false while any issue has level `error` |
| `value` (a state) / `result` (an action) | the state after the patch; what `run` answered, `null` when it did not finish |
| `issues[].source` | `form` (the form's own validation), `rule` (a rule for the agent), `server` (the backend refused) |
| `issues[].field` | the field, `""` for the whole value |
| `issues[].level` | `error` blocks; `warn` does not, and the agent tells the person |
| `issues[].message` | the words the person sees under the field |
| `issues[].rule` | the rule's id, for `source: "rule"` |

What you leave out of an issue the SDK fills: `level` is `error`, `field` is `""`, `source` is `form` for `set`,
`server` for `run`, and always `rule` for `check`. A string is an issue with that message.

## The context of the window

A window with a state or a screen publishes one document (the right `context.publish`):

```json
{
  "screen": { "title": "New event", "route": "/events/new", "prompt": "…" },
  "states": { "event": { "description": "…", "schema": {}, "value": { "title": "" }, "rules": [] } },
  "data": { "anything": "the view passed to hl.publish" }
}
```

`hl.publish(data)` keeps working beside them: what you publish goes under `data`. A window with no state and no screen
publishes its data as it always did. The whole document is 64 KiB at most: publish what the agent needs, not a table
of a thousand rows.

## Wrappers

Two examples to copy into the shared package of your shell (15–20 lines each beside the imports):

- Angular: [`exposeForm(name, formGroup, options)`](../apps/examples/angular-expose-form.ts) reads `getRawValue()`,
  writes `patchValue()`, turns the controls' errors into issues and takes the state back through `DestroyRef`.
- React with react-hook-form: [`useAgentForm(name, form, options)`](../apps/examples/react-use-agent-form.ts) reads
  `getValues()`, writes `reset()`, turns `formState.errors` into issues and takes the state back in the effect's cleanup.

```ts
// Angular, in the form's component
exposeForm('event', this.form, { description: 'The form of a new event.', schema: eventSchema });
exposeAction('event_save', { description: 'Save the event.', mutates: true, run: () => this.save() });
```

```tsx
// React
useAgentForm('event', form, { description: 'The form of a new event.', schema: eventSchema });
useAgentAction('event_save', { description: 'Save the event.', mutates: true, run: save });
```

With microfrontends the loader line lives in the shell once; a remote calls the wrappers and loads nothing itself.

## Limits

- 32 tools a window (a state is one, an action is one), a tool's schema 16 KiB, its description 1000 characters (the
  rules in words are part of it), an answer 256 KiB.
- A name is latin letters, digits, `_`, `.`, `-`, 59 characters at most, and starts with a letter.
- A view built for Harnessly from the start uses the same three: `hl.state`, `hl.action`, `hl.screen` after `connect()`.
