// Machines of a tenant or of the whole organization: the list, a machine's card and the actions on it. The paths of
// the live calls are `GET …/machines`, `PATCH /api/machines/{id}` (team, access), `POST …/restart`, `POST …/disable`,
// `POST …/enable` (hub-architecture §2.11). Money is USD per week for usage and RUB per month for a server.

export type MachineState = 'online' | 'idle' | 'offline' | 'disabled';

/** What a machine is doing right now because someone asked it to (an action in flight, on the hub's side). */
export type MachineBusyKind = 'resize' | 'restart' | 'reboot' | 'drain' | 'off' | 'on';

export interface PersonRef {
  id: string;
  name: string;
}

export type AccessMode = 'owner' | 'team' | 'tenant' | 'pick';

export interface MachineAccess {
  mode: AccessMode;
  /** For `pick`: who may start chats. */
  people: PersonRef[];
}

export interface OrgMachine {
  id: string;
  name: string;
  /** Whose it is; null for a shared one. */
  ownerName: string | null;
  /** What it is for («сборки и e2e»); null when nobody wrote it. */
  purpose: string | null;
  kind: 'personal' | 'shared';
  tenantId: string;
  tenantSlug: string;
  tenantName: string;
  teamId: string | null;
  teamName: string | null;
  state: MachineState;
  /** When it went offline or idle (RFC 3339); null while online. */
  since: string | null;
  /** The machine is switched off in the hub (disabled_at) — also `state: 'disabled'`, who and when for the hint. */
  disabledBy: string | null;
  disabledAt: string | null;
  busy: { kind: MachineBusyKind; chats?: number } | null;
  /** Chats running now; null while it is not reachable. */
  chats: number | null;
  /** 0..100; null while it is not reachable. */
  cpu: number | null;
  ram: number | null;
  disk: number | null;
  hw: { cores: number; ramGb: number; diskGb: number; note: string };
  /** A server the hub can resize and reboot (a rented VM); a laptop is changed on the laptop itself. */
  cloud: boolean;
  runnerVersion: string;
  runnerOutdated: boolean;
  /** USD for the last 7 days; null when the money is not visible to this role. */
  weekCostUsd: number | null;
  /** The rent per month in RUB for a cloud machine. */
  monthPriceRub: number | null;
  /** The viewer may change who uses it and restart it (a team lead and above). */
  canManage: boolean;
  /** The viewer may resize and disable it (a tenant's admin and above); `needs` is the hint when not. */
  canAdmin: boolean;
  needs: 'org_admin' | 'tenant_admin' | 'team_lead' | null;
}

export interface MachineGroup {
  key: string;
  name: string;
  /** «лид Аня», «админ Паша», «машины тенанта без команды». */
  sub: string;
  /** Where «Все N машин» leads (a tenant slug), when the group is a tenant of the organization. */
  tenantSlug: string | null;
  total: number;
  offline: number;
  idle: number;
  online: number;
  weekCostUsd: number | null;
  /** The machines loaded for the group, the ones with problems first. */
  machines: OrgMachine[];
}

export interface MachinesSummary {
  total: number;
  online: number;
  idleWeek: number;
  offline: number;
  chatsNow: number;
  /** Null: the hub does not count them. */
  chatsToday: number | null;
  /** Mean CPU of reachable machines, 0..100, and the hottest one. */
  cpuMean: number | null;
  hottest: { name: string; pct: number } | null;
  diskHot: { name: string; pct: number } | null;
  outdatedRunners: number;
  tenantsOffline: number;
  /** A short history of the share online and of chats, for the tiles' bars. */
  onlineSeries: number[];
  chatsSeries: number[];
  loadSeries: number[];
}

export interface MachinesList {
  summary: MachinesSummary;
  groups: MachineGroup[];
  /** The tenants for the filter of the organization's list (empty in a tenant). */
  tenants: { slug: string; name: string }[];
  /** Where the charts and logs of these machines are (ML Core); null when it is not connected. */
  mlCoreUrl: string | null;
}

export interface ResourcePlan {
  cores: number[];
  ramGb: number[];
  diskGb: number[];
  /** RUB per month. */
  price: { core: number; ramGb: number; diskGb: number };
  /** What is used now: the memory cannot go below it, the disk only grows. */
  usedRamGb: number;
  usedDiskGb: number;
}

export interface MachineDetail {
  machine: OrgMachine;
  /** The last 24 hours, newest last, 0..100. */
  series: { cpu: number[]; ram: number[]; disk: number[] };
  plan: ResourcePlan | null;
  access: MachineAccess;
  /** Who can be added to the access list. */
  candidates: PersonRef[];
  teamLabel: string | null;
  /** People who would get access with «команда» and «все в тенанте». */
  audience: { team: number | null; tenant: number };
}

export interface MachineScopeArg {
  /** A tenant slug, or `all` for the organization. */
  scope: string;
}

export type RestartWhat = 'runner' | 'machine';
export type RestartWhen = 'now' | 'after_chats';

export interface MachinesApi {
  machinesList(org: string, arg: MachineScopeArg): Promise<MachinesList>;
  machineDetail(id: string): Promise<MachineDetail>;
  resizeMachine(id: string, to: { cores: number; ramGb: number; diskGb: number }): Promise<MachineDetail>;
  restartMachine(id: string, what: RestartWhat, when: RestartWhen): Promise<MachineDetail>;
  /** Lets chats finish no longer: cancels a restart that waits for them. */
  cancelRestart(id: string): Promise<MachineDetail>;
  disableMachine(id: string): Promise<MachineDetail>;
  enableMachine(id: string): Promise<MachineDetail>;
  setMachineAccess(id: string, access: { mode: AccessMode; personIds: string[] }): Promise<MachineDetail>;
}
