import { describe, expect, it } from 'vitest';

import type { MachineGroup, MachinesList, MachinesSummary, OrgMachine } from '../../../org/api/machines';
import {
  applyOverlay,
  canOpen,
  cleanName,
  countByServers,
  cardSub,
  chatsText,
  deltaOf,
  filterLabel,
  filterOptions,
  filterTenants,
  flatRows,
  foldGroups,
  groupLimit,
  groupSt,
  groupWeek,
  hwLine,
  liveLoad,
  machineHit,
  offItems,
  orgTiles,
  ramFloor,
  restartHint,
  rowLimit,
  rowSt,
  rowStLabel,
  rowSub,
  rubOf,
  searchGroups,
  serverRows,
  statusOf,
  sizeText,
  snapStop,
  stepStop,
  stopPos,
  stopsOf,
  tenantLead,
  tenantTiles,
  toggleTenant,
  verText,
  weekText,
  withValue,
} from './calc';

const NOW = Date.parse('2026-11-02T12:00:00Z');
const hoursAgo = (h: number) => new Date(NOW - h * 3_600_000).toISOString();

const machine = (over: Partial<OrgMachine> = {}): OrgMachine => ({
  id: 'm1',
  name: 'dev-01',
  ownerName: 'Аня',
  purpose: null,
  kind: 'personal',
  tenantId: 't1',
  tenantSlug: 'product',
  tenantName: 'Продукт',
  teamId: 'tm1',
  teamName: 'Фронт',
  state: 'online',
  since: null,
  disabledBy: null,
  disabledAt: null,
  busy: null,
  chats: 2,
  cpu: 40,
  ram: 50,
  disk: 60,
  hw: { cores: 16, ramGb: 64, diskGb: 500, note: '' },
  cloud: true,
  runnerVersion: '1.2.0',
  runnerOutdated: false,
  weekCostUsd: 86,
  monthPriceRub: null,
  canManage: true,
  canAdmin: true,
  needs: null,
  weekChats: 41,
  ...over,
});

const group = (over: Partial<MachineGroup> = {}, machines: OrgMachine[] = [machine()]): MachineGroup => ({
  key: 'g1',
  name: 'Фронт',
  sub: 'лид Аня',
  tenantSlug: null,
  total: machines.length,
  offline: 0,
  idle: 0,
  online: machines.length,
  weekCostUsd: 100,
  machines,
  ...over,
});

const summary = (over: Partial<MachinesSummary> = {}): MachinesSummary => ({
  total: 8,
  online: 6,
  idleWeek: 1,
  offline: 1,
  chatsNow: 9,
  chatsToday: 64,
  cpuMean: 41,
  hottest: { name: 'build-01', pct: 88 },
  diskHot: { name: 'build-01', pct: 91 },
  outdatedRunners: 2,
  tenantsOffline: 1,
  onlineSeries: [6, 6, 5],
  chatsSeries: [1, 2, 3],
  loadSeries: [30, 40, 50],
  ...over,
});

