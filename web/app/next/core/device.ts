// The device's stable id: hello.device on the socket and X-Harnessly-Device on HTTP. Made once, kept in
// localStorage; without storage the id lives as long as the page.

import { uuidv7 } from '@/next/core/ids';

const KEY = 'hx.device';
let cached: string | null = null;

export function deviceId(): string {
  if (cached) return cached;
  try {
    const kept = localStorage.getItem(KEY);
    if (kept) return (cached = kept);
  } catch {}
  cached = `dev-${uuidv7()}`;
  try {
    localStorage.setItem(KEY, cached);
  } catch {}
  return cached;
}

/** Forgets the cached id (tests). */
export function resetDeviceId(): void {
  cached = null;
}
