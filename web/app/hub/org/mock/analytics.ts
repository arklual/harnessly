// Fixtures for Аналитика: the numbers of the board «Хаб» (tenant «Продукт», 19–25 окт). The only place with made-up
// analytics numbers. Series are deterministic, so screenshots and tests are stable.
import type {
  AnalyticsApi,
  AnalyticsData,
  AnalyticsFrame,
  AnalyticsQuery,
  Change,
  Kpi,
  KpiChart,
  KpiId,
  Peers,
  SeriesLine,
  Signal,
  Waste,
  Week,
} from '../api/analytics';
import { addDays, daysBetween } from '../dates';
import { later, type MockCtx } from './ctx';

/** The Monday of the current week of the fixtures; the weeks before it are the choices. */
const CURRENT_MONDAY = '2026-10-26';
const WEEKS = 12;

const weekOf = (monday: string): Week => ({ from: monday, to: addDays(monday, 6) });

const weeks = (): Week[] =>
  Array.from({ length: WEEKS }, (_, i) => weekOf(addDays(CURRENT_MONDAY, (i - (WEEKS - 1)) * 7)));

const LAST_CHANGE = { id: 'ch-sonnet', title: 'Субагенты на Sonnet', at: '2026-10-09' };

const hash = (s: string): number => {
  let h = 2166136261;
  for (const c of s) h = Math.imul(h ^ c.charCodeAt(0), 16777619);
  return h >>> 0;
};
/** A repeatable 0..1 for (seed, i). */
const rnd = (seed: string, i: number): number => (hash(`${seed}:${i}`) % 10_000) / 10_000;

/** A line around `base`, drifting by `trend` over `n` points, with noise `amp` (a share of base). */
const wave = (seed: string, n: number, base: number, trend: number, amp: number): number[] =>
  Array.from({ length: n }, (_, i) => {
    const t = n > 1 ? i / (n - 1) : 0;
    const swing = Math.sin(i * 0.9 + (hash(seed) % 7)) * 0.4 + (rnd(seed, i) - 0.5);
    return Math.max(0, base * (1 + trend * (t - 0.5)) * (1 + swing * amp));
  });

interface KpiSeed extends Omit<Kpi, 'series' | 'prevSeries' | 'value' | 'prev'> {
  value: number;
  prev: number;
  trend: number;
  amp: number;
  /** The screen shows a flat line (a start that does not move). */
  flat?: boolean;
}

const KPI_SEEDS: KpiSeed[] = [
  {
    id: 'moneyPerMr',
    group: 'result',
    title: 'Деньги на MR',
    hint: 'Вся работа агентов, делённая на смёрженные MR. Падает — результат дешевле.',
    format: 'usd',
    delta: 'rel',
    better: 'down',
    noise: 3,
    value: 18.5,
    prev: 21,
    trend: -0.3,
    amp: 0.12,
  },
  {
    id: 'yield',
    group: 'result',
    title: 'Доходимость',
    hint: 'Какая доля чатов дошла до результата: MR, правка или ответ, которым пользуются.',
    format: 'pct',
    delta: 'pts',
    better: 'up',
    noise: 2,
    value: 64,
    prev: 59,
    trend: 0.2,
    amp: 0.07,
  },
  {
    id: 'timeToMr',
    group: 'result',
    title: 'Время до MR',
    hint: 'От первого сообщения до открытого MR. Падает — быстрее.',
    format: 'minutes',
    delta: 'rel',
    better: 'down',
    noise: 5,
    value: 130,
    prev: 153,
    trend: -0.25,
    amp: 0.14,
  },
  {
    id: 'mrPerPerson',
    group: 'result',
    title: 'MR на человека',
    hint: 'Смёрженные MR на человека за месяц.',
    format: 'perMonth',
    delta: 'rel',
    better: 'up',
    noise: 3,
    value: 6.6,
    prev: 6.1,
    trend: 0.2,
    amp: 0.1,
  },
  {
    id: 'autonomy',
    group: 'agent',
    title: 'Автономность',
    hint: 'Сколько агент работает, пока человек не вмешался. Растёт — агенту доверяют больше.',
    format: 'minutes',
    delta: 'rel',
    better: 'up',
    noise: 3,
    value: 38,
    prev: 35,
    trend: 0.25,
    amp: 0.15,
  },
  {
    id: 'waiting',
    group: 'agent',
    title: 'Ждёт человека',
    hint: 'Как долго агент стоит и ждёт ответа. Падает — меньше простоя.',
    format: 'minutes',
    delta: 'rel',
    better: 'down',
    noise: 5,
    value: 50,
    prev: 49,
    trend: 0.05,
    amp: 0.3,
  },
  {
    id: 'parallel',
    group: 'agent',
    title: 'Параллельность',
    hint: 'Сколько сессий один человек ведёт одновременно.',
    format: 'count',
    delta: 'abs',
    better: 'up',
    noise: 0.1,
    value: 2.4,
    prev: 2.1,
    trend: 0.3,
    amp: 0.1,
  },
  {
    id: 'toolErrors',
    group: 'agent',
    title: 'Ошибки инструментов',
    hint: 'Доля вызовов инструментов, которые закончились ошибкой. Падает — лучше.',
    format: 'pct',
    delta: 'pts',
    better: 'down',
    noise: 0.3,
    value: 3.4,
    prev: 2.6,
    trend: 0.5,
    amp: 0.2,
  },
  {
    id: 'reread',
    group: 'env',
    title: 'Перечитанный контекст',
    hint: 'Доля токенов, которые агент читает повторно в длинной сессии.',
    format: 'pct',
    delta: 'pts',
    better: 'down',
    noise: 2,
    value: 31,
    prev: 35,
    trend: -0.15,
    amp: 0.05,
  },
  {
    id: 'sessionStart',
    group: 'env',
    title: 'Старт сессии',
    hint: 'Сколько токенов съедает окружение до первого вопроса.',
    format: 'tokens',
    delta: 'rel',
    better: 'down',
    noise: 5,
    value: 23_000,
    prev: 50_000,
    trend: 0,
    amp: 0.004,
    flat: true,
  },
  {
    id: 'cache',
    group: 'env',
    title: 'Кэш',
    hint: 'Доля токенов, которые взяты из кэша.',
    format: 'pct',
    delta: 'pts',
    better: 'up',
    noise: 1,
    value: 88,
    prev: 85,
    trend: 0.1,
    amp: 0.02,
  },
  {
    id: 'skills',
    group: 'env',
    title: 'Скиллы в ходу',
    hint: 'Доля установленных скиллов, которые хоть раз сработали за период.',
    format: 'pct',
    delta: 'pts',
    better: 'up',
    noise: 2,
    value: 35,
    prev: 31,
    trend: 0.15,
    amp: 0.03,
  },
];

