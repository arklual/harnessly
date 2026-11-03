// The one way from the workspace to the hub: the product switcher at the top of the sidebar. Which hub, and whether
// this person has one at all, is decided here; the sidebar only draws the switcher when it is given a link.
import { useEffect, useState } from 'react';
import { appConfig } from '@/platform/config';

const CACHE_KEY = 'hl-hub-entry';

interface IHubSource {
  hubUrl: string | null;
  hubLink: string | null;
}

/** The hub to offer: the one this machine is connected to, else the public link; no trailing slash, or null. */
export const hubTarget = (config: IHubSource = appConfig()): string | null => {
  const target = config.hubUrl || config.hubLink;
  return target ? target.replace(/\/+$/, '') : null;
};

/** The hub lives on the page's own origin: there the person may not have it at «/» yet. */
export const isSameOrigin = (target: string, origin: string = location.origin): boolean => {
  try {
    return new URL(target, origin).origin === origin;
  } catch {
    return false;
  }
};

const readCache = (target: string): boolean | null => {
  try {
    const v = sessionStorage.getItem(`${CACHE_KEY}:${target}`);
    return v === '1' ? true : v === '0' ? false : null;
  } catch {
    return null;
  }
};

const writeCache = (target: string, has: boolean): void => {
  try {
    sessionStorage.setItem(`${CACHE_KEY}:${target}`, has ? '1' : '0');
  } catch {
    // Without storage the probe just runs again on the next page load.
  }
};

/**
 * Does the page's own «/» serve the hub to this person? The gateway answers a person without the hub with a redirect
 * to the workspace, which a manual-redirect fetch shows as an opaque redirect.
 */
export const probeHubAtRoot = async (fetchImpl: typeof fetch = fetch): Promise<boolean> => {
  try {
    const res = await fetchImpl('/', {
      method: 'GET',
      redirect: 'manual',
      credentials: 'same-origin',
      headers: { accept: 'text/html' },
    });
    return res.type !== 'opaqueredirect' && res.ok;
  } catch {
    return false;
  }
};

/** Where the switcher's «Хаб» row goes, or null when the person has no hub to go to (resolved once per page load). */
export const useHubEntry = (): string | null => {
  const target = hubTarget();
  const probed = target !== null && isSameOrigin(target);
  const [has, setHas] = useState<boolean | null>(() => (target && probed ? readCache(target) : null));
  useEffect(() => {
    if (!target || !probed || has !== null) return;
    let alive = true;
    void probeHubAtRoot().then((ok) => {
      writeCache(target, ok);
      if (alive) setHas(ok);
    });
    return () => {
      alive = false;
    };
  }, [target, probed, has]);
  if (!target) return null;
  if (probed && has !== true) return null;
  return `${target}/`;
};
