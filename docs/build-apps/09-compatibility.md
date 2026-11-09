# Compatibility: an app that survives updates

An app that worked yesterday works after any update of harnessly, without a change by its author. This page is what
the host promises and what an app (or the agent writing it) does to keep that.

## What is stable

- **The SDK only grows.** No method, event, context field or right is removed or changes meaning. When something has
  to work otherwise, it gets a new name beside the old one, and the old one keeps working.
- **Deprecated works.** A deprecated method still runs; the app's inspector says «deprecated, use …». Only a new major
  version of the protocol, which the app picks itself, may drop it.
- **Rights are never taken back.** An install keeps the rights it was given. A new right comes only with a new version
  of the app and the person's click on the install screen.

## Declare the range of harnessly

`harnessly` in `app.json` is the range of host versions the app is written for:

```json
{ "harnessly": ">=0.16" }
```

A host outside the range does not start the app and says why («нужна другая версия harnessly»). Name the lowest
version that has what you use; leave the upper end open.

## Ask whether the host can, not which version it is

```js
const hl = await harnessly.connect({ protocol: 2 });
if (hl.has('files.read')) {
  const text = await hl.files.read('README.md');
}
```

`hl.has(name)` is true when this host does the method here and the app holds its right. It takes the SDK's names
(`files.read`, `storage.user.get`, `chat.send`) or a bridge method's own (`harnessly/notify`). On an older host a newer
method is simply false: the app goes on without it.

## Handle a refusal

A call is refused when the app lacks the right, the place does not allow it, or the host does not do it. Two ways to
meet it:

- **Protocol 1 (what `connect()` does without an argument, kept for apps written before 2).** The call rejects with `HarnesslyError`; `error.code` says why. Catch it:

  ```js
  try {
    await hl.chat.send('Готово');
  } catch (e) {
    if (e.code === 'permission_denied') showHint();
  }
  ```

- **Protocol 2 (use it in a new app: the templates do).** Ask for it at the handshake, and every method answers a refusal as a value instead of rejecting:

  ```js
  const hl = await harnessly.connect({ protocol: 2 });
  const answer = await hl.chat.read({ last: 10 });
  if (hl.refused(answer)) {
    // answer.error is 'denied' (no right, not allowed here), 'unsupported' (this host does not do it) or 'failed'
    return;
  }
  ```

The host speaks every protocol it ever spoke, so an app keeps working whichever it names. `hl.protocol` is the one
the host speaks with the view (1 on a host that does not know 2).

## Deprecated → replacement

| Deprecated | Use instead |
| --- | --- |
| `hl.flow.status()`, `hl.flow.go()` — removed before this promise, answers `invalid_params` | `hl.storage.<scope>.get/set` |
| `hl.data.get()`, `hl.data.set()` — removed before this promise, answers `invalid_params` | `hl.storage.<scope>.get/set` |

The table of the host is `deprecated` in `harnessly app catalog` and in [the reference](../apps/reference.md).

## What the person sees at install

Everything the app reaches is listed on the install screen in plain words, the dangerous on top, under the warning
that apps are not checked. An update that asks for more shows only what it adds, marked «new»; without the person's
word the old version keeps running. Ask for what the app needs, no more: every line is one more reason to say no.

## The corpus of old apps

`web/app/next/features/app-views/fixtures/compat` and the apps of `docs/apps` run against every change of the host
(`compat-corpus.test.ts`, `internal/apps/compat_test.go`): handshake, the calls they make on start, their page tools,
an install from before keeping its rights. A host change that breaks one of them does not merge.
