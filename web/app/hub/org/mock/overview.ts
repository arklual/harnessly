// Mock numbers of Обзор: the board «Хаб», scenes Обзор for the admin and the lead.
import type {
  ActivePerson,
  OverviewApi,
  OverviewData,
  PersonSpend,
  Signal,
  SpendCut,
  SpendDetail,
  SpendLine,
  SpendSpan,
} from '../api/overview';
import { later, type MockCtx } from './ctx';

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const RATE = 90;

const money = (rub: number, tokens: number, usd = Math.round(rub / RATE)) => ({ rub, usd, tokens });

const SIGNALS: Signal[] = [
  {
    id: 's-budget',
    severity: 'critical',
    title: 'Фронт: бюджет кончится 26 октября',
    detail: 'потрачено 110 из 120 тыс ₽ — темп выше обычного',
    action: { label: 'Поднять лимит или урезать', section: 'teams', query: 't=tm-front&dlg=limit' },
  },
  {
    id: 's-tool',
    severity: 'critical',
    title: 'MCP «Трекер задач»: 11% вызовов с ошибкой',
    detail: 'агенты повторяют вызов — лишние токены и время',
    action: { label: 'Смотреть ошибки', section: 'analytics', query: 'k=toolErrors' },
  },
  {
    id: 's-window',
    severity: 'warn',
    title: 'Claude Max №2: окно 87%',
    detail: 'сбросится в понедельник — агенты Фронта встанут раньше',
    action: { label: 'Перекинуть на №1', section: 'accounts', query: 'a=a2' },
  },
  {
    id: 's-idle',
    severity: 'warn',
    title: '5 человек не заходили 7 дней',
    detail: 'Маша Ким так и не приняла приглашение',
    action: { label: 'Смотреть людей', section: 'teams' },
  },
  {
    id: 's-unknown',
    severity: 'warn',
    title: 'Незнакомый аккаунт Anthropic',
    detail: 'на машине Глеба работает аккаунт, которого нет в списке организации',
    action: { label: 'Зарегистрировать', section: 'accounts', query: 'a=a-found&dlg=register' },
  },
];

/** Signals put off in this session of the mock. */
const snoozed = new Set<string>();

const SPAN_K: Record<SpendSpan, number> = { day: 0.035, week: 0.24, month: 1 };

const CUTS: Record<SpendCut, { key: string; label: string; rub: number; tokens: number }[]> = {
  team: [
    { key: 'tm-front', label: 'Фронт', rub: 118000, tokens: 410_000_000 },
    { key: 'tm-back', label: 'Бэкенд', rub: 96000, tokens: 380_000_000 },
    { key: 'tm-design', label: 'Дизайн', rub: 54000, tokens: 520_000_000 },
    { key: '', label: 'Без команды', rub: 44000, tokens: 150_000_000 },
  ],
  person: [
    { key: 'u-oleg', label: 'Олег', rub: 27900, tokens: 120_000_000 },
    { key: 'u-misha', label: 'Миша', rub: 18600, tokens: 80_000_000 },
    { key: 'u-anya', label: 'Аня', rub: 17500, tokens: 76_000_000 },
    { key: 'u-andrey', label: 'Андрей', rub: 12700, tokens: 55_000_000 },
    { key: 'u-gleb', label: 'Глеб', rub: 12300, tokens: 52_000_000 },
    { key: 'u-vika', label: 'Вика', rub: 1700, tokens: 6_000_000 },
  ],
  model: [
    { key: 'claude-opus-5-5', label: 'claude-opus-5-5', rub: 201000, tokens: 820_000_000 },
    { key: 'claude-sonnet-5-5', label: 'claude-sonnet-5-5', rub: 74000, tokens: 460_000_000 },
    { key: 'gpt-5.5-codex', label: 'gpt-5.5-codex', rub: 37000, tokens: 180_000_000 },
    { key: 'glm-5', label: 'glm-5', rub: 0, tokens: 12_000_000 },
  ],
  account: [
    { key: 'a2', label: 'Claude Max №2', rub: 18000, tokens: 610_000_000 },
    { key: 'a1', label: 'Claude Max №1', rub: 18000, tokens: 540_000_000 },
    { key: 'a3', label: 'Codex Pro', rub: 18000, tokens: 260_000_000 },
    { key: 'a-found', label: 'anthropic · gleb@…', rub: 0, tokens: 50_000_000 },
  ],
};