describe('row states and texts', () => {
  it('reads a disabled machine as not reachable but says so', () => {
    const m = machine({ state: 'disabled' });
    expect(rowSt(m)).toBe('off');
    expect(rowStLabel(m, NOW)).toBe('отключена');
  });

  it('labels idle and offline with the time since', () => {
    expect(rowStLabel(machine({ state: 'idle', since: hoursAgo(24 * 9) }), NOW)).toBe('простаивает 9 дн');
    expect(rowStLabel(machine({ state: 'offline', since: hoursAgo(5) }), NOW)).toBe('не на связи 5 ч');
    expect(rowStLabel(machine(), NOW)).toBe('на связи');
  });

  it('writes the hardware line and leaves out what is unknown', () => {
    expect(hwLine(machine())).toBe('16 ядер · 64 ГБ');
    expect(hwLine(machine({ hw: { cores: 0, ramGb: 36, diskGb: 0, note: 'M3 Pro' } }))).toBe(
      'M3 Pro · 36 ГБ',
    );
    expect(hwLine(machine({ hw: { cores: 1, ramGb: 0, diskGb: 0, note: '' } }))).toBe('1 ядро');
  });

  it('builds the subtitle per view', () => {
    const shared = machine({ kind: 'shared', ownerName: null, purpose: 'сборки и e2e' });
    expect(rowSub(machine(), 'team')).toBe('Аня · 16 ядер · 64 ГБ');
    expect(rowSub(shared, 'team')).toBe('общая · сборки и e2e · 16 ядер · 64 ГБ');
    expect(rowSub(shared, 'group')).toBe('сборки и e2e · 16 ядер · 64 ГБ');
    expect(rowSub(machine(), 'flat-org')).toBe('Продукт · Аня');
    expect(rowSub(machine({ kind: 'shared', ownerName: null, purpose: null }), 'flat-tenant')).toBe(
      'общая · 16 ядер · 64 ГБ',
    );
  });

  it('shows money and chats of the week, a dash when nothing ran or it is hidden', () => {
    expect(weekText(machine())).toBe('$86 · 41 чат');
    expect(weekText(machine({ weekChats: 12 }))).toBe('$86 · 12 чатов');
    expect(weekText(machine({ weekChats: 1 }))).toBe('$86 · 1 чат');
    expect(weekText(machine({ weekChats: 0 }))).toBe('—');
    expect(weekText(machine({ weekCostUsd: null }))).toBe('—');
    expect(weekText(machine({ weekChats: null }))).toBe('$86');
  });

  it('marks an outdated runner', () => {
    expect(verText(machine())).toBe('1.2.0');
    expect(verText(machine({ runnerOutdated: true }))).toBe('1.2.0 · старый');
    expect(verText(machine({ runnerVersion: '' }))).toBe('—');
  });

  it('keeps load and chats only for a machine that answers', () => {
    const off = machine({ state: 'offline', chats: null, cpu: 3 });
    expect(liveLoad(off, 3)).toBeNull();
    expect(liveLoad(machine(), 40)).toBe(40);
    expect(chatsText(off)).toBe('—');
    expect(chatsText(machine({ state: 'idle' }))).toBe('—');
    expect(chatsText(machine({ chats: 0 }))).toBe('0');
  });

  it('writes the money of a group', () => {
    expect(groupWeek(group({ weekCostUsd: 1221 }))).toBe('$1 221');
    expect(groupWeek(group({ weekCostUsd: null }))).toBeNull();
  });
});

