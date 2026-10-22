// Web push for the agent app: shows a notification for a push, brings the clicked conversation to the front and
// keeps the app icon badge honest. It caches nothing and answers no page requests, so a release never meets a stale shell.
// The worker lives next to the app (its directory is its scope), so the same file serves any base path.

const BASE = new URL('./', self.location.href).pathname;
const ICON = `${BASE}logo.svg`;
const PUSH_NAV_CACHE = 'push-nav';
const PUSH_NAV_KEY = `${BASE}_push-pending`;
const isWebKit =
  /AppleWebKit/.test(self.navigator.userAgent) && !/Chrome|Chromium|Edg\//.test(self.navigator.userAgent);

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));

const windows = () => self.clients.matchAll({ type: 'window', includeUncontrolled: true });
const isTop = (client) => client.frameType !== 'nested';
const ownsPage = (client) => {
  try {
    const url = new URL(client.url);
    return url.origin === self.location.origin && url.pathname.startsWith(BASE);
  } catch {
    return false;
  }
};

// A push payload is attacker-controlled the moment a subscription leaks: a click goes only to a page of this app.
const trustedUrl = (value) => {
  if (typeof value !== 'string' || !value) return null;
  try {
    const url = new URL(value, self.location.origin);
    if (url.origin !== self.location.origin || !url.pathname.startsWith(BASE)) return null;
    return `${url.pathname}${url.search}${url.hash}`;
  } catch {
    return null;
  }
};

const conversationUrl = (message) =>
  message.tabId ? `${BASE}?open=${encodeURIComponent(message.tabId)}` : BASE;

const countedKind = (kind) => kind && kind !== 'test' && kind !== 'release';

const updateBadge = async () => {
  if (!self.navigator?.setAppBadge) return;
  const shown = await self.registration.getNotifications();
  const count = shown.filter((n) => countedKind(n.data?.kind)).length;
  if (count) await self.navigator.setAppBadge(count);
  else await self.navigator.clearAppBadge?.();
};

self.addEventListener('push', (event) => {
  let message = {};
  try {
    message = event.data?.json() ?? {};
  } catch {
    message = { title: event.data?.text() ?? '' };
  }

  event.waitUntil(
    (async () => {
      // A person looking at the app gets an in-page notice. WebKit requires a visible notification for every push.
      if (!isWebKit && message.kind !== 'test') {
        const focused = (await windows()).find(
          (c) => isTop(c) && c.focused && c.visibilityState === 'visible',
        );
        if (focused) {
          focused.postMessage({ type: 'hl:push-message', message });
          return;
        }
      }
      await self.registration.showNotification(message.title || '', {
        body: message.body || '',
        icon: ICON,
        badge: ICON,
        tag: message.tag || undefined,
        renotify: Boolean(message.tag) && message.kind !== 'test',
        requireInteraction: message.kind === 'question',
        timestamp: typeof message.at === 'number' ? message.at : Date.now(),
        data: message,
      });
      await updateBadge();
    })(),
  );
});

self.addEventListener('message', (event) => {
  if (event.data?.type !== 'clear-notifications') return;
  event.waitUntil(
    (async () => {
      const shown = await self.registration.getNotifications();
      shown.forEach((n) => n.close());
      await self.navigator?.clearAppBadge?.();
    })(),
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const message = event.notification.data || {};

  // A release push reloads every open window of the app onto the new build.
  if (message.kind === 'release') {
    event.waitUntil(
      (async () => {
        await updateBadge();
        const tops = (await windows()).filter(
          (c) => isTop(c) && ownsPage(c) && typeof c.navigate === 'function',
        );
        if (!tops.length) return self.clients.openWindow(BASE);
        await Promise.all(tops.map((c) => c.navigate(c.url).catch(() => undefined)));
        return tops[0].focus();
      })(),
    );
    return;
  }

  const safeUrl = trustedUrl(message.url);
  event.waitUntil(
    (async () => {
      // iOS runs this after the page has already resumed: the page reads the target from this cache on focus.
      if (message.tabId && !safeUrl) {
        const cache = await caches.open(PUSH_NAV_CACHE);
        const nav = { tabId: message.tabId, workspaceId: message.workspaceId || null };
        await cache.put(PUSH_NAV_KEY, new Response(JSON.stringify(nav)));
      }
      await updateBadge();
      const tops = (await windows()).filter((c) => isTop(c) && ownsPage(c));

      if (safeUrl) {
        const existing = tops.find((c) => {
          const url = new URL(c.url);
          return `${url.pathname}${url.search}` === safeUrl.split('#')[0];
        });
        if (existing) return existing.focus();
        const client = tops.find((c) => typeof c.navigate === 'function');
        if (client) {
          await client.navigate(safeUrl);
          return client.focus();
        }
        return self.clients.openWindow(safeUrl);
      }

      const client = tops[0];
      if (client) {
        if (message.tabId) {
          client.postMessage({
            type: 'hl:open-conversation',
            tabId: message.tabId,
            workspaceId: message.workspaceId || null,
          });
        }
        return client.focus();
      }
      return self.clients.openWindow(conversationUrl(message));
    })(),
  );
});