const spendMock = (by: SpendCut, span: SpendSpan): SpendDetail => {
  const k = SPAN_K[span];
  const rows: SpendLine[] = CUTS[by].map((x) => ({
    key: x.key,
    label: x.label,
    rub: Math.round(x.rub * k),
    usd: Math.round((x.rub * k) / RATE),
    listRub: Math.round(x.rub * k * 1.6),
    estimated: false,
    tokens: Math.round(x.tokens * k),
    calls: Math.round((x.tokens * k) / 40_000),
    unpricedCalls: x.key === 'glm-5' ? 310 : 0,
    limit:
      by === 'team' && x.key === 'tm-front'
        ? { rub: 120000, pct: Math.round((x.rub * k * 100) / 120000), status: 'warn' }
        : null,
    forecastRub: span === 'day' ? null : Math.round(x.rub * k * 1.3),
  }));
  const rub = rows.reduce((n, x) => n + x.rub, 0);
  return {
    by,
    span,
    total: {
      rub,
      usd: Math.round(rub / RATE),
      listRub: Math.round(rub * 1.6),
      estimated: false,
      tokens: rows.reduce((n, x) => n + x.tokens, 0),
      calls: rows.reduce((n, x) => n + x.calls, 0),
    },
    limit: null,
    forecastRub: span === 'day' ? null : Math.round(rub * 1.3),
    rows,
    accountedUntil: new Date(Date.now() - 4 * 60_000).toISOString(),
  };
};

const people = (now: number): PersonSpend[] => [
  {
    userId: 'u-misha',
    name: 'Миша',
    isLead: false,
    lastActiveAt: new Date(now - DAY).toISOString(),
    invited: false,
    spentRub: 18600,
    limitRub: 20000,
  },
  {
    userId: 'u-oleg',
    name: 'Олег',
    isLead: false,
    lastActiveAt: new Date(now - 3 * HOUR).toISOString(),
    invited: false,
    spentRub: 27900,
    limitRub: 30000,
  },
  {
    userId: 'u-anya',
    name: 'Аня',
    isLead: true,
    lastActiveAt: new Date(now - 3 * HOUR).toISOString(),
    invited: false,
    spentRub: 17500,
    limitRub: 20000,
  },
  {
    userId: 'u-gleb',
    name: 'Глеб',
    isLead: false,
    lastActiveAt: new Date(now - HOUR).toISOString(),
    invited: false,
    spentRub: 12300,
    limitRub: 15000,
  },
  {
    userId: 'u-andrey',
    name: 'Андрей',
    isLead: false,
    lastActiveAt: new Date(now - 3 * HOUR).toISOString(),
    invited: false,
    spentRub: 12700,
    limitRub: 30000,
  },
  {
    userId: 'u-pasha',
    name: 'Паша',
    isLead: false,
    lastActiveAt: new Date(now - DAY).toISOString(),
    invited: false,
    spentRub: 12100,
    limitRub: 30000,
  },
  {
    userId: 'u-timur',
    name: 'Тимур',
    isLead: false,
    lastActiveAt: new Date(now - 2 * DAY).toISOString(),
    invited: false,
    spentRub: 7200,
    limitRub: 20000,
  },
  {
    userId: 'u-vika',
    name: 'Вика',
    isLead: false,
    lastActiveAt: new Date(now - 9 * DAY).toISOString(),
    invited: false,
    spentRub: 1700,
    limitRub: 20000,
  },
  {
    userId: 'u-masha',
    name: 'Маша Ким',
    isLead: false,
    lastActiveAt: null,
    invited: true,
    spentRub: 0,
    limitRub: 15000,
  },
];

// The last 14 days, oldest first: the shapes of the board's tiles.
const BARS = {
  spend: [9, 11, 10, 12, 14, 13, 12, 15, 14, 16, 15, 17, 16, 14],
  active: [4, 5, 5, 6, 6, 5, 7, 6, 5, 6, 7, 6, 6, 6],
  tasks: [5, 6, 7, 6, 8, 7, 9, 8, 9, 10, 9, 11, 10, 14],
};

