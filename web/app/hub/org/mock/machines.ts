// Fixtures for the machines screen: the tenant «Продукт» (the board's numbers) and a few others for the organization
// list. State lives in this module's closure, so an action changes what the next read shows.
import type {
  AccessMode,
  MachineAccess,
  MachineDetail,
  MachineGroup,
  MachinesApi,
  MachinesList,
  MachinesSummary,
  MovePlan,
  OrgMachine,
  PersonRef,
  ResourceQuote,
  RestartWhat,
  RestartWhen,
  ResourcePlan,
} from '../api/machines';
import { OrgApiError } from '../api/errors';
import { later, type MockCtx } from './ctx';
import { VIEWERS } from './shell';

const HOUR = 3_600_000;

const PEOPLE: PersonRef[] = ['Аня', 'Дима', 'Паша', 'Лена', 'Андрей', 'Маша', 'Олег', 'Лиза'].map(
  (name, i) => ({
    id: `p${i + 1}`,
    name,
  }),
);
const person = (name: string): PersonRef => PEOPLE.find((p) => p.name === name) ?? { id: name, name };

const PLAN: ResourcePlan = {
  cores: [4, 8, 16, 32, 64],
  ramGb: [16, 32, 64, 128, 256],
  diskGb: [250, 500, 1000, 2000],
  price: { core: 450, ramGb: 120, diskGb: 8 },
  usedRamGb: 91,
  usedDiskGb: 455,
};

interface Seed {
  id: string;
  name: string;
  owner?: string;
  purpose?: string;
  tenant: [string, string, string]; // id, slug, name
  team?: [string, string];
  state?: OrgMachine['state'];
  sinceH?: number;
  chats?: number;
  load?: [number, number, number];
  hw?: [number, number, number, string];
  cloud?: boolean;
  runner?: [string, boolean];
  week?: number;
  access?: MachineAccess;
}

const PRODUCT: Seed['tenant'] = ['t-product', 'product', 'Продукт'];
const FRONT: Seed['team'] = ['tm-front', 'Фронт'];
const BACK: Seed['team'] = ['tm-back', 'Бэкенд'];

