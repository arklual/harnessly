# Quick edit: let the person change your object by voice

The person says «поменяй заголовок на "Итоги недели"» over your app's object, and the change lands in a few seconds:
no chat turn, no agent reading the whole object. Harnessly plans the edit with a fast model; your app reads and writes
its own object. Declare three tools of your backend in `app.json`:

```json
"quickEdit": {
  "read": "boards.quick_read",
  "apply": "boards.quick_apply",
  "undo": "boards.quick_undo"
}
```

| Tool | Takes | Answers |
|---|---|---|
| `read` | `{object, selection?}` | `{text, version}`: the object as plain text, one element a line, and a version of that text |
| `apply` | `{object, version, edits: [{old, new}], confirmed?}` | `{changed, version, hint, ids?}`: how many edits landed, the new version, the changed lines, the elements to light up |
| `undo` (optional) | `{object, version}` | puts back the apply that answered `version`; `conflict` when the object changed since |

The rules the host keeps and your tools should keep too:

- Each `old` is found in the text exactly once; an edit that matches nothing or twice is refused (`not_unique`).
- A `version` that is not the object's now is refused (`conflict`): someone changed it meanwhile.
- An edit that removes lines needs the person's «да»: without `confirmed: true` answer `needs_yes` and change nothing.
- Undo with nothing to take back answers `nothing_to_undo`.

The tools answer JSON as `structuredContent` (or as the first text). Errors are the tool's error result with `{"code", "message"}` as its text. The tools run as the person, with the
rights the person has on the object: a person who may only read gets `forbidden`.

When the edit has landed, the host tells the app's open view with the event `quick-edit` (`hl.on('event')`,
`{kind: "quick-edit", object, ids, hint}`, no `events:` permission needed: it is the app's own object): light up the
changed elements for a second, so the person sees what moved.

The page asks the runtime with `POST <platform>/voice/edit/app {app, object, selection?, ask}`; a removal comes back as
`409 edit_needs_yes` with the planned words, and the same request with `confirm: true` applies that plan after «да».

Each tool is checked when the app is installed: `<server>.<tool>` of a server the app declares (`backend.mcp`).