describe('search and filters', () => {
  const a = machine({ id: 'a', name: 'anya-01', ownerName: 'Аня' });
  const b = machine({
    id: 'b',
    name: 'build-01',
    kind: 'shared',
    ownerName: null,
    purpose: 'сборки',
    state: 'offline',
  });
  const c = machine({ id: 'c', name: 'pasha-05', ownerName: 'Паша', state: 'idle' });
  const g1 = group({ key: 'g1', name: 'Фронт', sub: 'лид Аня' }, [a, b]);
  const g2 = group({ key: 'g2', name: 'Бэкенд', sub: 'лид Дима', offline: 1 }, [c]);

  it('matches a machine by name, person, team and tenant', () => {
    expect(machineHit(a, 'ANY')).toBe(true);
    expect(machineHit(a, 'аня')).toBe(true);
    expect(machineHit(a, 'фронт')).toBe(true);
    expect(machineHit(a, 'продукт')).toBe(true);
    expect(machineHit(a, 'zzz')).toBe(false);
    expect(machineHit(a, '  ')).toBe(true);
  });

  it('keeps a whole group when its own name matches, otherwise the matching rows', () => {
    expect(searchGroups([g1, g2], '')).toHaveLength(2);
    expect(searchGroups([g1, g2], 'бэкенд')[0]?.rows).toHaveLength(1);
    const hit = searchGroups([g1, g2], 'build');
    expect(hit).toHaveLength(1);
    expect(hit[0]?.rows.map((m) => m.id)).toEqual(['b']);
    expect(searchGroups([g1, g2], 'nothing-like-this')).toEqual([]);
  });

  it('puts problems first in the flat list', () => {
    expect(flatRows([g1, g2], '').map((m) => m.id)).toEqual(['b', 'c', 'a']);
    expect(flatRows([g1, g2], 'pasha').map((m) => m.id)).toEqual(['c']);
  });

  it('filters tenants by mode', () => {
    const pinned = group({ key: 'p', pinned: true });
    const all = [g1, g2, pinned];
    expect(filterTenants(all, { mode: 'all', pick: [] })).toHaveLength(3);
    expect(filterTenants(all, { mode: 'problems', pick: [] }).map((g) => g.key)).toEqual(['g2']);
    expect(filterTenants(all, { mode: 'pinned', pick: [] }).map((g) => g.key)).toEqual(['p']);
    expect(filterTenants(all, { mode: 'pick', pick: ['g1'] }).map((g) => g.key)).toEqual(['g1']);
  });

  it('labels the filter and toggles picks, falling back to all', () => {
    expect(filterLabel({ mode: 'all', pick: [] })).toBe('Все тенанты');
    expect(filterLabel({ mode: 'problems', pick: [] })).toBe('С проблемами');
    expect(filterLabel({ mode: 'pinned', pick: [] })).toBe('Закреплённые');
    const one = toggleTenant({ mode: 'all', pick: [] }, 'g1');
    expect(one).toEqual({ mode: 'pick', pick: ['g1'] });
    expect(filterLabel(one)).toBe('Тенанты: 1');
    expect(toggleTenant(one, 'g1')).toEqual({ mode: 'all', pick: [] });
  });

  it('lists at most 14 tenants of the filter, by name, narrowed by its search', () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      group({ key: `k${i}`, name: `Тенант ${String(i).padStart(2, '0')}` }),
    );
    expect(filterOptions(many, '')).toHaveLength(14);
    expect(filterOptions(many, '07').map((g) => g.name)).toEqual(['Тенант 07']);
    expect(filterOptions(many, '')[0]?.name).toBe('Тенант 00');
  });

  it('colours the dot of a tenant by its worst state', () => {
    expect(groupSt({ offline: 1, idle: 2 })).toBe('off');
    expect(groupSt({ offline: 0, idle: 2 })).toBe('idle');
    expect(groupSt({ offline: 0, idle: 0 })).toBe('on');
  });

  it('pages groups and rows', () => {
    expect([groupLimit(0), groupLimit(2)]).toEqual([12, 32]);
    expect([rowLimit(0), rowLimit(2)]).toEqual([25, 75]);
  });
});

describe('tiles and lead', () => {
  it('draws the four tiles of a tenant with the board wording', () => {
    const idle = machine({ state: 'idle', since: hoursAgo(24 * 9) });
    const tiles = tenantTiles(summary(), [group({}, [idle, machine({ disk: 91 })])], NOW);
    expect(tiles.map((t) => t.label)).toEqual(['На связи', 'Чаты сейчас', 'Нагрузка', 'Требует внимания']);
    expect(tiles[0]?.value).toBe('6 из 8 · одна простаивает 9 дней');
    expect(tiles[1]?.value).toBe('9 идут · 64 за сегодня');
    expect(tiles[2]?.value).toBe('в среднем 41% · build-01 под 88%');
    expect(tiles[3]?.value).toBe('диск build-01 91% · 2 раннера устарели');
    expect(tiles[3]?.strip?.map((p) => p.w)).toEqual([2, 1, 5]);
  });

  it('says everything is fine and shows dashes for what the hub does not count', () => {
    const tiles = tenantTiles(
      summary({
        idleWeek: 0,
        chatsToday: null,
        cpuMean: null,
        hottest: null,
        diskHot: null,
        outdatedRunners: 0,
      }),
      [],
      NOW,
    );
    expect(tiles[0]?.value).toBe('6 из 8');
    expect(tiles[1]?.value).toBe('9 идут · — за сегодня');
    expect(tiles[2]?.value).toBe('—');
    expect(tiles[3]?.value).toBe('всё в порядке');
  });

  it('draws the four tiles of the organization', () => {
    const groups = [group({ idle: 1 }), group({ key: 'g2', offline: 2 })];
    const tiles = orgTiles(
      summary({ total: 1240, online: 1102, idleWeek: 87, offline: 51, tenantsOffline: 23 }),
      groups,
    );
    expect(tiles[0]?.value).toBe('1 102 из 1 240 машин');
    expect(tiles[2]?.value).toBe('87 машин в 1 тенанте');
    expect(tiles[3]?.value).toBe('51 машина в 23 тенантах');
  });

  it('writes the lead of a tenant', () => {
    const ms = [machine(), machine(), machine({ kind: 'shared' })];
    expect(tenantLead('Продукт', ms)).toBe('Тенант Продукт · 3 машины: 2 личные и 1 общая');
    expect(tenantLead('Продукт', [])).toBe('Тенант Продукт · 0 машин');
  });
});

