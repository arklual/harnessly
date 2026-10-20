/**
 * The dictation key: one key of the person's choice, a single tap of which starts dictation and the next
 * ends it. No holding and no chord, so a lone modifier with its side (the right Option) can be the key. Pure: no
 * browser or React here.
 */
import { t } from '@/lib/i18n';

const MODIFIER = /^(Alt|Control|Shift|Meta)(Left|Right)$/;
const FUNCTION = /^F([1-9]|1[0-9]|2[0-4])$/;
const OTHER: readonly string[] = ['Pause', 'ScrollLock', 'Insert', 'ContextMenu'];

/** A lone modifier acts when it is let go: only then is it known that it was not the start of a chord. */
export const isModifierCode = (code: string): boolean => MODIFIER.test(code);

/**
 * `KeyboardEvent.code` of a key that may be the dictation key: modifiers with their side, function keys and a few keys
 * that type nothing. A key that types or edits text is refused, or the composer could not be typed in.
 */
export const isAssignableVoiceKey = (code: unknown): code is string =>
  typeof code === 'string' && (MODIFIER.test(code) || FUNCTION.test(code) || OTHER.includes(code));

const SIDE: Record<string, string> = { Left: t('lib.voiceKey.left'), Right: t('lib.voiceKey.right') };
const MODIFIER_NAME: Record<string, { mac: string; other: string }> = {
  Alt: { mac: '⌥ Option', other: 'Alt' },
  Control: { mac: '⌃ Control', other: 'Ctrl' },
  Shift: { mac: '⇧ Shift', other: 'Shift' },
  Meta: { mac: '⌘ Command', other: 'Win' },
};

/** The key as the person knows it: «Right ⌥ Option» on a Mac, «Right Alt» elsewhere. */
export const voiceKeyLabel = (code: string, mac: boolean): string => {
  const modifier = code.match(MODIFIER);
  if (!modifier) return code;
  const name = MODIFIER_NAME[modifier[1]];
  return t('lib.voiceKey.label', { side: SIDE[modifier[2]], key: mac ? name.mac : name.other });
};

/** A modifier held longer than this was held for something else (a chord that never came, a change of mind). */
export const VOICE_TAP_MAX_MS = 600;

interface IKeyPress {
  code: string;
  repeat?: boolean;
}

export interface IVoiceTap {
  /** True when this press is the tap (a key that is not a modifier acts on press). */
  down: (event: IKeyPress, now: number) => boolean;
  /** True when this release completes a tap of a modifier. */
  up: (event: IKeyPress, now: number) => boolean;
  /** A click, a lost focus: whatever was held is no longer a tap. */
  reset: () => void;
}

/**
 * Tells a tap of the dictation key from everything else done with it. Another key pressed while a modifier is down
 * makes it a chord (Option+letter types a character), and a chord never starts dictation.
 */
export const createVoiceTap = (key: string): IVoiceTap => {
  const modifier = isModifierCode(key);
  let pressedAt: number | null = null;
  return {
    down: ({ code, repeat = false }, now) => {
      if (code !== key) {
        pressedAt = null;
        return false;
      }
      if (repeat) return false;
      if (!modifier) return true;
      pressedAt = now;
      return false;
    },
    up: ({ code }, now) => {
      if (code !== key || pressedAt === null) return false;
      const held = now - pressedAt;
      pressedAt = null;
      return held <= VOICE_TAP_MAX_MS;
    },
    reset: () => {
      pressedAt = null;
    },
  };
};
