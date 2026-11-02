// The live org API: the hub's endpoints (docs/hub/orgs.md, analytics.md, limits.md), one adapter per section. The
// sections that take no organization or scope read them from what the shell last loaded for this address.
import type { OrgView, Scope as ApiScope } from '../../../hub-api/orgs';
import { OrgApiError } from './errors';
import type { OrgApi } from './index';
import { liveAnalytics } from './live/analytics';
import { liveMachines } from './live/machines';
import { liveMember } from './live/member';
import { liveOverview } from './live/overview';
import { liveShell, roleIn, viewerTeam } from './live/shell';
import { liveTeams } from './live/teams';

export const liveOrgApi = (): OrgApi => {
  let here: { view: OrgView; scope: ApiScope } | null = null;
  const place = () => {
    if (!here) throw new OrgApiError('not_ready', 'the shell has not loaded');
    return here;
  };
  const tenantsOf = ({ view, scope }: { view: OrgView; scope: ApiScope }) =>
    scope.kind === 'org' ? view.tenants : view.tenants.filter((t) => t.id === scope.id);
  const analyticsCtx = () => {
    const { view, scope } = place();
    const role = roleIn(view, scope);
    const tenant = tenantsOf({ view, scope })[0];
    const teamId = role === 'lead' ? viewerTeam(view, scope) : null;
    const team = view.teams.find((t) => t.id === teamId) ?? null;
    return {
      org: view.org.slug,
      scope: team ? ({ kind: 'team', id: team.id } as const) : scope,
      role,
      label: team
        ? `Команда ${team.name}`
        : scope.kind === 'org'
          ? `Организация ${view.org.name}`
          : `Тенант ${tenant?.name ?? ''}`,
      teamName: team?.name ?? null,
      people: team ? team.memberCount : tenantsOf({ view, scope }).reduce((n, t) => n + t.memberCount, 0),
    };
  };
  return {
    ...liveShell((view, scope) => {
      here = { view, scope };
    }),
    ...liveOverview(analyticsCtx),
    ...liveMember(),
    ...liveAnalytics(analyticsCtx),
    ...liveMachines(() => {
      const { view, scope } = place();
      const team = roleIn(view, scope) === 'lead' ? viewerTeam(view, scope) : null;
      return { org: view.org.slug, view, scope: team ? { kind: 'team', id: team } : scope };
    }),
    ...liveTeams(),
  };
};
