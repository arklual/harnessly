import { useEffect } from 'react';
import { homeFetch, prefixAppPath } from '@/lib/app-path';
import { getDeviceId } from '@/lib/push-device';
import { createVisibilityReporter, trackVisibility } from '@/lib/visibility-report';
import {
  PUSH_NAV_CACHE,
  SW_CLEAR_NOTIFICATIONS,
  parsePushNavigation,
  pushNavKey,
  readWorkerMessage,
  type IPushNavigation,
} from '@/lib/push-bridge';
import {
  OPEN_CONVERSATION_EVENT,
  PUSH_MESSAGE_EVENT,
  preparePush,
  refreshPushState,
  syncSubscription,
} from '@/hooks/use-web-push';

/** The server holds back push for a device it saw looking at the app: it is told when that changes. */
const sendVisibility = (visible: boolean) => {
  void homeFetch('/api/push/visibility', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ deviceId: getDeviceId(), visible }),
    keepalive: true,
  }).catch(() => {});
};

/** A click on a push that happened while the page slept: the worker left the target in its cache. */
const takePushNavigation = async (): Promise<IPushNavigation | null> => {
  try {
    if (typeof caches === 'undefined') return null;
    const cache = await caches.open(PUSH_NAV_CACHE);
    const key = pushNavKey(prefixAppPath('/'));
    const found = await cache.match(key);
    if (!found) return null;
    await cache.delete(key);
    return parsePushNavigation(await found.json());
  } catch {
    return null;
  }
};

const openConversation = (tabId: string) =>
  window.dispatchEvent(new CustomEvent(OPEN_CONVERSATION_EVENT, { detail: tabId }));

const clearNotifications = () => {
  void navigator.serviceWorker
    .getRegistration(prefixAppPath('/'))
    .then((reg) => reg?.active?.postMessage({ type: SW_CLEAR_NOTIFICATIONS }))
    .catch(() => {});
  void navigator.clearAppBadge?.().catch(() => {});
};

/**
 * Connects the page to its service worker (public/sw.js): a push shown in the page, a click that opens a
 * conversation (also one that happened while the page slept), the push subscription kept valid, and the
 * "looking at the app" signal. Mounted once, after sign-in; a page inside a frame stays out of it.
 */
const usePushBridge = (): void => {
  useEffect(() => {
    if (window.parent !== window) return;
    const cleanups: Array<() => void> = [];

    const start = async () => {
      const state = await refreshPushState();
      if (state === 'on') await syncSubscription();
      else if (state === 'off') await preparePush();
    };
    void start().catch(() => {});

    if ('serviceWorker' in navigator) {
      const onMessage = (event: MessageEvent) => {
        const message = readWorkerMessage(event.data);
        if (!message) return;
        if (message.kind === 'push') {
          window.dispatchEvent(new CustomEvent(PUSH_MESSAGE_EVENT, { detail: message.message }));
        } else {
          void takePushNavigation();
          openConversation(message.navigation.tabId);
        }
      };
      navigator.serviceWorker.addEventListener('message', onMessage);
      cleanups.push(() => navigator.serviceWorker.removeEventListener('message', onMessage));

      // iOS resumes the page before the worker has written the click target: look now and once a moment later.
      const timers = new Set<ReturnType<typeof setTimeout>>();
      const consumePending = () => {
        void takePushNavigation().then((nav) => nav && openConversation(nav.tabId));
      };
      const checkPending = () => {
        consumePending();
        const timer = setTimeout(() => {
          timers.delete(timer);
          consumePending();
        }, 300);
        timers.add(timer);
      };
      const onResume = () => {
        if (document.hidden) return;
        checkPending();
        clearNotifications();
      };
      checkPending();
      document.addEventListener('visibilitychange', onResume);
      window.addEventListener('pageshow', onResume);
      window.addEventListener('focus', onResume);
      cleanups.push(() => {
        document.removeEventListener('visibilitychange', onResume);
        window.removeEventListener('pageshow', onResume);
        window.removeEventListener('focus', onResume);
        timers.forEach((timer) => clearTimeout(timer));
      });
    }

    const stop = trackVisibility(createVisibilityReporter(sendVisibility), { window, document });
    cleanups.push(() => {
      stop();
      sendVisibility(false);
    });

    return () => cleanups.forEach((fn) => fn());
  }, []);
};

export default usePushBridge;
