// Fixtures for the teams and limits screen: the tenant «Продукт» of the organization «Акме» with the board's three
// teams and people without a team. State lives in this closure: a limit set here shows in the next read, and the
// journal gets its line. The rules follow docs/hub/orgs.md: nobody changes their own role or limit, a lead manages
// only their team, a team's and a tenant's limit have their own owners.
import { OrgApiError } from '../api/errors';
import type {
  AuditAction,
  AuditEntry,
  AuditPage,
  LimitRule,
  LimitScope,
  MemberRole,
  Needs,
  Person,
  TeamBlock,
  TeamExtras,
  TeamsApi,
  TeamsView,
} from '../api/teams';
import { later, type MockCtx } from './ctx';

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

interface P {
  id: string;
  name: string;
  team: string | null;
  role?: MemberRole;
  spent: number;
  limit: number | null;
  by?: string;
  ago?: number | null; // hours since last visit; null: never
  invited?: boolean;
  chats?: number;
  mrs?: number;
}
interface T {
  id: string;
  name: string;
  lead: string;
  limit: number;
}

const TENANT_LIMIT = 400_000;
const TEAMS: T[] = [
  { id: 'tm-front', name: 'Фронт', lead: 'Аня', limit: 120_000 },
  { id: 'tm-back', name: 'Бэкенд', lead: 'Дима', limit: 110_000 },
  { id: 'tm-design', name: 'Дизайн', lead: 'Лиза', limit: 70_000 },
];

