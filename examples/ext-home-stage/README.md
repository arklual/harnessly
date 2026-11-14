# ext-home-stage

A view of the start page that takes the whole stage (`"height": "stage"` on a `home` view): the greeting and the field
stand at the top, the view fills the rest and scrolls inside itself, and `hl.slot.greet({title, sub})` puts its own
greeting in place of the host's question. Plain HTML and the app SDK; the name is neutral (`example-corp`).

```sh
HARNESSLY_UI_SLOTS=on harnessly runtime   # the slots are off without the flag
harnessly app install examples/ext-home-stage
```

Open the start page. Without the app, or with the slots off, the page stays as it was. See `docs/apps.md`, «Slots of the
interface».
