# «Что нового»: one entry, one file

The «Что нового» card shows a slide per entry, leafed to the right; the entries this browser has not been told of come
first. Every entry is a file of its own here, so two branches that each add one never touch the same file and merge
without conflicts.

## Adding an entry

Add `entries/<branch-slug>.json` (lower-case words with dashes, for example `hrn-713-whats-new-cards.json`); a branch
with two features adds two files. Nothing else changes: the build picks up every file in this folder.

```json
{
  "date": "2026-11-21",
  "icon": "gallery-horizontal",
  "ru": { "title": "…", "text": "…" },
  "en": { "title": "…", "text": "…" }
}
```

- `date` is the day it goes out, `YYYY-MM-DD`. The newest day comes first; within a day the order is by file name. An
  entry merged a few days after its date is still news to whoever was told of the newer ones.
- `icon` is a Lucide icon name that `app/next/styles/icons.css` has a glyph for; add the glyph there if it has none.
- `ru` and `en` are both required: a title of a few words and one or two sentences in the person's words — what changed
  for them, no versions, no file names. Other languages may be added the same way; without them a person sees English.

`entries.test.ts` and the build refuse an entry without a language, a day or an icon. `?dev=whatsnew` shows the card
with the entries of the last week.
