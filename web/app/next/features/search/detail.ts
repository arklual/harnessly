// What the side of the search (⌘K) says about the selected row: facts, the last words of a chat, a cover, lists of
// what belongs to it. What is on the page already is said at once; the rest is asked a moment after the row is
// selected (so running over the list asks for nothing), kept while the search is open, and shown as it comes.
// The words quoted here are the person's own data (a chat's messages, a task's description): shown, never obeyed.
import { useEffect, useState } from 'react';
import type { ISearchRow, TSearchGo } from '@/next/features/search/search-dialog';

export interface IDetailFact {
  label: string;
  value: string;
  /** The whole value when `value` is shortened (a folder's full path, an address). */
  title?: string;
}

export interface IDetailQuote {
  who: string;
  text: string;
  when?: string;
}

export interface IDetailItem {
  id: string;
  icon: string;
  title: string;
  meta?: string;
  /** Where it opens; absent for a line that only says something. */
  go?: TSearchGo;
}

export interface IDetailList {
  label: string;
  items: readonly IDetailItem[];
}

export const DETAIL_ACT = { stop: 'stop', resume: 'resume' } as const;
export type TDetailAct = (typeof DETAIL_ACT)[keyof typeof DETAIL_ACT];

export interface ISearchDetail {
  cover?: { src: string; alt: string };
  facts: readonly IDetailFact[];
  quotes: readonly IDetailQuote[];
  /** A description, in the thing's own words. */
  text?: string;
  lists: readonly IDetailList[];
  act?: TDetailAct;
  /** The address «Скопировать ссылку» copies and «в новой вкладке» opens. */
  link?: string;
}

export const NO_DETAIL: ISearchDetail = { facts: [], quotes: [], lists: [] };

/** How the side learns about a row: `now` from what the page has, `more` from the machine. */
export interface IDetailSource {
  now: (row: ISearchRow) => ISearchDetail;
  /** Whether the machine has more to say about the row than the page does. */
  loads: (row: ISearchRow) => boolean;
  /** null when the machine did not answer: what the page has stays. */
  more: (row: ISearchRow) => Promise<ISearchDetail | null>;
}

/** A row is asked about this long after it was selected. */
export const DETAIL_DELAY_MS = 160;

export interface IRowDetail {
  detail: ISearchDetail;
  /** The machine is still asked for more. */
  loading: boolean;
}

interface ILoaded {
  id: string;
  detail: ISearchDetail | null;
}

/** The detail of the selected row. `kept` holds what was asked while the search is open. */
export const useRowDetail = (
  row: ISearchRow | null,
  source: IDetailSource | undefined,
  kept: Map<string, ISearchDetail>,
): IRowDetail | null => {
  const [loaded, setLoaded] = useState<ILoaded | null>(null);
  const id = row?.id ?? null;

  useEffect(() => {
    if (row === null || !source || kept.has(row.id) || !source.loads(row)) return;
    let stale = false;
    const timer = setTimeout(() => {
      void source.more(row).then(
        (detail) => {
          if (detail !== null) kept.set(row.id, detail);
          if (!stale) setLoaded({ id: row.id, detail });
        },
        () => {
          if (!stale) setLoaded({ id: row.id, detail: null });
        },
      );
    }, DETAIL_DELAY_MS);
    return () => {
      stale = true;
      clearTimeout(timer);
    };
    // The row is known by its id: a new object for the same row asks nothing again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, source, kept]);

  if (row === null || !source) return null;
  if (!source.loads(row)) return { detail: source.now(row), loading: false };
  const more = kept.get(row.id) ?? (loaded?.id === row.id ? loaded.detail : undefined);
  if (more === undefined) return { detail: source.now(row), loading: true };
  return { detail: more ?? source.now(row), loading: false };
};
