package main

// looksPromptText is the workspace prompt's part on the person's own look of harnessly (docs/looks.md): a
// theme, a background or a style is data the page applies, never CSS an agent writes; a live effect (fx) is a built-in one
// or a script against the page's sandbox SDK, never anything else.
const looksPromptText = "## The look of harnessly\n\n" +
	"When the person asks to change how harnessly itself looks — a theme, colours, a background of the chats, an animated one " +
	"(matrix, stars, aurora, rain, synthwave, snow…), grain, a font, the message bubbles or the field, a live effect (\"сделай тему как ночной Токио\", " +
	"\"поставь на эту папку матрицу\", \"фон со звёздами в этом чате\") — change it in that turn with the MCP tool `theme_set` " +
	"(`mcp__harnessly__theme_set` in Claude Code) or `harnessly theme preset <id> | set '<look JSON>' | reset [--scope all|folder|chat] --why \"…\"`; " +
	"`theme_get` / `harnessly theme presets` list the ready looks. `scope` is all chats (the default), this chat's folder or this chat. " +
	"The person's page applies it at once with «Применено · Отменить». Never write CSS or styles for it, and do not edit harnessly's files.\n\n" +
	"A live effect (\"живая тема\", snow that settles on the blocks, a lamp, a laser) is the look's `fx`: `{\"fx\":{\"preset\":\"snowplay|nightlamp|laser\"}}` " +
	"(`harnessly theme fx-presets`), or your own code when none fits — `theme_set` with `fx_code`, or `harnessly theme set '<look>' --fx-file effect.js`: " +
	"a script that starts with the line `//hx-fx v1` and uses only the page's sandbox SDK `hx` (docs/looks.md, «Живые эффекты»); " +
	"it runs in a sandboxed frame with no network and never sees the chat's text. Prefer a built-in effect; write code only for what none of them does.\n\n"
