// `/o/:org/:scope/:section` — the organization hub. `:scope` is `all` (the whole organization) or a tenant slug;
// `:section` is a screen of nav.ts. Anything the role may not open goes to its first screen.
import './org.css';

import { AnalyticsScreen } from './screens/analytics';
import { MachinesScreen } from './screens/machines';
import { OverviewScreen } from './screens/overview';
import { TeamsScreen } from './screens/teams';
import { lazy, Suspense, useEffect } from 'react';

import { hxOn } from '../hx/flag';
import { Redirect } from '../lib/router';
import { OrgProvider, useOrg } from './context';
import { sectionsOf } from './nav';
import { rememberOrg } from './step';
import { OrgShell } from './shell';
import { ALL_SCOPE, type Section } from './types';

const SECTIONS: readonly Section[] = [
  'overview',
  'analytics',
  'machines',
  'teams',
  'team',
  'env',
  'accounts',
  'tools',
  'efficiency',
  'settings',
  'extensions',
];

// The new hub of the board «Хаб» behind the organization's `ui.hx` feature; its code and styles load only then.
const HxApp = lazy(() => import('../hx'));

export const isSection = (v: string | undefined): v is Section => SECTIONS.some((s) => s === v);

const Screen = () => {
  const { section, shell, base, scope } = useOrg();
  const first = shell.tenants[0];
  // Only an organization admin looks at the whole organization.
  if (scope.kind === 'org' && shell.viewer.role !== 'admin' && first) {
    return <Redirect to={`/o/${shell.org.slug}/${first.slug}/${section}`} />;
  }
  const allowed = sectionsOf(shell.viewer.role);
  if (!allowed.includes(section)) return <Redirect to={`${base}/overview`} />;
  if (section === 'overview') return <OverviewScreen />;
  if (section === 'analytics') return <AnalyticsScreen />;
  if (section === 'machines') return <MachinesScreen />;
  return <TeamsScreen />;
};

export interface OrgRouteProps {
  org: string;
  scope?: string;
  section?: string;
}

export const OrgRoutes = ({ org, scope, section }: OrgRouteProps) => {
  useEffect(() => rememberOrg(org), [org]);
  if (!scope) return <Redirect to={`/o/${org}/${ALL_SCOPE}/overview`} />;
  if (!isSection(section)) return <Redirect to={`/o/${org}/${scope}/overview`} />;
  return (
    <OrgProvider key={org} org={org} scopeSlug={scope} section={section}>
      {(_, shell) =>
        hxOn(shell.features) ? (
          <Suspense fallback={null}>
            <HxApp />
          </Suspense>
        ) : (
          <OrgShell>
            <Screen />
          </OrgShell>
        )
      }
    </OrgProvider>
  );
};
