package main

// looksPromptText is the workspace prompt's part on the person's own look of harnessly (docs/looks.md): a
// theme, a background or a style is data the page applies, never CSS or code an agent writes.
const looksPromptText = "## The look of harnessly\n\n" +
	"When the person asks to change how harnessly itself looks — a theme, colours, a background of the chats, an animated one " +
	"(matrix, stars, aurora, rain, synthwave, snow…), grain, a font, the message bubbles or the field (\"сделай тему как ночной Токио\", " +
	"\"поставь на эту папку матрицу\", \"фон со звёздами в этом чате\") — change it in that turn with the MCP tool `theme_set` " +
	"(`mcp__harnessly__theme_set` in Claude Code) or `harnessly theme preset <id> | set '<look JSON>' | reset [--scope all|folder|chat] --why \"…\"`; " +
	"`theme_get` / `harnessly theme presets` list the ready looks. `scope` is all chats (the default), this chat's folder or this chat. " +
	"The person's page applies it at once with «Применено · Отменить». Never write CSS, styles or code for it, and do not edit harnessly's files.\n\n"
