// The hub UI's routes (docs/hub/api.md, "UI routes"): which screen a path shows, and who may see it.
import { lazy, type ReactNode, Suspense, useEffect, useState } from 'react';

import { Button, EmptyState, Spinner } from '@kit';

import { type ApiError } from './lib/api';
import { useApi, useEnv } from './lib/env';
import { errorText } from './lib/errors';
import { t } from './lib/i18n';
import { Link, matchRoute, navigate, Redirect, useLocation } from './lib/router';
import { SessionProvider, useSession } from './lib/session';
import { AccessScreen } from './screens/access';
import { AccountScreen } from './screens/account';
import { AgentsScreen } from './screens/agents';
import { hxOn } from './hx/flag';
import { AuthScreen } from './screens/auth';
import { BrowserScreen } from './screens/browser';
import { DoneScreen } from './screens/done';
import { HomeScreen } from './screens/home';
import { InviteScreen } from './screens/invite';
import { MachinesScreen } from './screens/machines';
import { NetworkScreen } from './screens/network';
import { NewSpaceScreen } from './screens/new-space';
import { NewSiteScreen, SiteScreen, useSitesOn } from './screens/sites';
import { SpaceProvider, useSpace } from './screens/space';
import { SpaceSettingsScreen } from './screens/space-settings';
import { OrgStepScreen } from './org/screens/org-step';
import { http } from './org/api/live/http';
import { orgTargetFor, sectionForSpaceRoute } from './org/redirect';
import { orgLanding, orgStepDue, orgStepOn } from './org/step';
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
  '/network',
  '/browser/:machineId',
  '/s/:slug',
  '/s/:slug/settings',
  '/s/:slug/setup/:step',
  '/org/new',
  '/o/:org',
  '/o/:org/:scope',
  '/o/:org/:scope/:section',
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
/** The organizations of the person, asked again twice when the hub does not answer: a slow hub is not «no organization». */
const orgsWithRetry = async (alive: () => boolean): Promise<OrgList | null> => {
  for (let attempt = 0; ; attempt++) {
    try {
      return await http.get<OrgList>('/api/orgs');
    } catch {
      if (attempt >= 2 || !alive()) return null;
      await new Promise((done) => setTimeout(done, 1000 * (attempt + 1)));
    }
  }
};