const POINTS = 30;

const kpisFor = (q: AnalyticsQuery): Kpi[] =>
  KPI_SEEDS.map(({ trend, amp, flat, ...k }) => {
    const seed = `${k.id}:${q.period.from}`;
    const series = flat
      ? wave(seed, POINTS, k.value, 0, amp).map((v, i) => (i < 2 ? k.prev : v))
      : wave(seed, POINTS, k.value, trend, amp);
    const prevSeries = wave(seed + 'p', POINTS, k.prev, 0, amp);
    return { ...k, series, prevSeries };
  });

const SIGNALS: Signal[] = [
  {
    id: 's-mcp',
    tone: 'high',
    title: 'MCP «Трекер задач» ломается в 11% вызовов',
    text: 'с 21.10, после обновления до 2.3 — агенты повторяют вызов, = $310 за период',
    action: { label: 'Смотреть ошибки', section: 'analytics' },
  },
  {
    id: 's-tests',
    tone: 'high',
    title: 'Тесты идут через пайп без pipefail',
    text: '66% прогонов — агент видит зелёный там, где тесты упали; 23 красных изменения ушли в MR',
    action: { label: 'Включить «Ворота по коду возврата»', section: 'analytics' },
  },
  {
    id: 's-design',
    tone: 'mid',
    title: 'Дизайн отвечает агенту за 1 ч 40 мин',
    text: 'вдвое дольше, чем в тенанте — агент всё это время стоит',
    action: { label: 'Смотреть людей', section: 'teams' },
  },
  {
    id: 's-sub',
    tone: 'mid',
    title: '10% запусков субагентов падают',
    text: 'упираются в лимит вложенности — работа идёт заново, больше всего у Фронта',
    action: { label: 'Смотреть сессии', section: 'machines' },
  },
];

