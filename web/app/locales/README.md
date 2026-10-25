# Page texts: how the dictionaries work

Every text the page shows lives in a dictionary, never in the component. English is the default and Russian is the
second language. The page picks one by `RUNTIME_LOCALE` (`en` or `ru`), which the runtime passes to the page in its
config. The Go runtime reads the same variable for the texts it answers with (see "The runtime's own texts" below).

## Files

```
web/app/locales/en/<namespace>.json   English, the source of truth
web/app/locales/ru/<namespace>.json   Russian, the same keys
```

Each file is a flat object `{ "<key>": "text" }`. `web/app/lib/i18n.ts` loads every file of a language and merges them,
so a namespace is only a way to keep files small. A key missing in Russian falls back to the English text; a key
missing in English shows the key itself, which is a bug to fix. Keep both languages complete.

### Namespaces

A namespace is the first part of a key and names the part of the page it belongs to.

| Namespace                                                                        | Part of the page                                   |
| -------------------------------------------------------------------------------- | -------------------------------------------------- |
| `agents`                                                                         | the agents page and its shared pieces              |
| `chat`, `composer`, `header`, `sidebar`, `sideChat`                              | the conversation view and its chrome               |
| `docs`, `design`, `devtools`, `gallery`, `vault`, `share`, `stats`, `efficiency` | the tabs of the right pane and the dashboards      |
| `inspector`, `pane`                                                              | the inspector islands and the right pane itself    |
| `timeline`                                                                       | the conversation timeline                          |
| `workspace`, `pages`, `auth`, `hooks`, `primitives`, `ui`                        | the shell, routes, sign-in, hooks and base widgets |
| `lib`                                                                            | `app/lib` and `app/types`: helpers and data labels |
| `next`                                                                           | the new interface (`app/next`) and its switch      |

## Adding a text

1. Pick the key `<namespace>.<component>.<name>`. The component is the file name in camelCase without the extension
   (`doc-viewer.tsx` is `docViewer`); the name is a short English meaning (`openConversation`, `errorUnreachable`) that
   is unique within the file.
2. Add the key to `en/<namespace>.json` and to `ru/<namespace>.json`. Keep the keys sorted the way the file already is.
3. Use it:

```tsx
import { t } from '@/lib/i18n';

<button title={t('docs.docViewer.copyLink')}>{t('docs.docViewer.copyLinkLabel')}</button>;
```

`t` can be called at module level too: the language is known before any module runs.

### Placeholders

Write `{name}` in the text and pass the values: `t('docs.docViewer.opened', { title })` for
`"opened": "Opened {title}"`. Never build a sentence from pieces in code, because word order differs between languages:
give the whole sentence a key.

### Plurals

A counted text is an object with one entry per plural category of the language, and `{n}` is the count:

```json
// en
"lines": { "one": "{n} line", "other": "{n} lines" }
// ru
"lines": { "one": "{n} строка", "few": "{n} строки", "many": "{n} строк", "other": "{n} строки" }
```

```tsx
import { plural } from '@/lib/i18n';

plural(count, 'docs.docViewer.lines');
```

`plural` selects the category with `Intl.PluralRules` for the page language. Do not write `% 10` or `% 100` helpers.

## What stays as it is

- Regular expressions and tables that recognise data, for instance tracker state names in either language or
  speech-recognition noise.
- Values another service compares against: protocol values, stored identifiers, values already saved by an older
  version.
- Tests may use any language they need.

A Cyrillic search over the page code (`grep -rnP '[А-Яа-яЁё]' web/app --include=*.ts --include=*.tsx`) should find only
these. Leave a short English comment on each such place saying why it stays.

## Style

Plain, short, sentence case. Prefer a colon or a period to a dash. Name things the way the interface labels them. The
English text is written first and the Russian one translates it, not the other way round.

## Adding a language

1. Copy `en/` to a new folder `<code>/` and translate the values. Keep the keys and the `{placeholders}`; give plural
   entries the categories the language needs (`Intl.PluralRules('<code>').resolvedOptions().pluralCategories`).
2. Teach the loader about it: widen the `'en' | 'ru'` type of the `locale` field and the check that reads it
   (`read.locale === 'ru'`) in `web/app/platform/config.ts`, and add the language to `load()` in `web/app/lib/i18n.ts`.
3. The runtime already passes `RUNTIME_LOCALE` on to the page unchanged (`pageConfigFromEnv` in
   `cmd/runtime/web_config.go`), so nothing is needed there.
4. The Go runtime answers its own texts (error bodies, notification text, the snapshot page, the command catalog) in
   English or Russian only, through `internal/locale`. For another language, extend `locale.Pick` and `locale.T` there.

## The runtime's own texts

The Go side has its own small helper: `locale.T(en, ru)` for a fixed message and `locale.Pick(en, ru)` for a text built
from parts. Both read `RUNTIME_LOCALE` on every call, and anything but `ru` is English. Go tests that keep Russian
goldens set `RUNTIME_LOCALE=ru` in a `TestMain`; the tests of the English output set it with `t.Setenv`.

Where the page and the runtime write the same text (the placeholder of a hidden secret, the notes of a shared snapshot),
the two are kept byte for byte equal per language and each side still recognises the other language's text.

## Tools

`node scripts/i18n-extract.cjs <path to the typescript package> app <out.json>`, run from `web/`, lists every Russian
literal left in the page code with its position and shape, using the TypeScript parser. Run it before a release to see
what is not in a dictionary yet.
