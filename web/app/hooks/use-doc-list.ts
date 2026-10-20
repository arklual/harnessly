import { useEffect, useState } from 'react';
import { fetchDocList } from '@/lib/doc-threads-client';
import { isPageHidden, subscribeFrameVisibility } from '@/lib/frame-visibility';
import type { IDocSummary } from '@/types/doc-threads';
import { t } from '@/lib/i18n';

const POLL_MS = 30_000;

/** Bound documents for the sidebar's "Documents" section; null while the first answer is in flight. */
const useDocList = (): { docs: IDocSummary[] | null; error: string | null } => {
  const [docs, setDocs] = useState<IDocSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const poll = async () => {
      if (isPageHidden()) {
        timer = setTimeout(poll, POLL_MS);
        return;
      }
      try {
        const { docs: next } = await fetchDocList();
        if (!cancelled) {
          setDocs(next);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : t('hooks.useDocList.loadFailed'));
      } finally {
        if (!cancelled) timer = setTimeout(poll, POLL_MS);
      }
    };

    void poll();
    const unsubscribe = subscribeFrameVisibility(() => {
      if (!isPageHidden() && timer) {
        clearTimeout(timer);
        void poll();
      }
    });
    const onVisibility = () => {
      if (document.visibilityState === 'visible' && timer) {
        clearTimeout(timer);
        void poll();
      }
    };
    document.addEventListener('visibilitychange', onVisibility);

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      unsubscribe();
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, []);

  return { docs, error };
};

export default useDocList;