describe('resource sliders', () => {
  it('keeps a current size the plans do not list, in order', () => {
    expect(withValue([4, 8, 16], 12)).toEqual([4, 8, 12, 16]);
    expect(withValue([4, 8, 16], 8)).toEqual([4, 8, 16]);
    expect(withValue([4, 8], 0)).toEqual([4, 8]);
  });
  it('locks the stops below the floor', () => {
    expect(stopsOf([16, 32, 64, 128, 256], 105)).toEqual({ vals: [16, 32, 64, 128, 256], firstOk: 3 });
    expect(stopsOf([4, 8], 0).firstOk).toBe(0);
    expect(stopsOf([4, 8], 99).firstOk).toBe(1);
  });

  it('keeps a margin over the used memory', () => {
    expect(ramFloor(91)).toBe(105);
    expect(ramFloor(0)).toBe(0);
  });

  it('snaps a pointer to the nearest allowed stop', () => {
    expect(snapStop(0, 5, 0)).toBe(0);
    expect(snapStop(0.49, 5, 0)).toBe(2);
    expect(snapStop(1.4, 5, 0)).toBe(4);
    expect(snapStop(0.1, 5, 3)).toBe(3);
    expect(snapStop(0.5, 1, 0)).toBe(0);
  });

  it('steps by keys inside the allowed range', () => {
    expect(stepStop(2, 1, 5, 0)).toBe(3);
    expect(stepStop(4, 1, 5, 0)).toBe(4);
    expect(stepStop(3, -1, 5, 3)).toBe(3);
    expect(stepStop(3, -1, 5, 0)).toBe(2);
  });

  it('places stops along the track', () => {
    expect(stopPos(0, 5)).toBe(0);
    expect(stopPos(2, 5)).toBe(50);
    expect(stopPos(0, 1)).toBe(0);
  });
});

describe('money', () => {
  it('turns kopecks into roubles for RUB only', () => {
    expect(rubOf({ amount: 3_376_000, currency: 'RUB' })).toBe(33_760);
    expect(rubOf({ amount: 100, currency: 'USD' })).toBeNull();
    expect(rubOf(null)).toBeNull();
  });

  it('writes the delta with its direction', () => {
    expect(deltaOf({ amount: -320_000, currency: 'RUB' })).toEqual({ text: '−3 200 ₽', dir: 'down' });
    expect(deltaOf({ amount: 120_000, currency: 'RUB' })).toEqual({ text: '+1 200 ₽', dir: 'up' });
    expect(deltaOf({ amount: 0, currency: 'RUB' })).toBeNull();
  });

  it('writes a size', () => {
    expect(sizeText({ cores: 16, ramGb: 128, diskGb: 1000 })).toBe('16 ядер · 128 ГБ · 1 ТБ');
    expect(sizeText({ cores: 4, ramGb: 16, diskGb: 250 })).toBe('4 ядра · 16 ГБ · 250 ГБ');
  });
});

