import { t } from '@/lib/i18n';
import { useCallback, useEffect, useState } from 'react';
import {
  DiskFailure,
  fetchDiskPage,
  fetchDiskRoots,
  type IDiskEntry,
  type IDiskLocation,
  type IDiskRoot,
} from '@/lib/hl-disk-api';

interface IListing {
  /** `null` until the first page arrives: loading is not an empty folder. */
  entries: IDiskEntry[] | null;
  roots: IDiskRoot[] | null;
  nextCursor: string | null;
  truncated: boolean;
  failure: DiskFailure | null;
  loadingMore: boolean;
}

const EMPTY: IListing = {
  entries: null,
  roots: null,
  nextCursor: null,
  truncated: false,
  failure: null,
  loadingMore: false,
};

const keyOf = (location: IDiskLocation | null): string =>
  location ? `${location.root}:${location.path}` : '';

const failureOf = (error: unknown): DiskFailure =>
  error instanceof DiskFailure ? error : new DiskFailure(t('inspector.useDiskListing.failed'), true);

/** The roots at the top, otherwise one folder with its pages; a state of another folder is never shown for this one. */
const useDiskListing = (location: IDiskLocation | null) => {
  const key = keyOf(location);
  const [state, setState] = useState<{ key: string; listing: IListing }>({ key, listing: EMPTY });
  const [attempt, setAttempt] = useState(0);
  const listing = state.key === key ? state.listing : EMPTY;

  useEffect(() => {
    const controller = new AbortController();
    const settle = (change: (current: IListing) => IListing) =>
      setState((current) =>
        controller.signal.aborted
          ? current
          : { key, listing: change(current.key === key ? current.listing : EMPTY) },
      );
    const load = async () => {
      try {
        if (!location) {
          const roots = await fetchDiskRoots(controller.signal);
          settle((current) => ({ ...current, roots, failure: null }));
          return;
        }
        const page = await fetchDiskPage(location, null, controller.signal);
        settle((current) => ({ ...current, ...page, failure: null, loadingMore: false }));
      } catch (error) {
        if (error instanceof Error && error.name === 'AbortError') return;
        settle((current) => ({ ...current, failure: failureOf(error) }));
      }
    };
    void load();
    return () => controller.abort();
    // `location` is read through `key`: a new object for the same folder must not reload it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, attempt]);

  const loadMore = useCallback(async () => {
    if (!location || !listing.nextCursor || listing.loadingMore) return;
    const cursor = listing.nextCursor;
    setState((current) => ({ key, listing: { ...current.listing, loadingMore: true } }));
    try {
      const page = await fetchDiskPage(location, cursor, new AbortController().signal);
      setState((current) =>
        current.key === key
          ? {
              key,
              listing: {
                ...current.listing,
                entries: [...(current.listing.entries ?? []), ...page.entries],
                nextCursor: page.nextCursor,
                truncated: page.truncated,
                loadingMore: false,
                failure: null,
              },
            }
          : current,
      );
    } catch (error) {
      setState((current) =>
        current.key === key
          ? { key, listing: { ...current.listing, loadingMore: false, failure: failureOf(error) } }
          : current,
      );
    }
  }, [key, location, listing.nextCursor, listing.loadingMore]);

  /** A fresh first page; what is on screen stays until it arrives. */
  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  return { ...listing, loadMore, reload };
};

export default useDiskListing;

/** Roots with their names, for places opened from the favorites without passing the roots page. */
export const useDiskRoots = (): IDiskRoot[] | null => {
  const [roots, setRoots] = useState<IDiskRoot[] | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    fetchDiskRoots(controller.signal).then(
      (loaded) => {
        if (!controller.signal.aborted) setRoots(loaded);
      },
      () => {},
    );
    return () => controller.abort();
  }, []);
  return roots;
};
