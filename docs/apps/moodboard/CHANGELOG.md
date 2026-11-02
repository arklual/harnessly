# Changelog

## 0.1.0

- A document type of its own, `acme.moodboard`: the editor is a view of the app, the cards live in the document's own storage
  (`hl.storage.doc`), the format is described by `schema/moodboard.json`. Version 2 of the format renamed `notes` to `cards`;
  `migrations/1-to-2.json` brings a document of the first version up.
