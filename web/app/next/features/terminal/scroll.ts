// The terminal's scrolling: the history lives in tmux, which scrolls it in copy mode when the wheel comes to
// it as a mouse report. These are the pure parts of xterm-socket.ts.

/** tmux's window title under the runtime's tmux.conf: hx-scroll:1 while the pane is scrolled back. */
const SCROLL_TITLE = /^hx-scroll:([01])$/;
/** Mouse reports (SGR and X10) and focus reports: what xterm sends on its own, not a key the person typed. */
const NOT_TYPED = /^\x1b\[(<|M|I$|O$)/;
/** SGR wheel reports, the way xterm sends the wheel to tmux: 64 up, 65 down, at the first cell. */
const WHEEL_UP = '\x1b[<64;1;1M';
const WHEEL_DOWN = '\x1b[<65;1;1M';

/** Whether the pane is scrolled back, by tmux's title; null for a title that says nothing of it. */
export const scrolledByTitle = (title: string): boolean | null => {
  const match = SCROLL_TITLE.exec(title);
  return match ? match[1] === '1' : null;
};

/** Whether the bytes xterm sends are a key the person typed (and so bring a scrolled-back pane down). */
export const isTyped = (data: string): boolean => !NOT_TYPED.test(data);

/** xterm turns the wheel into ↑/↓ on a screen without scrollback when nobody asked for the mouse: such a wheel must
 *  not reach xterm, or the shell and Claude Code page through their input history. */
export const wheelAllowed = (buffer: 'normal' | 'alternate', mouseTracking: string): boolean =>
  !(buffer === 'alternate' && mouseTracking === 'none');

/** A swipe of dy pixels over rows of rowHeight: the wheel reports it makes (a finger down scrolls back) and the
 *  pixels they used. */
export const swipeWheel = (dy: number, rowHeight: number): { reports: string; used: number } => {
  if (rowHeight <= 0) return { reports: '', used: 0 };
  const notches = Math.trunc(dy / rowHeight);
  return {
    reports: (notches > 0 ? WHEEL_UP : WHEEL_DOWN).repeat(Math.abs(notches)),
    used: notches * rowHeight,
  };
};
