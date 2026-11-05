// Fixtures for Аналитика: the numbers of the board «Хаб» (tenant «Продукт», 19–25 окт). The only place with made-up
// analytics numbers. Series are deterministic, so screenshots and tests are stable.
import type {
  AnalyticsApi,
  AnalyticsData,
  AnalyticsFrame,
  AnalyticsQuery,
  Change,
  FrameWeek,
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
import { BOARD_CURVES } from './analytics-board';
import { monthPeriod } from '../api/analytics';
import { KPI_COPY } from '../api/kpi-copy';
import { later, type MockCtx } from './ctx';

/** The Monday of the current (unfinished) week of the fixtures; the weeks before it are the choices. */
const CURRENT_MONDAY = '2026-10-26';
/** The last day of the fixtures: the week of 26 Oct is still going. */
const TODAY = '2026-10-30';
const WEEKS = 13;
/** What the agents cost in each week, oldest first: the bars of the picker. */
const WEEK_SPEND = [610, 640, 590, 700, 720, 760, 740, 800, 690, 720, 780, 830, 520];

const weekOf = (monday: string): Week => ({ from: monday, to: addDays(monday, 6) });

const weeks = (): FrameWeek[] =>
  Array.from({ length: WEEKS }, (_, i) => ({
    ...weekOf(addDays(CURRENT_MONDAY, (i - (WEEKS - 1)) * 7)),
    spendUsd: WEEK_SPEND[i] ?? null,
    current: i === WEEKS - 1,
  }));

const LAST_CHANGE = { id: 'ch-sonnet', title: 'Субагенты на Sonnet', at: '2026-10-09' };

interface KpiSeed extends Omit<Kpi, 'series' | 'prevSeries' | 'value' | 'prev'> {
  value: number;
  prev: number;
}

/** The effects name the metrics the same way the tiles do. */
const withCopy = (changes: Change[]): Change[] =>
  changes.map((c) => ({
    ...c,
    effects: c.effects.map((e) => ({ ...e, title: KPI_COPY[e.kpi].title })),
  }));

const KPI_SEEDS: KpiSeed[] = [
  {
    id: 'moneyPerMr',
    group: 'result',
    title: 'Деньги на MR',
    hint: 'Вся работа агентов, делённая на смёрженные MR. Падает — результат дешевле',
    format: 'usd',
    delta: 'rel',
    better: 'down',
    noise: 3,
    value: 18.5,
    prev: 21,
  },
  {
    id: 'yield',
    group: 'result',
    title: 'Доходимость',
    hint: 'Доля сессий, которые дошли до смёрженного MR, а не брошены на полпути',
    format: 'pct',
    delta: 'pts',
    better: 'up',
    noise: 2,
    value: 64,
    prev: 59,
  },
  {
    id: 'timeToMr',
    group: 'result',
    title: 'Время до MR',
    hint: 'Медиана от первого сообщения в сессии до открытого MR',
    format: 'minutes',
    delta: 'rel',
    better: 'down',
    noise: 5,
    value: 130,
    prev: 153,
  },
  {
    id: 'mrPerPerson',
    group: 'result',
    title: 'MR на человека',
    hint: 'Смёрженные MR на одного активного человека за период',
    format: 'perMonth',
    delta: 'rel',
    better: 'up',
    noise: 3,
    value: 6.6,
    prev: 6.1,
  },
  {
    id: 'autonomy',
    group: 'agent',
    title: 'Автономность',
    hint: 'Сколько агент работает сам между двумя вмешательствами человека',
    format: 'minutes',
    delta: 'rel',
    better: 'up',
    noise: 3,
    value: 38,
    prev: 35,
  },
  {
    id: 'waiting',
    group: 'agent',
    title: 'Ждёт человека',
    hint: 'p90 от вопроса агента до ответа человека. Ночь не считается',
    format: 'minutes',
    delta: 'rel',
    better: 'down',
    noise: 5,
    value: 50,
    prev: 49,
  },
  {
    id: 'parallel',
    group: 'agent',
    title: 'Параллельность',
    hint: 'Сколько сессий у человека идёт одновременно в рабочее время',
    format: 'count',
    delta: 'abs',
    better: 'up',
    noise: 0.1,
    value: 2.4,
    prev: 2.1,
  },
  {
    id: 'toolErrors',
    group: 'agent',
    title: 'Ошибки инструментов',
    hint: 'Сломанные вызовы Bash, MCP и прочего: каждый — лишний шаг и повтор',
    format: 'pct',
    delta: 'pts',
    better: 'down',
    noise: 0.3,
    value: 3.4,
    prev: 2.6,
  },
  {
    id: 'reread',
    group: 'env',
    title: 'Перечитанный контекст',
    hint: 'Доля входа модели — старые ответы инструментов, которые она читает на каждом шаге',
    format: 'pct',
    delta: 'pts',
    better: 'down',
    noise: 2,
    value: 31,
    prev: 35,
  },
  {
    id: 'sessionStart',
    group: 'env',
    title: 'Старт сессии',
    hint: 'Токены до первого слова человека: промпт, скиллы, инструменты, память',
    format: 'tokens',
    delta: 'rel',
    better: 'down',
    noise: 5,
    value: 23_000,
    prev: 50_000,
  },
  {
    id: 'cache',
    group: 'env',
    title: 'Кэш',
    hint: 'Доля входа, прочитанная из кэша. Чем выше, тем дешевле каждый шаг',
    format: 'pct',
    delta: 'pts',
    better: 'up',
    noise: 1,
    value: 88,
    prev: 85,
  },
  {
    id: 'skills',
    group: 'env',
    title: 'Скиллы в ходу',
    hint: 'Доля скиллов из листинга, которые хоть раз загружались. Остальные только занимают старт',
    format: 'pct',
    delta: 'pts',
    better: 'up',
    noise: 2,
    value: 35,
    prev: 31,
  },
];

/** The tiles' lines: the board's shapes scaled to the KPI's own level, the period against the comparison. */
const sparkFor = (seed: KpiSeed): { series: number[]; prevSeries: number[] } => {
  const curves = BOARD_CURVES[seed.id];
  const lo = Math.min(seed.value, seed.prev) * 0.7;
  const hi = Math.max(seed.value, seed.prev) * 1.2;
  const at = (n: number): number => lo + n * (hi - lo);
  return { series: curves.sparkNow.map(at), prevSeries: curves.sparkPrev.map(at) };
};

const kpisFor = (): Kpi[] =>
  KPI_SEEDS.map((seed) => {
    return { ...seed, ...KPI_COPY[seed.id], ...sparkFor(seed) };
  });

const SIGNALS: Signal[] = [
  {
    id: 's-mcp',
    tone: 'high',
    title: 'MCP «Трекер задач» ломается в 11% вызовов',
    text: 'с 21.10, после обновления до 2.3 — агенты повторяют вызов, ≈ $310 за период',
    action: { label: 'Смотреть ошибки', section: 'analytics', query: 'k=toolErrors' },
    detail: [
      { label: 'Фронт', value: '14%' },
      { label: 'Бэкенд', value: '9%' },
      { label: 'Дизайн', value: '3%' },
      { label: 'Пользуются', value: '18 человек' },
    ],
  },
  {
    id: 's-tests',
    tone: 'high',
    title: 'Тесты идут через пайп без pipefail',
    text: '66% прогонов — агент видит зелёный там, где тесты упали; 23 красных изменения ушли в MR',
    action: { label: 'Включить «Ворота по коду возврата»', section: 'env' },
    detail: [
      { label: 'Прогонов', value: '9 800' },
      { label: 'Через пайп', value: '6 470' },
      { label: 'Упали, но «зелёные»', value: '≈ 410' },
    ],
  },
  {
    id: 's-design',
    tone: 'mid',
    title: 'Дизайн отвечает агенту за 1 ч 40 мин',
    text: 'вдвое дольше, чем в тенанте — агент всё это время стоит',
    action: { label: 'Смотреть команду', section: 'team', query: 't=tm-design' },
    detail: [
      { label: 'Дизайн', value: '1 ч 40 мин' },
      { label: 'Среднее по тенанту', value: '50 мин' },
      { label: 'Вопросов от агентов', value: '212' },
    ],
  },
  {
    id: 's-sub',
    tone: 'mid',
    title: '10% запусков субагентов падают',
    text: 'упираются в лимит вложенности — работа идёт заново, больше всего у Фронта',
    action: { label: 'Смотреть машины', section: 'machines' },
    detail: [
      { label: 'Запусков', value: '1 400' },
      { label: 'Упали', value: '140' },
      { label: 'Из них лимит вложенности', value: '62%' },
    ],
  },
];

const CHANGES: Change[] = withCopy([
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
]);

/** What a change looks like on the chart: a written edit, a new version of something, a switch of the model. */
const CHANGE_ICON: Record<Change['kind'], 'edit' | 'announce' | 'rocket'> = {
  plugins: 'edit',
  model: 'announce',
  tool: 'rocket',
};

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
      detail: [
        { label: 'Сессий', value: '462' },
        { label: 'Средняя цена', value: '$0,80' },
        { label: 'Чаще всего', value: 'Фронт, Без команды' },
      ],
    },
    {
      id: 'w-retry',
      title: 'Повторы после ошибок',
      text: 'сломанный вызов → агент повторяет. С поправкой на охват: ошибка у 18 человек весит больше, чем у трёх',
      usd: 310,
      tone: 'rose',
      detail: [
        { label: '«Трекер задач»', value: '$190 · 18 человек' },
        { label: 'Субагенты', value: '$70 · 9 человек' },
        { label: 'Браузер', value: '$30 · 7 человек' },
        { label: 'Прочее', value: '$20' },
      ],
    },
    {
      id: 'w-long',
      title: 'Длинные сессии после 250K',
      text: 'каждый шаг перечитывает весь разговор — новая сессия обошлась бы в 3–4 раза дешевле',
      usd: 260,
      tone: 'amber',
      detail: [
        { label: 'Сессий длиннее 250K', value: '118' },
        { label: 'Чаще всего', value: 'Бэкенд — 27% денег' },
      ],
    },
    {
      id: 'w-skills',
      title: 'Лишние скиллы в старте',
      text: '41 скилл ни разу не загружался, но его описание едет в каждую сессию',
      usd: 140,
      tone: 'teal',
      detail: [
        { label: 'Токенов на сессию', value: '≈ 9K' },
        { label: 'Плагинов целиком лишних', value: '6' },
      ],
    },
  ],
};

