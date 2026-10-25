// Which keys a focused terminal leaves to the page. Everything else is the terminal's: xterm turns it into bytes for
// the session, and nothing of the page (the search, the rail, the digits of an open question, the composer) sees it.

export interface ITerminalKey {
  key: string;
  code: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}

/**
 * The page's own keys that stay while the terminal has the focus: ⌃` folds the dock, ⌥⌘S / ⌥⌃S is the side chat,
 * ⌘K and ⌘B are the search and the rail. ⌃K and ⌃B are not: they are the shell's (kill the line, one char back).
 */
export const isReservedKey = (event: ITerminalKey): boolean => {
  if (event.ctrlKey && !event.metaKey && !event.altKey && event.key === '`') return true;
  if (event.altKey && (event.metaKey || event.ctrlKey) && event.code === 'KeyS') return true;
  if (event.metaKey && !event.ctrlKey && !event.altKey) return event.code === 'KeyK' || event.code === 'KeyB';
  return false;
};

/** ⌃⇧C copies the selection and ⌃⇧V pastes it, as in the terminals of Linux and Windows. */
export const isCopyKey = (event: ITerminalKey): boolean =>
  event.ctrlKey && event.shiftKey && !event.metaKey && !event.altKey && event.code === 'KeyC';
export const isPasteKey = (event: ITerminalKey): boolean =>
  event.ctrlKey && event.shiftKey && !event.metaKey && !event.altKey && event.code === 'KeyV';

/**
 * Keys that a terminal's own keyboard sends as something else than xterm's default: the word jumps of ⌥←/⌥→ are
 * ESC b / ESC f (what the Mac terminals send, and what readline, zsh and the agents' prompts understand), and on a
 * Mac ⌘←, ⌘→ and ⌘⌫ are the line's start, its end and its erasing, as in Terminal.app. null: xterm's own bytes.
 */
export const mappedBytes = (event: ITerminalKey, mac: boolean): string | null => {
  if (event.ctrlKey || event.shiftKey) return null;
  if (event.altKey && !event.metaKey) {
    if (event.key === 'ArrowLeft') return '\x1bb';
    if (event.key === 'ArrowRight') return '\x1bf';
    return null;
  }
  if (mac && event.metaKey && !event.altKey) {
    if (event.key === 'ArrowLeft') return '\x01';
    if (event.key === 'ArrowRight') return '\x05';
    if (event.key === 'Backspace') return '\x15';
  }
  return null;
};
