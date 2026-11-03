// The tenant's own actions on the hub's space endpoints. It reuses the old space screens' client (lib/api.ts) call for call,
// and turns its ApiError into the org layer's OrgApiError with the text the old screens already show.
import { ApiError, createApi, type Fetcher } from '../../../lib/api';
import { errorText } from '../../../lib/errors';
import type { Invite } from '../../../lib/types';
import { OrgApiError } from '../errors';
import type { TenantApi, TenantInvite } from '../tenant';

const CONTAINER_OFF = 'Контейнеры на этом хабе не настроены. Выберите другой доступ.';

const wrap = async <T>(run: () => Promise<T>, text?: (e: ApiError) => string | null): Promise<T> => {
  try {
    return await run();
  } catch (e) {
    if (!(e instanceof ApiError)) throw e;
    const own = text?.(e);
    throw new OrgApiError(e.code === 'network' ? 'network' : 'hub', own ?? errorText(e));
  }
};

const inviteOf = (i: Invite): TenantInvite => ({
  id: i.id,
  kind: i.kind,
  email: i.email,
  url: i.url ?? null,
  sent: i.sent ?? null,
  expiresAt: i.expiresAt,
  uses: i.uses,
  maxUses: i.maxUses,
});

export const liveTenant = (fetcher?: Fetcher): TenantApi => {
  const api = createApi(fetcher);
  return {
    spaceMachines: (spaceId) =>
      wrap(async () =>
        (await api.machines.list(spaceId)).machines.map((m) => ({
          id: m.id,
          name: m.name,
          status: m.status,
        })),
      ),
    issueMachineCode: (spaceId, profile, confirmFull) =>
      wrap(
        () => api.machines.issueCode(spaceId, profile, confirmFull),
        (e) => (e.code === 'provider_off' && profile === 'container' ? CONTAINER_OFF : null),
      ),
    tenantInvites: (spaceId) => wrap(async () => (await api.spaces.invites(spaceId)).invites.map(inviteOf)),
    inviteByEmail: (spaceId, email) =>
      wrap(async () => {
        const { invites } = await api.spaces.invite(spaceId, [email]);
        const first = invites[0];
        if (!first) throw new OrgApiError('invalid_request', 'Проверьте адрес почты.');
        return inviteOf(first);
      }),
    inviteByLink: (spaceId) =>
      wrap(async () => inviteOf((await api.spaces.inviteLink(spaceId, { days: 7 })).invite)),
    revokeInvite: (spaceId, inviteId) => wrap(() => api.spaces.revokeInvite(spaceId, inviteId)),
    tenantMembers: (spaceId) =>
      wrap(async () =>
        (await api.spaces.members(spaceId)).members.map((m) => ({
          userId: m.userId,
          name: m.name,
          email: m.email,
          role: m.role,
        })),
      ),
    setTenantRole: (spaceId, userId, role) =>
      wrap(async () => {
        await api.spaces.setRole(spaceId, userId, role);
      }),
    removeTenantMember: (spaceId, userId) => wrap(() => api.spaces.removeMember(spaceId, userId)),
    renameTenant: (spaceId, name) =>
      wrap(async () => (await api.spaces.update(spaceId, { name })).space.name),
  };
};
