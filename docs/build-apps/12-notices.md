# Notices: ask the person in the smart notice

The smart notice is the black plate at the top of the person's screen. It shows what waits for them: a code, an agent's
question, a finished chat. Your app and your agent can put their own element there and get the answer back, without the
person leaving what they are doing. The same description works three ways:

| Who asks | How |
|---|---|
| An app's view | `hl.notice.ask(spec)`, `hl.notice.show(spec)` (needs the `notices` right in `app.json`) |
| An agent, from a shell | `harnessly notice ask …`, `harnessly notice show …` |
| An agent, by MCP | the tools `notice_ask`, `notice_show`, `notice_update`, `notice_close` |

Every plate carries the label of its source, which the caller cannot change: «Приложение · <app name>» from the
manifest, or «Агент · <chat title>». What you send is only the text under it.

## What it can show

| Kind | Inside | Answer | When to use it |
|---|---|---|---|
| `status` | an icon and a line or two | none; goes by itself in ~8 s | something finished or started |
| `progress` | a bar, moved by `update` | none; stays until `close` or its timeout | long work the person waits for |
| `choice` | the question and 2–6 options (`own: true` adds a line for own words) | `{choice, option}` or `{text}` | a fork the person decides |
| `confirm` | what will happen, yes and no (`danger: true` for what cannot be undone) | `true` / `false` | before an irreversible step |
| `field` | one line of text (`placeholder`) | the text | a name, a title, a short comment |
| `code` | 4–8 digit cells (`digits`) — apps only | the digits | a confirmation code (a mail confirming a subscription, a device pairing) — never a sign-in code |
| `custom` (apps only) | your own layout and buttons, up to 420×320 | the pressed button's `data-answer` | when the ready elements are not enough |

```js
// In a view (the app's manifest lists "notices" in permissions)
const r = await hl.notice.ask({
  kind: 'choice',
  title: 'Куда выкатить сборку 482?',
  options: ['На стенд', 'В тест', 'Никуда'],
  own: true,
});
if (r.status === 'answered') deploy(r.value.option ?? r.value.text);

const { id } = await hl.notice.show({ kind: 'progress', title: 'Собираю сборку 482', progress: 0 });
await hl.notice.update(id, { progress: 0.6, text: 'Тесты · 120 из 200' });
await hl.notice.close(id);
```

```sh
# From an agent
harnessly notice ask --kind confirm --title "Удалить 3 старые таблицы?" --text "orders_tmp, orders_bak" --danger --yes "Удалить"
# {"status":"answered","value":true}   (exit 4 when not answered)
```

`ask` resolves with `{status, value}`:

- `answered` — `value` is the answer (see the table);
- `dismissed` — the person said no or closed it;
- `timeout` — nobody answered in time (`timeoutSec`, 10 minutes by default, a day at most);
- `muted` — the person turned your notices off.

Hiding the plate is not an answer: the question waits as a small drop in the chat's right column, and the person comes
back to it.

## Style: tokens and the kit

The ready kinds are drawn by harnessly in the plate's own style, the same springs, stack and drop as everything else.
A `custom` view is drawn right in the plate, and it is description only: layout, text and buttons. There is no script,
no handler, no field, no form, no frame and no link in it — a view cannot run code, ask for anything typed or take the
person to another page. A view that brings one is refused (`bad-notice`), and the page draws only the allowed part.

What a view may hold:

- tags: `div`, `span`, `p`, `b`, `strong`, `i`, `em`, `small`, `br`, `hr`, `ul`, `ol`, `li`, `h3`, `h4`, `button`,
  `img` (`src` only as `data:image/png|jpeg|webp|gif;base64,…`);
- attributes: `class` (the kit's `ink-*` classes only), `data-answer`, `data-on`, `data-tone`, `alt`, `aria-label`,
  `title`.

A button with `data-answer` answers: its value as JSON, or as text when it is not JSON. The view gets these tokens:

| Token | What it is |
|---|---|
| `--ink-bg`, `--ink-fg` | the plate's black and white |
| `--ink-fg2`, `--ink-fg3` | secondary and quiet text |
| `--ink-fill`, `--ink-fill2` | a surface on the plate, a stronger one |
| `--ink-line` | a hairline |
| `--ink-danger` | what cannot be undone |
| `--ink-radius`, `--ink-radius-lg`, `--ink-gap` | corners and spacing |

and the kit built on them: `.ink-grid`, `.ink-row`, `.ink-end`, `.ink-tile` (`data-on` for the marked one), `.ink-btn`
(`data-tone="soft"` or `"danger"`), `.ink-panel`, `.ink-text`, `.ink-muted`. Your `<style>` blocks, `style` attributes
and other classes are dropped: a custom view looks like the plate or it does not show.

```js
const r = await hl.notice.ask({
  kind: 'custom',
  title: 'Какую обложку взять?',
  view: `
    <div class="ink-grid">
      <button class="ink-tile" data-answer='{"cover":"dark"}'>Тёмная</button>
      <button class="ink-tile" data-answer='{"cover":"light"}'>Светлая</button>
    </div>
    <p class="ink-muted">Обложка встанет на все письма рассылки.</p>`,
});
// r.value → {cover: 'dark'}
```

The plate grows to the view's height, 320 px at most. An agent cannot send a custom view: it has the ready kinds.

## Never: passwords, keys, sign-ins

The slot does not ask for secrets, and there is no way to make it:

- no kind has a password field, and none will;
- a `field` or `code` worded as a password, a key, a token or a sign-in is refused (`no-secrets`);
- a `custom` view has no field at all: one that brings an input, a form, a script or a link is refused, and the page
  draws none of them;
- a text the person types that looks like a key (`sk-…`, `ghp_…`, a JWT, a private key, a long random word) is not sent:
  the plate tells them to put it in the safe, and the machine refuses it again (`looks-like-key`);
- an agent never gets a one-time code through the slot: `harnessly secret run --code` puts it straight into the command
  that needs it.

For secrets use the safe: `harnessly secret ask NAME --why …` for an agent, `hl.secrets.put` and the `secrets` right for
an app. For signing in to a site use the browser's sign-in from the safe. See [Sign-in and secrets](10-auth.md).

## Limits and the person's control

So that the slot stays the person's and not the loudest app's:

- an app needs the `notices` right in its manifest; the person sees «Спрашивать вас в умном уведомлении» when installing;
- one waiting notice of a source per chat is shown at a time, at most three of a source on the machine; the rest wait
  their turn (twelve open at most, then `busy`);
- at most 5 new notices a minute per source (`too-often`); the same question asked again while it is open joins it;
- `status` and `progress` make no sound and leave no drop; questions sound like an agent's question;
- the plate's menu has «Не показывать от …»: the source is off until the person turns it back on, and its open notices end
  as `muted`.

Ask for a decision the work cannot go on without. Progress the person does not wait for, routine news and anything
that fits the chat belong in the chat.
