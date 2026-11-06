# ext-ui-slots

A package that fills the interface slots of harnessly with no React and no fork: a settings block, a first-run step, a
button at a machine and the product's name and accent. Everything is plain HTML and the app SDK; the name is neutral
(`example-corp`).

```sh
HARNESSLY_UI_SLOTS=on harnessly runtime   # the slots are off without the flag
harnessly app install examples/ext-ui-slots
```

Open Settings → Extensions for the block and the step, Settings → Machines for the button. The rights are the manifest's
`permissions` (here only `storage`). See `docs/apps.md`, «Slots of the interface».
