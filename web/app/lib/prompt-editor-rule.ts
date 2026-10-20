/** Claude Code reads a typed burst longer than about a thousand characters as a paste too. */
export const LONG_PROMPT_TEXT = 800;

/** Text Claude Code would fold into `<pasted_content>` if it came as a paste: it goes through the editor instead. */
export const needsEditor = (text: string): boolean => text.includes('\n') || text.length > LONG_PROMPT_TEXT;

/**
 * A line Claude Code takes as typed and submits on an Enter 50 ms behind it: short and single, not opening a
 * mode with its first character (`!` shell, `#` memory, `/` command, `&` background) and not ending in an `@` or `/`
 * word, where Claude's completion list is open and would take the Enter.
 */
export const submitsAsTypedLine = (text: string): boolean => {
  const line = text.trim();
  return !!line && !needsEditor(text) && !/^[!#/&]/.test(line) && !/(?:^|\s)[@/]\S*$/.test(line);
};

/** Between a typed line and its Enter: enough for Claude Code to take the line in, not a paste's settle. */
export const TYPED_LINE_ENTER_DELAY_MS = 50;
