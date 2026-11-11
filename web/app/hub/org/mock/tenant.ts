// Fixtures for the tenant's own actions: a machine code that is "used" a few seconds after it was shown, a few invites
// and members to change. State lives in the mock instance, so a dialog sees what the one before it did.
import { OrgApiError } from '../api/errors';
import type {
  IssuedCode,
  SpaceMachine,
  TenantApi,
  TenantInvite,
  TenantMember,
  TenantRole,
} from '../api/tenant';
import { later, type MockCtx } from './ctx';

/** Names a tenant was given in the mock; the shell reads them back. */
export const renamedTenants = new Map<string, string>();

const DAY = 24 * 3_600_000;
const ARRIVES_AFTER = 4_000;

const PEOPLE: TenantMember[] = [
  { userId: 'u-andrey', name: 'Андрей', email: 'andrey@acme.example', role: 'owner' },
  { userId: 'u-anya', name: 'Аня', email: 'anya@acme.example', role: 'member' },
  { userId: 'u-dima', name: 'Дима', email: 'dima@acme.example', role: 'member' },
  { userId: 'u-liza', name: 'Лиза', email: 'liza@acme.example', role: 'member' },
];

export const tenantMock = (_ctx: MockCtx): TenantApi => {
  void _ctx;
  const machines: SpaceMachine[] = [{ id: 'm-base', name: 'build-01', status: 'online' }];
  let issuedAt: number | null = null;
  let seq = 0;
  let invites: TenantInvite[] = [
    {
      id: 'inv-1',
      kind: 'email',
      email: 'new.person@acme.example',
      url: null,
      sent: true,
      expiresAt: new Date(Date.now() + 5 * DAY).toISOString(),
      uses: 0,
      maxUses: 1,
      role: 'member',
    },
  ];
  let members = PEOPLE.map((m) => ({ ...m }));

  const invite = (
    kind: 'email' | 'link',
    email: string | null,
    role: TenantRole = 'member',
  ): TenantInvite => {
    seq += 1;
    return {
      id: `inv-new-${seq}`,
      kind,
      email,
      url: `https://hub.acme.example/invite/mock-${seq}`,
      sent: kind === 'email' ? true : null,
      expiresAt: new Date(Date.now() + 7 * DAY).toISOString(),
      uses: 0,
      maxUses: kind === 'email' ? 1 : null,
      role,
    };
  };
  const listed = (i: TenantInvite): TenantInvite => ({ ...i, url: null });

  return {
    spaceMachines: async () => {
      if (
        issuedAt !== null &&
        Date.now() - issuedAt >= ARRIVES_AFTER &&
        !machines.some((m) => m.id === 'm-new')
      ) {
        machines.push({ id: 'm-new', name: 'dev-laptop', status: 'online' });
      }
      return later(
        machines.map((m) => ({ ...m })),
        120,
      );
    },
    issueMachineCode: async (_space, profile, confirmFull) => {
      if (profile === 'full' && !confirmFull)
        throw new OrgApiError('invalid_request', 'Нужно подтверждение.');
      issuedAt = Date.now();
      const code = 'MOCK-7QX4';
      const issued: IssuedCode = {
        code,
        profile,
        expiresAt: new Date(issuedAt + 15 * 60_000).toISOString(),
        command: `curl -fsSL https://hub.acme.example/install.sh | sh -s -- --code ${code} --profile ${profile}`,
      };
      return later(issued);
    },
    claimLocalMachine: async () => {
      const local: SpaceMachine = { id: 'm-local', name: 'Эта машина', status: 'online' };
      if (!machines.some((m) => m.id === local.id)) machines.push(local);
      return later({ ...local });
    },
    tenantInvites: async () => later(invites.map(listed)),
    inviteByEmail: async (_space, email, role) => {
      const made = invite('email', email, role);
      invites = [listed(made), ...invites];
      return later(made);
    },
    inviteByLink: async () => {
      const made = invite('link', null);
      invites = [listed(made), ...invites];
      return later(made);
    },
    revokeInvite: async (_space, id) => {
      invites = invites.filter((i) => i.id !== id);
      return later(undefined);
    },
    tenantMembers: async () => later(members.map((m) => ({ ...m }))),
    setTenantRole: async (_space, userId, role: TenantRole) => {
      const owners = members.filter((m) => m.role === 'owner');
      const target = members.find((m) => m.userId === userId);
      if (target?.role === 'owner' && role === 'member' && owners.length === 1) {
        throw new OrgApiError('hub', 'В тенанте должен остаться хотя бы один владелец.');
      }
      members = members.map((m) => (m.userId === userId ? { ...m, role } : m));
      return later(undefined);
    },
    removeTenantMember: async (_space, userId) => {
      const target = members.find((m) => m.userId === userId);
      if (target?.role === 'owner' && members.filter((m) => m.role === 'owner').length === 1) {
        throw new OrgApiError('hub', 'В тенанте должен остаться хотя бы один владелец.');
      }
      members = members.filter((m) => m.userId !== userId);
      return later(undefined);
    },
    planDeleteTenant: async () =>
      later({
        blockers: [
          { kind: 'machines', count: machines.length },
          { kind: 'members', count: members.length - 1 },
        ].filter((b) => b.count > 0),
        removes: [],
        canDelete: false,
      }),
    deleteTenant: async () => {
      throw new OrgApiError('hub', 'В тенанте остались машины и люди.');
    },
    renameTenant: async (space, name) => {
      const saved = name.trim();
      renamedTenants.set(space, saved);
      return later(saved);
    },
  };
};
