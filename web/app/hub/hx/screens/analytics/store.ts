// What the screen asks the API for, kept by its question: choosing the same period or the same metric twice does not
// ask again, and the big chart's series can be fetched while the pointer is still on the tile. Answers that failed are
// not kept, so a retry asks again.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import type {
  AnalyticsApi,
  AnalyticsData,
  AnalyticsQuery,
  KpiChart,
  KpiId,
} from '../../../org/api/analytics';

export interface AnalyticsStore {
  data: (q: AnalyticsQuery) => Promise<AnalyticsData>;
  chart: (q: AnalyticsQuery, kpi: KpiId) => Promise<KpiChart>;
  forget: (key: string) => void;
}

export const dataKey = (q: AnalyticsQuery): string =>
  `${q.period.from}_${q.period.to}~${q.compare.from}_${q.compare.to}`;
export const chartKey = (q: AnalyticsQuery, kpi: KpiId): string => `${dataKey(q)}~${kpi}`;

export const createStore = (api: Pick<AnalyticsApi, 'analytics' | 'analyticsChart'>): AnalyticsStore => {
  const kept = new Map<string, Promise<unknown>>();
  const keep = <T>(key: string, ask: () => Promise<T>): Promise<T> => {
    const had = kept.get(key);
    if (had) return had as Promise<T>;
    const p = ask();
    kept.set(key, p);
    p.catch(() => {
      if (kept.get(key) === p) kept.delete(key);
    });
    return p;
  };
  return {
    data: (q) => keep(`d~${dataKey(q)}`, () => api.analytics(q)),
    chart: (q, kpi) => keep(`c~${chartKey(q, kpi)}`, () => api.analyticsChart(q, kpi)),
    forget: (key) => {
      kept.delete(`d~${key}`);
      kept.delete(`c~${key}`);
    },
  };
};

export const useStore = (api: Pick<AnalyticsApi, 'analytics' | 'analyticsChart'>): AnalyticsStore =>
  useMemo(() => createStore(api), [api]);

interface Slot<T> {
  key: string | null;
  data: T | null;
  error: unknown;
}

export interface Answer<T> {
  /** The last answer that came, for the key asked now or for the one before it. */
  data: T | null;
  /** The answer is for the key asked now. */
  fresh: boolean;
  error: unknown;
  reload: () => void;
}

/**
 * An answer that stays on the screen while the next one is on its way: `data` holds the last one that came and
 * `fresh` says whether it belongs to `key`. A failure of the key asked now is `error`.
 */
export const useAnswer = <T>(key: string | null, ask: () => Promise<T>, forget: () => void): Answer<T> => {
  const [slot, setSlot] = useState<Slot<T>>({ key: null, data: null, error: null });
  const [again, setAgain] = useState(0);
  // The latest `ask` is read when the effect runs; it is not a reason to ask again.
  const call = useRef({ ask, forget });
  useEffect(() => {
    call.current = { ask, forget };
  });
  useEffect(() => {
    if (key === null) return;
    let live = true;
    call.current.ask().then(
      (data) => {
        if (live) setSlot({ key, data, error: null });
      },
      (error: unknown) => {
        if (live) setSlot((s) => ({ key, data: s.data, error }));
      },
    );
    return () => {
      live = false;
    };
  }, [key, again, call]);
  const reload = useCallback(() => {
    call.current.forget();
    setSlot((s) => ({ ...s, key: null, error: null }));
    setAgain((n) => n + 1);
  }, [call]);
  const mine = slot.key === key;
  return {
    data: slot.data,
    fresh: mine && slot.error == null && slot.data !== null,
    error: mine ? slot.error : null,
    reload,
  };
};
