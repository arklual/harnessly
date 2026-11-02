// «Управление» on mock data, for the eye and for screenshots: ?dev=manage&hub=mock. Without `hub=mock` the page shows
// the local report alone (no hub). ?theme=light|dark&lang=en|ru. The machine's report is faked here, in the page.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/designly.css';
import '@/next/styles/manage.css';
import '@/next/dev/manage-gallery.css';

import { useEffect } from 'react';
import { pinLocale } from '@/next/i18n';
import { ManageScreen } from '@/next/legacy/screens';
import { useRouter } from '@/platform/router';

const DAY = 86_400_000;
const dayKey = (ago: number) => new Date(Date.now() + 3 * 3_600_000 - ago * DAY).toISOString().slice(0, 10);

const fakeReport = () => {
  const days: Record<string, object> = {};
  const usage: unknown[] = [];
  for (let i = 29; i >= 0; i--) {
    const day = dayKey(i);
    days[day] = {
      prompts: 20 + ((i * 7) % 30),
      sessions: 2 + (i % 4),
      turnMs: 3_600_000 * (1 + (i % 3)),
      tools: 80,
      toolErrors: 2,
      interrupts: 1,
      denials: 0,
      compactions: 0,
      prs: i % 5 === 0 ? 1 : 0,
      hours: Array(24).fill(0),
    };
    usage.push([day, 'claude', 'claude-opus-5-5', 'main', 1000, 500, 8000, 2000, 2 + ((i * 3) % 9), 12]);
    usage.push([day, 'claude', 'claude-sonnet-5-5', 'main', 800, 300, 6000, 1000, 0.6 + (i % 3) * 0.2, 9]);
  }
  const now = Date.now();
  const sessions = [
    ['s-1', 'Форма оплаты: валидация телефона', 'checkout', 0.2],
    ['s-2', 'Каталог: фильтр по дате', 'catalog', 3],
    ['s-3', 'Перенос дат в ISO', 'checkout', 26],
  ].map(([id, title, project, hAgo]) => ({
    id,
    provider: 'claude',
    title,
    project,
    start: Math.floor((now - (Number(hAgo) + 1) * 3_600_000) / 1000),
    end: Math.floor((now - Number(hAgo) * 3_600_000) / 1000),
    prompts: 12,
    turnMs: 1_800_000,
    input: 1,
    output: 1,
    cacheRead: 1,
    cacheWrite: 1,
    cost: id === 's-1' ? 4.2 : id === 's-2' ? 2.8 : 7.7,
    subagentCost: 0,
    model: 'claude-opus-5-5',
    tasks: [],
    prs: [],
  }));
  return {
    viewer: 'dev',
    runtimes: [
      {
        person: 'dev',
        machineId: 'm1',
        machineName: 'dev-main',
        report: {
          version: 1,
          person: 'dev',
          machine: 'dev-main',
          generatedAt: new Date().toISOString(),
          timezone: 'Europe/Moscow',
          days,
          usage,
          sessions,
          collecting: null,
        },
      },
    ],
  };
};

// This chunk loads only with ?dev=manage: the machine's report is answered here, in the page, from the start.
const realFetch = window.fetch.bind(window);
window.fetch = ((input: RequestInfo | URL, init?: RequestInit) =>
  String(input).includes('/api/platform/stats')
    ? Promise.resolve(Response.json(fakeReport()))
    : realFetch(input, init)) as typeof window.fetch;

const ManageGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);
  return (
    <div className="hx-manage-gallery">
      <ManageScreen onOpen={() => undefined} />
    </div>
  );
};

export default ManageGallery;
