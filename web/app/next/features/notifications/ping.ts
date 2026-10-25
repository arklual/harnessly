// How a notice gets the person's attention besides the card: a soft two-note chime (made on the spot, no file to load),
// «(2) …» before the tab's title and a dot on its icon while notices wait, and a notification of the system while the
// tab is in the background. Every call is safe where the browser has no such thing: it does nothing.

/** The chime: two short sine notes, quiet, a little up. Hz, seconds from the start, seconds long. */
const NOTES = [
  { hz: 880, at: 0, len: 0.16 },
  { hz: 1318.5, at: 0.09, len: 0.22 },
] as const;
const VOLUME = 0.06;

let audio: AudioContext | null = null;

/** Browsers start sound only after the person touched the page: the context is made and woken on the first touch. */
export const unlockSound = (): (() => void) => {
  const wake = () => {
    try {
      audio ??= new AudioContext();
      void audio.resume().catch(() => undefined);
    } catch {
      // No Web Audio here: the notices stay silent.
    }
  };
  const events = ['pointerdown', 'keydown'] as const;
  events.forEach((name) => window.addEventListener(name, wake, { capture: true, passive: true }));
  return () => events.forEach((name) => window.removeEventListener(name, wake, { capture: true }));
};

export const playChime = (): void => {
  if (!audio || audio.state !== 'running') return;
  const start = audio.currentTime + 0.01;
  for (const note of NOTES) {
    const osc = audio.createOscillator();
    const gain = audio.createGain();
    osc.type = 'sine';
    osc.frequency.value = note.hz;
    gain.gain.setValueAtTime(0, start + note.at);
    gain.gain.linearRampToValueAtTime(VOLUME, start + note.at + 0.015);
    gain.gain.exponentialRampToValueAtTime(0.0001, start + note.at + note.len);
    osc.connect(gain).connect(audio.destination);
    osc.start(start + note.at);
    osc.stop(start + note.at + note.len + 0.02);
  }
};

const COUNT_PREFIX = /^\(\d+\)\s/;

/** The tab's title with «(n) » before it while n notices wait, and without it at none. */
export const titleWithCount = (title: string, count: number): string => {
  const base = title.replace(COUNT_PREFIX, '');
  return count > 0 ? `(${count}) ${base}` : base;
};

const DOT_SIZE = 64;
let iconHref: string | null = null;
let iconDotted: Promise<string | null> | null = null;

const iconLink = (): HTMLLinkElement | null => document.querySelector<HTMLLinkElement>('link[rel~="icon"]');

/** The app's icon with a dot in its corner, drawn once. */
const dottedIcon = (href: string): Promise<string | null> =>
  new Promise((resolve) => {
    const img = new Image();
    img.onload = () => {
      try {
        const canvas = document.createElement('canvas');
        canvas.width = DOT_SIZE;
        canvas.height = DOT_SIZE;
        const ctx = canvas.getContext('2d');
        if (!ctx) return resolve(null);
        ctx.drawImage(img, 0, 0, DOT_SIZE, DOT_SIZE);
        ctx.globalCompositeOperation = 'destination-out';
        ctx.beginPath();
        ctx.arc(50, 14, 17, 0, Math.PI * 2);
        ctx.fill();
        ctx.globalCompositeOperation = 'source-over';
        ctx.fillStyle = '#7b61ff';
        ctx.beginPath();
        ctx.arc(50, 14, 12, 0, Math.PI * 2);
        ctx.fill();
        resolve(canvas.toDataURL('image/png'));
      } catch {
        resolve(null);
      }
    };
    img.onerror = () => resolve(null);
    img.src = href;
  });

/** Puts the dot on the tab's icon, or takes it off. */
export const setIconDot = (on: boolean): void => {
  const link = iconLink();
  if (!link) return;
  if (!on) {
    delete link.dataset.dot;
    if (iconHref !== null) link.href = iconHref;
    return;
  }
  link.dataset.dot = 'on';
  iconHref ??= link.href;
  iconDotted ??= dottedIcon(iconHref);
  void iconDotted.then((dotted) => {
    // The notices may have gone while it was drawn.
    if (dotted && link.dataset.dot === 'on') link.href = dotted;
  });
};

export const SYSTEM_PERMISSION = {
  granted: 'granted',
  denied: 'denied',
  ask: 'default',
  none: 'unsupported',
} as const;
export type TSystemPermission = (typeof SYSTEM_PERMISSION)[keyof typeof SYSTEM_PERMISSION];

export const systemPermission = (): TSystemPermission => {
  if (typeof Notification === 'undefined') return SYSTEM_PERMISSION.none;
  return Notification.permission;
};

/** Asks the browser once, from the person's own click in Settings; answers what it said. */
export const askSystemPermission = async (): Promise<TSystemPermission> => {
  if (typeof Notification === 'undefined') return SYSTEM_PERMISSION.none;
  if (Notification.permission !== 'default') return Notification.permission;
  try {
    return await Notification.requestPermission();
  } catch {
    return Notification.permission;
  }
};

const registration = async (): Promise<ServiceWorkerRegistration | null> => {
  try {
    return 'serviceWorker' in navigator ? ((await navigator.serviceWorker.getRegistration()) ?? null) : null;
  } catch {
    return null;
  }
};

/**
 * A notification of the system for a tab in the background. A browser subscribed to web push gets the runtime's own
 * push for the same event, so it gets nothing more from here. The tag is the event's: a second window of the app
 * showing the same one replaces it instead of adding another. Shown by the service worker where there is one, so a
 * click opens the chat the way a push does.
 */
export const showSystem = async (notice: {
  key: string;
  title: string;
  text: string;
  url: string;
  onClick: () => void;
}): Promise<void> => {
  if (systemPermission() !== SYSTEM_PERMISSION.granted) return;
  const reg = await registration();
  try {
    if (reg) {
      const subscribed = await reg.pushManager?.getSubscription().catch(() => null);
      if (subscribed) return;
      await reg.showNotification(notice.title, {
        body: notice.text,
        tag: notice.key,
        data: { url: notice.url },
      });
      return;
    }
    const shown = new Notification(notice.title, { body: notice.text, tag: notice.key });
    shown.onclick = () => {
      window.focus();
      notice.onClick();
      shown.close();
    };
  } catch {
    // The browser said no after all: the card in the page is still there.
  }
};
