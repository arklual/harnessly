// Browser push for agents that finished or wait for an answer: the subscription state, turning it on and off
// (the runtime keeps subscriptions: /api/push/*), and the events the page raises when a push arrives or is clicked.
import { create } from 'zustand';
import { appFetch, prefixAppPath } from '@/lib/app-path';
import { getDeviceId, isMobileDevice } from '@/lib/push-device';
import { t } from '@/lib/i18n';

export type TPushState = 'unknown' | 'unsupported' | 'needs-install' | 'denied' | 'off' | 'on';

/** A push was clicked: open the conversation in `detail` (a tab id). */
export const OPEN_CONVERSATION_EVENT = 'hl:open-conversation';
/** A push arrived while the page is open; `detail` is its payload. */
export const PUSH_MESSAGE_EVENT = 'hl:push-message';

interface IPushStore {
  state: TPushState;
  busy: boolean;
}

export const usePushStore = create<IPushStore>(() => ({ state: 'unknown', busy: false }));

const supported = () =>
  typeof window !== 'undefined' &&
  'serviceWorker' in navigator &&
  'PushManager' in window &&
  'Notification' in window;
const standalone = () =>
  window.matchMedia?.('(display-mode: standalone)').matches ||
  (navigator as Navigator & { standalone?: boolean }).standalone === true;
const iosBrowser = () => /iPhone|iPad|iPod/.test(navigator.userAgent) && !standalone();

const STEP_TIMEOUT_MS = 15_000;

class PushStepError extends Error {
  constructor(
    readonly step: string,
    message: string,
  ) {
    super(message);
  }
}

const withTimeout = <T>(promise: Promise<T>, step: string, message: string): Promise<T> =>
  Promise.race([
    promise,
    new Promise<never>((_, reject) =>
      setTimeout(() => reject(new PushStepError(step, message)), STEP_TIMEOUT_MS),
    ),
  ]);

/**
 * The worker registration without `serviceWorker.ready`: in a Home Screen app on iOS that promise can stay pending
 * although the worker is registered and active, while `getRegistration` answers at once.
 */
const workerRegistration = async (): Promise<ServiceWorkerRegistration> => {
  const scope = prefixAppPath('/');
  const reg =
    (await navigator.serviceWorker.getRegistration(scope)) ??
    (await navigator.serviceWorker.register(prefixAppPath('/sw.js'), { scope }));
  if (reg.active) return reg;
  const pending = reg.installing ?? reg.waiting;
  if (!pending) throw new Error('worker registration has no worker');
  await new Promise<void>((resolve, reject) => {
    pending.addEventListener('statechange', () => {
      if (pending.state === 'activated') resolve();
      else if (pending.state === 'redundant') reject(new Error('worker became redundant'));
    });
  });
  return reg;
};

const toKey = (base64: string) => {
  const pad = '='.repeat((4 - (base64.length % 4)) % 4);
  const raw = atob((base64 + pad).replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
};

const vapidKey = async (): Promise<Uint8Array<ArrayBuffer>> => {
  const res = await appFetch('/api/push/vapid-key');
  if (!res.ok) throw new Error(t('hooks.useWebPush.keyFailed', { status: res.status }));
  const { publicKey } = (await res.json()) as { publicKey: string };
  return toKey(publicKey);
};

const sameKey = (a: ArrayBuffer | null | undefined, b: Uint8Array): boolean => {
  if (!a || a.byteLength !== b.byteLength) return false;
  const x = new Uint8Array(a);
  return x.every((value, i) => value === b[i]);
};

/** The subscription with the device it belongs to: the server holds back push for a device it sees looking. */
const saveSubscription = async (sub: PushSubscription): Promise<boolean> => {
  const res = await appFetch('/api/push/subscribe', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ subscription: sub.toJSON(), deviceId: getDeviceId(), mobile: isMobileDevice() }),
  });
  return res.ok;
};

interface IPrepared {
  reg: ServiceWorkerRegistration;
  key: Uint8Array<ArrayBuffer>;
}

let prepared: IPrepared | null = null;
let preparing: Promise<IPrepared> | null = null;

/**
 * Worker and key are fetched ahead of the tap: WebKit accepts a subscription only while the tap is still being
 * handled, and any wait before `subscribe()` spends that window.
 */
export const preparePush = (): Promise<IPrepared> => {
  if (prepared) return Promise.resolve(prepared);
  preparing ??= Promise.all([
    withTimeout(workerRegistration(), 'service-worker', t('hooks.useWebPush.workerHangs')),
    withTimeout(vapidKey(), 'vapid-key', t('hooks.useWebPush.keyHangs')),
  ])
    .then(([reg, key]) => (prepared = { reg, key }))
    .catch((error) => {
      preparing = null;
      throw error;
    });
  return preparing;
};

