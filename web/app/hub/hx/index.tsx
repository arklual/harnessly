// The new hub (the board «Хаб» one to one) for an organization that has `ui.hx` on. One chunk with its styles; every
// section is a chunk of its own, so the first screen does not wait for the others.
import './styles';

import { lazy, Suspense } from 'react';

import { Redirect } from '../lib/router';
import { useOrg } from '../org/context';
import type { Section } from '../org/types';
import { hxSectionsOf } from './nav';
import { HxShell } from './shell';

const SCREENS: Record<Section, ReturnType<typeof lazy>> = {
  overview: lazy(() => import('./screens/overview')),
  analytics: lazy(() => import('./screens/analytics')),
  machines: lazy(() => import('./screens/machines')),
  teams: lazy(() => import('./screens/teams')),
  team: lazy(() => import('./screens/team')),
  env: lazy(() => import('./screens/env')),
  accounts: lazy(() => import('./screens/accounts')),
  efficiency: lazy(() => import('./screens/efficiency')),
  tools: lazy(() => import('./screens/tools')),
  settings: lazy(() => import('./screens/settings')),
};

const Screen = () => {
  const { section, shell, base, scope } = useOrg();
  const first = shell.tenants[0];
  if (scope.kind === 'org' && shell.viewer.role !== 'admin' && first) {
    return <Redirect to={`/o/${shell.org.slug}/${first.slug}/${section}`} />;
  }
  if (!hxSectionsOf(shell.viewer.role, shell.features ?? []).includes(section)) {
    return <Redirect to={`${base}/overview`} />;
  }
  const S = SCREENS[section];
  return (
    <Suspense fallback={<div className="hxh-wait" role="status" aria-label="Загрузка" />}>
      <S key={section} />
    </Suspense>
  );
};

const HxApp = () => (
  <HxShell>
    <Screen />
  </HxShell>
);

export default HxApp;
