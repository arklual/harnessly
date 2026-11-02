// A shortcut as one string: its modifiers in a fixed order and the key's place on the keyboard, «Mod+Shift+KeyO».
// The place (KeyboardEvent.code), not the letter, so a shortcut is the same key in the Russian layout and any other.
// Mod is ⌘ on a Mac and Ctrl elsewhere, so one saved shortcut fits both; Ctrl is the Mac's own ⌃ (on another system
// the Ctrl key is Mod). The Windows key is the system's: a press with it is no shortcut.

export const MOD = { mod: 'Mod', ctrl: 'Ctrl', alt: 'Alt', shift: 'Shift' } as const;
const ORDER = [MOD.mod, MOD.ctrl, MOD.alt, MOD.shift] as const;

export interface IKeyLike {
  key: string;
  code: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}

export const isMac = (): boolean =>
  typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);

/** The Russian letters on the places of the Latin ones, for a key that comes without its place (a forwarded one). */
const RU_PLACE: Record<string, string> = {
  й: 'KeyQ',
  ц: 'KeyW',
  у: 'KeyE',
  к: 'KeyR',
  е: 'KeyT',
  н: 'KeyY',
  г: 'KeyU',
  ш: 'KeyI',
  щ: 'KeyO',
  з: 'KeyP',
  х: 'BracketLeft',
  ъ: 'BracketRight',
  ф: 'KeyA',
  ы: 'KeyS',
  в: 'KeyD',
  а: 'KeyF',
  п: 'KeyG',
  р: 'KeyH',
  о: 'KeyJ',
  л: 'KeyK',
  д: 'KeyL',
  ж: 'Semicolon',
  э: 'Quote',
  я: 'KeyZ',
  ч: 'KeyX',
  с: 'KeyC',
  м: 'KeyV',
  и: 'KeyB',
  т: 'KeyN',
  ь: 'KeyM',
  б: 'Comma',
  ю: 'Period',
  ё: 'Backquote',
};
const SIGN_PLACE: Record<string, string> = {
  ' ': 'Space',
  ',': 'Comma',
  '.': 'Period',
  '/': 'Slash',
  '?': 'Slash',
  '\\': 'Backslash',
  ';': 'Semicolon',
  "'": 'Quote',
  '[': 'BracketLeft',
  ']': 'BracketRight',
  '-': 'Minus',
  '=': 'Equal',
  '`': 'Backquote',
};

/** The key's place: the event's own, else read from the letter it typed. */
export const placeOf = (key: string, code: string): string => {
  if (code) return code;
  const lower = key.toLowerCase();
  if (/^[a-z]$/.test(lower)) return `Key${lower.toUpperCase()}`;
  if (/^[0-9]$/.test(key)) return `Digit${key}`;
  if (Object.hasOwn(RU_PLACE, lower)) return RU_PLACE[lower];
  if (Object.hasOwn(SIGN_PLACE, key)) return SIGN_PLACE[key];
  return key;
};

const MODIFIER_KEYS = new Set(['Shift', 'Control', 'Alt', 'Meta', 'AltGraph', 'CapsLock', 'Fn', 'OS']);

/** The shortcut a press is; null for a lone modifier and for anything with the Windows key. */
export const chordOf = (event: IKeyLike, mac = isMac()): string | null => {
  if (MODIFIER_KEYS.has(event.key)) return null;
  if (!mac && event.metaKey) return null;
  const parts: string[] = [];
  if (mac ? event.metaKey : event.ctrlKey) parts.push(MOD.mod);
  if (mac && event.ctrlKey) parts.push(MOD.ctrl);
  if (event.altKey) parts.push(MOD.alt);
  if (event.shiftKey) parts.push(MOD.shift);
  const place = placeOf(event.key, event.code);
  if (!place || place === 'Unidentified') return null;
  return [...parts, place].join('+');
};

/** The shortcut's modifiers and key. */
export const partsOf = (chord: string): { mods: readonly string[]; place: string } => {
  const parts = chord.split('+');
  return { mods: parts.slice(0, -1), place: parts[parts.length - 1] };
};

/** A shortcut with Mod, ⌃ or Alt: it works while text is typed; a plain key (with Shift or without) is the field's. */
export const isCommandChord = (chord: string): boolean =>
  partsOf(chord).mods.some((m) => m === MOD.mod || m === MOD.ctrl || m === MOD.alt);

/** Whether a string is a shortcut this file writes: known modifiers in order, then a key. */
export const isChord = (chord: string): boolean => {
  const { mods, place } = partsOf(chord);
  if (!/^[A-Za-z][A-Za-z0-9]{0,23}$/.test(place) || MODIFIER_KEYS.has(place)) return false;
  let at = -1;
  for (const m of mods) {
    const i = ORDER.indexOf(m as (typeof ORDER)[number]);
    if (i <= at) return false;
    at = i;
  }
  return true;
};

const MAC_MOD: Record<string, string> = { Mod: '⌘', Ctrl: '⌃', Alt: '⌥', Shift: '⇧' };
const PC_MOD: Record<string, string> = { Mod: 'Ctrl', Ctrl: 'Ctrl', Alt: 'Alt', Shift: 'Shift' };
const PLACE_LABEL: Record<string, string> = {
  ArrowLeft: '←',
  ArrowRight: '→',
  ArrowUp: '↑',
  ArrowDown: '↓',
  Escape: 'Esc',
  PageUp: 'PgUp',
  PageDown: 'PgDn',
  Comma: ',',
  Period: '.',
  Slash: '/',
  Backslash: '\\',
  Semicolon: ';',
  Quote: "'",
  BracketLeft: '[',
  BracketRight: ']',
  Minus: '-',
  Equal: '=',
  Backquote: '`',
  Enter: '↵',
  Backspace: '⌫',
  Delete: 'Del',
  Tab: 'Tab',
};

/** The key's own name, as its cap says; `space` is the word for the space bar in the page's language. */
export const placeLabel = (place: string, space: string): string => {
  if (place === 'Space') return space;
  if (Object.hasOwn(PLACE_LABEL, place)) return PLACE_LABEL[place];
  const m = /^(?:Key|Digit|Numpad)(.+)$/.exec(place);
  return m ? m[1] : place;
};

/** The shortcut as this keyboard writes it: ⇧⌘O on a Mac, Ctrl+Shift+O elsewhere. */
export const chordLabel = (chord: string, space: string, mac = isMac()): string => {
  const { mods, place } = partsOf(chord);
  const key = placeLabel(place, space);
  if (mac) {
    const order = [MOD.ctrl, MOD.alt, MOD.shift, MOD.mod];
    return [...order.filter((m) => mods.includes(m)).map((m) => MAC_MOD[m]), key].join('');
  }
  return [...mods.map((m) => PC_MOD[m]), key].join('+');
};