const SEED: P[] = [
  {
    id: 'p-anya',
    name: 'Аня',
    team: 'tm-front',
    role: 'lead',
    spent: 17_500,
    limit: 20_000,
    by: 'Андрей',
    ago: 3,
    chats: 34,
    mrs: 6,
  },
  {
    id: 'p-igor',
    name: 'Игорь',
    team: 'tm-front',
    spent: 18_600,
    limit: 20_000,
    by: 'Аня',
    ago: 24,
    chats: 36,
    mrs: 7,
  },
  {
    id: 'p-andrey',
    name: 'Андрей',
    team: 'tm-front',
    spent: 12_700,
    limit: 30_000,
    by: 'Аня',
    ago: 3,
    chats: 25,
    mrs: 5,
  },
  {
    id: 'p-masha',
    name: 'Маша Ким',
    team: 'tm-front',
    spent: 0,
    limit: 15_000,
    by: 'Аня',
    ago: null,
    invited: true,
  },
  {
    id: 'p-oleg',
    name: 'Олег',
    team: 'tm-front',
    spent: 27_900,
    limit: 30_000,
    by: 'Аня',
    ago: 3,
    chats: 34,
    mrs: 6,
  },
  {
    id: 'p-pasha',
    name: 'Паша',
    team: 'tm-front',
    spent: 12_100,
    limit: 30_000,
    by: 'Аня',
    ago: 24,
    chats: 6,
    mrs: 1,
  },
  {
    id: 'p-vika',
    name: 'Вика',
    team: 'tm-front',
    spent: 1_700,
    limit: 20_000,
    by: 'Аня',
    ago: 9 * 24,
    chats: 0,
    mrs: 0,
  },
  {
    id: 'p-gleb',
    name: 'Глеб',
    team: 'tm-front',
    spent: 12_300,
    limit: 15_000,
    by: 'Аня',
    ago: 1,
    chats: 34,
    mrs: 6,
  },
  {
    id: 'p-timur',
    name: 'Тимур',
    team: 'tm-front',
    spent: 7_200,
    limit: 20_000,
    by: 'Аня',
    ago: 48,
    chats: 24,
    mrs: 4,
  },
  {
    id: 'p-dima',
    name: 'Дима',
    team: 'tm-back',
    role: 'lead',
    spent: 14_000,
    limit: 25_000,
    by: 'Андрей',
    ago: 2,
    chats: 30,
    mrs: 7,
  },
  {
    id: 'p-rita',
    name: 'Рита',
    team: 'tm-back',
    spent: 15_100,
    limit: 20_000,
    by: 'Дима',
    ago: 5,
    chats: 28,
    mrs: 6,
  },
  {
    id: 'p-maks',
    name: 'Макс',
    team: 'tm-back',
    spent: 12_400,
    limit: 20_000,
    by: 'Дима',
    ago: 26,
    chats: 22,
    mrs: 5,
  },
  {
    id: 'p-nina',
    name: 'Нина',
    team: 'tm-back',
    spent: 13_800,
    limit: 20_000,
    by: 'Дима',
    ago: 4,
    chats: 25,
    mrs: 4,
  },
  {
    id: 'p-yan',
    name: 'Ян',
    team: 'tm-back',
    spent: 11_200,
    limit: 20_000,
    by: 'Дима',
    ago: 7,
    chats: 20,
    mrs: 3,
  },
  {
    id: 'p-vlad',
    name: 'Влад',
    team: 'tm-back',
    spent: 9_600,
    limit: 20_000,
    by: 'Дима',
    ago: 30,
    chats: 16,
    mrs: 3,
  },
  {
    id: 'p-sonya',
    name: 'Соня',
    team: 'tm-back',
    spent: 8_400,
    limit: 20_000,
    by: 'Дима',
    ago: 6,
    chats: 14,
    mrs: 2,
  },
  {
    id: 'p-petya',
    name: 'Петя',
    team: 'tm-back',
    spent: 6_700,
    limit: 20_000,
    by: 'Дима',
    ago: 8,
    chats: 11,
    mrs: 2,
  },
  {
    id: 'p-liza',
    name: 'Лиза',
    team: 'tm-design',
    role: 'lead',
    spent: 11_000,
    limit: 20_000,
    by: 'Андрей',
    ago: 2,
    chats: 18,
    mrs: 3,
  },
  {
    id: 'p-katya',
    name: 'Катя',
    team: 'tm-design',
    spent: 10_900,
    limit: 15_000,
    by: 'Лиза',
    ago: 3,
    chats: 17,
    mrs: 3,
  },
  {
    id: 'p-misha',
    name: 'Миша',
    team: 'tm-design',
    spent: 9_800,
    limit: 15_000,
    by: 'Лиза',
    ago: 5,
    chats: 12,
    mrs: 2,
  },
  {
    id: 'p-ira',
    name: 'Ира',
    team: 'tm-design',
    spent: 8_300,
    limit: 15_000,
    by: 'Лиза',
    ago: 20,
    chats: 10,
    mrs: 2,
  },
  {
    id: 'p-fedor',
    name: 'Фёдор',
    team: 'tm-design',
    spent: 6_800,
    limit: 15_000,
    by: 'Лиза',
    ago: 28,
    chats: 9,
    mrs: 1,
  },
  { id: 'p-ulya', name: 'Ульяна', team: null, spent: 6_100, limit: null, ago: 6, chats: 12, mrs: 2 },
  { id: 'p-zahar', name: 'Захар', team: null, spent: 4_800, limit: null, ago: 10, chats: 9, mrs: 1 },
  { id: 'p-tasya', name: 'Тася', team: null, spent: 3_600, limit: null, ago: 30, chats: 7, mrs: 1 },
  { id: 'p-yura', name: 'Юра', team: null, spent: 2_700, limit: null, ago: 50, chats: 4, mrs: 0 },
  { id: 'p-yana', name: 'Яна', team: null, spent: 1_900, limit: null, ago: 8 * 24, chats: 2, mrs: 0 },
  { id: 'p-emma', name: 'Эмма', team: null, spent: 800, limit: null, ago: 12 * 24, chats: 1, mrs: 0 },
];

const RULES: Record<string, { axis: 'account' | 'model'; label: string; amount: number; spent: number }[]> = {
  'p-oleg': [{ axis: 'model', label: 'Opus', amount: 20_000, spent: 18_400 }],
  'tm-front': [
    { axis: 'account', label: 'Claude Max №2', amount: 60_000, spent: 52_000 },
    { axis: 'model', label: 'Opus', amount: 70_000, spent: 61_000 },
  ],
};

