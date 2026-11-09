// Every action the keyboard can do, in one list: its screen (where it works), its shortcuts as they come (a person can
// change them in Settings → «Клавиши»), and its words in next.keys.cmd.<id>. Esc and the digits of the video are
// fixed: the sheet shows them, Settings does not change them.

export const KEY_SCOPE = {
  global: 'global',
  composer: 'composer',
  deck: 'deck',
  video: 'video',
  pictures: 'pictures',
  site: 'site',
  files: 'files',
} as const;
export type TKeyScope = (typeof KEY_SCOPE)[keyof typeof KEY_SCOPE];

export interface IKeyCommand {
  id: string;
  scope: TKeyScope;
  keys: readonly string[];
  /** Shown on the shortcuts sheet, not changed in Settings. */
  fixed?: boolean;
}

const cmd = (scope: TKeyScope, id: string, ...keys: string[]): IKeyCommand => ({
  id: `${scope}.${id}`,
  scope,
  keys,
});
const fixed = (scope: TKeyScope, id: string, ...keys: string[]): IKeyCommand => ({
  ...cmd(scope, id, ...keys),
  fixed: true,
});
const DIGITS = Array.from({ length: 10 }, (_, n) => `Digit${n}`);

export const KEY_COMMANDS = [
  cmd('global', 'help', 'Mod+Slash', 'Shift+Slash'),
  cmd('global', 'search', 'Mod+KeyK'),
  cmd('global', 'newChat', 'Mod+Shift+KeyO'),
  cmd('global', 'focusField', 'Shift+Escape'),
  cmd('global', 'copyAnswer', 'Mod+Alt+KeyC'),
  cmd('global', 'newerChat', 'Alt+ArrowUp'),
  cmd('global', 'olderChat', 'Alt+ArrowDown'),
  cmd('global', 'rail', 'Mod+KeyB'),
  cmd('global', 'pane', 'Mod+Backslash'),
  cmd('global', 'sideChat', 'Mod+Alt+KeyS'),
  cmd('global', 'settings', 'Mod+Comma'),
  cmd('global', 'notice', 'Mod+KeyJ'),
  cmd('global', 'navigator', 'Mod+Alt+KeyJ'),
  ...Array.from({ length: 9 }, (_, n) => cmd('global', `chat${n + 1}`, `Mod+Digit${n + 1}`)),
  cmd('global', 'stepBack', 'Alt+ArrowLeft'),
  cmd('global', 'stepOn', 'Alt+ArrowRight'),

  // Dictation and the field's own actions. ⌘⇧Space is free in the browsers and the systems; ⌃C is the Mac's own ⌃
  // (on another system Ctrl+C stays the copy), ⌘⇧U is «upload».
  cmd('composer', 'dictate', 'Mod+Shift+Space'),
  // Esc stops a dictation and keeps the text (it is not a command here); cancelling it has no key until one is given.
  cmd('composer', 'cancelDictation'),
  cmd('composer', 'send', 'Mod+Enter'),
  cmd('composer', 'stopAgent', 'Ctrl+KeyC', 'Mod+Period'),
  cmd('composer', 'attach', 'Mod+Shift+KeyU'),

  cmd('deck', 'next', 'ArrowRight', 'ArrowDown', 'PageDown', 'Space'),
  cmd('deck', 'prev', 'ArrowLeft', 'ArrowUp', 'PageUp'),
  cmd('deck', 'first', 'Home'),
  cmd('deck', 'last', 'End'),
  cmd('deck', 'show', 'KeyP'),
  cmd('deck', 'rec', 'KeyR'),
  cmd('deck', 'comment', 'KeyC'),
  cmd('deck', 'full', 'KeyF'),
  fixed('deck', 'back', 'Escape'),

  cmd('video', 'toggle', 'Space', 'KeyK'),
  cmd('video', 'back5', 'ArrowLeft'),
  cmd('video', 'forward5', 'ArrowRight'),
  cmd('video', 'back10', 'Shift+ArrowLeft', 'KeyJ'),
  cmd('video', 'forward10', 'Shift+ArrowRight', 'KeyL'),
  cmd('video', 'frameBack', 'Comma'),
  cmd('video', 'frameForward', 'Period'),
  cmd('video', 'prevScene', 'ArrowUp', 'PageUp'),
  cmd('video', 'nextScene', 'ArrowDown', 'PageDown'),
  cmd('video', 'start', 'Home'),
  cmd('video', 'end', 'End'),
  cmd('video', 'markIn', 'KeyI'),
  cmd('video', 'markOut', 'KeyO'),
  cmd('video', 'comment', 'KeyC'),
  cmd('video', 'full', 'KeyF'),
  fixed('video', 'tenth', ...DIGITS),

  cmd('pictures', 'prev', 'ArrowLeft'),
  cmd('pictures', 'next', 'ArrowRight'),
  fixed('pictures', 'close', 'Escape'),

  cmd('site', 'reload', 'Mod+KeyR'),
  cmd('site', 'hardReload', 'Mod+Shift+KeyR'),
  cmd('site', 'devtools', 'Mod+Alt+KeyI'),
  cmd('site', 'inspect', 'Mod+Shift+KeyC'),
  cmd('site', 'pick', 'KeyV'),
  fixed('site', 'stopInspect', 'Escape'),

  cmd('files', 'quickOpen', 'Mod+KeyP'),
  cmd('files', 'find', 'Mod+Shift+KeyF'),
] as const satisfies readonly IKeyCommand[];

export const KEY_SCOPES: readonly TKeyScope[] = Object.values(KEY_SCOPE);

const BY_ID = new Map<string, IKeyCommand>(KEY_COMMANDS.map((c) => [c.id, c]));

export const commandOf = (id: string): IKeyCommand | undefined => BY_ID.get(id);