const SEEDS: Seed[] = [
  {
    id: 'm1',
    name: 'anya-01',
    owner: 'Аня',
    tenant: PRODUCT,
    team: FRONT,
    chats: 2,
    load: [34, 52, 48],
    hw: [8, 32, 500, 'Аня'],
    runner: ['0.16.30', false],
    week: 78,
  },
  {
    id: 'm2',
    name: 'andrey-01',
    owner: 'Андрей',
    tenant: PRODUCT,
    team: FRONT,
    chats: 3,
    load: [41, 60, 55],
    hw: [12, 36, 1000, 'M3 Pro'],
    cloud: false,
    runner: ['0.16.30', false],
    week: 64,
  },
  {
    id: 'm3',
    name: 'pasha-01',
    owner: 'Паша',
    tenant: PRODUCT,
    team: FRONT,
    chats: 1,
    load: [22, 38, 40],
    hw: [8, 32, 500, 'Паша'],
    runner: ['0.16.30', false],
    week: 79,
  },
  {
    id: 'm4',
    name: 'dima-01',
    owner: 'Дима',
    tenant: PRODUCT,
    team: BACK,
    chats: 2,
    load: [47, 58, 62],
    hw: [16, 64, 500, 'Дима'],
    runner: ['0.16.30', false],
    week: 18,
  },
  {
    id: 'm5',
    name: 'andrey-gpu',
    owner: 'Андрей',
    tenant: PRODUCT,
    team: BACK,
    state: 'offline',
    sinceH: 5,
    hw: [8, 32, 250, 'Ubuntu 22.04'],
    runner: ['0.16.28', true],
    week: 14,
  },
  {
    id: 'm6',
    name: 'migrate-01',
    purpose: 'миграции',
    tenant: PRODUCT,
    chats: 0,
    load: [9, 31, 33],
    hw: [8, 32, 250, 'Ubuntu 24.04'],
    runner: ['0.16.30', false],
    week: 9,
  },
  {
    id: 'm7',
    name: 'build-01',
    purpose: 'сборки и e2e',
    tenant: PRODUCT,
    chats: 2,
    load: [88, 71, 91],
    hw: [32, 128, 500, 'Ubuntu 24.04'],
    runner: ['0.16.30', false],
    week: 54,
    access: { mode: 'pick', people: ['Аня', 'Дима', 'Паша', 'Лена'].map(person) },
  },
  {
    id: 'm8',
    name: 'pasha-05',
    owner: 'Паша',
    tenant: PRODUCT,
    team: BACK,
    state: 'idle',
    sinceH: 9 * 24,
    chats: 0,
    load: [1, 8, 50],
    hw: [8, 32, 500, 'Паша'],
    runner: ['0.16.28', true],
    week: 9,
  },
  {
    id: 'm9',
    name: 'infra-01',
    purpose: 'кластер',
    tenant: ['t-platform', 'platform', 'Платформа'],
    chats: 4,
    load: [52, 61, 44],
    hw: [32, 128, 1000, 'Ubuntu 24.04'],
    runner: ['0.16.30', false],
    week: 112,
  },
  {
    id: 'm10',
    name: 'infra-02',
    purpose: 'кластер',
    tenant: ['t-platform', 'platform', 'Платформа'],
    state: 'offline',
    sinceH: 2,
    hw: [32, 128, 1000, 'Ubuntu 24.04'],
    runner: ['0.16.30', false],
    week: 96,
  },
  {
    id: 'm11',
    name: 'mobile-01',
    owner: 'Олег',
    tenant: ['t-clients', 'clients', 'Клиенты'],
    chats: 1,
    load: [30, 45, 38],
    hw: [8, 32, 500, 'Олег'],
    runner: ['0.16.30', false],
    week: 41,
  },
  {
    id: 'm12',
    name: 'anya-bl',
    owner: 'Аня',
    tenant: ['t-billing', 'billing', 'Биллинг'],
    state: 'offline',
    sinceH: 2,
    hw: [8, 32, 500, 'Аня'],
    runner: ['0.16.30', false],
    week: 12,
  },
  {
    id: 'm13',
    name: 'lena-bl',
    owner: 'Лена',
    tenant: ['t-billing', 'billing', 'Биллинг'],
    chats: 2,
    load: [38, 49, 41],
    hw: [16, 64, 500, 'Лена'],
    runner: ['0.16.30', false],
    week: 28,
  },
  {
    id: 'm14',
    name: 'scan-01',
    purpose: 'сканер',
    tenant: ['t-scanner', 'scanner', 'Сканер'],
    state: 'idle',
    sinceH: 12 * 24,
    chats: 0,
    load: [1, 12, 30],
    hw: [8, 32, 250, 'Ubuntu 24.04'],
    runner: ['0.16.30', false],
    week: 5,
  },
];

const SUBS: Record<string, string> = {
  'tm-front': 'лид Аня',
  'tm-back': 'лид Дима',
  none: 'машины тенанта без команды',
  platform: 'админ Вика',
  clients: 'админ Олег',
  billing: 'админ Аня',
  scanner: 'админ Паша',
  product: 'админ Андрей',
};

/** A smooth deterministic curve around `base`, so the same machine always draws the same picture. */
const curve = (seed: number, base: number): number[] =>
  Array.from({ length: 24 }, (_, i) => {
    if (i === 23) return base; // the last point is the current reading, as in the list
    const v = base + Math.sin(i * 0.9 + seed) * 6 + Math.cos(i * 0.37 + seed * 2) * 4;
    return Math.max(1, Math.min(100, Math.round(v)));
  });

const COUNTS_PER_USD = 0.55;

