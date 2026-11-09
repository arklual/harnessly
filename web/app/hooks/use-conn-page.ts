import { useCallback, useEffect, useState } from 'react';
import { appFetch } from '@/lib/app-path';

export interface IConnPage {
  title: string;
  url: string;
  body: string;
}
export type TConnPageLoad =
  | { status: 'loading' }
  | { status: 'ready'; page: IConnPage }
  | { status: 'error'; error: 'not-found' | 'denied' | 'failed' };

const text = (v: unknown): string => (typeof v === 'string' ? v : '');

const request = async (conn: string, id: string, signal: AbortSignal): Promise<TConnPageLoad> => {
  try {
    const query = new URLSearchParams({ id });
    const response = await appFetch(`/api/platform/connections/${encodeURIComponent(conn)}/page?${query}`, {
      signal,
    });
    if (response.ok) {
      const body = (await response.json()) as { page?: Record<string, unknown> };
      const page = body?.page;
      if (!page || typeof page !== 'object') return { status: 'error', error: 'failed' };
      return {
        status: 'ready',
        page: { title: text(page.title), url: text(page.url), body: text(page.body) },
      };
    }
    if (response.status === 404) return { status: 'error', error: 'not-found' };
    if (response.status === 401 || response.status === 403) return { status: 'error', error: 'denied' };
    return { status: 'error', error: 'failed' };
  } catch {
    return { status: 'error', error: 'failed' };
  }
};

/** A wiki page of a connection for the pane's reader: loaded when the page changes, again on `reload`. */
const useConnPage = (conn: string | null, id: string | null): [TConnPageLoad, () => void] => {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<{ at: string; state: TConnPageLoad } | null>(null);
  const at = conn && id ? `${attempt}|${conn}|${id}` : '';
  useEffect(() => {
    if (!conn || !id) return;
    const controller = new AbortController();
    void request(conn, id, controller.signal).then((state) => {
      if (!controller.signal.aborted) setLoad({ at, state });
    });
    return () => controller.abort();
  }, [conn, id, at]);
  const reload = useCallback(() => setAttempt((value) => value + 1), []);
  return [load && load.at === at ? load.state : { status: 'loading' }, reload];
};

export default useConnPage;
