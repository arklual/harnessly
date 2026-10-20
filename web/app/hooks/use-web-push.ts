// Browser push for agents that finished or wait for an answer: the subscription state, turning it on and off
// (the runtime keeps subscriptions: /api/push/*), and the events the page raises when a push is clicked.
import { create } from 'zustand';
import { appFetch, prefixAppPath } from '@/lib/app-path';
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

const registration = async () => {
  const known = await navigator.serviceWorker.getRegistration(prefixAppPath('/'));
  return known ?? navigator.serviceWorker.register(prefixAppPath('/sw.js'), { scope: prefixAppPath('/') });
};

const toKey = (base64: string) => {
  const pad = '='.repeat((4 - (base64.length % 4)) % 4);
  const raw = atob((base64 + pad).replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
};

export const refreshPushState = async (): Promise<TPushState> => {
  let state: TPushState;
  if (!supported()) state = iosBrowser() ? 'needs-install' : 'unsupported';
  else if (Notification.permission === 'denied') state = 'denied';
  else {
    const reg = await navigator.serviceWorker.getRegistration(prefixAppPath('/'));
    state = (await reg?.pushManager.getSubscription()) ? 'on' : 'off';
  }
  usePushStore.setState({ state });
  return state;
};

export const enablePush = async (): Promise<{ state: TPushState; error?: string }> => {
  if (!supported()) return { state: await refreshPushState() };
  usePushStore.setState({ busy: true });
  try {
    if ((await Notification.requestPermission()) !== 'granted') return { state: await refreshPushState() };
    const keyRes = await appFetch('/api/push/vapid-key');
    if (!keyRes.ok) throw new Error(t('hooks.useWebPush.keyFailed', { status: keyRes.status }));
    const { publicKey } = (await keyRes.json()) as { publicKey: string };
    const reg = await registration();
    const sub =
      (await reg.pushManager.getSubscription()) ??
      (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: toKey(publicKey) }));
    const saved = await appFetch('/api/push/subscribe', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(sub.toJSON()),
    });
    if (!saved.ok) throw new Error(t('hooks.useWebPush.subscribeFailed', { status: saved.status }));
    return { state: await refreshPushState() };
  } catch (error) {
    return { state: await refreshPushState(), error: error instanceof Error ? error.message : String(error) };
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

export const sendTestPush = async (): Promise<boolean> => {
  const res = await appFetch('/api/push/test', { method: 'POST' });
  return res.ok;
};
