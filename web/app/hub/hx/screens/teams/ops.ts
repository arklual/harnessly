// The actions of the team screens as optimistic operations: what the screen shows at once and the call to the hub.
import { OrgApiError } from '../../../org/api/errors';
import type { MemberRole } from '../../../org/api/teams';
import {
  patchMachines,
  patchMove,
  patchNewTeam,
  patchPersonLimit,
  patchRename,
  patchRole,
  patchTeamLimit,
} from './model';
import type { Op } from './use-teams';

export const personLimit = (id: string, rub: number | null): Op => ({
  patch: (v) => patchPersonLimit(v, id, rub),
  run: (api, org, scope) => api.setLimit(org, scope, { kind: 'user', id }, rub),
});

export const teamLimit = (id: string, rub: number): Op => ({
  patch: (v) => patchTeamLimit(v, id, rub),
  run: (api, org, scope) => api.setLimit(org, scope, { kind: 'team', id }, rub),
});

/** The monthly limit of the whole tenant: a ceiling over every team limit inside it. */
export const tenantLimit = (rub: number): Op => ({
  patch: (v) => ({ ...v, scope: { ...v.scope, limitRub: rub } }),
  run: (api, org, scope) => api.setLimit(org, scope, { kind: 'tenant', id: scope }, rub),
});

export const setRole = (id: string, role: MemberRole): Op => ({
  patch: (v) => patchRole(v, id, role),
  run: (api, org, scope) => api.setRole(org, scope, id, role),
});

export const moveTo = (id: string, teamId: string | null): Op => ({
  patch: (v) => patchMove(v, id, teamId),
  run: (api, org, scope) => api.moveToTeam(org, scope, id, teamId),
});

// No optimistic patch: the card waits for the hub and closes by itself when the person is gone.
export const removePerson = (id: string): Op => ({
  patch: (v) => v,
  run: (api, org, scope) => api.removeFromTenant(org, scope, id),
});

export const newTeam = (name: string, tenantId?: string): Op => ({
  patch: (v) => patchNewTeam(v, name),
  run: (api, org, scope) => api.createTeam(org, scope, name, tenantId),
});

export const renameTeam = (id: string, name: string): Op => ({
  patch: (v) => patchRename(v, id, name),
  run: (api, org, scope) => api.renameTeam(org, scope, id, name),
});

// No optimistic patch: the screen waits for the hub, then leaves the team's page.
export const deleteTeam = (id: string): Op => ({
  patch: (v) => v,
  run: (api, org, scope) => api.deleteTeam(org, scope, id),
});

export const machinesPerPerson = (teamId: string, n: number): Op => ({
  patch: (v) => patchMachines(v, teamId, n),
  run: (api, org, scope) => {
    if (!api.setMachinesPerPerson)
      return Promise.reject(
        new OrgApiError('invalid_request', 'Хаб пока не умеет ограничивать машины на человека.'),
      );
    return api.setMachinesPerPerson(org, scope, teamId, n);
  },
});