const ACTIVE_WHO = (now: number): ActivePerson[] => [
  {
    userId: 'u-anna',
    name: 'Анна Белова',
    email: 'anna@example.org',
    working: true,
    chats: 4,
    closed: 2,
    lastAt: new Date(now - 60_000).toISOString(),
    rub: 1900,
    listRub: 7400,
    tokens: 9_400_000,
  },
  {
    userId: 'u-ivan',
    name: 'Иван Орлов',
    email: 'ivan@example.org',
    working: false,
    chats: 2,
    closed: 1,
    lastAt: new Date(now - 3 * 3600_000).toISOString(),
    rub: 640,
    listRub: 2300,
    tokens: 3_100_000,
  },
  {
    userId: 'u-masha',
    name: 'Маша Ким',
    email: 'masha@example.org',
    working: false,
    chats: 0,
    closed: 0,
    lastAt: null,
    rub: 0,
    listRub: 0,
    tokens: 0,
  },
  ...(
    [
      ['u-dima', 'Дима Соколов', true, 5, 3, 6100],
      ['u-lena', 'Лена Морозова', false, 3, 1, 3800],
      ['u-pasha', 'Паша Егоров', false, 1, 0, 900],
      ['u-oleg', 'Олег Титов', false, 2, 1, 1700],
      ['u-liza', 'Лиза Гордеева', false, 0, 0, 0],
    ] as const
  ).map(([userId, name, working, chats, closed, listRub], i): ActivePerson => ({
    userId,
    name,
    email: `${userId.slice(2)}@example.org`,
    working,
    chats,
    closed,
    lastAt:
      chats > 0 || working
        ? new Date(now - (i + 1) * 47 * 60_000).toISOString()
        : new Date(now - 4 * 86400_000).toISOString(),
    rub: Math.round(listRub / 4),
    listRub,
    tokens: listRub * 1300,
  })),
];

const activeOf = (who: ActivePerson[]) => ({
  now: who.filter((p) => p.working || p.chats > 0).length,
  total: who.length,
  bars: BARS.active,
  who,
});

export const overviewMock = (ctx: MockCtx): OverviewApi => ({
  snoozeSignal: (id) => {
    snoozed.add(id);
    return later(undefined);
  },
  unsnoozeSignal: (id) => {
    snoozed.delete(id);
    return later(undefined);
  },
  overview: async (_tenant, period) => {
    const now = Date.now();
    const k = period === 'week' ? 0.24 : 1;
    const data: OverviewData = {
      estimated: false,
      tiles: {
        spend: { rub: 10400, usd: 115, tokens: 48_000_000, bars: BARS.spend },
        active: activeOf(ACTIVE_WHO(now)),
        tasks: { closed: 14, inProgress: 6, bars: BARS.tasks },
        subscriptions: { accounts: 3, worstName: '№2', worstPct: 87 },
      },
      signals: SIGNALS.filter((x) => !snoozed.has(x.id)),
      subscriptions: [
        { id: 'a2', name: 'Claude Max №2', teams: ['Фронт', 'Дизайн'], resetsOn: 'пн', usedPct: 87 },
        { id: 'a1', name: 'Claude Max №1', teams: ['Бэкенд'], resetsOn: 'ср', usedPct: 62 },
        { id: 'a3', name: 'Codex Pro', teams: ['Фронт', 'Бэкенд'], resetsOn: 'пт', usedPct: 40 },
      ],
      byTeam:
        ctx.role === 'admin'
          ? {
              total: money(Math.round(312000 * k), Math.round(1_460_000_000 * k), Math.round(3450 * k)),
              teams: [
                {
                  id: 'tm-front',
                  name: 'Фронт',
                  ...money(Math.round(118000 * k), Math.round(410_000_000 * k), Math.round(1300 * k)),
                },
                {
                  id: 'tm-back',
                  name: 'Бэкенд',
                  ...money(Math.round(96000 * k), Math.round(380_000_000 * k), Math.round(1060 * k)),
                },
                {
                  id: 'tm-design',
                  name: 'Дизайн',
                  ...money(Math.round(54000 * k), Math.round(520_000_000 * k), Math.round(600 * k)),
                },
                {
                  id: 'none',
                  name: 'Без команды',
                  noTeam: true,
                  ...money(Math.round(44000 * k), Math.round(150_000_000 * k), Math.round(490 * k)),
                },
              ],
              efficiency: [
                { id: 'tm-back', name: 'Бэкенд', tasksPerPerson: 8.1, resultPct: 74 },
                { id: 'tm-front', name: 'Фронт', tasksPerPerson: 7.4, resultPct: 69 },
                { id: 'tm-design', name: 'Дизайн', tasksPerPerson: 5.2, resultPct: 81 },
                { id: 'none', name: 'Без команды', noTeam: true, tasksPerPerson: 3.0, resultPct: 52 },
              ],
            }
          : null,
      byPerson:
        ctx.role === 'lead'
          ? { teamName: 'Фронт', spentRub: 110000, limitRub: 120000, people: people(now) }
          : null,
    };
    return later(data);
  },
  spendDetail: async (by, span) => later(spendMock(by, span)),
});