describe('the status line of the card', () => {
  it('says what is going on while an action runs', () => {
    expect(statusOf(machine(), 'restart', null, NOW)).toEqual({
      st: 'busy',
      label: 'Перезапускаем раннер',
      hint: 'около 30 секунд',
    });
    expect(statusOf(machine({ chats: 2 }), 'drain', null, NOW).hint).toContain('2 чата ещё идут');
  });

  it('reads the states', () => {
    expect(statusOf(machine(), null, null, NOW)).toEqual({
      st: 'on',
      label: 'На связи',
      hint: 'раннер отвечает',
    });
    expect(statusOf(machine(), null, 'перезапущена только что', NOW).hint).toBe('перезапущена только что');
    expect(statusOf(machine({ state: 'offline', since: hoursAgo(48) }), null, 'x', NOW)).toEqual({
      st: 'off',
      label: 'Не на связи',
      hint: 'пропала 2 дн назад — выключена или нет сети',
    });
    expect(statusOf(machine({ state: 'idle', since: hoursAgo(24 * 9) }), null, null, NOW)).toEqual({
      st: 'idle',
      label: 'Простаивает',
      hint: 'на связи, но чатов не было 9 дн',
    });
  });

  it('says who and when switched a machine off', () => {
    const off = machine({ state: 'disabled', disabledBy: 'Андрей', disabledAt: hoursAgo(0) });
    expect(statusOf(off, null, null, NOW)).toEqual({
      st: 'disabled',
      label: 'Отключена',
      hint: 'только что, Андрей · чаты на ней не запустить',
    });
    expect(statusOf(machine({ state: 'disabled' }), null, null, NOW).hint).toBe('чаты на ней не запустить');
    const old = machine({ state: 'disabled', disabledBy: 'Андрей', disabledAt: hoursAgo(5) });
    expect(statusOf(old, null, null, NOW).hint).toBe('5 ч назад, Андрей · чаты на ней не запустить');
  });

  it('writes the subtitle of the card', () => {
    expect(cardSub(machine())).toBe('Продукт · личная, Аня');
    expect(cardSub(machine({ kind: 'shared', ownerName: null, purpose: 'сборки и e2e' }))).toBe(
      'Продукт · общая, сборки и e2e',
    );
  });
});

describe('optimistic list', () => {
  const a = machine({ id: 'a' });
  const b = machine({ id: 'b', state: 'offline' });
  const list: MachinesList = {
    summary: summary({ online: 1, offline: 1 }),
    groups: [group({ online: 1, offline: 1, total: 2 }, [a, b])],
    tenants: [],
    mlCoreUrl: null,
  };

  it('returns the same list without changes', () => {
    expect(applyOverlay(list, {})).toBe(list);
  });

  it('replaces a machine and moves the counters', () => {
    const next = applyOverlay(list, { a: { ...a, state: 'disabled' } });
    expect(next.groups[0]?.machines[0]?.state).toBe('disabled');
    expect(next.groups[0]).toMatchObject({ online: 0, offline: 2, idle: 0 });
    expect(next.summary).toMatchObject({ online: 0, offline: 2 });
    expect(list.groups[0]?.online).toBe(1);
  });

  it('replaces without moving counters when the bucket stays', () => {
    const next = applyOverlay(list, { a: { ...a, cpu: 99 } });
    expect(next.groups[0]?.machines[0]?.cpu).toBe(99);
    expect(next.groups[0]).toMatchObject({ online: 1, offline: 1 });
  });
});

describe('the dialogs of the card', () => {
  const plan = {
    cores: [4, 8],
    ramGb: [16, 32],
    diskGb: [100, 500],
    price: { core: 450, ramGb: 120, diskGb: 8 },
    usedRamGb: 10,
    usedDiskGb: 100,
  };
  it('lists what switching off does, with the disk rent of a server', () => {
    const items = offItems(machine({ cloud: true }), plan);
    expect(items.map((i) => i.icon)).toEqual(['circle-stop', 'users', 'hard-drive', 'coins']);
    expect(items[3]?.text).toMatch(/4\s000 ₽ в месяц/);
  });
  it('has no money line for a laptop and no number without a plan', () => {
    expect(offItems(machine({ cloud: false }), plan)).toHaveLength(3);
    expect(offItems(machine({ cloud: true }), null)[3]?.text).toContain('по тарифу');
  });
  it('says what a restart does', () => {
    expect(restartHint('runner', machine())).toContain('около 30 секунд');
    expect(restartHint('machine', machine({ cloud: false }))).not.toContain('через');
    expect(
      restartHint('machine', machine({ cloud: true, rent: { provider: 'Hoster', plan: 'p', price: null } })),
    ).toContain('через Hoster');
  });
});

