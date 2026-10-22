import { useEffect, useRef, useState } from 'react';
import { jsonRequest } from '@/lib/conversation-loading';
import { t } from '@/lib/i18n';
import { prefixAppPath } from '@/lib/app-path';

export interface ITranscriptSearchHit {
  fileId: string;
  path: string;
  machineId: string;
  machineName: string;
  provider: string;
  aiTitle: string | null;
  role: 'user' | 'assistant';
  byteOffset: number;
  recordedAt: string | null;
  snippet: string;
}

interface ISearchResponse {
  hits: ITranscriptSearchHit[];
  hasMore: boolean;
}

const MIN_QUERY_LENGTH = 2;
const DEBOUNCE_MS = 250;
const PAGE_SIZE = 20;

/** "Search conversations": a debounced, cancellable query against /agents/api/platform/search, empty below two characters. */
const useTranscriptSearch = (query: string) => {
  const [hits, setHits] = useState<ITranscriptSearchHit[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const requestId = useRef(0);

  const [prevQuery, setPrevQuery] = useState<string | null>(null);
  if (query !== prevQuery) {
    setPrevQuery(query);
    if (query.trim().length >= MIN_QUERY_LENGTH) {
      setLoading(true);
      setError(null);
    }
  }

  useEffect(() => {
    const trimmed = query.trim();
    // Stale results stay in state but unused: the caller only reads them once the
    // query is long enough again, so nothing needs clearing on a short query.
    if (trimmed.length < MIN_QUERY_LENGTH) return;
    const id = ++requestId.current;
    let cancelled = false;
    (async () => {
      await new Promise((resolve) => setTimeout(resolve, DEBOUNCE_MS));
      if (cancelled || requestId.current !== id) return;
      try {
        const body = await jsonRequest<ISearchResponse>(
          prefixAppPath(`/api/platform/search?q=${encodeURIComponent(trimmed)}&limit=${PAGE_SIZE}`),
        );
        if (cancelled || requestId.current !== id) return;
        setHits(body.hits);
        setHasMore(body.hasMore);
      } catch {
        if (cancelled || requestId.current !== id) return;
        setError(t('hooks.useTranscriptSearch.unavailable'));
        setHits([]);
        setHasMore(false);
      } finally {
        if (!cancelled && requestId.current === id) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [query]);

  return { hits, hasMore, loading, error };
};

export default useTranscriptSearch;
