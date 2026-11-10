// Sinopia's keys (HRN-808): the editor's screens (deck, video, site) run the host's shortcut commands through its SDK
// (hl.keys.on), on the keys the person set in Settings, and a key pressed inside a board is passed on as the host's
// (hl.keys.press). The same hook the editor used before, so its screens are not rewritten.
import { useEffect, useRef, useState } from 'react';
import type { TAppKeyHandlers, TAppKeyScope } from '@/next/features/app-views/bridge';
import { sinopiaUi } from '@/next/features/app-views/sinopia/host';

/** The editor's screens with shortcut commands. */
export const KEY_SCOPE = { deck: 'deck', video: 'video', site: 'site' } as const satisfies Record<
  string,
  TAppKeyScope
>;

export type TKeyHandlers = TAppKeyHandlers;

/** Keys pressed where text is written: an input, a text area, editable text. */
export const isTypingTarget = (target: EventTarget | null): boolean => {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName);
};

/** While `on`, the screen runs these commands (by id) on their keys; the handlers are read on every press. */
export const useKeys = (scope: TAppKeyScope, handlers: TKeyHandlers, on = true) => {
  const latest = useRef(handlers);
  useEffect(() => {
    latest.current = handlers;
  });
  useEffect(() => {
    if (!on) return;
    return sinopiaUi().keys.on(scope, () => latest.current);
  }, [scope, on]);
};

/** A key pressed inside a board, run as the host's shortcut. */
export const pressKey = (chord: string, typing = false): void => {
  void sinopiaUi()
    .keys.press(chord, typing)
    .catch(() => undefined);
};

const hints = new Map<string, string | null>();

/** How the host writes a command's key, with the person's own keys; null until known or when it has none. */
export const useKeyHint = (command: string): string | null => {
  const [hint, setHint] = useState<string | null>(() => hints.get(command) ?? null);
  useEffect(() => {
    let live = true;
    sinopiaUi()
      .keys.hints([command])
      .then(
        (got) => {
          const label = got[command] ?? null;
          hints.set(command, label);
          if (live) setHint(label);
        },
        () => undefined,
      );
    return () => {
      live = false;
    };
  }, [command]);
  return hint;
};
