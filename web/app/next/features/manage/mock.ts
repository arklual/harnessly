// The only place of «Управление» with made-up numbers: the hub's answers for the gallery and the tests.
import type { ManageHub } from '@/next/features/manage/api';
import type { HubMe } from '@/next/features/manage/hub-types';

const DAY = 86_400_000;

export const mockMe = (now = Date.now()): HubMe => {
  const days = Array.from({ length: 30 }, (_, i) => ({
    day: new Date(now - (29 - i) * DAY).toISOString().slice(0, 10),
    cost: Math.round(180 + 120 * Math.sin(i / 3) + (i % 7) * 40),
  }));
  const at = new Date(now - 12 * 60_000).toISOString();
  return {
    limits: {
      currency: 'RUB',
      measuredUntil: at,
      askTo: { name: 'Аня' },
      pendingRequest: null,
      limits: [
        {
          id: 'l-person',
          axis: 'person',
          label: 'Мой лимит на месяц',
          used: 12_700,
          amount: 30_000,
          warnPct: 80,
          setBy: { name: 'Аня', role: 'лид команды' },
          resetsAt: new Date(now + 9 * DAY).toISOString(),
          effective: true,
        },
        {
          id: 'l-team',
          axis: 'team',
          label: 'Команда Фронт',
          used: 110_000,
          amount: 120_000,
          warnPct: 80,
          setBy: { name: 'Андрей', role: 'админ тенанта' },
          resetsAt: new Date(now + 9 * DAY).toISOString(),
          effective: false,
        },
      ],
    },
    usage: {
      currency: 'RUB',
      measuredUntil: at,
      days,
      byModel: [
        { model: 'Opus 5.5', cost: 8_900 },
        { model: 'Sonnet 5.5', cost: 3_100 },
        { model: 'Haiku 5.5', cost: 400 },
      ],
      machineCost: 300,
      forecast: 17_400,
    },
    sessions: {
      measuredUntil: at,
      sessions: [
        { key: 's-1', endedAt: null, cost: 4_200, machine: 'dev-main', outcome: 'running' },
        {
          key: 's-2',
          endedAt: new Date(now - 3 * 3_600_000).toISOString(),
          cost: 6_400,
          machine: 'dev-main',
          outcome: 'mr_merged',
        },
      ],
    },
    kpis: {
      measuredUntil: at,
      kpis: [
        { id: 'moneyPerMr', value: 1_850, previous: 2_100 },
        { id: 'yield', value: 0.64, previous: 0.59 },
        { id: 'timeToMr', value: 130, previous: 153 },
        { id: 'mrPerMonth', value: 6.6, previous: 6.1 },
      ],
    },
  };
};

export const mockHub = (): ManageHub => {
  let asked = false;
  const later = <T>(v: T): Promise<T> => new Promise((r) => setTimeout(() => r(v), 350));
  return {
    load: async () => {
      const me = mockMe();
      return later(
        asked ? { ...me, limits: { ...me.limits, pendingRequest: { at: new Date().toISOString() } } } : me,
      );
    },
    askMore: async () => {
      await later(null);
      asked = true;
    },
  };
};
