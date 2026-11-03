// What a tenant's owner does with the tenant itself: adds a machine with a one-time code, invites people, changes the
// members and the name. These are the hub's space endpoints (docs/hub/api.md); a tenant is a space, its id is the space
// id. The calls are the old space screens' own (lib/api.ts), so both generations talk to the hub the same way.

/** How the agents of a machine are isolated from the person's own files. */
export type MachineProfile = 'dir' | 'container' | 'full';

/** The one-time code of a new machine and the command that uses it, exactly as the hub built them. */
export interface IssuedCode {
  code: string;
  expiresAt: string;
  profile: MachineProfile;
  command: string;
}

/** A machine as the add-machine dialog needs it: to see which one dialed in. */
export interface SpaceMachine {
  id: string;
  name: string;
  status: 'pending' | 'online' | 'offline';
}

export type TenantRole = 'owner' | 'member';

export interface TenantMember {
  userId: string;
  name: string;
  email: string;
  role: TenantRole;
}

export interface TenantInvite {
  id: string;
  kind: 'email' | 'link';
  email: string | null;
  /** Only in the answer that created it. */
  url: string | null;
  /** An email invite the hub could not send: the person passes the link by hand. */
  sent: boolean | null;
  expiresAt: string;
  uses: number;
  maxUses: number | null;
}

export interface TenantApi {
  /** Machines of the tenant, to wait for the one that dials in with a code. */
  spaceMachines(spaceId: string): Promise<SpaceMachine[]>;
  /** POST …/machines/codes. `full` needs the confirmation, a container may be off on this hub. */
  issueMachineCode(spaceId: string, profile: MachineProfile, confirmFull: boolean): Promise<IssuedCode>;
  tenantInvites(spaceId: string): Promise<TenantInvite[]>;
  /** Invites one address; the invitee joins as a member. */
  inviteByEmail(spaceId: string, email: string): Promise<TenantInvite>;
  inviteByLink(spaceId: string): Promise<TenantInvite>;
  revokeInvite(spaceId: string, inviteId: string): Promise<void>;
  tenantMembers(spaceId: string): Promise<TenantMember[]>;
  setTenantRole(spaceId: string, userId: string, role: TenantRole): Promise<void>;
  removeTenantMember(spaceId: string, userId: string): Promise<void>;
  /** Answers the name the hub saved. */
  renameTenant(spaceId: string, name: string): Promise<string>;
}