const build = (s: Seed, role: MockCtx['role'], now: number): OrgMachine => {
  const state = s.state ?? 'online';
  const reach = state !== 'offline';
  const ownTeam = s.team?.[0] === VIEWERS.lead.teamId;
  const [cores, ramGb, diskGb, note] = s.hw ?? [8, 32, 500, ''];
  return {
    id: s.id,
    name: s.name,
    ownerName: s.owner ?? null,
    purpose: s.purpose ?? null,
    kind: s.owner ? 'personal' : 'shared',
    tenantId: s.tenant[0],
    tenantSlug: s.tenant[1],
    tenantName: s.tenant[2],
    teamId: s.team?.[0] ?? null,
    teamName: s.team?.[1] ?? null,
    state,
    since: s.sinceH ? new Date(now - s.sinceH * HOUR).toISOString() : null,
    disabledBy: null,
    disabledAt: null,
    busy: null,
    chats: reach ? (s.chats ?? 0) : null,
    cpu: reach ? (s.load?.[0] ?? 5) : null,
    ram: reach ? (s.load?.[1] ?? 20) : null,
    disk: reach ? (s.load?.[2] ?? 30) : null,
    hw: { cores, ramGb, diskGb, note: s.cloud === false ? note : '' },
    cloud: s.cloud ?? true,
    runnerVersion: s.runner?.[0] ?? '0.16.30',
    runnerOutdated: s.runner?.[1] ?? false,
    weekCostUsd: role === 'member' ? null : (s.week ?? 0),
    monthPriceRub: (s.cloud ?? true) ? 450 * cores + 120 * ramGb + 8 * diskGb : null,
    mine: s.owner !== undefined && s.owner === VIEWERS[role].name,
    weekChats: role === 'member' ? null : Math.round((s.week ?? 0) * COUNTS_PER_USD),
    os: s.cloud === false ? 'macOS 15' : s.hw?.[3] && s.hw[3].startsWith('Ubuntu') ? s.hw[3] : 'Ubuntu 24.04',
    rent:
      (s.cloud ?? true)
        ? {
            provider: 'Selectel',
            plan: `${cores} vCPU · ${ramGb} ГБ`,
            price: { amount: (450 * cores + 120 * ramGb + 8 * diskGb) * 100, currency: 'RUB' },
          }
        : null,
    managed: { budget: true, env: true, resources: true, access: true },
    canManage: role === 'admin' || (role === 'lead' && ownTeam),
    canAdmin: role === 'admin',
    needs: role === 'admin' ? null : 'tenant_admin',
  };
};

const rank = (m: OrgMachine) =>
  m.state === 'offline' ? 0 : m.state === 'idle' ? 1 : m.state === 'disabled' ? 2 : 3;

const summarize = (ms: OrgMachine[]): MachinesSummary => {
  const reach = ms.filter((m) => m.cpu !== null);
  const hottest = reach.reduce<OrgMachine | null>((a, m) => ((m.cpu ?? 0) > (a?.cpu ?? -1) ? m : a), null);
  const diskHot = reach.reduce<OrgMachine | null>((a, m) => ((m.disk ?? 0) > (a?.disk ?? -1) ? m : a), null);
  const chatsNow = ms.reduce((n, m) => n + (m.chats ?? 0), 0);
  return {
    total: ms.length,
    online: ms.filter((m) => m.state === 'online').length,
    idleWeek: ms.filter((m) => m.state === 'idle').length,
    offline: ms.filter((m) => m.state === 'offline').length,
    chatsNow,
    chatsToday: chatsNow * 7 + 4,
    cpuMean: reach.length ? Math.round(reach.reduce((n, m) => n + (m.cpu ?? 0), 0) / reach.length) : null,
    hottest: hottest && hottest.cpu !== null ? { name: hottest.name, pct: hottest.cpu } : null,
    diskHot: diskHot && diskHot.disk !== null ? { name: diskHot.name, pct: diskHot.disk } : null,
    outdatedRunners: ms.filter((m) => m.runnerOutdated).length,
    tenantsOffline: new Set(ms.filter((m) => m.state === 'offline').map((m) => m.tenantId)).size,
    onlineSeries: [11, 11, 12, 12, 12, 11, 12, 12, 12, 12, 13, 12, 12, ms.length].map((n) => n),
    chatsSeries: [0, 1, 1, 0, 2, 4, 6, 9, 7, 8, 11, 9, 10, chatsNow],
    loadSeries: curve(3, 40).slice(0, 14),
  };
};

