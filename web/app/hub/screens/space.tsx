// A space opened by its slug: the space itself and its live data (machines, installs, logins) behind one context, so
// the setup steps and the home page share one event stream.
import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';

import { Button } from '@kit';

import { isApiError } from '../lib/api';
import { errorText } from '../lib/errors';
import { useApi } from '../lib/env';
import { type Live, useSpaceLive } from '../lib/hooks';
import { t } from '../lib/i18n';
import { lostSpace } from '../lib/live';
import { Link, navigate } from '../lib/router';
import { useSession } from '../lib/session';
import type { Space } from '../lib/types';
import { PlainPage } from '../ui/frame';
import { EmptyState } from '@kit';
import { ListSkeleton } from '../ui/parts';

export interface SpaceCtx {
  space: Space;
  owner: boolean;
  live: Live;
  setSpace: (space: Space) => void;
}

const Ctx = createContext<SpaceCtx | null>(null);

export const useSpace = (): SpaceCtx => {
  const c = useContext(Ctx);
  if (!c) throw new Error('SpaceProvider is missing');
  return c;
};

const Live = ({
  space,
  setSpace,
  children,
}: {
  space: Space;
  setSpace: (s: Space) => void;
  children: ReactNode;
}) => {
  const api = useApi();
  const { reload } = useSession();
  const live = useSpaceLive(space.id);
  // Removed from the space while it was open: the list of spaces is read again and the page goes to it.
  const lost = lostSpace(live.error);
  useEffect(() => {
    if (!lost) return;
    void reload().finally(() => navigate('/'));
  }, [lost, reload]);
  // The stream opened again: the person's role may be another one, and with it what the page offers.
  const opened = live.opened;
  const slug = space.slug;
  useEffect(() => {
    if (opened < 2) return;
    let alive = true;
    api.spaces.bySlug(slug).then(
      (r) => {
        if (alive) setSpace(r.space);
      },
      () => {
        // A space that is gone is told by the machines' answer above.
      },
    );
    return () => {
      alive = false;
    };
  }, [api, slug, opened, setSpace]);
  const value = useMemo<SpaceCtx>(
    () => ({ space, owner: space.role === 'owner', live, setSpace }),
    [space, live, setSpace],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
};

export const SpaceProvider = ({ slug, children }: { slug: string; children: ReactNode }) => {
  const api = useApi();
  const [space, setSpace] = useState<Space | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    void api.spaces
      .bySlug(slug)
      .then((r) => {
        if (alive) {
          setSpace(r.space);
          setError(null);
        }
      })
      .catch((e: unknown) => {
        if (alive) setError(e);
      });
    return () => {
      alive = false;
    };
  }, [api, slug, attempt]);

  const keep = useCallback((s: Space) => setSpace(s), []);

  if (error && !space) {
    const gone = isApiError(error, 'not_found') || isApiError(error, 'forbidden');
    return (
      <PlainPage>
        <EmptyState
          title={gone ? t('space.notFound') : t('space.loadFailed')}
          actions={
            gone ? (
              <Link to="/" className="hub-link">
                {t('space.toHome')}
              </Link>
            ) : (
              <Button onClick={() => setAttempt((n) => n + 1)}>{t('common.retry')}</Button>
            )
          }
        >
          {gone ? t('space.notFoundText') : errorText(error)}
        </EmptyState>
      </PlainPage>
    );
  }
  if (!space) {
    return (
      <PlainPage>
        <ListSkeleton rows={3} />
      </PlainPage>
    );
  }
  return (
    <Live key={space.id} space={space} setSpace={keep}>
      {children}
    </Live>
  );
};

const ORDER = ['machines', 'agents', 'access', 'done'] as const;

/** Moves the onboarding to a step: the space remembers the furthest step it reached, going back changes nothing. */
export const useStepNav = () => {
  const api = useApi();
  const { space, setSpace } = useSpace();
  return useCallback(
    async (to: (typeof ORDER)[number]) => {
      if (ORDER.indexOf(to) > ORDER.indexOf(space.onboardingStep)) {
        const { space: moved } = await api.spaces.update(space.id, { onboardingStep: to });
        setSpace(moved);
      }
      navigate(`/s/${space.slug}/setup/${to}`);
    },
    [api, space, setSpace],
  );
};

/** Where the segment of a finished step in the step bar leads. */
export const useGoStep = (here: string) => {
  const { space } = useSpace();
  return useCallback(
    (i: number) => {
      if (i === 0) navigate('/settings');
      else if (i === 1)
        navigate(`/new?space=${encodeURIComponent(space.slug)}&back=${encodeURIComponent(here)}`);
      else
        navigate(
          `/s/${space.slug}/setup/${(['machines', 'agents', 'access', 'done'] as const)[i - 2] ?? 'machines'}`,
        );
    },
    [space.slug, here],
  );
};
