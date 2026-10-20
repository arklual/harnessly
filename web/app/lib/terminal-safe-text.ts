export const ESCAPE_MARK = '␛';

// C0 except \t (0x09) and \n (0x0a), ESC (0x1b) handled apart, DEL and C1.
const CONTROLS = /[\u0000-\u0008\u000b-\u001a\u001c-\u001f\u007f-\u009f]/g;

/**
 * Text bound for a terminal prompt. A terminal treats any control character as a key press or a sequence; one `ESC [ 2 0 1 ~`
 * ends a bracketed paste early and the rest is typed. ESC stays visible as `␛`, the other controls go, tabs and newlines stay.
 */
export const stripControlChars = (text: string): string =>
  text.replaceAll('\u001b', ESCAPE_MARK).replace(CONTROLS, '');

/** The same for a value that must stay on one line, such as an attribute. */
export const oneLineSafe = (text: string): string => stripControlChars(text.replace(/\s+/g, ' ')).trim();