export const machinesMock = (ctx: MockCtx): MachinesApi => {
  const now = Date.now();
  const store = new Map<string, OrgMachine>(SEEDS.map((s) => [s.id, build(s, ctx.role, now)]));
  const access = new Map<string, MachineAccess>(
    SEEDS.map((s) => [s.id, s.access ?? { mode: 'owner', people: s.owner ? [person(s.owner)] : [] }]),
  );
  const timers = new Map<string, ReturnType<typeof setTimeout>>();

  const get = (id: string): OrgMachine => {
    const m = store.get(id);
    if (!m) throw new OrgApiError('not_found', 'machine not found');
    return m;
  };
  const put = (m: OrgMachine) => store.set(m.id, m);
  const movePlan = (m: OrgMachine, tenantId: string): MovePlan => {
    const revoked = (access.get(m.id)?.people ?? []).map((p) => p.name);
    return {
      from: { id: 'current', name: 'Текущий тенант' },
      to: { id: tenantId, name: tenantId },
      noop: false,
      teamCleared: null,
      teamAssigned: null,
      accessRevoked: revoked,
      limits: { fromRules: 0, toRules: 1 },
      kept: ['id машины', 'ключ', 'история'],
    };
  };
  const detail = (m: OrgMachine): MachineDetail => ({
    machine: m,
    series:
      m.cpu === null
        ? { cpu: [], ram: [], disk: [] }
        : {
            cpu: curve(m.id.length + m.name.length, m.cpu),
            ram: curve(m.name.length * 2, m.ram ?? 20),
            disk: curve(m.name.length * 3, m.disk ?? 30),
          },
    plan: m.cloud
      ? {
          ...PLAN,
          usedRamGb: Math.round(((m.ram ?? 20) / 100) * m.hw.ramGb),
          usedDiskGb: Math.round(((m.disk ?? 30) / 100) * m.hw.diskGb),
        }
      : null,
    access: access.get(m.id) ?? { mode: 'owner', people: [] },
    candidates: PEOPLE,
    teamLabel: m.teamName,
    consoleUrl: m.cloud ? 'https://console.example.invalid/servers' : null,
    audience: { team: m.teamId ? 9 : null, tenant: 28 },
  });
  const need = (m: OrgMachine, admin: boolean) => {
    if (admin ? !m.canAdmin : !m.canManage) throw new OrgApiError('forbidden', 'forbidden');
  };
  /** The action takes a while on a real hub; here too, and the card shows it (state `busy`). */
  const work = async (
    m: OrgMachine,
    kind: NonNullable<OrgMachine['busy']>['kind'],
    ms: number,
    done: (m: OrgMachine) => OrgMachine,
  ) => {
    put({ ...m, busy: { kind, chats: m.chats ?? 0, opId: `op-${m.id}`, cancellable: kind === 'drain' } });
    await later(null, ms);
    const cur = get(m.id);
    put({ ...done(cur), busy: null });
    return detail(get(m.id));
  };

  return {
    machinesList: async (_org, { scope }) => {
      // a lead sees the whole tenant (acts only on the own team: `canManage`), a member only their own
      const all = [...store.values()].filter((m) => ctx.role !== 'member' || m.mine === true);
      const inScope = scope === 'all' ? all : all.filter((m) => m.tenantSlug === scope);
      const byKey = new Map<string, OrgMachine[]>();
      for (const m of inScope) {
        const key = scope === 'all' ? m.tenantSlug : (m.teamId ?? 'none');
        byKey.set(key, [...(byKey.get(key) ?? []), m]);
      }
      const groups: MachineGroup[] = [...byKey.entries()].map(([key, ms]) => {
        const sorted = [...ms].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
        const first = sorted[0];
        const name = scope === 'all' ? (first?.tenantName ?? key) : (first?.teamName ?? 'Общие');
        return {
          key,
          name,
          sub: SUBS[key] ?? '',
          tenantSlug: scope === 'all' ? key : null,
          pinned: scope === 'all' && key === 'platform',
          total: ms.length,
          offline: ms.filter((m) => m.state === 'offline').length,
          idle: ms.filter((m) => m.state === 'idle').length,
          online: ms.filter((m) => m.state === 'online').length,
          weekCostUsd: ctx.role === 'member' ? null : ms.reduce((n, m) => n + (m.weekCostUsd ?? 0), 0),
          machines: sorted.slice(0, 5),
        };
      });
      // The organization lists the tenants where something is wrong first; a tenant keeps its teams in their order.
      if (scope === 'all')
        groups.sort((a, b) => b.offline - a.offline || b.idle - a.idle || a.name.localeCompare(b.name));
      const tenants =
        scope === 'all'
          ? [...new Map(all.map((m) => [m.tenantSlug, m.tenantName])).entries()].map(([slug, name]) => ({
              slug,
              name,
            }))
          : [];
      return later<MachinesList>({
        summary: summarize(inScope),
        groups,
        tenants,
        mlCoreUrl: 'https://ml-core.example.invalid/machines',
      });
    },
    machineDetail: async (id) => later(detail(get(id))),
    resizeMachine: async (id, to) => {
      const m = get(id);
      need(m, true);
      return work(m, 'resize', 2600, (c) => ({
        ...c,
        hw: { ...c.hw, cores: to.cores, ramGb: to.ramGb, diskGb: to.diskGb },
        monthPriceRub: 450 * to.cores + 120 * to.ramGb + 8 * to.diskGb,
      }));
    },
    quoteResources: async (id, to): Promise<ResourceQuote> => {
      const m = get(id);
      const price = (c: number, r: number, d: number) => ({
        amount: (450 * c + 120 * r + 8 * d) * 100,
        currency: 'RUB',
      });
      const next = price(to.cores, to.ramGb, to.diskGb);
      const current = price(m.hw.cores, m.hw.ramGb, m.hw.diskGb);
      return later(
        { price: next, current, delta: { amount: next.amount - current.amount, currency: 'RUB' } },
        160,
      );
    },
    renameMachine: async (id, name) => {
      const m = get(id);
      need(m, true);
      const next = name.trim();
      if (next === '' || next.length > 60)
        throw new OrgApiError('invalid_request', 'Название — от 1 до 60 символов');
      put({ ...m, name: next });
      return later(detail(get(id)), 400);
    },
    planMoveMachine: async (id, tenantId) => later(movePlan(get(id), tenantId), 250),
    moveMachine: async (id, tenantId) => {
      const m = get(id);
      need(m, true);
      const plan = movePlan(m, tenantId);
      await later(null, 500);
      store.delete(id);
      return plan;
    },
    removeMachine: async (id) => {
      need(get(id), true);
      await later(null, 700);
      store.delete(id);
    },
    trustMachine: async () => later(undefined, 200),
    openMachine: async (id) => later({ url: `#machine-${get(id).id}` }, 200),
    restartMachine: async (id, what: RestartWhat, when: RestartWhen) => {
      const m = get(id);
      need(m, false);
      if (when === 'after_chats' && (m.chats ?? 0) > 0) {
        put({ ...m, busy: { kind: 'drain', chats: m.chats ?? 0, opId: `op-${id}`, cancellable: true } });
        timers.set(
          id,
          setTimeout(() => put({ ...get(id), busy: null, chats: 0 }), 9000),
        );
        return later(detail(get(id)));
      }
      return work(m, what === 'runner' ? 'restart' : 'reboot', what === 'runner' ? 2200 : 3200, (c) => c);
    },
    cancelRestart: async (id) => {
      const m = get(id);
      need(m, false);
      clearTimeout(timers.get(id));
      put({ ...m, busy: null });
      return later(detail(get(id)));
    },
    disableMachine: async (id) => {
      const m = get(id);
      need(m, true);
      return work(m, 'off', 1600, (c) => ({
        ...c,
        state: 'disabled',
        chats: 0,
        disabledBy: VIEWERS[ctx.role].name,
        disabledAt: new Date().toISOString(),
      }));
    },
    enableMachine: async (id) => {
      const m = get(id);
      need(m, true);
      return work(m, 'on', 2000, (c) => ({ ...c, state: 'online', disabledBy: null, disabledAt: null }));
    },
    setMachineAccess: async (id, next: { mode: AccessMode; personIds: string[] }) => {
      const m = get(id);
      need(m, false);
      access.set(id, {
        mode: next.mode,
        people: next.personIds
          .map((pid) => PEOPLE.find((p) => p.id === pid))
          .filter((p): p is PersonRef => p !== undefined),
      });
      return later(detail(m), 500);
    },
  };
};
