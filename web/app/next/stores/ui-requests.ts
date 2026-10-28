// Things the field's palette asks of the screen, which owns them: the goal dialog is the header's, the list of earlier
// sessions to resume is the shell's, a fork's «Влить обратно» sheet is the chat view's. A screen that owns one listens to its kind while it is on the page; a request
// with nobody listening is lost, which is right: there is nothing to open.
import { useEffect, useRef } from 'react';

export const UI_REQUEST = {
  goal: 'goal',
  resume: 'resume',
  merge: 'merge',
  continue: 'continue',
  moveVm: 'move-vm',
} as const;
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

/** The chat whose «Продолжить в другой модели» was asked for from a menu: it may not be on the screen yet. */
let continueFor: string | null = null;

/** Asks the chat `id` to open its «Продолжить в другой модели» sheet, now or as soon as it is on the page. */
export const requestContinue = (id: string): void => {
  continueFor = id;
  requestUi(UI_REQUEST.continue);
};

/** Runs `handler` when the chat `id` is asked to continue in another model, also when it was asked before it mounted. */
export const useContinueRequest = (id: string, handler: () => void): void => {
  const latest = useRef(handler);
  useEffect(() => {
    latest.current = handler;
  });
  const take = () => {
    if (continueFor !== id) return;
    continueFor = null;
    latest.current();
  };
  useUiRequest(UI_REQUEST.continue, take);
  useEffect(take, [id]);
};
