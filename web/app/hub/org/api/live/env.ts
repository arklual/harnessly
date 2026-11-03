// «Окружение» on the hub's endpoints (docs/hub/environment.md): the layers and their entries, the catalog, and a person's
// own switches. The answers are the wire types as they are; the screen reads them without a second shape in between.
import type {
  CatalogItemResponse,
  CatalogResponse,
  EnvCard,
  EnvEntryRow,
  EnvResponse,
  MeEnvItem,
  MeEnvResponse,
} from '../../../../hub-api/env';
import { ALL_SCOPE } from '../../types';
import type { EnvApi } from '../env';
import { http, orgPath, type Query } from './http';

/** `?scope=` of the environment screen: a tenant's slug, or nothing for all the tenants the person looks at. */
export const scopeQuery = (scope: string): Query => ({ scope: scope === ALL_SCOPE ? undefined : scope });

const entry = (org: string, id: string): string => orgPath(org, `/env/entries/${encodeURIComponent(id)}`);

export const liveEnv = (): EnvApi => ({
  envView: async (org, scope) => {
    const v = await http.get<EnvResponse>(orgPath(org, '/env'), scopeQuery(scope));
    // A hub older than the list of what the machines call answers without it.
    return { ...v, observed: v.observed ?? [] };
  },
  envCard: (org, entryId) => http.get<EnvCard>(orgPath(org, `/env/items/${encodeURIComponent(entryId)}`)),
  envAdd: (org, req) => http.post<EnvEntryRow>(orgPath(org, '/env/entries'), req),
  envPatch: (org, entryId, req) => http.patch<EnvEntryRow>(entry(org, entryId), req),
  envRemove: async (org, entryId) => {
    await http.del(entry(org, entryId));
  },
  envCatalog: async (org, target, kind) => {
    const res = await http.get<CatalogResponse>(orgPath(org, '/catalog'), {
      kind,
      layer: target.layer,
      scopeId: target.scopeId,
    });
    return res.items;
  },
  catalogAdd: async (org, req) => (await http.post<CatalogItemResponse>(orgPath(org, '/catalog'), req)).item,
  envMine: (space) => http.get<MeEnvResponse>('/api/me/env', { space }),
  envMineSet: (itemId, off, space) =>
    http.put<MeEnvItem>(`/api/me/env/${encodeURIComponent(itemId)}`, { off, space }),
});