const need = (hint: Needs | null): Needs | null => hint;

export const teamsMock = (ctx: MockCtx): TeamsApi => {
  const now = Date.now();
  const role = ctx.role;
  const selfId = role === 'lead' ? 'p-anya' : 'p-andrey';
  const leadTeam = 'tm-front';
  let tenantLimit = TENANT_LIMIT;
  // The team «Фронт» also works in the tenant «Платформа»: its limit and environment are its own there.
  const platformState = { tenantLimit: 200_000, teamLimit: 40_000, teamSpent: 16_000 };
  const machinesPer = new Map<string, number>([
    ['product:tm-front', 2],
    ['platform:tm-front', 1],
  ]);
  const teams = TEAMS.map((t) => ({ ...t }));
  const people = SEED.map((p) => ({ ...p }));
  const limitSetAt = new Map<string, number>();
  const rules = new Map(
    Object.entries(RULES).map(([k, v]) => [k, v.map((r, i) => ({ ...r, id: `${k}:${i}` }))]),
  );
  const audit: AuditEntry[] = [
    entry(
      'a1',
      now - 2 * HOUR,
      'Андрей',
      'limit.set',
      { kind: 'user', id: 'p-oleg', label: 'Олег' },
      { limitRub: 20_000 },
      { limitRub: 30_000 },
    ),
    entry(
      'a2',
      now - 5 * HOUR,
      'Аня',
      'limit.set',
      { kind: 'user', id: 'p-gleb', label: 'Глеб' },
      { limitRub: 10_000 },
      { limitRub: 15_000 },
    ),
    entry(
      'a3',
      now - 2 * DAY,
      'Андрей',
      'limit.set',
      { kind: 'team', id: 'tm-front', label: 'Фронт' },
      { limitRub: 100_000 },
      { limitRub: 120_000 },
    ),
    entry('a4', now - 3 * DAY, 'Андрей', 'grant.add', { kind: 'user', id: 'p-dima', label: 'Дима' }, null, {
      role: 'lead',
      team: 'Бэкенд',
    }),
    entry(
      'a5',
      now - 6 * DAY,
      'Аня',
      'member.remove',
      { kind: 'user', id: 'p-x', label: 'Рустам' },
      { team: 'Фронт' },
      null,
    ),
  ];
  let seq = 10;

  function entry(
    id: string,
    at: number,
    actor: string,
    action: AuditAction,
    target: AuditEntry['target'],
    before: AuditEntry['before'],
    after: AuditEntry['after'],
  ): AuditEntry {
    return {
      id,
      at: new Date(at).toISOString(),
      actor: { userId: `u-${actor}`, name: actor },
      action,
      target,
      before,
      after,
      self: false,
    };
  }
  const log = (
    action: AuditAction,
    target: AuditEntry['target'],
    before: AuditEntry['before'],
    after: AuditEntry['after'],
  ) => {
    audit.unshift(entry(`a${seq++}`, Date.now(), 'Андрей', action, target, before, after));
  };

  const teamOf = (id: string | null) => teams.find((t) => t.id === id);
  const visible = (p: P) => role !== 'lead' || p.team === leadTeam;

  const personRow = (p: P): Person => {
    const t = teamOf(p.team);
    const isSelf = p.id === selfId;
    const isLead = p.role === 'lead';
    const inMyTeam = role === 'lead' ? p.team === leadTeam : true;
    const limitNeeds: Needs | null = role === 'admin' ? null : isLead ? 'tenant_admin' : null;
    const canLimit = !isSelf && inMyTeam && limitNeeds === null;
    const eff =
      p.limit !== null
        ? { amountRub: p.limit, source: 'personal' as const, setBy: p.by ?? null, setAt: setAt(p) }
        : t
          ? { amountRub: t.limit, source: 'team' as const, setBy: 'Андрей', setAt: null }
          : { amountRub: tenantLimit, source: 'tenant' as const, setBy: 'Андрей', setAt: null };
    return {
      id: p.id,
      name: p.name,
      role: p.role ?? 'member',
      teamId: p.team,
      invited: p.invited ?? false,
      lastActiveAt: p.ago === null || p.ago === undefined ? null : new Date(now - p.ago * HOUR).toISOString(),
      spentRub: p.spent,
      limitRub: p.limit,
      limitSetBy: p.by ?? null,
      limitSetAt: setAt(p),
      effective: eff,
      chatsWeek: p.chats ?? null,
      mrsWeek: p.mrs ?? null,
      machines: p.invited ? null : (p.chats ?? 0) > 30 ? 2 : (p.chats ?? 0) > 0 ? 1 : 0,
      self: isSelf,
      can: {
        limit: canLimit,
        role: !isSelf && inMyTeam,
        team: !isSelf && role === 'admin',
        remove: !isSelf && inMyTeam,
      },
      needs: {
        limit: canLimit || isSelf ? null : need(limitNeeds ?? 'team_lead'),
        role: !isSelf && inMyTeam ? null : 'tenant_admin',
        team: role === 'admin' ? null : 'tenant_admin',
      },
      rules: (rules.get(p.id) ?? []).map(ruleRow),
    };
  };
  const setAt = (p: P): string | null =>
    p.limit === null ? null : new Date(limitSetAt.get(p.id) ?? now - 3 * DAY).toISOString();
  const ruleRow = (r: {
    id: string;
    axis: 'account' | 'model';
    label: string;
    amount: number;
    spent: number;
  }): LimitRule => ({
    id: r.id,
    axis: r.axis,
    label: r.label,
    amountRub: r.amount,
    spentRub: r.spent,
    setBy: 'Андрей',
    canEdit: role === 'admin',
    needs: role === 'admin' ? null : 'tenant_admin',
  });

  const extrasOf = (t: T, slug: string): TeamExtras => {
    const inPlatform = t.id === 'tm-front';
    const isPlatform = slug === 'platform';
    const members = people.filter((p) => p.team === t.id);
    const now = members.reduce((n, p) => n + (personRow(p).machines ?? 0), 0);
    const per = machinesPer.get(`${slug}:${t.id}`) ?? 2;
    const tenantLimitRub = isPlatform ? platformState.tenantLimit : tenantLimit;
    const tenantSpentRub = isPlatform
      ? platformState.tenantLimit - 140_000
      : people.reduce((n, p) => n + p.spent, 0);
    return {
      tenants: inPlatform
        ? [
            { slug: 'product', name: 'Продукт' },
            { slug: 'platform', name: 'Платформа' },
          ]
        : [{ slug: 'product', name: 'Продукт' }],
      tenantName: isPlatform ? 'Платформа' : 'Продукт',
      tenantLimitRub,
      tenantSpentRub,
      machinesPerPerson: per,
      machinesNow: isPlatform ? 3 : now,
      machinesFree: 6,
      accounts: isPlatform ? 'Claude Max ×1' : 'Claude Max ×2, Codex ×1',
      secrets: isPlatform ? 3 : 7,
      env: isPlatform ? { skills: 2, mcp: 2, repos: 1, docs: 1 } : { skills: 3, mcp: 3, repos: 1, docs: 1 },
    };
  };

  const view = (slug = 'product'): TeamsView => {
    const platform = slug === 'platform';
    const productBlocks: TeamBlock[] = [
      ...teams.map((t): TeamBlock => {
        const members = people.filter((p) => p.team === t.id);
        const handed = members.reduce((n, p) => n + (p.limit ?? 0), 0);
        const canTeamLimit = role === 'admin';
        return {
          id: t.id,
          name: t.name,
          leadName: t.lead,
          spentRub: members.reduce((n, p) => n + p.spent, 0),
          limitRub: t.limit,
          limitSetBy: 'Андрей',
          handedOutPct: t.limit ? Math.round((handed / t.limit) * 100) : null,
          siblingsLimitRub: teams.filter((x) => x.id !== t.id).reduce((n, x) => n + x.limit, 0),
          can: { limit: canTeamLimit },
          needs: { limit: canTeamLimit ? null : 'tenant_admin' },
          people: members.map(personRow),
          rules: (rules.get(t.id) ?? []).map(ruleRow),
          extras: extrasOf(t, slug),
        };
      }),
      ...(role === 'admin'
        ? [
            {
              id: null,
              name: 'Без команды',
              leadName: null,
              spentRub: people.filter((p) => p.team === null).reduce((n, p) => n + p.spent, 0),
              limitRub: null,
              limitSetBy: null,
              handedOutPct: null,
              siblingsLimitRub: 0,
              can: { limit: false },
              needs: { limit: null },
              people: people.filter((p) => p.team === null).map(personRow),
              rules: [],
            } satisfies TeamBlock,
          ]
        : []),
    ].filter((b) => role !== 'lead' || b.id === leadTeam);
    const blocks: TeamBlock[] = platform
      ? productBlocks
          .filter((b) => b.id === 'tm-front')
          .map((b) => ({
            ...b,
            spentRub: platformState.teamSpent,
            limitRub: platformState.teamLimit,
            siblingsLimitRub: 0,
          }))
      : productBlocks;
    const all = people.filter(visible);
    const spent = all.reduce((n, p) => n + p.spent, 0);
    const mine = teamOf(leadTeam);
    return {
      scope:
        role === 'lead' && mine
          ? {
              label: `Команда ${mine.name}`,
              limitRub: mine.limit,
              spentRub: spent,
              setBy: 'Андрей',
              can: { limit: false },
              needs: 'tenant_admin',
            }
          : {
              label: platform ? 'Тенант Платформа' : 'Тенант Продукт',
              limitRub: platform ? platformState.tenantLimit : tenantLimit,
              spentRub: platform ? platformState.tenantLimit - 140_000 : spent,
              setBy: 'Андрей',
              can: { limit: role === 'admin' },
              needs: role === 'admin' ? null : 'org_admin',
            },
      teams: blocks,
      activeWeek: all.filter((p) => p.ago !== null && p.ago !== undefined && p.ago < 7 * 24).length,
      total: all.length,
      activeSeries: [14, 16, 15, 12, 4, 3, 17, 19, 18, 21, 20, 8, 9, 23],
      notAccepted: all.filter((p) => p.invited).length,
      notVisiting: all.filter((p) => !p.invited && (p.ago ?? 0) >= 7 * 24).length,
      can: { createTeam: role === 'admin', invite: role !== 'member' },
      spendKnown: true,
    };
  };
  const find = (id: string) => {
    const p = people.find((x) => x.id === id);
    if (!p) throw new OrgApiError('not_found', 'person not found');
    return p;
  };
  const forbid = (p: P, field: 'limit' | 'role' | 'team' | 'remove') => {
    const row = personRow(p);
    if (row.self)
      throw new OrgApiError('forbidden', 'Себе менять нельзя: это может сделать другой администратор.');
    if (!row.can[field]) throw new OrgApiError('forbidden', 'У вашей роли нет доступа к этому.');
  };

  return {
    teamsView: (_o, slug) => later(view(slug)),
    setLimit: async (_o, s, on: LimitScope, amount) => {
      if (on.kind === 'user') {
        const p = find(on.id);
        forbid(p, 'limit');
        if (amount !== null && amount < p.spent)
          throw new OrgApiError('invalid_request', 'Меньше, чем уже потрачено, поставить нельзя.');
        log(
          amount === null ? 'limit.remove' : 'limit.set',
          { kind: 'user', id: p.id, label: p.name },
          { limitRub: p.limit },
          { limitRub: amount },
        );
        p.limit = amount;
        p.by = 'Андрей';
        limitSetAt.set(p.id, Date.now());
      } else if (on.kind === 'team') {
        const t = teamOf(on.id);
        if (!t) throw new OrgApiError('not_found', 'team not found');
        if (role !== 'admin') throw new OrgApiError('forbidden', 'У вашей роли нет доступа к этому.');
        if (amount === null) throw new OrgApiError('invalid_request', 'У команды лимит есть всегда.');
        const inPlatform = s === 'platform';
        log(
          'limit.set',
          { kind: 'team', id: t.id, label: t.name },
          { limitRub: inPlatform ? platformState.teamLimit : t.limit },
          { limitRub: amount },
        );
        if (inPlatform) platformState.teamLimit = amount;
        else t.limit = amount;
      } else {
        if (role !== 'admin') throw new OrgApiError('forbidden', 'У вашей роли нет доступа к этому.');
        if (amount === null) throw new OrgApiError('invalid_request', 'У тенанта лимит есть всегда.');
        log(
          'limit.set',
          { kind: 'tenant', id: 't-product', label: 'Продукт' },
          { limitRub: tenantLimit },
          { limitRub: amount },
        );
        tenantLimit = amount;
      }
      return later(view(s), 500);
    },
    setMachinesPerPerson: async (_o, s, teamId, n) => {
      if (role === 'member') throw new OrgApiError('forbidden', 'У вашей роли нет доступа к этому.');
      if (!Number.isInteger(n) || n < 1 || n > 3)
        throw new OrgApiError('invalid_request', 'Машин на человека — от 1 до 3.');
      machinesPer.set(`${s}:${teamId}`, n);
      return later(view(s), 400);
    },
    setRuleAmount: async (_o, s, ruleId, amount) => {
      if (role !== 'admin') throw new OrgApiError('forbidden', 'У вашей роли нет доступа к этому.');
      for (const list of rules.values()) {
        const r = list.find((x) => x.id === ruleId);
        if (r) r.amount = amount;
      }
      return later(view(s), 400);
    },
    setRole: async (_o, s, personId, next) => {
      const p = find(personId);
      forbid(p, 'role');
      log(
        next === 'lead' ? 'grant.add' : 'grant.remove',
        { kind: 'user', id: p.id, label: p.name },
        { role: p.role ?? 'member' },
        { role: next },
      );
      p.role = next;
      return later(view(s), 500);
    },
    moveToTeam: async (_o, s, personId, teamId) => {
      const p = find(personId);
      forbid(p, 'team');
      log(
        'member.primary',
        { kind: 'user', id: p.id, label: p.name },
        { team: teamOf(p.team)?.name ?? null },
        { team: teamOf(teamId)?.name ?? null },
      );
      p.team = teamId;
      return later(view(s), 500);
    },
    removeFromTenant: async (_o, s, personId) => {
      const p = find(personId);
      forbid(p, 'remove');
      log(
        'member.remove',
        { kind: 'user', id: p.id, label: p.name },
        { team: teamOf(p.team)?.name ?? null },
        null,
      );
      people.splice(people.indexOf(p), 1);
      return later(view(s), 500);
    },
    createTeam: async (_o, s, name) => {
      if (role !== 'admin') throw new OrgApiError('forbidden', 'У вашей роли нет доступа к этому.');
      const trimmed = name.trim();
      if (trimmed.length < 1 || trimmed.length > 60)
        throw new OrgApiError('invalid_request', 'Название — от 1 до 60 знаков.');
      teams.push({ id: `tm-${seq++}`, name: trimmed, lead: '', limit: 50_000 });
      log('team.create', { kind: 'team', id: trimmed, label: trimmed }, null, { name: trimmed });
      return later(view(s), 500);
    },
    teamsAudit: async (_o, _s, before): Promise<AuditPage> => {
      const list =
        role === 'member'
          ? []
          : audit.filter((a) =>
              role === 'lead' ? a.target?.kind === 'user' || a.target?.id === leadTeam : true,
            );
      const start = before ? list.findIndex((a) => a.id === before) + 1 : 0;
      const slice = list.slice(start, start + 4);
      const last = slice[slice.length - 1];
      return later({ events: slice, next: last && start + 4 < list.length ? last.id : null });
    },
  };
};
