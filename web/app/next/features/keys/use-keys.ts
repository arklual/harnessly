// One listener for the whole page's shortcuts. A screen that has keys (the shell, a deck, a video, a picture open full
// size) says which actions it runs while it is on; a press goes to the latest such screen that runs one of the actions
// of its shortcut, so a picture over a deck takes the arrows, and the deck has them back when it closes. A plain key
// (no ⌘, Ctrl or Alt) typed into a field is the field's; a press another handler already took is left alone. A screen
// may run an action of another screen's list too: a deck runs «step back / on» (⌥← ⌥→) of the global one.
import { useEffect, useRef } from 'react';
import { createStore, useStore } from '@/next/core/store';
import { chordOf, isCommandChord } from '@/next/features/keys/chord';
import type { TKeyScope } from '@/next/features/keys/commands';
import { commandsOfChord, type TKeyOverrides } from '@/next/features/keys/keymap';
import { meStore } from '@/next/stores/me';

/** An action's handler; `false` when the press is not its after all (the next screen down may have it). */
export type TKeyHandlers = Partial<Record<string, (chord: string) => void | false>>;

interface IKeyEntry {
  scope: TKeyScope;
  handlers: () => TKeyHandlers;
}

const stack: IKeyEntry[] = [];

/** The screens with keys on now, the latest last: the shortcuts sheet lists theirs. */
export const keyScopesStore = createStore<readonly TKeyScope[]>([]);
export const useKeyScopes = (): readonly TKeyScope[] => useStore(keyScopesStore);

/** The person's own shortcuts; none until the settings are known. */
export const keyOverrides = (): TKeyOverrides => {
  const me = meStore.get();
  return me.k === 'ready' ? me.me.settings.keys : {};
};
export const useKeyOverrides = (): TKeyOverrides => {
  const me = useStore(meStore);
  return me.k === 'ready' ? me.me.settings.keys : {};
};

/** Keys pressed where text is written: an input, a text area, editable text. */
export const isTypingTarget = (target: EventTarget | null): boolean => {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName);
};

/** A shortcut being recorded in Settings is not run. */
let paused = 0;
export const pauseKeys = (): (() => void) => {
  paused += 1;
  return () => {
    paused -= 1;
  };
};

/** Runs a press as the latest screen's action; true when one ran. Exported for the keys a board forwards. */
export const runChord = (chord: string, typing: boolean): boolean => {
  if (paused > 0) return false;
  if (typing && !isCommandChord(chord) && !chord.endsWith('Escape')) return false;
  const ids = commandsOfChord(chord, keyOverrides()).map((c) => c.id);
  if (ids.length === 0) return false;
  for (let i = stack.length - 1; i >= 0; i -= 1) {
    const handlers = stack[i].handlers();
    for (const id of ids) {
      const run = handlers[id];
      if (run !== undefined && run(chord) !== false) return true;
    }
  }
  return false;
};

const onKeyDown = (event: KeyboardEvent) => {
  if (event.defaultPrevented || event.isComposing) return;
  const chord = chordOf(event);
  if (chord === null) return;
  // Space on a focused button presses the button.
  if (chord === 'Space' && event.target instanceof HTMLButtonElement) return;
  if (runChord(chord, isTypingTarget(event.target))) event.preventDefault();
};

const publish = () => keyScopesStore.set(stack.map((e) => e.scope));

const register = (entry: IKeyEntry): (() => void) => {
  if (stack.length === 0) window.addEventListener('keydown', onKeyDown);
  stack.push(entry);
  publish();
  return () => {
    const at = stack.indexOf(entry);
    if (at >= 0) stack.splice(at, 1);
    if (stack.length === 0) window.removeEventListener('keydown', onKeyDown);
    publish();
  };
};

/** While `on`, the screen runs these actions (by id) on their shortcuts. */
export const useKeys = (scope: TKeyScope, handlers: TKeyHandlers, on = true) => {
  const latest = useRef(handlers);
  useEffect(() => {
    latest.current = handlers;
  });
  useEffect(() => {
    if (!on) return;
    return register({ scope, handlers: () => latest.current });
  }, [scope, on]);
};