export const refreshPushState = async (): Promise<TPushState> => {
  let state: TPushState;
  if (!supported()) state = iosBrowser() ? 'needs-install' : 'unsupported';
  else if (Notification.permission === 'denied') state = 'denied';
  else if (Notification.permission !== 'granted') state = 'off';
  else {
    const reg = await navigator.serviceWorker.getRegistration(prefixAppPath('/')).catch(() => undefined);
    state = (await reg?.pushManager.getSubscription().catch(() => null)) ? 'on' : 'off';
  }
  usePushStore.setState({ state });
  return state;
};

/**
 * Keeps a granted subscription bound to the runtime's current key and known to it: a key change or a restored
 * data directory would otherwise silently drop pushes.
 */
export const syncSubscription = async (): Promise<void> => {
  const { reg, key } = await preparePush();
  const sub = await reg.pushManager.getSubscription();
  if (!sub) return;
  if (!sameKey(sub.options.applicationServerKey, key)) {
    await sub.unsubscribe().catch(() => {});
    usePushStore.setState({ state: 'off' });
    return;
  }
  await saveSubscription(sub);
};

/** The failure happens on the phone, where nothing else is logged: the runtime log gets the reason. */
const reportFailure = (step: string, error: unknown) => {
  const { name, message } = error instanceof Error ? error : { name: 'Unknown', message: String(error) };
  void appFetch('/api/push/diagnostic', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      step,
      name,
      message,
      permission: 'Notification' in window ? Notification.permission : 'unsupported',
      standalone: standalone(),
      userAgent: navigator.userAgent,
    }),
    keepalive: true,
  }).catch(() => {});
};

const describeFailure = (step: string, error: unknown): string => {
  if (error instanceof PushStepError) return error.message;
  const { name, message } = error instanceof Error ? error : { name: '', message: String(error) };
  if (name === 'NotAllowedError') return t('hooks.useWebPush.notAllowed', { reason: message || name });
  if (name === 'InvalidStateError') return t('hooks.useWebPush.staleSubscription');
  return `${step}: ${name ? `${name} - ` : ''}${message}`;
};

/** Must run straight from a tap, and `subscribe()` must be the first thing it waits for. */
export const enablePush = async (): Promise<{ state: TPushState; error?: string }> => {
  if (!supported()) return { state: await refreshPushState() };
  const ready = prepared;
  usePushStore.setState({ busy: true });
  let step = 'prepare';
  try {
    if (!ready) {
      await preparePush();
      throw new PushStepError(step, t('hooks.useWebPush.stillLoading'));
    }
    step = 'subscribe';
    // The permission prompt belongs to subscribe(): asking separately would spend the tap on a separate wait.
    const sub = await withTimeout(
      ready.reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: ready.key }),
      step,
      t('hooks.useWebPush.subscribeHangs'),
    );
    step = 'register';
    if (!(await withTimeout(saveSubscription(sub), step, t('hooks.useWebPush.registerHangs'))))
      throw new PushStepError(step, t('hooks.useWebPush.subscribeFailed', { status: 'rejected' }));
    return { state: await refreshPushState() };
  } catch (error) {
    if (error instanceof DOMException && error.name === 'InvalidStateError') {
      await ready?.reg.pushManager
        .getSubscription()
        .then((old) => old?.unsubscribe())
        .catch(() => {});
    }
    reportFailure(error instanceof PushStepError ? error.step : step, error);
    const state = await refreshPushState();
    return { state, error: state === 'denied' ? undefined : describeFailure(step, error) };
  } finally {
    usePushStore.setState({ busy: false });
  }
};

export const disablePush = async (): Promise<void> => {
  usePushStore.setState({ busy: true });
  try {
    const reg = await navigator.serviceWorker.getRegistration(prefixAppPath('/'));
    const sub = await reg?.pushManager.getSubscription();
    if (sub) {
      await appFetch('/api/push/subscribe', {
        method: 'DELETE',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ endpoint: sub.endpoint }),
      }).catch(() => {});
      await sub.unsubscribe();
    }
  } finally {
    usePushStore.setState({ busy: false });
    await refreshPushState();
  }
};

/** A test push to this device only: the runtime wants the subscription's endpoint. */
export const sendTestPush = async (): Promise<boolean> => {
  const reg = await navigator.serviceWorker.getRegistration(prefixAppPath('/'));
  const sub = await reg?.pushManager.getSubscription();
  if (!sub) return false;
  const res = await appFetch('/api/push/test', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ endpoint: sub.endpoint }),
  });
  return res.ok;
};