describe('rename and open', () => {
  it('cleans a name the way the hub does', () => {
    expect(cleanName('  build   box ')).toBe('build box');
    expect(cleanName('   ')).toBeNull();
    expect(cleanName('x'.repeat(60))).toHaveLength(60);
    expect(cleanName('x'.repeat(61))).toBeNull();
  });

  it('opens only a machine that is on', () => {
    expect(canOpen(machine({ state: 'online' }))).toBe(true);
    expect(canOpen(machine({ state: 'idle' }))).toBe(true);
    expect(canOpen(machine({ state: 'offline' }))).toBe(false);
    expect(canOpen(machine({ state: 'disabled' }))).toBe(false);
  });

  it('does not open a machine the hub only watches', () => {
    expect(canOpen(machine({ state: 'online', opensRuntime: false }))).toBe(false);
    expect(canOpen(machine({ state: 'online', opensRuntime: true }))).toBe(true);
  });
});

describe('a server and its seats', () => {
  const anya = machine({ id: 'a', name: 'dev-anya', host: 'dev.example.com', chats: 2, cpu: 40, disk: 91 });
  const boris = machine({
    id: 'b',
    name: 'dev-boris',
    ownerName: 'Борис',
    host: 'dev.example.com',
    state: 'offline',
    since: hoursAgo(3),
    chats: null,
    cpu: null,
    ram: null,
    disk: null,
    weekCostUsd: 14,
    weekChats: 9,
    runnerVersion: '1.1.0',
    runnerOutdated: true,
  });
  const alone = machine({ id: 'c', name: 'build-01', host: 'build.example.com' });
  const noHost = machine({ id: 'd', name: 'old-01' });
  const elsewhere = machine({ id: 'e', name: 'dev-other', host: 'dev.example.com', tenantId: 't2' });

  it('folds the seats of one hostname in a tenant and leaves the rest as machines', () => {
    const rows = serverRows([anya, alone, boris, noHost, elsewhere]);
    expect(
      rows.map((r) => (r.kind === 'server' ? `server:${r.s.seats.map((m) => m.id).join('+')}` : r.m.id)),
    ).toEqual(['server:a+b', 'c', 'd', 'e']);
  });

  it('tells the load once and adds up the chats and the money', () => {
    const [row] = serverRows([anya, boris]);
    if (row?.kind !== 'server') throw new Error('want a server');
    expect(row.s.server).toMatchObject({
      name: 'dev',
      state: 'online',
      chats: 2,
      cpu: 40,
      disk: 91,
      weekCostUsd: 100,
      weekChats: 50,
      runnerVersion: 'разные',
      runnerOutdated: true,
      purpose: '2 места: Аня, Борис',
    });
  });

  it('is off only when every seat is', () => {
    const off = { state: 'offline' as const, since: hoursAgo(5) };
    const [row] = serverRows([{ ...anya, ...off }, boris]);
    expect(row?.kind === 'server' && row.s.server.state).toBe('offline');
    expect(row?.kind === 'server' && row.s.server.since).toBe(hoursAgo(3));
    const [gone] = serverRows([
      { ...anya, state: 'disabled' },
      { ...boris, state: 'disabled' },
    ]);
    expect(gone?.kind === 'server' && gone.s.server.state).toBe('disabled');
  });

  it('counts servers in the tiles and the heads of the groups', () => {
    const list: MachinesList = {
      summary: summary({ total: 3, online: 2, idleWeek: 0, offline: 1, outdatedRunners: 1 }),
      groups: [group({ total: 3, online: 2, offline: 1 }, [anya, boris, alone])],
      tenants: [],
      mlCoreUrl: null,
    };
    const next = countByServers(list);
    expect(next.summary).toMatchObject({ total: 2, online: 2, offline: 0, outdatedRunners: 1 });
    expect(next.groups[0]).toMatchObject({ total: 2, online: 2, offline: 0 });
    expect(next.groups[0]?.machines).toHaveLength(3);
    expect(foldGroups(list.groups)[0]?.machines.map((m) => m.name)).toEqual(['dev', 'build-01']);
  });

  it('returns the same list when every machine has a server of its own', () => {
    const list: MachinesList = {
      summary: summary(),
      groups: [group({}, [alone, noHost])],
      tenants: [],
      mlCoreUrl: null,
    };
    expect(countByServers(list)).toBe(list);
  });
});