const CHANGES: Change[] = [
  {
    id: 'ch-mcp',
    kind: 'tool',
    title: 'MCP «Трекер задач» 2.3',
    at: '2026-10-21',
    meta: 'окружение тенанта · Дима',
    effects: [
      {
        kpi: 'toolErrors',
        title: 'Ошибки инструментов',
        format: 'pct',
        before: 2.9,
        after: 3.4,
        verdict: 'worse',
      },
      { kpi: 'moneyPerMr', title: 'Деньги на MR', format: 'usd', before: 19, after: 18.5, verdict: 'noise' },
      { kpi: 'yield', title: 'Доходимость', format: 'pct', before: 63, after: 64, verdict: 'noise' },
    ],
  },
  {
    id: 'ch-sonnet',
    kind: 'model',
    title: 'Субагенты на Sonnet',
    at: '2026-10-09',
    meta: 'окружение организации · Аня',
    effects: [
      { kpi: 'moneyPerMr', title: 'Деньги на MR', format: 'usd', before: 24, after: 19, verdict: 'better' },
      {
        kpi: 'toolErrors',
        title: 'Ошибки инструментов',
        format: 'pct',
        before: 2.7,
        after: 2.9,
        verdict: 'noise',
      },
      { kpi: 'yield', title: 'Доходимость', format: 'pct', before: 61, after: 62, verdict: 'noise' },
    ],
  },
  {
    id: 'ch-plugins',
    kind: 'plugins',
    title: 'Диета плагинов',
    at: '2026-10-02',
    meta: 'окружение организации · Аня',
    effects: [
      {
        kpi: 'sessionStart',
        title: 'Старт сессии',
        format: 'tokens',
        before: 50_000,
        after: 23_000,
        verdict: 'better',
      },
      {
        kpi: 'reread',
        title: 'Перечитанный контекст',
        format: 'pct',
        before: 35,
        after: 31,
        verdict: 'better',
      },
      { kpi: 'cache', title: 'Кэш', format: 'pct', before: 85, after: 88, verdict: 'better' },
    ],
  },
];

const WASTE: Waste = {
  wasteUsd: 1080,
  totalUsd: 3450,
  items: [
    {
      id: 'w-abandoned',
      title: 'Брошенные сессии',
      text: 'сессии без MR и без вывода: 36% таких — во «Фронте»',
      usd: 370,
      tone: 'violet',
    },
    {
      id: 'w-retry',
      title: 'Повторы после ошибок',
      text: 'сломанный вызов → агент повторяет. С поправкой на охват: ошибка у 18 человек весит больше, чем у трёх',
      usd: 310,
      tone: 'rose',
    },
    {
      id: 'w-long',
      title: 'Длинные сессии после 250K',
      text: 'каждый шаг перечитывает весь разговор — новая сессия обошлась бы в 3–4 раза дешевле',
      usd: 260,
      tone: 'amber',
    },
    {
      id: 'w-skills',
      title: 'Лишние скиллы в старте',
      text: '41 скилл ни разу не загружался, но его описание едет в каждую сессию',
      usd: 140,
      tone: 'teal',
    },
  ],
};

const TEAM_PEERS: Peers = {
  title: 'Команды рядом',
  sub: 'Каждая клетка — сравнение команды со средним по тенанту за период',
  columns: [
    'Деньги на MR',
    'Доходимость',
    'Время до MR',
    'Автономность',
    'Ошибки',
    'Ждёт человека',
    'Перечитано',
    'Кэш',
  ],
  rows: [
    {
      id: 'back',
      name: 'Бэкенд',
      cells: [
        { text: '$14,2', score: 1 },
        { text: '74%', score: 1 },
        { text: '1 ч 48', score: 0.5 },
        { text: '47 мин', score: 1 },
        { text: '2,1%', score: 0.5 },
        { text: '35 мин', score: 0.5 },
        { text: '29%', score: 0.5 },
        { text: '91%', score: 0.5 },
      ],
    },
    {
      id: 'front',
      name: 'Фронт',
      cells: [
        { text: '$19,8', score: 0 },
        { text: '61%', score: 0 },
        { text: '2 ч 20', score: 0 },
        { text: '36 мин', score: 0 },
        { text: '4,6%', score: -1 },
        { text: '41 мин', score: 0 },
        { text: '34%', score: -0.5 },
        { text: '87%', score: 0 },
      ],
    },
    {
      id: 'design',
      name: 'Дизайн',
      cells: [
        { text: '$23,1', score: -0.5 },
        { text: '81%', score: 1 },
        { text: '2 ч 50', score: -0.5 },
        { text: '26 мин', score: -0.5 },
        { text: '2,9%', score: 0 },
        { text: '1 ч 40', score: -1 },
        { text: '27%', score: 0.5 },
        { text: '85%', score: -0.5 },
      ],
    },
    {
      id: 'none',
      name: 'Без команды',
      cells: [
        { text: '$27,4', score: -1 },
        { text: '52%', score: -1 },
        { text: '3 ч 05', score: -1 },
        { text: '29 мин', score: -0.5 },
        { text: '3,8%', score: -0.5 },
        { text: '52 мин', score: -0.5 },
        { text: '36%', score: -1 },
        { text: '83%', score: -1 },
      ],
    },
  ],
};

