// «Окружение» on the hub's endpoints (docs/hub/environment.md): the layers and their entries, the catalog, and a person's
// own switches. The answers are the wire types as they are; the screen reads them without a second shape in between.
import type {
  CatalogItemResponse,
  CatalogResponse,
  EnvCard,
  EnvEntryRow,
  EnvOrderResponse,
  EnvRunResponse,
  EnvRunsResponse,
  EnvTryResponse,
  ApplyResponse,
  GroupResponse,
  MeEnvGroup,
  MeEnvPresetResponse,
  PresetResponse,
  MeMachinesResponse,
  MeRunResponse,
  MeRunsResponse,
  EnvResponse,
  MeEnvItem,
  MeEnvResponse,
} from '../../../../hub-api/env';
import { ALL_SCOPE } from '../../types';
import type { EnvApi } from '../env';
import { http, orgPath, type Query } from './http';

/** `?scope=` of the environment screen: a tenant's slug, or nothing for all the tenants the person looks at. */
export const scopeQuery = (scope: string): Query => ({ scope: scope === ALL_SCOPE ? undefined : scope });

const withSpace = (path: string, space?: string): string =>
  space ? `${path}?space=${encodeURIComponent(space)}` : path;

const group = (org: string, id: string): string => orgPath(org, `/env/groups/${encodeURIComponent(id)}`);

const preset = (org: string, id: string): string => orgPath(org, `/env/presets/${encodeURIComponent(id)}`);

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
  envMineGroup: (groupId, on, space) =>
    http.put<MeEnvGroup>(`/api/me/env/groups/${encodeURIComponent(groupId)}`, { on, space }),
  envGroupAdd: async (org, req) => (await http.post<GroupResponse>(orgPath(org, '/env/groups'), req)).group,
  envGroupPatch: async (org, groupId, req) =>
    (await http.patch<GroupResponse>(group(org, groupId), req)).group,
  envGroupRemove: async (org, groupId) => {
    await http.del(group(org, groupId));
  },
  envMinePreset: (presetId, space) =>
    http.put<MeEnvPresetResponse>(`/api/me/env/presets/${encodeURIComponent(presetId)}`, { space }),
  envPresetAdd: async (org, req) =>
    (await http.post<PresetResponse>(orgPath(org, '/env/presets'), req)).preset,
  envPresetPatch: async (org, presetId, req) =>
    (await http.patch<PresetResponse>(preset(org, presetId), req)).preset,
  envPresetRemove: async (org, presetId) => {
    await http.del(preset(org, presetId));
  },
  envApply: (org, req) => http.post<ApplyResponse>(orgPath(org, '/env/apply'), req),
  catalogPatch: async (org, itemId, req) =>
    (await http.patch<CatalogItemResponse>(orgPath(org, `/catalog/${encodeURIComponent(itemId)}`), req)).item,
  catalogDelete: async (org, itemId) => {
    await http.del(orgPath(org, `/catalog/${encodeURIComponent(itemId)}`));
  },
  envOrder: async (org, req) => (await http.put<EnvOrderResponse>(orgPath(org, '/env/order'), req)).entryIds,
  envRuns: async (org, target, errors) =>
    (
      await http.get<EnvRunsResponse>(orgPath(org, '/env/runs'), {
        layer: target.layer,
        scopeId: target.scopeId,
        errors: errors ? '1' : undefined,
      })
    ).runs,
  envRun: async (org, runId) =>
    (await http.get<EnvRunResponse>(orgPath(org, `/env/runs/${encodeURIComponent(runId)}`))).run,
  envTry: (org, req) => http.post<EnvTryResponse>(orgPath(org, '/env/try'), req),
  meMachines: async (space) =>
    (await http.get<MeMachinesResponse>('/api/me/env/machines', { space })).machines,
  meRun: (req, space) => http.post<MeRunResponse>(withSpace('/api/me/env/run', space), req),
  meRuns: async (space) => (await http.get<MeRunsResponse>('/api/me/env/runs', { space })).runs,
});
