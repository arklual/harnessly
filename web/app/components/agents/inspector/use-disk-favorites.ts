import { t } from '@/lib/i18n';
import { useCallback, useEffect, useState } from 'react';
import {
  DiskFailure,
  FAVORITE_STATE,
  favoriteStateOf,
  fetchDiskFavorites,
  pinDiskFavorite,
  unpinDiskFavorite,
  type IDiskFavorite,
  type IDiskLocation,
} from '@/lib/hl-disk-api';

interface IFavoriteError {
  failure: DiskFailure;
  place: IDiskLocation;
}

const failureOf = (error: unknown): DiskFailure =>
  error instanceof DiskFailure ? error : new DiskFailure(t('inspector.useDiskFavorites.failed'), true);

/** Pinned folders of the machine. A failed load leaves `favorites` null: the list of folders does not depend on it. */
const useDiskFavorites = () => {
  const [favorites, setFavorites] = useState<IDiskFavorite[] | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<IFavoriteError | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    fetchDiskFavorites(controller.signal).then(
      (loaded) => {
        if (controller.signal.aborted) return;
        setFavorites(loaded);
        setLoadFailed(false);
      },
      (failure: unknown) => {
        if (!controller.signal.aborted)
          setLoadFailed(!(failure instanceof Error && failure.name === 'AbortError'));
      },
    );
    return () => controller.abort();
  }, []);

  /** Pessimistic: the pin changes on screen only with the server's answer; meanwhile every pin is locked. */
  const toggle = useCallback(
    async (place: IDiskLocation) => {
      const state = favoriteStateOf(favorites, place);
      if (busy || state === FAVORITE_STATE.unknown) return;
      setBusy(true);
      setError(null);
      try {
        setFavorites(
          state === FAVORITE_STATE.pinned ? await unpinDiskFavorite(place) : await pinDiskFavorite(place),
        );
      } catch (failure) {
        setError({ failure: failureOf(failure), place });
      } finally {
        setBusy(false);
      }
    },
    [favorites, busy],
  );

  const dismissError = useCallback(() => setError(null), []);

  return { favorites, loadFailed, busy, error, toggle, dismissError };
};

export default useDiskFavorites;
