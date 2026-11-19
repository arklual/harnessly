// `/o/:org/:scope/:section` — the organization hub. `:scope` is `all` (the whole organization) or a tenant slug;
// `:section` is a screen of nav.ts. Anything the role may not open goes to its first screen.
import './org.css';

import { lazy, Suspense, useEffect } from 'react';

import { Redirect } from '../lib/router';
import { OrgProvider } from './context';
import { rememberOrg } from './step';
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
  'map',
  'profile',
];

// The organization hub (hx); its code and styles load with the first organization screen.
const HxApp = lazy(() => import('../hx'));

export const isSection = (v: string | undefined): v is Section => SECTIONS.some((s) => s === v);

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
      {() => (
        <Suspense fallback={null}>
          <HxApp />
        </Suspense>
      )}
    </OrgProvider>
  );
};
