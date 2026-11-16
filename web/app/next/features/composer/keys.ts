// The field's keys: Enter sends right away (into the agent's turn while it works, read at its next step), Alt/⌥+Enter
// sends after the turn (into the queue), Shift+Enter is a new line. Ctrl/Cmd+Enter sends right away too: a person who
// turned «Enter sends» off (Settings) has Enter as a new line everywhere, and that is what sends. On a touch keyboard
// Enter is a new line and the send button sends. An IME composition owns its Enter.

export const KEY_ACTION = { send: 'send', queue: 'queue', newline: 'newline' } as const;
export type TKeyAction = (typeof KEY_ACTION)[keyof typeof KEY_ACTION];

interface IKeyLike {
  key: string;
  shiftKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  isComposing: boolean;
}

/** What a key press in the field means; null: let the field have it. */
export const keyAction = (event: IKeyLike, touch: boolean, enterSends = true): TKeyAction | null => {
  if (event.key !== 'Enter' || event.isComposing) return null;
  if (event.altKey) return event.ctrlKey || event.metaKey || event.shiftKey ? null : KEY_ACTION.queue;
  if (event.ctrlKey || event.metaKey) return KEY_ACTION.send;
  if (event.shiftKey || touch || !enterSends) return KEY_ACTION.newline;
  return KEY_ACTION.send;
};

/** ⌘⇧S (Ctrl+Shift+S): put the draft aside. */
export const isStashKey = (event: IKeyLike): boolean =>
  event.key.toLowerCase() === 's' &&
  event.shiftKey &&
  !event.altKey &&
  (event.ctrlKey || event.metaKey) &&
  !event.isComposing;

/** The shortcut as the person's keyboard writes it. */
export const stashHint = (): string =>
  typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘⇧S' : 'Ctrl+Shift+S';