const Landing = () => {
  const api = useApi();
  const { me, config } = useSession();
  const [failed, setFailed] = useState(0);
  const [tries, setTries] = useState(0);
  useEffect(() => {
    let alive = true;
    void (async () => {
      if (!me) {
        navigate('/signin', { replace: true });
        return;
      }
      // A member of an organization lands in its hub; without HUB_ORGS=on the spaces as before.
      let orgs: OrgList | null = null;
      if (orgStepOn(config)) {
        orgs = await orgsWithRetry(() => alive);
        if (!alive) return;
        // The hub did not answer: say so, never lead to «make an organization» as if there were none.
        if (orgs === null) {
          setFailed(tries + 1);
          return;
        }
        const target = orgLanding(orgs);
        if (target) {
          navigate(target, { replace: true });
          return;
        }
      }
      if (me.spaces.length === 0) {
        navigate('/new', { replace: true });
        return;
      }
      // The organization step comes once after the release, and can be put off.
      if (orgStepDue(config, me, orgs)) {
        navigate('/org/new', { replace: true });
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
  }, [api, me, config, tries]);
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

/**
 * `/apps/<id>`: the app's page lives on a machine; the person's machine in their last space opens it (HRN-913). With
 * none of them online, an app installed for the space opens at the hub, from the store (HRN-898).
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

const SETUP = {
  machines: <MachinesScreen />,
  agents: <AgentsScreen />,
  access: <AccessScreen />,
  done: <DoneScreen />,
} as const;

const SpaceRoutes = ({
  sub,
  step,
  site,
}: {
  sub: 'home' | 'settings' | 'setup' | 'site-new' | 'site';
  step: string | undefined;
  site?: string;
}) => {
  const { space, owner } = useSpace();
  const sitesOn = useSitesOn();
  useEffect(() => rememberSpace(space.slug), [space.slug]);
  if (sub === 'site-new' || sub === 'site') {
    if (!sitesOn) return <NotFound />;
    return sub === 'site-new' ? <NewSiteScreen /> : <SiteScreen key={site} name={site ?? ''} />;
  }
  if (sub === 'settings') return <SpaceSettingsScreen />;
  if (sub === 'home') return <HomeScreen />;
  const screen = step && step in SETUP ? SETUP[step as keyof typeof SETUP] : null;
  if (!screen) return <NotFound />;
  // Setting a space up is the owners' work.
  if (!owner) return <Redirect to={`/s/${space.slug}`} />;
  return screen;
};

/** The old address of a tenant of an organization leads to the organization hub; any other space opens as before. */
const SpaceEntry = ({
  route,
  slug,
  step,
  children,
}: {
  route: string;
  slug: string;
  step: string | undefined;
  children: ReactNode;
}) => {
  const { config } = useSession();
  const key = `${route}|${slug}|${step ?? ''}`;
  // Only the routes with a place in the organization hub need the question, and only where organizations are on.
  const asks = sectionForSpaceRoute(route, step) !== null && orgStepOn(config);
  const [answer, setAnswer] = useState<{ key: string; to: string | null } | null>(null);
  useEffect(() => {
    if (!asks) return;
    let alive = true;
    void orgTargetFor(config, route, slug, step).then((to) => alive && setAnswer({ key, to }));
    return () => {
      alive = false;
    };
  }, [asks, config, route, slug, step, key]);
  if (!asks) return <>{children}</>;
  if (answer?.key !== key) return <Loading />;
  return answer.to ? <Redirect to={answer.to} /> : <>{children}</>;
};

const HxAuth = lazy(() => import('./hx/auth').then((m) => ({ default: m.HxAuth })));
const HxInvite = lazy(() => import('./hx/auth').then((m) => ({ default: m.HxInvite })));
const HxOrgNew = lazy(() => import('./hx/auth').then((m) => ({ default: m.HxOrgNew })));

const Routes = () => {
  const { path, search } = useLocation();
  const { me, config } = useSession();
  const match = matchRoute(ROUTES, path);
  // Before sign-in there is no organization: the instance's own flag picks the kit.
  const hx = hxOn(config.features, search);

  if (!match) return <NotFound />;
  const { route, params } = match;
  if (route === '/') return <Landing />;
  if (hx && (route === '/signin' || route === '/signup')) {
    return (
      <Suspense fallback={null}>
        <HxAuth mode={route === '/signin' ? 'signin' : 'signup'} />
      </Suspense>
    );
  }
  if (hx && route === '/invite/:token') {
    return (
      <Suspense fallback={null}>
        <HxInvite token={params.token ?? ''} />
      </Suspense>
    );
  }
  if (route === '/signin') return <AuthScreen mode="signin" />;
  if (route === '/signup') return <AuthScreen mode="signup" />;
  if (route === '/invite/:token') return <InviteScreen token={params.token ?? ''} />;
  if (!me) return <Redirect to={`/signin?next=${encodeURIComponent(path + search)}`} />;
  if (route === '/new') return <NewSpaceScreen />;
  if (route === '/org/new') {
    return hx ? (
      <Suspense fallback={null}>
        <HxOrgNew />
      </Suspense>
    ) : (
      <OrgStepScreen />
    );
  }
  if (route === '/settings') return <AccountScreen />;
  if (route === '/network') return <NetworkScreen />;
  if (route === '/apps/:app') return <AppRedirect app={params.app ?? ''} search={search} />;
  if (route === '/browser/:machineId') return <BrowserScreen machineId={params.machineId ?? ''} />;
  if (route === '/o/:org' || route === '/o/:org/:scope' || route === '/o/:org/:scope/:section') {
    return <OrgRoutes org={params.org ?? ''} scope={params.scope} section={params.section} />;
  }
  const slug = params.slug ?? '';
  const SUB = {
    '/s/:slug': 'home',
    '/s/:slug/settings': 'settings',
    '/s/:slug/sites/new': 'site-new',
    '/s/:slug/sites/:name': 'site',
  } as const;
  const sub = route in SUB ? SUB[route as keyof typeof SUB] : 'setup';
  return (
    <SpaceEntry route={route} slug={slug} step={params.step}>
      <SpaceProvider slug={slug}>
        <SpaceRoutes sub={sub} step={params.step} site={params.name} />
      </SpaceProvider>
    </SpaceEntry>
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
