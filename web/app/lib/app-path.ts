import { appBasePath, platformApi } from '@/platform/config';
import { addressMachineId, currentMachineId } from '@/runtime/machine';

/** Where the runtime serves this page (RUNTIME_APP_BASE_PATH), read once from the page's config. */
export const APP_BASE_PATH = appBasePath();

/** The code names the platform API `/api/platform`; the runtime may serve it under another prefix (RUNTIME_PLATFORM_API). */
const PLATFORM_API = '/api/platform';
export const platformApiPath = (value: string): string => {
  const prefix = platformApi();
  if (
    prefix === PLATFORM_API ||
    !(value === PLATFORM_API || value.startsWith(`${PLATFORM_API}/`) || value.startsWith(`${PLATFORM_API}?`))
  )
    return value;
  return `${prefix}${value.slice(PLATFORM_API.length)}`;
};

/** The upstream terminal screen; the app root belongs to the Hl workspace. */
export const LEGACY_PATH = '/legacy';

export const prefixAppPath = (raw: string, basePath = APP_BASE_PATH): string => {
  const value = platformApiPath(raw);
  if (
    !basePath ||
    !value.startsWith('/') ||
    value.startsWith('//') ||
    value === basePath ||
    value.startsWith(`${basePath}/`)
  )
    return value;
  return `${basePath}${value}`;
};

/**
 * Next's router adds `basePath` itself; a path built with `prefixAppPath`/`scopedAppPath` (or one
 * that arrived from the service worker / a push payload, which need the real prefix) has to shed
 * it again before `router.push`, or the app ends up on `/agents/agents/...`.
 */
export const stripAppPath = (value: string, basePath = APP_BASE_PATH): string => {
  if (value === basePath) return '/';
  if (value.startsWith(`${basePath}/`)) return value.slice(basePath.length);
  return value;
};

export { currentMachineId, onViewMachineChange, setViewMachine, viewMachineId } from '@/runtime/machine';

/** A path of the home machine and the gateway's own lists: never scoped to the machine on screen. */
export const homeAppPath = (value: string): string => {
  const machine = addressMachineId();
  return machine ? scopedAppPath(value, machine) : prefixAppPath(value);
};

/** Runtime URLs carry their machine explicitly so open tabs cannot change each other's routing. */
export const scopedAppPath = (value: string, machineId = currentMachineId()): string => {
  const prefixed = prefixAppPath(value);
  if (!machineId || !prefixed.startsWith(`${APP_BASE_PATH}/`)) return prefixed;
  const hashAt = prefixed.indexOf('#');
  const hash = hashAt < 0 ? '' : prefixed.slice(hashAt);
  const pathAndQuery = hashAt < 0 ? prefixed : prefixed.slice(0, hashAt);
  const queryAt = pathAndQuery.indexOf('?');
  const pathname = queryAt < 0 ? pathAndQuery : pathAndQuery.slice(0, queryAt);
  const query = queryAt < 0 ? '' : pathAndQuery.slice(queryAt + 1);
  const params = new URLSearchParams(query);
  if (!params.has('machine')) params.set('machine', machineId);
  return `${pathname}?${params}${hash}`;
};

export const appFetch: typeof globalThis.fetch = (input, init) => {
  if (typeof input === 'string') {
    if (typeof window !== 'undefined' && /^https?:\/\//.test(input)) {
      const url = new URL(input);
      if (url.origin === window.location.origin)
        return globalThis.fetch(
          new URL(scopedAppPath(`${url.pathname}${url.search}${url.hash}`), url.origin).href,
          init,
        );
    }
    return globalThis.fetch(scopedAppPath(input), init);
  }
  if (typeof window === 'undefined') return globalThis.fetch(input, init);
  const url = new URL(input instanceof URL ? input.href : input.url);
  if (url.origin !== window.location.origin) return globalThis.fetch(input, init);
  const scoped = new URL(scopedAppPath(`${url.pathname}${url.search}${url.hash}`), url.origin);
  return globalThis.fetch(input instanceof URL ? scoped : new Request(scoped, input), init);
};

/** `fetch` of the home machine and the gateway's lists (`homeAppPath`), whatever conversation is on screen. */
export const homeFetch = (input: string, init?: RequestInit): Promise<Response> =>
  globalThis.fetch(homeAppPath(input), init);
