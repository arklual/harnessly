// The video player's keys, from the one list of shortcuts (features/keys; the person may change them in Settings).
// As they come: Space or K plays and pauses; ←/→ jump 5 s (Shift: 10 s), J/L jump 10 s, «,» and «.» step a frame;
// PageUp/PageDown or ↑/↓ change the scene; Home/End go to the ends; 0–9 go to a tenth of the video; I and O mark the
// range; C comments; F goes full screen; ⌥← and ⌥→ change the scene even from the chat's field. The keys are places on
// the keyboard, so the Russian layout has the same. Keys typed into a field are the field's.
import { KEY_SCOPE } from '@/next/features/keys/commands';
import { useKeys, type TKeyHandlers } from '@/next/features/keys/use-keys';

export const VIDEO_KEY = {
  toggle: 'toggle',
  back5: 'back5',
  forward5: 'forward5',
  back10: 'back10',
  forward10: 'forward10',
  frameBack: 'frameBack',
  frameForward: 'frameForward',
  prevScene: 'prevScene',
  nextScene: 'nextScene',
  start: 'start',
  end: 'end',
  tenth: 'tenth',
  markIn: 'markIn',
  markOut: 'markOut',
  comment: 'comment',
  full: 'full',
} as const;
type TPlainKey = Exclude<(typeof VIDEO_KEY)[keyof typeof VIDEO_KEY], 'tenth'>;

export type TVideoKeyAction = { key: TPlainKey } | { key: typeof VIDEO_KEY.tenth; n: number };

const PLAIN: Record<TPlainKey, string> = {
  toggle: 'video.toggle',
  back5: 'video.back5',
  forward5: 'video.forward5',
  back10: 'video.back10',
  forward10: 'video.forward10',
  frameBack: 'video.frameBack',
  frameForward: 'video.frameForward',
  prevScene: 'video.prevScene',
  nextScene: 'video.nextScene',
  start: 'video.start',
  end: 'video.end',
  markIn: 'video.markIn',
  markOut: 'video.markOut',
  comment: 'video.comment',
  full: 'video.full',
};

/** The digit a chord is, «Digit7» or «Numpad7»; null for anything else. */
export const digitOf = (chord: string): number | null => {
  const m = /^(?:Digit|Numpad)([0-9])$/.exec(chord);
  return m ? Number(m[1]) : null;
};

/** The video's handlers of the list's actions. */
export const videoHandlers = (onKey: (action: TVideoKeyAction) => void): TKeyHandlers => {
  const handlers: TKeyHandlers = {
    'global.stepBack': () => onKey({ key: VIDEO_KEY.prevScene }),
    'global.stepOn': () => onKey({ key: VIDEO_KEY.nextScene }),
    'video.tenth': (chord) => {
      const n = digitOf(chord);
      if (n === null) return false;
      onKey({ key: VIDEO_KEY.tenth, n });
    },
  };
  for (const key of Object.keys(PLAIN) as TPlainKey[]) handlers[PLAIN[key]] = () => onKey({ key });
  return handlers;
};

/** While `on`, the video's keys run `onKey`; the keys it acts on do nothing else (no scroll on an arrow or Space). */
export const useVideoKeys = (on: boolean, onKey: (action: TVideoKeyAction) => void) => {
  useKeys(KEY_SCOPE.video, videoHandlers(onKey), on);
};
