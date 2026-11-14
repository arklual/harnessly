// Is the service behind a built-in app up? The documents and boards apps show views of services of their own; when
// one is down its frame would wait and then say «не запустилось». The hub asks each service for /readyz and keeps the
// answer in <platform api>/docs/_status (`services`), so the page asks the hub, never the service: a calm «сейчас не
// открывается» stands in for the view while the service is down, and the view opens by itself once it is back.
import { useCallback, useEffect, useState } from 'react';
import { pageClock } from '@/next/core/clock';
import { platformPath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

/** The service each built-in app's view lives on, as the hub's status names it. */
export const APP_SERVICE = {
  'harnessly.docs': 'documents',
  'harnessly.boards': 'boards',
} as const;
export type TAppService = (typeof APP_SERVICE)[keyof typeof APP_SERVICE];

export const appService = (app: string): TAppService | null =>
  Object.prototype.hasOwnProperty.call(APP_SERVICE, app)
    ? APP_SERVICE[app as keyof typeof APP_SERVICE]
    : null;

/**
 * A space of the person's own (the boards, the documents): its page is the space on the whole screen, a list and then
 * what is open, with the one field over it. No chat comes along from the sidebar or the chat on screen: a
 * chat is made over the space by the first words of its field.
 */
export const isSpaceApp = (app: string): boolean => appService(app) !== null;

/** `unknown`: no answer yet, or a page with no hub (standalone): the view opens as it always did. */
export const SERVICE_STATE = { up: 'up', down: 'down', unknown: 'unknown' } as const;
export type TServiceState = (typeof SERVICE_STATE)[keyof typeof SERVICE_STATE];

/** How often the page asks while the service is up, and while it is down (the hub itself asks every 30 seconds). */
export const POLL_UP_MS = 60_000;
export const POLL_DOWN_MS = 10_000;
const STATUS_TIMEOUT_MS = 8_000;

/** The service's state in the hub's answer: down only when the hub says so. */
export const serviceStateOf = (status: unknown, service: TAppService): TServiceState => {
  if (typeof status !== 'object' || status === null) return SERVICE_STATE.unknown;
  const services = (status as { services?: unknown }).services;
  if (typeof services !== 'object' || services === null) return SERVICE_STATE.unknown;
  const one = (services as Record<string, unknown>)[service];
  if (typeof one !== 'object' || one === null) return SERVICE_STATE.unknown;
  const { up } = one as { up?: unknown };
  if (up === true) return SERVICE_STATE.up;
  if (up === false) return SERVICE_STATE.down;
  return SERVICE_STATE.unknown;
};

const askStatus = async (machine: MachineId, service: TAppService): Promise<TServiceState> => {
  try {
    const status = await runtimeJson<unknown>(
      machine,
      platformPath('/docs/_status'),
      undefined,
      STATUS_TIMEOUT_MS,
    );
    return serviceStateOf(status, service);
  } catch {
    // No hub, no documents, or the hub did not answer: the view itself says what is wrong.
    return SERVICE_STATE.unknown;
  }
};

export interface IAppServiceHealth {
  state: TServiceState;
  /** Asks again now («Повторить»). */
  retry: () => void;
  checking: boolean;
}

/** The state of the app's service, asked while the page is open; a non built-in app is always `unknown`. */
export const useAppService = (machine: MachineId, app: string): IAppServiceHealth => {
  const service = appService(app);
  const [state, setState] = useState<TServiceState>(SERVICE_STATE.unknown);
  const [checking, setChecking] = useState(false);
  const [round, setRound] = useState(0);
  useEffect(() => {
    if (service === null) return undefined;
    const clock = pageClock();
    let timer: unknown = null;
    let stopped = false;
    const ask = async () => {
      setChecking(true);
      const next = await askStatus(machine, service);
      if (stopped) return;
      setChecking(false);
      setState(next);
      timer = clock.setTimeout(() => void ask(), next === SERVICE_STATE.down ? POLL_DOWN_MS : POLL_UP_MS);
    };
    void ask();
    return () => {
      stopped = true;
      if (timer !== null) clock.clearTimeout(timer);
    };
  }, [machine, service, round]);

  const retry = useCallback(() => setRound((n) => n + 1), []);
  return { state, retry, checking };
};
