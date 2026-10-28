// The hub UI's routes (docs/hub/api.md, "UI routes"): which screen a path shows, and who may see it.
import { useEffect } from 'react';

import { Button, EmptyState, Spinner } from '@kit';

import { type ApiError } from './lib/api';
import { useApi } from './lib/env';
import { errorText } from './lib/errors';
import { t } from './lib/i18n';
import { Link, matchRoute, navigate, Redirect, useLocation } from './lib/router';
import { SessionProvider, useSession } from './lib/session';
import { AccessScreen } from './screens/access';
import { AccountScreen } from './screens/account';
import { AgentsScreen } from './screens/agents';
import { AuthScreen } from './screens/auth';
import { DoneScreen } from './screens/done';
import { HomeScreen } from './screens/home';
import { InviteScreen } from './screens/invite';
import { MachinesScreen } from './screens/machines';
import { NewSpaceScreen } from './screens/new-space';
import { SpaceProvider, useSpace } from './screens/space';
import { SpaceSettingsScreen } from './screens/space-settings';
import { PlainPage } from './ui/frame';

const ROUTES = [
  '/',
  '/signin',
  '/signup',
  '/invite/:token',
  '/new',
  '/settings',
  '/s/:slug',
  '/s/:slug/settings',
  '/s/:slug/setup/:step',
] as const;

const LAST_SPACE = 'hl-hub-space';

const lastSpace = (): string | null => {
  try {
    return localStorage.getItem(LAST_SPACE);
  } catch {
    return null;
  }
};

const rememberSpace = (slug: string) => {
  try {
    localStorage.setItem(LAST_SPACE, slug);
  } catch {
    // not remembered
  }
};

const Loading = () => (
  <div className="hub">
    <div className="hub-page">
      <div className="hub-enter" role="status">
        <Spinner size="s" />
        {t('common.loading')}
      </div>
    </div>
  </div>
);

const NotFound = () => (
  <PlainPage>
    <EmptyState
      title={t('notFound.title')}
      actions={
        <Link to="/" className="hub-link">
          {t('space.toHome')}
        </Link>
      }
    >
      {t('notFound.text')}
    </EmptyState>
  </PlainPage>
);

const SessionFailed = ({ error, retry }: { error: ApiError; retry: () => void }) => (
  <PlainPage>
    <EmptyState title={t('session.failed')} actions={<Button onClick={retry}>{t('common.retry')}</Button>}>
      {errorText(error)}
    </EmptyState>
  </PlainPage>
);

/** `/`: signed out → sign-in; no space → the first one is made; else the last space, or where its setup stopped. */
const Landing = () => {
  const api = useApi();
  const { me } = useSession();
  useEffect(() => {
    let alive = true;
    void (async () => {
      if (!me) {
        navigate('/signin', { replace: true });
        return;
      }
      if (me.spaces.length === 0) {
        navigate('/new', { replace: true });
        return;
      }
      const remembered = lastSpace();
      const ref = me.spaces.find((s) => s.slug === remembered) ?? me.spaces[0];
      if (!ref) return;
      try {
        const { space } = await api.spaces.bySlug(ref.slug);
        if (!alive) return;
        navigate(
          space.role === 'owner' && space.onboardingStep !== 'done'
            ? `/s/${space.slug}/setup/${space.onboardingStep}`
            : `/s/${space.slug}`,
          { replace: true },
        );
      } catch {
        if (alive) navigate(`/s/${ref.slug}`, { replace: true });
      }
    })();
    return () => {
      alive = false;
    };
  }, [api, me]);
  return <Loading />;
};

const SETUP = {
  machines: <MachinesScreen />,
  agents: <AgentsScreen />,
  access: <AccessScreen />,
  done: <DoneScreen />,
} as const;

const SpaceRoutes = ({ sub, step }: { sub: 'home' | 'settings' | 'setup'; step: string | undefined }) => {
  const { space, owner } = useSpace();
  useEffect(() => rememberSpace(space.slug), [space.slug]);
  if (sub === 'settings') return <SpaceSettingsScreen />;
  if (sub === 'home') return <HomeScreen />;
  const screen = step && step in SETUP ? SETUP[step as keyof typeof SETUP] : null;
  if (!screen) return <NotFound />;
  // Setting a space up is the owners' work.
  if (!owner) return <Redirect to={`/s/${space.slug}`} />;
  return screen;
};

const Routes = () => {
  const { path, search } = useLocation();
  const { me } = useSession();
  const match = matchRoute(ROUTES, path);

  if (!match) return <NotFound />;
  const { route, params } = match;
  if (route === '/') return <Landing />;
  if (route === '/signin') return <AuthScreen mode="signin" />;
  if (route === '/signup') return <AuthScreen mode="signup" />;
  if (route === '/invite/:token') return <InviteScreen token={params.token ?? ''} />;
  if (!me) return <Redirect to={`/signin?next=${encodeURIComponent(path + search)}`} />;
  if (route === '/new') return <NewSpaceScreen />;
  if (route === '/settings') return <AccountScreen />;
  const slug = params.slug ?? '';
  const sub = route === '/s/:slug' ? 'home' : route === '/s/:slug/settings' ? 'settings' : 'setup';
  return (
    <SpaceProvider slug={slug}>
      <SpaceRoutes sub={sub} step={params.step} />
    </SpaceProvider>
  );
};

export const App = () => (
  <SessionProvider
    loading={<Loading />}
    failed={(error, retry) => <SessionFailed error={error} retry={retry} />}
  >
    <Routes />
  </SessionProvider>
);
