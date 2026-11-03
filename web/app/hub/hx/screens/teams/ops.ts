// The actions of the team screens as optimistic operations: what the screen shows at once and the call to the hub.
import type { MemberRole } from '../../../org/api/teams';
import {
  patchMachines,
  patchMove,
  patchNewTeam,
  patchPersonLimit,
  patchRemove,
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

export const setRole = (id: string, role: MemberRole): Op => ({
  patch: (v) => patchRole(v, id, role),
  run: (api, org, scope) => api.setRole(org, scope, id, role),
});

export const moveTo = (id: string, teamId: string | null): Op => ({
  patch: (v) => patchMove(v, id, teamId),
  run: (api, org, scope) => api.moveToTeam(org, scope, id, teamId),
});

export const removePerson = (id: string): Op => ({
  patch: (v) => patchRemove(v, id),
  run: (api, org, scope) => api.removeFromTenant(org, scope, id),
});

export const newTeam = (name: string): Op => ({
  patch: (v) => patchNewTeam(v, name),
  run: (api, org, scope) => api.createTeam(org, scope, name),
});

export const machinesPerPerson = (teamId: string, n: number): Op => ({
  patch: (v) => patchMachines(v, teamId, n),
  run: (api, org, scope) => {
    if (!api.setMachinesPerPerson) return Promise.reject(new Error('machines per person: not supported'));
    return api.setMachinesPerPerson(org, scope, teamId, n);
  },
});
