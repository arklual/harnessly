// The hub's side of «Управление»: one interface, a live client over `<hubUrl>/api/me/*` and a mock. The mock is chosen
// by `?hub=mock` (a gallery, tests); otherwise a configured hub is asked, and without one the screen runs on the local
// report alone, as it always did.
import { appConfig } from '@/platform/config';
import type { HubMe, MeKpis, MeLimits, MeSessions, MeUsage } from '@/next/features/manage/hub-types';
import { mockHub } from '@/next/features/manage/mock';

export class HubError extends Error {
  /** `off`: the hub does not serve the organization endpoints (404): not a failure, the feature is just not on. */
  readonly kind: 'off' | 'failed';
  constructor(kind: HubError['kind'], message: string) {
    super(message);
    this.name = 'HubError';
    this.kind = kind;
  }
}

export interface ManageHub {
  load(): Promise<HubMe>;
  /** Asks the person who set the limit for more. Resolves when the hub has taken the request. */
  askMore(message: string): Promise<void>;
}

const call = async <T>(base: string, path: string, init?: RequestInit): Promise<T> => {
  let res: Response;
  try {
    res = await fetch(`${base}${path}`, { credentials: 'include', ...init });
  } catch {
    throw new HubError('failed', 'network');
  }
  if (res.status === 404) throw new HubError('off', 'not found');
  if (!res.ok) throw new HubError('failed', String(res.status));
  return (await res.json()) as T;
};

export const liveHub = (base: string): ManageHub => ({
  load: async () => {
    const [limits, usage, sessions, kpis] = await Promise.all([
      call<MeLimits>(base, '/api/me/limits'),
      call<MeUsage>(base, '/api/me/usage'),
      call<MeSessions>(base, '/api/me/sessions'),
      call<MeKpis>(base, '/api/me/kpis'),
    ]);
    return { limits, usage, sessions, kpis };
  },
  askMore: async (message) => {
    await call<unknown>(base, '/api/me/limit-requests', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ message }),
    });
  },
});

/** The hub the screen talks to, or null when this runtime has none. */
export const manageHub = (): ManageHub | null => {
  if (new URLSearchParams(location.search).get('hub') === 'mock') return mockHub();
  const url = appConfig().hubUrl;
  return url ? liveHub(url) : null;
};
