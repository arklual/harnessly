/** Pointer events do not carry a useful click count, so keep a browser-like sequence per button. */
export function nextClick(
  previous: { time: number; x: number; y: number; button: number; count: number } | null,
  event: { clientX: number; clientY: number; button: number },
  time: number,
) {
  const repeated =
    previous &&
    time - previous.time <= 500 &&
    previous.button === event.button &&
    Math.hypot(event.clientX - previous.x, event.clientY - previous.y) <= 5;
  return {
    time,
    x: event.clientX,
    y: event.clientY,
    button: event.button,
    count: repeated ? (previous.count % 3) + 1 : 1,
  };
}

/** A move reports button=-1; the held-button bitmask still identifies a drag. */
export function pointerButton(button: number, buttons: number) {
  if (button >= 0) return ['left', 'middle', 'right'][button] ?? 'none';
  return buttons & 1 ? 'left' : buttons & 2 ? 'right' : buttons & 4 ? 'middle' : 'none';
}

/** Technical CDP messages stay in the console; the pane exposes translated recovery advice. */
export function noticeKey(message: string) {
  if (/dialog/i.test(message)) return 'noticeDialog';
  if (/unknown browser tab|no longer available|not attached|target closed|session.*closed/i.test(message))
    return 'noticePage';
  if (/deadline|timeout|timed out/i.test(message)) return 'noticeTimeout';
  if (/closed|disconnect/i.test(message)) return 'noticeConnection';
  if (/clipboard/i.test(message)) return 'noticeClipboard';
  return 'noticeFailed';
}
