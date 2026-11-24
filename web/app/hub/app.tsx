// The hub UI's routes (docs/hub/api.md, "UI routes"): which screen a path shows, and who may see it.
import { lazy, type ReactNode, Suspense, useEffect, useState } from 'react';

import { Button, EmptyState, Spinner } from '@kit';

import { type ApiError } from './lib/api';
import { useApi, useEnv } from './lib/env';
import { errorText } from './lib/errors';
import { t } from './lib/i18n';
import { Link, matchRoute, navigate, Redirect, useLocation } from './lib/router';
import { SessionProvider, useSession } from './lib/session';
import { BrowserScreen } from './pages/browser';
import { ConnectScreen } from './pages/connect';
import { NetworkScreen } from './pages/network';
import { SwitchesScreen } from './pages/switches';
import { EventsScreen } from './pages/events';
import { SitesPage } from './pages/sites';
import { http } from './org/api/live/http';
import { dataMode } from './org/mode';
import { orgTargetFor } from './org/redirect';
import { orgLanding, orgStepOn } from './org/step';
import { ALL_SCOPE, type Section } from './org/types';
import type { OrgList } from '../hub-api/orgs';
import { OrgRoutes } from './org/routes';
import { PlainPage } from './ui/frame';
import { appPathOn, appTarget } from './lib/app-address';

const ROUTES = [
  '/',
  '/signin',
  '/signup',
  '/invite/:token',
  '/new',
  '/settings',
  '/profile',
  '/connect',
  '/network',
  '/switches',
  '/events',
  '/browser/:machineId',
  '/s/:slug',
  '/s/:slug/settings',
  '/s/:slug/setup/:step',
  '/org/new',
  '/o/:org',
  '/o/:org/:scope',
  '/o/:org/:scope/:section',
  '/s/:slug/sites',
  '/s/:slug/sites/new',
  '/s/:slug/sites/:name',
  '/apps/:app',
] as const;

const LAST_SPACE = 'hl-hub-space';

