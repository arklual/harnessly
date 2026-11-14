// The chat's right column on made-up data, for the eye and for screenshots: ?dev=rail with the installation's
// next-ui feature. ?theme=light|dark&lang=en|ru pick the look; ?shots=0 (none), loading or a number of
// pictures (default 7). The recap, the pictures the agent looked at, five subagents (two running, three over), four
// named teammates (two at work, one waiting for a message, one shut down), a dynamic workflow and a command, as the
// column draws them. ?switch=1 changes between two chats every two seconds on a real chat stage, the second one's
// pictures coming 400 ms late the first time (a chat not in memory yet), for a recording of the change of chat;
// &legacy=1 draws the stage anew for each chat, as the shell did before HRN-1438. ?pane=flows draws the pane's tab of subagents instead, with the workflow's agents by phase.
// Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/background.css';
import '@/next/dev/rail-gallery.css';

import { useEffect, useState } from 'react';
import ChatStage from '@/next/features/shell/chat-stage';
import '@/next/styles/motion.css';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import BackgroundPanel from '@/next/features/background/background-panel';
import SubagentView from '@/next/features/pane/subagent-view';
import { sampleWorkflow } from '@/next/dev/workflow-sample';
import '@/next/styles/subagents.css';
import '@/next/styles/pane.css';
import type { BackgroundTask, ConvId, Effort, Shot } from '@/next/protocol/v2';

type TTheme = 'light' | 'dark';

const CONV = 'tab-gallery' as ConvId;
const MIN = 60_000;
const NOW = Date.now();
const NO_STOPPING: ReadonlySet<string> = new Set();

const task = (
  id: string,
  title: string,
  patch: Partial<BackgroundTask> & { model?: string; effort?: Effort },
): BackgroundTask => ({
  id,
  conversationId: CONV,
  kind: 'subagent',
  title,
  startedAt: NOW - 5 * MIN,
  endedAt: null,
  status: 'running',
  devServer: false,
  stoppable: true,
  outputPath: null,
  model: 'claude-sonnet-5-5',
  effort: 'high',
  last: 'Bash · cd /home/coder/workspace/harnessly/.worktrees/abc-3156-railmin',
  ...patch,
});

const TASKS: BackgroundTask[] = [
  task('a1', 'Apps section redesign (More menu)', { startedAt: NOW - 33 * MIN - 37_000 }),
  task('a2', 'Stand + screenshots for 0.15.19', { startedAt: NOW - 63 * MIN }),
  task('a3', 'Harden peer envelope', {
    status: 'done',
    startedAt: NOW - 20 * MIN,
    endedAt: NOW - 20 * MIN + 2 * MIN + 13_000,
    last: 'Конверт ужесточён, всё зелёное',
  }),
  task('a4', 'Runtime honesty: F5 replay, native fallbacks', {
    status: 'done',
    startedAt: NOW - 50 * MIN,
    endedAt: null,
    last: 'Оба фикса сделаны и закоммичены',
  }),
  task('a5', 'Codex gaps: login, model switch, usage', {
    status: 'done',
    startedAt: NOW - 70 * MIN,
    endedAt: null,
    last: 'Сделаны пункты 1, 3 и 4',
  }),
  task('aimpl-g1-28a3', 'impl-g1', {
    teammate: true,
    startedAt: NOW - 13 * MIN - 4_000,
    stoppable: false,
    model: 'sonnet',
    effort: undefined,
    role: 'G1: access tables + data layer',
  }),
  task('aimpl-h1-9ded', 'impl-h1', {
    teammate: true,
    startedAt: NOW - 12 * MIN - 20_000,
    stoppable: false,
    model: 'sonnet',
    effort: undefined,
    role: 'H1: agent ops + snapshot',
  }),
  task('arev-t4-9fa9', 'rev-t4', {
    teammate: true,
    status: 'idle',
    startedAt: NOW - 18 * MIN,
    endedAt: NOW - 17 * MIN,
    stoppable: false,
    model: 'sonnet',
    effort: undefined,
    role: 'Review Task 4',
  }),
  task('aimpl-t3-85e5', 'impl-t3', {
    teammate: true,
    status: 'done',
    startedAt: NOW - 40 * MIN,
    endedAt: NOW - 24 * MIN,
    stoppable: false,
    model: 'sonnet',
    effort: undefined,
    role: 'Task 3: board load measurement',
  }),
  task('c1', 'npm run dev', {
    kind: 'command',
    model: undefined,
    effort: undefined,
    last: 'ready in 412 ms',
  }),
];

const FLOW = sampleWorkflow(CONV, NOW, 'toolu_gallery_workflow');
TASKS.push(FLOW);

const RECAP = {
  text: 'Выкатываем большой релиз 0.15.19 со всеми фичами. MR !107 стоит на автомерже, после зелёного CI тег и выкат в прод пойдут сами, ориентир по времени около часа.',
  at: NOW - 60 * MIN,
};

