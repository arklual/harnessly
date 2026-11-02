// Mock numbers of Обзор: the board «Хаб», scenes Обзор for the admin and the lead.
import type { OverviewApi, OverviewData, PersonSpend, Signal } from '../api/overview';
import { later, type MockCtx } from './ctx';

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const RATE = 90;

const money = (rub: number, tokens: number) => ({ rub, usd: Math.round(rub / RATE), tokens });

const SIGNALS: Signal[] = [
  {
    id: 's-budget',
    severity: 'critical',
    title: 'Фронт: бюджет кончится 26 октября',
    detail: 'потрачено 110 из 120 тыс ₽ — темп выше обычного',
    action: { label: 'Поднять лимит или урезать', section: 'teams' },
  },
  {
    id: 's-tool',
    severity: 'critical',
    title: 'MCP «Трекер задач»: 11% вызовов с ошибкой',
    detail: 'агенты повторяют вызов — лишние токены и время',
    action: { label: 'Смотреть ошибки', section: 'analytics' },
  },
  {
    id: 's-window',
    severity: 'warn',
    title: 'Claude Max №2: окно 87%',
    detail: 'сбросится в понедельник — агенты Фронта встанут раньше',
    action: { label: 'Перекинуть на №1', section: 'teams' },
  },
  {
    id: 's-idle',
    severity: 'warn',
    title: '5 человек не заходили 7 дней',
    detail: 'Маша Ким так и не приняла приглашение',
    action: { label: 'Смотреть людей', section: 'teams' },
  },
];

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

const BARS = {
  spend: [5, 6, 6, 7, 6, 8, 8, 7, 9, 9, 10, 10, 11, 8],
  active: [20, 21, 21, 22, 22, 23, 22, 23, 22, 23, 23, 23, 22, 23],
  tasks: [4, 5, 6, 6, 7, 9, 8, 10, 11, 10, 12, 13, 14, 15],
};

export const overviewMock = (ctx: MockCtx): OverviewApi => ({
  overview: async (_tenant, period) => {
    const now = Date.now();
    const k = period === 'week' ? 0.24 : 1;
    const data: OverviewData = {
      estimated: false,
      tiles: {
        spend: { rub: 10400, usd: 115, tokens: 48_000_000, bars: BARS.spend },
        active: { now: 23, total: 28, bars: BARS.active },
        tasks: { closed: 14, inProgress: 6, bars: BARS.tasks },
        subscriptions: { accounts: 3, worstName: '№2', worstPct: 87 },
      },
      signals: SIGNALS,
      subscriptions: [
        { id: 'a2', name: 'Claude Max №2', teams: ['Фронт', 'Дизайн'], resetsOn: 'пн', usedPct: 87 },
        { id: 'a1', name: 'Claude Max №1', teams: ['Бэкенд'], resetsOn: 'ср', usedPct: 62 },
        { id: 'a3', name: 'Codex Pro', teams: ['Фронт', 'Бэкенд'], resetsOn: 'пт', usedPct: 40 },
      ],
      byTeam:
        ctx.role === 'admin'
          ? {
              total: money(Math.round(312000 * k), Math.round(1_400_000_000 * k)),
              teams: [
                {
                  id: 'tm-front',
                  name: 'Фронт',
                  ...money(Math.round(118000 * k), Math.round(530_000_000 * k)),
                },
                {
                  id: 'tm-back',
                  name: 'Бэкенд',
                  ...money(Math.round(96000 * k), Math.round(430_000_000 * k)),
                },
                {
                  id: 'tm-design',
                  name: 'Дизайн',
                  ...money(Math.round(54000 * k), Math.round(240_000_000 * k)),
                },
                {
                  id: 'none',
                  name: 'Без команды',
                  ...money(Math.round(44000 * k), Math.round(200_000_000 * k)),
                },
              ],
              efficiency: [
                { id: 'tm-back', name: 'Бэкенд', tasksPerPerson: 8.1, resultPct: 74 },
                { id: 'tm-front', name: 'Фронт', tasksPerPerson: 7.4, resultPct: 69 },
                { id: 'tm-design', name: 'Дизайн', tasksPerPerson: 5.2, resultPct: 81 },
                { id: 'none', name: 'Без команды', tasksPerPerson: 3.0, resultPct: 52 },
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
});
