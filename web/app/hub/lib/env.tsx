// Everything the screens take from the browser, behind one object, so a gallery can run the real screens against a
// fake hub (dev/fake-hub.ts) and tests can swap any part. The default is the real browser.
import { createContext, type ReactNode, useContext } from 'react';

import { createApi, type HubApi } from './api';
import { seal, canSeal } from './seal';
import { createPasskey, getPasskey, passkeysSupported } from './webauthn';

export interface EventSourceLike {
  onopen: ((ev: Event) => void) | null;
  onerror: ((ev: Event) => void) | null;
  addEventListener(type: string, listener: (ev: MessageEvent<string>) => void): void;
  close(): void;
}

/** A tab opened inside a click and pointed at its address once that is known (popup blockers allow only that). */
export interface PendingWindow {
  go(url: string): void;
  close(): void;
}

export interface HubEnv {
  api: HubApi;
  eventSource(url: string): EventSourceLike;
  passkeys: {
    supported(): boolean;
    get(publicKey: unknown): Promise<unknown | null>;
    create(publicKey: unknown): Promise<unknown | null>;
  };
  seal: typeof seal;
  canSeal(): Promise<boolean>;
  copyText(text: string): Promise<boolean>;
  /** A full page navigation (an OAuth start, the runtime page): not a route of this UI. */
  assign(url: string): void;
  /** Opens a blank tab in a click handler. Null when the browser refused. */
  openBlank(): PendingWindow | null;
  /** Opens a URL in a new tab. */
  openTab(url: string): void;
  now(): number;
  origin(): string;
}

const copyText = async (text: string): Promise<boolean> => {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // An insecure origin or a refused permission: the old way through a hidden field.
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.className = 'k-visually-hidden';
    document.body.append(area);
    area.select();
    let ok = false;
    try {
      ok = document.execCommand('copy');
    } catch {
      ok = false;
    }
    area.remove();
    return ok;
  }
};

export const browserEnv = (): HubEnv => ({
  api: createApi(),
  eventSource: (url) => new EventSource(url, { withCredentials: true }),
  passkeys: { supported: passkeysSupported, get: getPasskey, create: createPasskey },
  seal,
  canSeal,
  copyText,
  assign: (url) => window.location.assign(url),
  openBlank: () => {
    const w = window.open('', '_blank');
    if (!w) return null;
    // The page that opens may be another origin (a machine's): it must not reach back to the hub's window.
    try {
      w.opener = null;
    } catch {
      // already cut off
    }
    return {
      go: (url) => {
        w.location.href = url;
      },
      close: () => w.close(),
    };
  },
  openTab: (url) => {
    window.open(url, '_blank', 'noopener,noreferrer');
  },
  now: () => Date.now(),
  origin: () => window.location.origin,
});

const EnvContext = createContext<HubEnv | null>(null);

export const EnvProvider = ({ env, children }: { env: HubEnv; children: ReactNode }) => (
  <EnvContext.Provider value={env}>{children}</EnvContext.Provider>
);

export const useEnv = (): HubEnv => {
  const env = useContext(EnvContext);
  if (!env) throw new Error('EnvProvider is missing');
  return env;
};

export const useApi = (): HubApi => useEnv().api;