const lastSpace = (): string | null => {
  try {
    return localStorage.getItem(LAST_SPACE);
  } catch {
    return null;
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

/** The organizations of the person, asked again twice when the hub does not answer: a slow hub is not «no organization». */
const orgsWithRetry = async (alive: () => boolean): Promise<OrgList | null> => {
  // The dev server's mocks (org/mode.ts) have one organization; the org screens never ask the hub for the list.
  if (import.meta.env.DEV && dataMode() === 'mock') return MOCK_ORG_LIST;
  for (let attempt = 0; ; attempt++) {
    try {
      return await http.get<OrgList>('/api/orgs');
    } catch {
      if (attempt >= 2 || !alive()) return null;
      await new Promise((done) => setTimeout(done, 1000 * (attempt + 1)));
    }
  }
};

const MOCK_ORG_LIST: OrgList = {
  orgs: [
    {
      id: 'o-acme',
      slug: 'acme',
      name: 'Акме',
      timezone: 'Europe/Moscow',
      currency: 'RUB',
      createdAt: '2026-10-01T09:00:00Z',
      role: 'admin',
    },
  ],
  looseSpaces: [],
};

/**
 * Where a signed-in person goes in the organization hub: `section` of the organization they opened last (`/` with no
 * section), or making the first organization when they have none. The hub not answering is said, never taken for «none».
 */
const OrgLanding = ({ section, search = '' }: { section?: Section; search?: string }) => {
  const { me, config } = useSession();
  const [failed, setFailed] = useState(0);
  const [tries, setTries] = useState(0);
  const off = !orgStepOn(config);
  useEffect(() => {
    let alive = true;
    void (async () => {
      if (!me) {
        navigate('/signin', { replace: true });
        return;
      }
      if (off) return;
      const orgs = await orgsWithRetry(() => alive);
      if (!alive) return;
      if (orgs === null) {
        setFailed(tries + 1);
        return;
      }
      const target = orgLanding(orgs);
      navigate(target ? (section ? `${target}/${ALL_SCOPE}/${section}${search}` : target) : '/org/new', {
        replace: true,
      });
    })();
    return () => {
      alive = false;
    };
  }, [me, off, section, search, tries]);
  if (off) {
    return (
      <PlainPage>
        <EmptyState title={t('landing.orgsOffTitle')}>{t('landing.orgsOff')}</EmptyState>
      </PlainPage>
    );
  }
  if (failed > tries) {
    return (
      <PlainPage>
        <EmptyState
          title={t('session.failed')}
          actions={<Button onClick={() => setTries(failed)}>{t('common.retry')}</Button>}
        >
          {t('landing.orgsFailed')}
        </EmptyState>
      </PlainPage>
    );
  }
  return <Loading />;
};

/** An old space address (`/s/<slug>`, its settings and setup) leads to the tenant in its organization, else to `/`. */
const SpaceRedirect = ({ route, slug, step }: { route: string; slug: string; step: string | undefined }) => {
  const { config } = useSession();
  const [to, setTo] = useState<string | null>(null);
  useEffect(() => {
    let alive = true;
    void orgTargetFor(config, route, slug, step).then((t) => alive && setTo(t ?? '/'));
    return () => {
      alive = false;
    };
  }, [config, route, slug, step]);
  return to ? <Redirect to={to} /> : <Loading />;
};

/**
 * `/apps/<id>`: the app's page lives on a machine; the person's machine in their last space opens it. With
 * none of them online, an app installed for the space opens at the hub, from the store.
 */
const AppRedirect = ({ app, search }: { app: string; search: string }) => {
  const api = useApi();
  const env = useEnv();
  const { me } = useSession();
  const [none, setNone] = useState(false);
  useEffect(() => {
    let alive = true;
    void (async () => {
      const ref = me?.spaces.find((s) => s.slug === lastSpace()) ?? me?.spaces[0];
      try {
        if (!ref) throw new Error('no space');
        const { space } = await api.spaces.bySlug(ref.slug);
        const { machines } = await api.machines.list(space.id);
        const online = machines.some((m) => m.status === 'online');
        // The space's apps are asked only when no machine is online; a hub without a store has none to give.
        const installed = online
          ? []
          : await api.spaces.apps(space.id).then(
              (r) => r.apps,
              () => [],
            );
        const to = appTarget(machines, app, installed);
        if (!to) throw new Error('no machine');
        if ('hub' in to) {
          if (alive) env.assign(to.hub);
          return;
        }
        const { url } = await api.machines.open(to.machine, appPathOn(to.machine, app, search));
        if (alive) env.assign(url);
      } catch {
        if (alive) setNone(true);
      }
    })();
    return () => {
      alive = false;
    };
  }, [api, env, me, app, search]);
  if (!none) return <Loading />;
  return (
    <PlainPage>
      <EmptyState
        title={t('appRedirect.title')}
        actions={
          <Link to="/" className="hub-link">
            {t('space.toHome')}
          </Link>
        }
      >
        {t('appRedirect.text', { path: appPathOn('<machine>', app) })}
      </EmptyState>
    </PlainPage>
  );
};

const HxAuth = lazy(() => import('./hx/auth').then((m) => ({ default: m.HxAuth })));
const HxInvite = lazy(() => import('./hx/auth').then((m) => ({ default: m.HxInvite })));
const HxOrgNew = lazy(() => import('./hx/auth').then((m) => ({ default: m.HxOrgNew })));

const lazyPage = (node: ReactNode) => <Suspense fallback={<Loading />}>{node}</Suspense>;

const Routes = () => {
  const { path, search } = useLocation();
  const { me } = useSession();
  const match = matchRoute(ROUTES, path);

  if (!match) return <NotFound />;
  const { route, params } = match;
  if (route === '/') return <OrgLanding />;
  if (route === '/signin' || route === '/signup') {
    return lazyPage(<HxAuth mode={route === '/signin' ? 'signin' : 'signup'} />);
  }
  if (route === '/invite/:token') return lazyPage(<HxInvite token={params.token ?? ''} />);
  if (!me) return <Redirect to={`/signin?next=${encodeURIComponent(path + search)}`} />;
  if (route === '/org/new') return lazyPage(<HxOrgNew />);
  // The first space is made with the first organization.
  if (route === '/new') return <Redirect to="/" />;
  if (route === '/settings' || route === '/profile') return <OrgLanding section="profile" search={search} />;
  if (route === '/connect') return <ConnectScreen />;
  if (route === '/network') return <NetworkScreen />;
  if (route === '/switches') return <SwitchesScreen />;
  if (route === '/events') return <EventsScreen />;
  if (route === '/apps/:app') return <AppRedirect app={params.app ?? ''} search={search} />;
  if (route === '/browser/:machineId') return <BrowserScreen machineId={params.machineId ?? ''} />;
  if (route === '/o/:org' || route === '/o/:org/:scope' || route === '/o/:org/:scope/:section') {
    return <OrgRoutes org={params.org ?? ''} scope={params.scope} section={params.section} />;
  }
  const slug = params.slug ?? '';
  if (route === '/s/:slug/sites' || route === '/s/:slug/sites/new' || route === '/s/:slug/sites/:name') {
    return (
      <SitesPage
        key={slug}
        slug={slug}
        page={route === '/s/:slug/sites' ? 'list' : route === '/s/:slug/sites/new' ? 'new' : 'site'}
        name={params.name}
      />
    );
  }
  return <SpaceRedirect route={route} slug={slug} step={params.step} />;
};

export const App = () => (
  <SessionProvider
    loading={<Loading />}
    failed={(error, retry) => <SessionFailed error={error} retry={retry} />}
  >
    <Routes />
  </SessionProvider>
);