/** The tenant's own level of each column, as the cell's bubble shows it. */
const TENANT_MEAN = ['$18,5', '64%', '2 ч 10', '38 мин', '3,4%', '50 мин', '31%', '88%'];

const withMean = (
  cells: { text: string; score: number }[],
): { text: string; score: number; mean: string }[] =>
  cells.map((c, i) => ({ ...c, mean: TENANT_MEAN[i] ?? '—' }));

const TEAM_PEERS: Peers = {
  title: 'Команды рядом',
  sub: 'Каждая клетка — сравнение команды со средним по тенанту за период',
  columns: (
    ['moneyPerMr', 'yield', 'timeToMr', 'autonomy', 'toolErrors', 'waiting', 'reread', 'cache'] as const
  ).map((k) => KPI_COPY[k].title),
  rows: [
    {
      id: 'back',
      name: 'Бэкенд',
      cells: withMean([
        { text: '$14,2', score: 1 },
        { text: '74%', score: 1 },
        { text: '1 ч 48', score: 0.5 },
        { text: '47 мин', score: 1 },
        { text: '2,1%', score: 0.5 },
        { text: '35 мин', score: 0.5 },
        { text: '29%', score: 0.5 },
        { text: '91%', score: 0.5 },
      ]),
    },
    {
      id: 'front',
      name: 'Фронт',
      cells: withMean([
        { text: '$19,8', score: 0 },
        { text: '61%', score: 0 },
        { text: '2 ч 20', score: 0 },
        { text: '36 мин', score: 0 },
        { text: '4,6%', score: -1 },
        { text: '41 мин', score: 0 },
        { text: '34%', score: -0.5 },
        { text: '87%', score: 0 },
      ]),
    },
    {
      id: 'design',
      name: 'Дизайн',
      cells: withMean([
        { text: '$23,1', score: -0.5 },
        { text: '81%', score: 1 },
        { text: '2 ч 50', score: -0.5 },
        { text: '26 мин', score: -0.5 },
        { text: '2,9%', score: 0 },
        { text: '1 ч 40', score: -1 },
        { text: '27%', score: 0.5 },
        { text: '85%', score: -0.5 },
      ]),
    },
    {
      id: 'none',
      name: 'Без команды',
      cells: withMean([
        { text: '$27,4', score: -1 },
        { text: '52%', score: -1 },
        { text: '3 ч 05', score: -1 },
        { text: '29 мин', score: -0.5 },
        { text: '3,8%', score: -0.5 },
        { text: '52 мин', score: -0.5 },
        { text: '36%', score: -0.5 },
        { text: '83%', score: -0.5 },
      ]),
    },
  ],
};

