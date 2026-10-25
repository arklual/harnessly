// Things the field's palette asks of the screen, which owns them: the goal dialog is the header's, the list of earlier
// sessions to resume is the shell's, a fork's «Влить обратно» sheet is the chat view's. A screen that owns one listens to its kind while it is on the page; a request
// with nobody listening is lost, which is right: there is nothing to open.
import { useEffect, useRef } from 'react';

export const UI_REQUEST = { goal: 'goal', resume: 'resume', merge: 'merge' } as const;
export type TUiRequest = (typeof UI_REQUEST)[keyof typeof UI_REQUEST];

const listeners = new Map<TUiRequest, Set<() => void>>();

export const requestUi = (kind: TUiRequest): void => {
  for (const listener of listeners.get(kind) ?? []) listener();
};

/** Runs `handler` for every request of `kind` while the caller is on the page. */
export const useUiRequest = (kind: TUiRequest, handler: () => void): void => {
  const latest = useRef(handler);
  useEffect(() => {
    latest.current = handler;
  });
  useEffect(() => {
    const run = () => latest.current();
    const set = listeners.get(kind) ?? new Set<() => void>();
    set.add(run);
    listeners.set(kind, set);
    return () => {
      set.delete(run);
    };
  }, [kind]);
};
