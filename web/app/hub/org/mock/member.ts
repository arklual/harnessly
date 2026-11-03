// Mock numbers of the participant's view: the board «Хаб», scene Обзор, role Участник.
import type { MemberApi, MyOverview } from '../api/member';
import { later, type MockCtx } from './ctx';

const MIN = 60_000;
const HOUR = 60 * MIN;

export const memberMock = (_ctx: MockCtx): MemberApi => ({
  myOverview: async () => {
    const now = Date.now();
    const at = (ms: number) => new Date(now - ms).toISOString();
    const breakdown: MyOverview['breakdown'] = [
      { label: 'Opus 5.5', kind: 'model', rub: 8900 },
      { label: 'Sonnet 5.5', kind: 'model', rub: 3100 },
      { label: 'Haiku 5.5', kind: 'model', rub: 400 },
      { label: 'Машина andrey-main', kind: 'machine', rub: 300 },
    ];
    const data: MyOverview = {
      estimated: false,
      spentRub: 12700,
      breakdown,
      limit: {
        spentRub: 12700,
        limitRub: 30000,
        forecastRub: 13400,
        warnPct: 80,
        mode: 'stop',
        setBy: { userId: 'u-anya', name: 'Аня' },
        // The limit is monthly: it starts over on the 1st.
        resetsAt: new Date(new Date(now).getFullYear(), new Date(now).getMonth() + 1, 1).toISOString(),
        breakdown,
      },
      chats: [
        {
          id: 'c1',
          title: 'Форма оплаты: валидация телефона',
          machine: 'andrey-main',
          end: 'running',
          mr: null,
          endedAt: null,
          costUsd: 4.2,
        },
        {
          id: 'c2',
          title: 'Каталог: фильтр по дате',
          machine: 'andrey-main',
          end: 'running',
          mr: null,
          endedAt: null,
          costUsd: 2.8,
        },
        {
          id: 'c3',
          title: 'Тесты на корзину',
          machine: 'andrey-main',
          end: 'running',
          mr: null,
          endedAt: at(12 * MIN),
          costUsd: 1.1,
        },
        {
          id: 'c4',
          title: 'Шапка: моб. меню на iOS',
          machine: 'andrey-main',
          end: 'merged',
          mr: '!412',
          endedAt: at(2 * HOUR),
          costUsd: 6.4,
        },
        {
          id: 'c5',
          title: 'Скелетоны на странице события',
          machine: 'andrey-main',
          end: 'review',
          mr: '!409',
          endedAt: at(26 * HOUR),
          costUsd: 3.9,
        },
        {
          id: 'c6',
          title: 'Перенос дат в ISO',
          machine: 'andrey-main',
          end: 'abandoned',
          mr: null,
          endedAt: at(27 * HOUR),
          costUsd: 7.7,
        },
      ],
      tiles: {
        chats: { week: 25, running: 3, bars: [3, 4, 5, 2, 0, 1, 4, 5, 3, 6, 4, 1, 0, 5] },
        mr: { week: 5, merged: 4, bars: [1, 0, 1, 0, 0, 0, 1, 1, 0, 2, 1, 0, 0, 1] },
        machine: {
          name: 'andrey-main',
          online: true,
          loadPct: 41,
          bars: [15, 21, 27.5, 19, 6, 4, 23.5, 30.5, 26, 22, 19.5, 5, 3, 20.5],
        },
      },
    };
    return later(data);
  },
  requestMoreLimit: () => later({ sent: true as const }, 700),
});