const PEOPLE_PEERS: Peers = {
  title: 'Люди рядом',
  sub: 'Каждая клетка — сравнение человека со средним по команде за период',
  columns: TEAM_PEERS.columns,
  rows: [
    ['Аня', 1],
    ['Глеб', 0],
    ['Олег', -1],
    ['Паша', 0.5],
    ['Глеб', -0.5],
  ].map(([name, s], i) => ({
    id: String(name),
    name: String(name),
    cells: (TEAM_PEERS.rows[0]?.cells ?? []).map((c, j) => ({
      text: c.text,
      score: Math.max(-1, Math.min(1, Number(s) * (j % 2 === 0 ? 1 : -0.5) + (i % 2 ? 0 : 0.0))),
    })),
  })),
};

const LINES: { id: string; name: string; share: number }[] = [
  { id: 'front', name: 'Фронт', share: 0.38 },
  { id: 'back', name: 'Бэкенд', share: 0.31 },
  { id: 'design', name: 'Дизайн', share: 0.2 },
  { id: 'none', name: 'Без команды', share: 0.11 },
];
const PEOPLE_LINES = [
  { id: 'anya', name: 'Аня', share: 0.3 },
  { id: 'vika', name: 'Вика', share: 0.25 },
  { id: 'oleg', name: 'Олег', share: 0.25 },
  { id: 'pasha', name: 'Паша', share: 0.2 },
];

/** Ten weeks ending with the period, so the pickers' ranges and the minimap have something to show. */
const CHART_DAYS = 70;

const chartFor = (ctx: MockCtx, q: AnalyticsQuery, kpiId: KpiId): KpiChart => {
  const seed = KPI_SEEDS.find((k) => k.id === kpiId);
  const first = addDays(q.period.to, -(CHART_DAYS - 1));
  const days = Array.from({ length: CHART_DAYS }, (_, i) => addDays(first, i));
  const base = seed?.value ?? 1;
  const trend = seed?.trend ?? 0;
  const amp = seed?.amp ?? 0.1;
  const spec =
    ctx.role === 'admin' ? LINES : ctx.role === 'lead' ? PEOPLE_LINES : [{ id: 'me', name: 'Мои', share: 1 }];
  // The lines add up to the KPI's level for the stacked view: each is its share of it.
  const lines: SeriesLine[] = spec.map((l) => ({
    id: l.id,
    name: l.name,
    values: wave(`${kpiId}:${l.id}`, CHART_DAYS, base * l.share, trend, amp).map(
      (v) => Math.round(v * 100) / 100,
    ),
  }));
  const len = daysBetween(q.period.from, q.period.to) + 1;
  const periodFrom = days.indexOf(q.period.from);
  const compareLine = wave(`${kpiId}:cmp:${q.compare.from}`, len, seed?.prev ?? base, 0, amp).map(
    (v) => Math.round(v * 100) / 100,
  );
  const markers = CHANGES.map((c) => ({
    id: c.id,
    kind: c.kind,
    title: c.title,
    index: days.indexOf(c.at),
  })).filter((m) => m.index >= 0);
  return {
    kpi: kpiId,
    days,
    lines,
    compareLine,
    periodFrom: Math.max(0, periodFrom),
    periodTo: CHART_DAYS - 1,
    markers,
  };
};

export const analyticsMock = (ctx: MockCtx): AnalyticsApi => ({
  analyticsFrame: () => {
    const all = weeks();
    const defaultPeriod = all[all.length - 2] ?? weekOf(CURRENT_MONDAY);
    const frame: AnalyticsFrame = { weeks: all, defaultPeriod, lastChange: LAST_CHANGE };
    return later(frame);
  },
  analytics: (q) => {
    const member = ctx.role === 'member';
    const lead = ctx.role === 'lead';
    const data: AnalyticsData = {
      scopeLabel: member ? 'Только твоя работа' : lead ? 'Команда Фронт' : 'Все команды тенанта',
      summary: member
        ? { spendUsd: 118, sessions: 25, mrs: 5 }
        : lead
          ? { spendUsd: 310, sessions: 96, mrs: 14 }
          : { spendUsd: 810, sessions: 302, mrs: 44 },
      kpis: kpisFor(q),
      signals: member ? [] : lead ? SIGNALS.slice(0, 3) : SIGNALS,
      changes: CHANGES,
      waste: member
        ? null
        : lead
          ? { ...WASTE, wasteUsd: 360, totalUsd: 1180, items: WASTE.items.slice(0, 3) }
          : WASTE,
      peers: member ? null : lead ? PEOPLE_PEERS : TEAM_PEERS,
    };
    return later(data, 400);
  },
  analyticsChart: (q, kpi) => later(chartFor(ctx, q, kpi), 450),
});
