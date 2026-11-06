import type { ToolsApi, ToolsData } from '../tools';
import { contractGet, orgPath } from './http';
import type { AnalyticsCtx } from './analytics';

export const liveTools = (ctx: () => AnalyticsCtx): ToolsApi => ({
  toolsList: async (_org, { from, to }) => {
    const c = ctx();
    const out = await contractGet<ToolsData>(orgPath(c.org, '/tools'), {
      scope: `${c.scope.kind}:${c.scope.id}`,
      from,
      to,
    });
    return { ...out, items: out.items ?? [], days: out.days ?? [] };
  },
});