/** Order of the team lines in the board's data. */
const TEAM_LINES = [
  { id: 'front', name: 'Фронт' },
  { id: 'back', name: 'Бэкенд' },
  { id: 'design', name: 'Дизайн' },
  { id: 'none', name: 'Без команды' },
] as const;

/** The big chart shows 30 days: 1–30 Oct. */
const CHART_FIRST = '2026-10-01';
const CHART_DAYS = 30;

const chartFor = (ctx: MockCtx, q: AnalyticsQuery, kpiId: KpiId): KpiChart => {
  const curves = BOARD_CURVES[kpiId];
  const days = Array.from({ length: CHART_DAYS }, (_, i) => addDays(CHART_FIRST, i));
  const lines: SeriesLine[] =
    ctx.role === 'member'
      ? [{ id: 'me', name: 'Мои', values: curves.now }]
      : TEAM_LINES.map((l, i) => ({ id: l.id, name: l.name, values: curves.teams[i] ?? [] }));
  const len = daysBetween(q.period.from, q.period.to) + 1;
  const periodFrom = Math.max(0, days.indexOf(q.period.from));
  const markers = CHANGES.map((c) => ({
    id: c.id,
    kind: c.kind,
    title: c.title,
    index: days.indexOf(c.at),
    icon: CHANGE_ICON[c.kind],
    detail: c.meta,
  })).filter((m) => m.index >= 0);
  return {
    kpi: kpiId,
    days,
    lines,
    compareLine: curves.prev.slice(0, len),
    periodFrom,
    periodTo: CHART_DAYS - 1,
    markers,
    total: curves.now,
    previous: curves.prev,
  };
};

export const analyticsMock = (ctx: MockCtx): AnalyticsApi => ({
  analyticsFrame: () => {
    const all = weeks();
    const defaultPeriod = monthPeriod(all);
    const frame: AnalyticsFrame = { weeks: all, today: TODAY, defaultPeriod, lastChange: LAST_CHANGE };
    return later(frame);
  },
  analytics: () => {
    const member = ctx.role === 'member';
    const lead = ctx.role === 'lead';
    const data: AnalyticsData = {
      scopeLabel: member ? 'Только твои чаты' : lead ? 'Команда Фронт' : 'Все команды тенанта',
      summary: member ? { spendUsd: 96, sessions: 41, mrs: 6 } : { spendUsd: 810, sessions: 302, mrs: 44 },
      kpis: kpisFor(),
      signals: member ? [] : SIGNALS,
      changes: CHANGES,
      waste: member ? null : WASTE,
      peers: member ? null : TEAM_PEERS,
    };
    return later(data, 400);
  },
  analyticsChart: (q, kpi) => later(chartFor(ctx, q, kpi), 450),
});