const SHOT_PAGES = ['Главная', 'Оплата', 'Билеты', 'Профиль', 'Корзина', 'Поиск', 'Возврат'] as const;
const SHOT_HUES = [212, 150, 32, 280, 350, 190, 60] as const;
const SHOT_WHO = [
  undefined,
  'Apps section redesign (More menu)',
  undefined,
  'Stand + screenshots for 0.15.19',
] as const;

/** A made-up page as a picture: a header, a card and a few lines, in one hue. */
const picture = (index: number): string => {
  const hue = SHOT_HUES[index % SHOT_HUES.length];
  const title = SHOT_PAGES[index % SHOT_PAGES.length];
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="390" height="844" viewBox="0 0 390 844">` +
    `<rect width="390" height="844" fill="hsl(${hue} 30% 96%)"/>` +
    `<rect width="390" height="64" fill="hsl(${hue} 55% 46%)"/>` +
    `<text x="20" y="40" font-family="sans-serif" font-size="20" font-weight="600" fill="white">${title}</text>` +
    `<rect x="20" y="92" width="350" height="180" rx="16" fill="hsl(${hue} 45% 88%)"/>` +
    `<rect x="20" y="296" width="220" height="14" rx="7" fill="hsl(${hue} 20% 70%)"/>` +
    `<rect x="20" y="326" width="320" height="10" rx="5" fill="hsl(${hue} 20% 82%)"/>` +
    `<rect x="20" y="350" width="280" height="10" rx="5" fill="hsl(${hue} 20% 82%)"/>` +
    `<rect x="20" y="760" width="350" height="52" rx="14" fill="hsl(${hue} 55% 46%)"/></svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
};

const shotsOf = (count: number): Shot[] =>
  Array.from({ length: count }, (_, i) => {
    const agent = SHOT_WHO[i % SHOT_WHO.length];
    return {
      id: `shot-${i}`,
      name: `${SHOT_PAGES[i % SHOT_PAGES.length].toLowerCase()}-${i + 1}.png`,
      at: NOW - (i * 4 + 1) * MIN,
      ...(agent ? { agent } : {}),
    };
  });

const OTHER_RECAP = {
  text: 'Вкладки окна выровнены: равные отступы у иконки и крестика, «+» на оси вкладок. Осталось снять светлую тему.',
  at: NOW - 6 * MIN,
};
const OTHER_TASKS = TASKS.slice(0, 2);
const OTHER_SHOTS = shotsOf(3);
// The same arrays on every render, as the stores give them: the column compares what it shows by reference.
const FIRST_SHOTS = shotsOf(7);
const SWITCH_MS = 2000;
const LATE_MS = 400;

/** Two chats in turn on a chat stage: the column's change of chat, for a recording. */
const SwitchScene = ({ legacy }: { legacy: boolean }) => {
  const [second, setSecond] = useState(false);
  const [late, setLate] = useState(true);
  useEffect(() => {
    const timer = setInterval(() => setSecond((v) => !v), SWITCH_MS);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    if (!second || !late) return;
    const timer = setTimeout(() => setLate(false), LATE_MS);
    return () => clearTimeout(timer);
  }, [second, late]);
  const chat = second ? 'chat-b' : 'chat-a';
  const side = (
    <BackgroundPanel
      chat={chat}
      tasks={second ? OTHER_TASKS : TASKS}
      recap={second ? OTHER_RECAP : RECAP}
      shots={second ? (late ? null : OTHER_SHOTS) : FIRST_SHOTS}
      shotSrc={srcOf}
      shown
      now={NOW}
      stopping={NO_STOPPING}
      onStop={() => undefined}
    />
  );
  return (
    <div className="hx-root hx-rail-gallery hx-rail-gallery--stage" data-play="1">
      <ChatStage key={legacy ? chat : undefined} chatKey={chat} paneOpen={false} side={side}>
        <div className="hx-feed">
          <div className="hx-feed__inner">{chat}</div>
        </div>
      </ChatStage>
    </div>
  );
};

const srcOf = (shot: Shot): string => picture(Number(shot.id.slice(5)));

const RailGallery = () => {
  const { query } = useRouter();
  const theme: TTheme = query.theme === 'dark' ? 'dark' : 'light';
  const lang = query.lang === 'en' ? 'en' : 'ru';
  pinLocale(lang);
  const wanted = query.shots;
  const shots = wanted === 'loading' ? null : shotsOf(wanted === undefined ? 7 : Number(wanted) || 0);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);
  if (query.switch === '1') return <SwitchScene legacy={query.legacy === '1'} />;
  if (query.pane === 'flows')
    return (
      <div className="hx-root hx-rail-gallery">
        <SubagentView
          agents={[]}
          workflows={[FLOW]}
          focus={typeof query.focus === 'string' ? query.focus : null}
        />
      </div>
    );
  return (
    <div className="hx-root hx-rail-gallery">
      <BackgroundPanel
        tasks={TASKS}
        recap={RECAP}
        shots={shots}
        shotSrc={(shot) => picture(Number(shot.id.slice(5)))}
        shown
        now={NOW}
        stopping={NO_STOPPING}
        onStop={() => undefined}
        onHide={() => undefined}
      />
    </div>
  );
};

export default RailGallery;
