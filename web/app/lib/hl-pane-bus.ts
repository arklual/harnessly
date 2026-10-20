import {
  cleanPanelState,
  parsePanelResult,
  type IPanelResult,
  type IPanelState,
  type TPaneTabKind,
} from '@/lib/hl-panel-command';
import type { IPanelBus } from '@/lib/hl-panel-run';
import { t } from '@/lib/i18n';

/**
 * The page's side of the agent → pane channel: three window events the pane's tab registry listens to.
 * A listener may answer through `detail.respond(result)` (also later, within the wait) and/or call `event.preventDefault()`
 * to say it handled the event. Nobody answering is not an error the agent can be blamed with, so it is said as it is.
 */

export const OPEN_PANE_TAB_EVENT = 'hl:open-pane-tab' as const;
export const SET_PANE_VISIBLE_EVENT = 'hl:set-pane-visible' as const;
export const GET_PANE_STATE_EVENT = 'hl:get-pane-state' as const;

const WAIT_MS = 1500;
/** A listener that only prevented the default is given this long to also respond. */
const HANDLED_GRACE_MS = 250;

type TAnswer = { handled: false } | { handled: true; value?: unknown };

const ask = (
  target: EventTarget,
  name: string,
  detail: Record<string, unknown>,
  waitMs: number,
): Promise<TAnswer> =>
  new Promise((resolve) => {
    let settled = false;
    const timers: ReturnType<typeof setTimeout>[] = [];
    const settle = (answer: TAnswer) => {
      if (settled) return;
      settled = true;
      timers.forEach(clearTimeout);
      resolve(answer);
    };
    const respond = (value: unknown) => settle({ handled: true, value });
    const event = new CustomEvent(name, { cancelable: true, detail: { ...detail, respond } });
    timers.push(setTimeout(() => settle({ handled: false }), waitMs));
    target.dispatchEvent(event);
    if (event.defaultPrevented)
      timers.push(setTimeout(() => settle({ handled: true }), Math.min(HANDLED_GRACE_MS, waitMs)));
  });

const unconfirmed = (what: string): IPanelResult => ({
  ok: true,
  message: t('lib.hlPaneBus.unconfirmed', { what }),
});

const outcome = (answer: TAnswer, done: string, what: string): IPanelResult => {
  if (!answer.handled) return unconfirmed(what);
  const result = parsePanelResult(answer.value);
  return result ?? { ok: true, message: done };
};

export const createPaneBus = (
  target: EventTarget,
  options: { overload: () => string | null; waitMs?: number },
): IPanelBus => {
  const waitMs = options.waitMs ?? WAIT_MS;
  return {
    open: async (tab: TPaneTabKind, path?: string) => {
      const answer = await ask(
        target,
        OPEN_PANE_TAB_EVENT,
        { kind: tab, ...(path ? { target: path } : {}) },
        waitMs,
      );
      return outcome(
        answer,
        path ? t('lib.hlPaneBus.openedTabAt', { tab, path }) : t('lib.hlPaneBus.openedTab', { tab }),
        t('lib.hlPaneBus.openSent', { tab }),
      );
    },
    setVisible: async (visible: boolean) => {
      const answer = await ask(target, SET_PANE_VISIBLE_EVENT, { visible }, waitMs);
      return outcome(
        answer,
        visible ? t('lib.hlPaneBus.shown') : t('lib.hlPaneBus.hidden'),
        visible ? t('lib.hlPaneBus.showSent') : t('lib.hlPaneBus.hideSent'),
      );
    },
    state: async (): Promise<IPanelState | null> => {
      const answer = await ask(target, GET_PANE_STATE_EVENT, {}, waitMs);
      return answer.handled ? (cleanPanelState(answer.value) ?? null) : null;
    },
    overload: options.overload,
  };
};
