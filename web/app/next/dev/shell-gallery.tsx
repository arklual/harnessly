// The gallery of the shell (`?dev=shell`, only where the installation has next-ui): the sidebar, header, notification
// centre, toasts, search, dialogs and the pane on made-up data, each state reachable by the address, for screenshots
// and timing runs. The data lives here; every action answers after a pause, and the ones marked so fail once.
//
//   ?dev=shell                         the shell with a chat open
//   &route=home                        the greeting instead
//   &list=loading|empty|error          the sidebar's list states
//   &rail=1                            the desktop sidebar folded
//   &open=search|notif|newproject|resume|pane|drawer|toast   a layer open on load
//   &term=1                           the agent's terminal panel under the chat
//   &slow=1                            every action takes 4 s (the «longer than usual» state)
//   &favs=none                         the favorites with nothing pinned (the empty state)
//   &tabs=1|many                       the strip of chat tabs over the window (many: more than fit)
//   &max=1                             the pane open «во всю ширину»
//   &paneTabs=many                     more tabs in the pane's strip, one with a long title
//   &health=ok|warn|critical           the machine's own verdict (default warn): its dot, banner, card and notification
//   &theme=light|dark&lang=ru|en       the look and the language of the texts that are keys
import { useCallback, useEffect, useMemo, useState } from 'react';
import ChatHeader from '@/next/features/header/chat-header';
import NotificationCenter, { type INotification } from '@/next/features/notifications/notification-center';
import { notificationsFromChats } from '@/next/features/notifications/from-chats';
import { notificationsFromMachines } from '@/next/features/notifications/from-machines';
import { alertOf, cardOf, choiceOf } from '@/next/bindings/shell-model';
import { pinLocale } from '@/next/i18n';
import type { HealthIssue, HealthLevel, MachineLoad, MessageHit } from '@/next/protocol/v2';
import type { MachineView } from '@/next/stores/machines';
import { toast, ToastHost } from '@/next/ui/toast';
import SearchDialog from '@/next/features/search/search-dialog';
import { CHAT_SCOPES, messageGroups, recentChats, searchChats } from '@/next/features/search/search-chats';
import { foldText } from '@/next/features/search/highlight';
import AppFrame from '@/next/features/shell/app-frame';
import ChatStage from '@/next/features/shell/chat-stage';
import TabStrip, { PACE, type IStripTab } from '@/next/features/shell/tab-strip';
import Greeting from '@/next/features/shell/greeting';
import PaneDrawer from '@/next/features/shell/pane-drawer';
import ToolPane, { TOOLS_TAB } from '@/next/features/pane/tool-pane';
import ToolsLauncher, { type ITool } from '@/next/features/pane/tools-launcher';
import BrowserView from '@/next/features/pane/browser-view';
import { BackgroundButton } from '@/next/features/header/header-buttons';
import DesignlyHome from '@/next/features/designly/designly-home';
import ResumeDialog, { type IResumeSession } from '@/next/features/shell/resume-dialog';
import NewProjectDialog from '@/next/features/sidebar/new-project-dialog';
import Sidebar from '@/next/features/sidebar/sidebar';
import {
  CHAT_STATUS,
  SIDEBAR_VIEW,
  type IChatActions,
  type ISidebarChat,
  type ISidebarFavorites,
  type ISidebarProject,
} from '@/next/features/sidebar/types';
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/site.css';
import '@/next/styles/chat.css';
import '@/next/styles/base.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/shell.css';
import AgentTerminal from '@/next/features/terminal/agent-terminal';
import '@/next/styles/sidebar.css';
import '@/next/styles/header.css';
import '@/next/styles/tabs.css';

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;
const ANSWER_MS = 600;
const SLOW_ANSWER_MS = 4000;

const params = new URLSearchParams(window.location.search);
const LIST = params.get('list');
const OPEN = params.get('open');
const SLOW = params.get('slow') === '1';
const TERM = params.get('term') === '1';
const FAVS = params.get('favs');
const TABS = params.get('tabs');
const PANE_TABS_MANY = params.get('paneTabs') === 'many';
const THEME = params.get('theme') === 'dark' ? 'dark' : 'light';
pinLocale(params.get('lang') === 'en' ? 'en' : 'ru');

const STRIP_TABS: readonly IStripTab[] = [
  {
    id: 'c-plan',
    title: 'Доступ к dev-машине Acme Admin',
    icon: 'message-square',
    busy: PACE.working,
    attention: false,
  },
  { id: 'c-release', title: 'Релиз 0.15.20', icon: 'message-square', busy: null, attention: false },
  {
    id: 'c-onboarding',
    title: 'Онбординг организаторов и первый платёж',
    icon: 'message-square',
    busy: null,
    attention: true,
  },
  { id: 'c-deploy', title: 'Выкат', icon: 'message-square', busy: null, attention: false },
];
const MANY_STRIP_TABS: readonly IStripTab[] = [
  ...STRIP_TABS,
  ...Array.from({ length: 8 }, (_, i) => ({
    id: `x${i}`,
    title: `Длинное название беседы номер ${i + 1}`,
    icon: 'message-square',
    busy: null,
    attention: false,
  })),
];

const PROJECTS: readonly ISidebarProject[] = [
  { id: 'p-harnessly', name: 'harnessly' },
  { id: 'p-payments', name: 'payments' },
];

/** The favorites as the machine would keep them: two groups, one chat of none; the third pinned chat is unplaced. */
const SEED_FAVORITES: ISidebarFavorites = {
  groups: [
    { id: 'fg-release', name: 'Релизы', chats: ['c-release', 'c-deploy'] },
    { id: 'fg-ideas', name: 'Идеи', chats: [] },
  ],
  loose: ['c-onboarding'],
};

/** What the machine makes of «place» (the runtime's wire.Favorites.Placed), for the gallery's made-up one. */
const placed = (
  layout: ISidebarFavorites,
  id: string,
  groupId: string | null,
  beforeId: string | null,
): ISidebarFavorites => {
  const out = {
    groups: layout.groups.map((group) => ({ ...group, chats: group.chats.filter((x) => x !== id) })),
    loose: layout.loose.filter((x) => x !== id),
  };
  const target = groupId === null ? out.loose : out.groups.find((group) => group.id === groupId)?.chats;
  if (!target) return layout;
  const at = beforeId === null ? -1 : target.indexOf(beforeId);
  target.splice(at < 0 ? target.length : at, 0, id);
  return out;
};

const PANE_TOOLS: ITool[] = [
  {
    id: 'side',
    group: 'chat',
    icon: 'message-square-text',
    name: 'Боковой чат',
    hint: 'Вопрос про этот чат, агент не отвлечётся',
    kbd: '⌥⌘S',
  },
  { id: 'plan', group: 'chat', icon: 'list-checks', name: 'План', hint: 'Шаги агента и где он сейчас' },
  {
    id: 'subagents',
    group: 'chat',
    icon: 'workflow',
    name: 'Сабагенты',
    hint: 'Кого запускал агент и что он вернул',
    live: '2 работают',
  },
  { id: 'files', group: 'chat', icon: 'files', name: 'Файлы', hint: 'Папка чата: код и документы' },
  {
    id: 'terminal',
    group: 'vm',
    icon: 'square-terminal',
    name: 'Агентский терминал',
    hint: 'Что запускает агент в папке чата',
    kbd: '⌃`',
  },
  {
    id: 'shell',
    group: 'vm',
    icon: 'terminal',
    name: 'Терминал',
    hint: 'Своя оболочка на acme-runner-2 — сколько угодно вкладок',
    live: 'Открыто: 2',
  },
  { id: 'browser', group: 'vm', icon: 'globe', name: 'Браузер', hint: 'Любой адрес, дев-серверы и стенды' },
  { id: 'machine', group: 'vm', icon: 'cpu', name: 'Машина', hint: 'Нагрузка и тяжёлые процессы' },
  { id: 'designly', group: 'links', icon: 'palette', name: 'Designly', hint: 'Макеты и правки по ним' },
];
const PANE_GROUPS = [
  { id: 'chat', label: 'Этот чат' },
  { id: 'vm', label: 'Машина acme-runner-2' },
  { id: 'links', label: 'Сервисы' },
];
const PANE_PORTS = [
  {
    port: 9150,
    command: 'python3',
    cwd: null,
    group: 'other',
    kind: 'site',
    responding: true,
    title: 'Acme Files',
    description: 'VM ↔ Mac через локальный Teleport-туннель',
    icon: null,
  },
  {
    port: 5173,
    command: 'node',
    cwd: null,
    group: 'chat',
    kind: 'site',
    responding: true,
    title: 'harnessly',
    description: null,
    icon: null,
  },
  {
    port: 3000,
    command: 'node',
    cwd: null,
    group: 'other',
    kind: 'site',
    responding: true,
    title: null,
    description: null,
    icon: null,
  },
] as const;

/** The pane's real strip, tools and new browser tab on made-up data. */
const GalleryPane = ({
  maxed,
  onToggleMax,
  onClose,
}: {
  maxed: boolean;
  onToggleMax: () => void;
  onClose: () => void;
}) => {
  const [tab, setTab] = useState({ browser: 'b1', designly: 'd1' }[params.get('tab') ?? ''] ?? TOOLS_TAB);
  const [stamp] = useState(() => Date.now());
  return (
    <ToolPane
      tabs={[
        { id: 'b1', icon: 'globe', name: 'Новая вкладка' },
        { id: 'p1', icon: 'list-checks', name: 'План' },
        { id: 'd1', icon: 'palette', name: 'Designly' },
        ...(PANE_TABS_MANY
          ? [
              { id: 'f1', icon: 'file-text', name: 'Редизайн оболочки: блоки и вкладки' },
              { id: 'f2', icon: 'sparkles', name: 'Аудит состояний интерфейса' },
              { id: 'f3', icon: 'terminal', name: 'Терминал' },
              { id: 'f4', icon: 'file-text', name: 'README.md' },
            ]
          : [
              { id: 'f1', icon: 'sparkles', name: 'Аудит состояний' },
              { id: 'f2', icon: 'file-text', name: 'План выката' },
            ]),
      ]}
      tab={tab}
      maxed={maxed}
      strip={
        <BackgroundButton
          background={{ running: 2, total: 3, recap: false, open: false, onToggle: () => {} }}
        />
      }
      onPick={setTab}
      onCloseTab={() => {}}
      onToggleMax={onToggleMax}
      onClose={onClose}
    >
      {tab === TOOLS_TAB && (
        <ToolsLauncher tools={PANE_TOOLS} groups={PANE_GROUPS} onOpen={() => {}} onAddress={() => {}} />
      )}
      {tab === 'd1' && (
        <DesignlyHome
          cards={[
            {
              id: 'a',
              title: 'Ссылки рассылки',
              boards: 3,
              updatedAt: stamp - 3_600_000,
              image: null,
              kind: 'site',
              pinned: true,
              drawing: false,
              owner: null,
            },
            {
              id: 'b',
              title: 'Питч-дек',
              boards: 8,
              updatedAt: stamp - 86_400_000,
              image: null,
              kind: 'slides',
              pinned: false,
              drawing: true,
              owner: null,
            },
          ]}
          error={null}
          now={stamp}
          onPin={() => Promise.resolve()}
          onOpen={() => {}}
          onRetry={() => {}}
          compose={null}
        />
      )}
      {tab === 'b1' && (
        <BrowserView
          address=""
          canBack={false}
          canForward={false}
          src={null}
          error={null}
          ports={PANE_PORTS.map((p) => ({ ...p }))}
          portsError={null}
          onRetryPorts={() => {}}
          reloadKey={0}
          onGo={() => {}}
          onHistory={() => {}}
          onReload={() => {}}
          onNewTab={() => {}}
        />
      )}
    </ToolPane>
  );
};

const FEED_TEXT = Array.from(
  { length: 24 },
  (_, i) =>
    `Абзац ${i + 1}. Агент разобрал задачу, прошёл по файлам панели, поправил анимацию и проверил, что лента остаётся на месте, пока колонка чата меняет ширину вместе с правой панелью.`,
);

/** What the runtime's message search would find in the made-up chats: the words are matched here, the way it does. */
const seedMessages = (now: number): readonly MessageHit[] => {
  const hit = (
    chat: string,
    n: number,
    ago: number,
    role: MessageHit['role'],
    snippet: string,
  ): MessageHit => ({
    convId: chat as MessageHit['convId'],
    entryId: `c:00000000-0000-4000-8000-00000000000${n}#0`,
    turnId: `c:00000000-0000-4000-8000-00000000000${n}#0`,
    at: now - ago,
    role,
    snippet,
  });
  return [
    hit(
      'c-deploy',
      1,
      70 * MIN,
      'user',
      '…выкат на прод упал на миграции, посмотри логи джобы и скажи, что делать…',
    ),
    hit(
      'c-deploy',
      2,
      66 * MIN,
      'agent',
      'Миграция упала на дубле индекса: откатил её и выкатил заново, прод жив.',
    ),
    hit(
      'c-onboarding',
      3,
      5 * DAY,
      'agent',
      'Для платежей нужна миграция схемы: добавим таблицу выплат и индекс по организатору.',
    ),
    hit(
      'c-refunds',
      4,
      2 * HOUR,
      'user',
      'Сверь акт по возвратам за октябрь и найди расхождения по миграции платежей',
    ),
  ];
};

const seedChats = (now: number): ISidebarChat[] => {
  const chat = (id: string, title: string, ago: number, extra: Partial<ISidebarChat> = {}): ISidebarChat => ({
    id,
    title,
    status: CHAT_STATUS.none,
    pinned: false,
    projectId: null,
    createdAt: now - ago,
    lastPromptAt: now - ago,
    lastActivityAt: now - ago,
    terminal: true,
    canStopTerminal: true,
    ...extra,
  });
  return [
    chat('c-release', 'Чеклист релиза 1.80', 3 * DAY, { pinned: FAVS !== 'none', terminal: false }),
    chat('c-onboarding', 'Онбординг в payments', 5 * DAY, { pinned: FAVS !== 'none' }),
    chat('c-sidebar', 'Сайдбар нового интерфейса', 2 * MIN, {
      status: CHAT_STATUS.thinking,
      projectId: 'p-harnessly',
    }),
    chat('c-queue', 'Очередь сообщений v2', 40 * MIN, {
      status: CHAT_STATUS.question,
      projectId: 'p-harnessly',
    }),
    chat('c-refunds', 'Возвраты: акт сверки', 2 * HOUR, {
      status: CHAT_STATUS.perm,
      projectId: 'p-payments',
    }),
    chat('c-l1', 'Форма оплаты с проверкой карты', 20 * MIN, {
      status: CHAT_STATUS.thinking,
      launch: { id: 'l1', prompt: 'Форма оплаты с проверкой карты', model: 'Opus 5.5' },
    }),
    chat('c-l2', 'Форма оплаты с проверкой карты', 20 * MIN, {
      status: CHAT_STATUS.done,
      launch: { id: 'l1', prompt: 'Форма оплаты с проверкой карты', model: 'Sonnet 5.5' },
    }),
    chat('c-plan', 'План миграции на сторы', 12 * MIN, { status: CHAT_STATUS.plan }),
    chat('c-bg', 'Прогон тестов в фоне', 8 * MIN, {
      status: CHAT_STATUS.background,
      waitsFor: 'Ждёт: 2 сабагента, команда npm test, пробуждение в 12:30',
      projectId: 'p-harnessly',
    }),
    chat('c-sidebar-review', 'Ревью диффа сайдбара', MIN, {
      status: CHAT_STATUS.thinking,
      projectId: 'p-harnessly',
      parentId: 'c-sidebar',
    }),
    chat('c-sidebar-tests', 'Прогон тестов сайдбара', 90 * 1000, {
      status: CHAT_STATUS.done,
      projectId: 'p-harnessly',
      parentId: 'c-sidebar',
    }),
    chat('c-images', 'Пропажа картинок S3', 25 * MIN, { status: CHAT_STATUS.done }),
    chat('c-images-logs', 'Логи S3 за неделю', 20 * MIN, { status: CHAT_STATUS.done, parentId: 'c-images' }),
    chat('c-deploy', 'Выкат на прод упал на миграции', 70 * MIN, {
      status: CHAT_STATUS.failed,
      pinned: FAVS !== 'none',
    }),
    chat('c-refunds-pin', 'Возвраты: заявка и документы', 6 * HOUR, { pinned: FAVS !== 'none' }),
    chat('c-stopped', 'Прогон Playwright по галерее', 3 * HOUR, { status: CHAT_STATUS.stopped }),
    chat('c-machine', 'Сборка образа runner-2', 4 * HOUR, { status: CHAT_STATUS.waitingMachine }),
    chat('c-limit', 'Сверка платежей за сентябрь', 90 * MIN, {
      status: CHAT_STATUS.limited,
      waitsFor: 'Ждёт сброса лимита до 18:00',
    }),
    chat('c-copy', 'Тексты экрана оплаты', 26 * HOUR),
    chat('c-posthog', 'PostHog: дашборд оплаты', 30 * HOUR),
    chat('c-angular', 'Angular 22: линт и API', 3 * DAY),
    chat('c-scanner', 'Сканер: офлайн-режим двери', 12 * DAY),
  ];
};

const GB = 1024 ** 3;
const BOOTED = Date.now();

/** The machine's load and its verdict per state of `&health`, the way the runtime sends them. */
const HEALTH_STATES = {
  ok: {
    load: { cpu: 0.38, memUsed: 12.4 * GB, memTotal: 32 * GB, diskFree: 120 * GB, diskTotal: 260 * GB },
    issues: [],
    other: [],
  },
  warn: {
    load: { cpu: 0.42, memUsed: 19.6 * GB, memTotal: 32 * GB, diskFree: 21 * GB, diskTotal: 260 * GB },
    issues: [{ metric: 'disk', level: 'warn' }],
    other: [{ metric: 'mem', level: 'warn' }],
  },
  critical: {
    load: { cpu: 0.96, memUsed: 28.9 * GB, memTotal: 32 * GB, diskFree: 2.6 * GB, diskTotal: 260 * GB },
    issues: [
      { metric: 'disk', level: 'critical' },
      { metric: 'mem', level: 'warn' },
      { metric: 'cpu', level: 'warn' },
    ],
    other: [{ metric: 'mem', level: 'critical' }],
  },
} as const satisfies Record<
  HealthLevel,
  {
    load: Omit<MachineLoad, 'at' | 'busyAgents'>;
    issues: readonly HealthIssue[];
    other: readonly HealthIssue[];
  }
>;
const HEALTH_PARAM = params.get('health');
const HEALTH: HealthLevel = HEALTH_PARAM === 'ok' || HEALTH_PARAM === 'critical' ? HEALTH_PARAM : 'warn';
const levelOf = (issues: readonly HealthIssue[]): HealthLevel => issues[0]?.level ?? 'ok';

const viewOf = (
  id: string,
  name: string,
  issues: readonly HealthIssue[],
  load?: Omit<MachineLoad, 'at' | 'busyAgents'>,
  reach: MachineView['machine']['reach'] = 'online',
): MachineView => ({
  machine: {
    id,
    name,
    host: `${id}.machines.example.com`,
    reach,
    lastSeenAt: BOOTED,
    runtime: { version: '1.82.0', proto: 2, features: [] },
    agents: [
      {
        provider: 'claude',
        version: '2.3.1',
        status: 'ready',
        models: [],
        canSwitchModel: true,
      },
    ],
    ...(load ? { load: { ...load, at: BOOTED, busyAgents: 3 } } : {}),
    health: { level: levelOf(issues), since: BOOTED - 4 * MIN, issues: [...issues] },
  },
  runtime: 'v2',
  epoch: null,
  features: [],
  limits: null,
});

const STATE = HEALTH_STATES[HEALTH];
const MACHINE_VIEW = viewOf('runner-2', 'Моя ВМ', STATE.issues, STATE.load);
const OTHER_VIEWS = [
  viewOf('dev-department', 'Ромина ВМ', STATE.other),
  viewOf('macbook', 'MacBook', [], undefined, 'unreachable'),
];
const MACHINE = cardOf(MACHINE_VIEW, false);
const OTHERS = OTHER_VIEWS.map(choiceOf);
const MACHINE_NOTICES = [MACHINE_VIEW, ...OTHER_VIEWS].flatMap(({ machine }) => {
  const alert = alertOf(machine, false);
  return alert && machine.health
    ? [{ id: machine.id, name: machine.name, alert, since: machine.health.since }]
    : [];
});

const sessionsOf = (now: number): IResumeSession[] => [
  {
    id: 's1',
    provider: 'claude',
    first: 'Почини падение e2e на очереди',
    last: 'Готово: тест зелёный, причина — гонка в outbox',
    dir: '~/workspace/harnessly',
    branch: 'abc-3143-delivery',
    machine: 'Моя ВМ',
    at: now - 20 * MIN,
    off: false,
  },
  {
    id: 's2',
    provider: 'codex',
    first: 'Разбери отчёт Т-Банка за сентябрь',
    last: 'Сверил 1 204 строки, расхождений 3',
    dir: '~/workspace/payments',
    branch: null,
    machine: 'Ромина ВМ',
    at: now - 5 * HOUR,
    off: false,
  },
  {
    id: 's3',
    provider: 'claude',
    first: 'Свёрстай карточку мероприятия',
    last: 'Жду ответа: какой вариант обложки',
    dir: '~/workspace/storefront',
    branch: 'feat/event-card',
    machine: 'MacBook',
    at: now - 2 * DAY,
    off: true,
  },
];

/** The list as `&list=` asks: loading and failed are null, empty is []. */
const listOf = (chats: readonly ISidebarChat[]): readonly ISidebarChat[] | null => {
  if (LIST === 'loading' || LIST === 'error') return null;
  if (LIST === 'empty') return [];
  return chats;
};

/** An answer after a pause; the ids in `failOnce` fail the first time. */
const answer = (failOnce: Set<string>, key: string) =>
  new Promise<void>((resolve, reject) =>
    setTimeout(
      () => {
        if (failOnce.has(key)) {
          failOnce.delete(key);
          reject(new Error('Сервер ответил 503'));
        } else resolve();
      },
      SLOW ? SLOW_ANSWER_MS : ANSWER_MS,
    ),
  );

const Gallery = () => {
  const [now] = useState(() => Date.now());
  const [chats, setChats] = useState<ISidebarChat[]>(() => seedChats(now));
  const [projects, setProjects] = useState<readonly ISidebarProject[]>(PROJECTS);
  // The strip keeps its own order, as the tabs store does: a carried tab stays where it is put.
  const [stripTabs, setStripTabs] = useState<readonly IStripTab[]>(
    TABS === 'many' ? MANY_STRIP_TABS : STRIP_TABS,
  );
  const [favorites, setFavorites] = useState<ISidebarFavorites>(
    FAVS === 'none' ? { groups: [], loose: [] } : SEED_FAVORITES,
  );
  const [currentId, setCurrentId] = useState<string | null>(
    params.get('route') === 'home' ? null : 'c-sidebar',
  );
  const [rail, setRail] = useState(params.get('rail') === '1');
  const [drawerOpen, setDrawerOpen] = useState(OPEN === 'drawer');
  const [notif, setNotif] = useState<{ anchor: HTMLElement | null; side: 'right' | 'bottom' } | null>(null);
  const [searchOpen, setSearchOpen] = useState(OPEN === 'search');
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState('all');
  const [newProject, setNewProject] = useState(OPEN === 'newproject');
  const [resume, setResume] = useState(OPEN === 'resume');
  const [pane, setPane] = useState(OPEN === 'pane' || params.get('max') === '1');
  const [paneMax, setPaneMax] = useState(params.get('max') === '1');
  const [background, setBackground] = useState(false);
  const [failOnce] = useState(() => new Set(['delete:c-copy', 'pin:c-angular']));

  useEffect(() => {
    // The centre opens from «Уведомления» once the sidebar is on screen.
    const frame = requestAnimationFrame(() => {
      if (OPEN === 'notif')
        setNotif({
          anchor: document.querySelectorAll<HTMLElement>('.hx-nav__item')[3] ?? null,
          side: 'right',
        });
    });
    if (OPEN === 'toast') {
      toast('Ссылка скопирована', { tone: 'success' });
      toast('Не удалось закрепить чат', {
        tone: 'error',
        description: 'Сервер ответил 503',
        action: { label: 'Повторить', onClick: () => {} },
      });
    }
    return () => cancelAnimationFrame(frame);
  }, []);

  // A new chat comes in a moment after load: it slides into «Сегодня».
  useEffect(() => {
    const timer = setTimeout(
      () =>
        setChats((current) => [
          {
            id: 'c-fresh',
            title: 'Новый: разбор лагов шторки',
            status: CHAT_STATUS.thinking,
            pinned: false,
            projectId: null,
            createdAt: Date.now(),
            lastPromptAt: Date.now(),
            lastActivityAt: Date.now(),
            terminal: true,
            canStopTerminal: true,
          },
          ...current,
        ]),
      2500,
    );
    return () => clearTimeout(timer);
  }, []);

  const patch = useCallback(
    (id: string, change: Partial<ISidebarChat>) =>
      setChats((current) => current.map((chat) => (chat.id === id ? { ...chat, ...change } : chat))),
    [],
  );

  const actions = useMemo<IChatActions>(
    () => ({
      onRename: (id, title) => answer(failOnce, `rename:${id}`).then(() => patch(id, { title })),
      onPin: (id, pinned) => answer(failOnce, `pin:${id}`).then(() => patch(id, { pinned })),
      onMove: (id, projectId) => answer(failOnce, `move:${id}`).then(() => patch(id, { projectId })),
      onDelete: (id) =>
        answer(failOnce, `delete:${id}`).then(() =>
          setChats((current) => current.filter((chat) => chat.id !== id)),
        ),
      onPlaceFavorite: (id, groupId, beforeId) =>
        answer(failOnce, `place:${id}`).then(() => {
          patch(id, { pinned: true });
          setFavorites((current) => placed(current, id, groupId, beforeId));
        }),
      onCreateGroup: (name) =>
        answer(failOnce, 'group:new').then(() => {
          const id = `fg-${name.toLowerCase().replace(/\s+/g, '-')}-${Date.now()}`;
          setFavorites((current) => ({ ...current, groups: [...current.groups, { id, name, chats: [] }] }));
          return id;
        }),
      onRenameGroup: (id, name) =>
        answer(failOnce, `group:${id}`).then(() =>
          setFavorites((current) => ({
            ...current,
            groups: current.groups.map((group) => (group.id === id ? { ...group, name } : group)),
          })),
        ),
      onDeleteGroup: (id) =>
        answer(failOnce, `group:${id}`).then(() =>
          setFavorites((current) => {
            const gone = current.groups.find((group) => group.id === id);
            return {
              groups: current.groups.filter((group) => group.id !== id),
              loose: [...(gone?.chats ?? []), ...current.loose],
            };
          }),
        ),
      onMoveGroup: (id, beforeId) =>
        answer(failOnce, `group:${id}`).then(() =>
          setFavorites((current) => {
            const group = current.groups.find((item) => item.id === id);
            if (!group) return current;
            const rest = current.groups.filter((item) => item.id !== id);
            const at = rest.findIndex((item) => item.id === beforeId);
            rest.splice(at < 0 ? rest.length : at, 0, group);
            return { ...current, groups: rest };
          }),
        ),
      onNewChatIn: (projectId) => {
        setCurrentId(null);
        void toast(`Новый чат в проекте ${projects.find((p) => p.id === projectId)?.name ?? ''}`, {
          tone: 'info',
        });
      },
      onCopyLink: () =>
        answer(failOnce, 'copy').then(() => void toast('Ссылка скопирована', { tone: 'success' })),
      onTerminal: () => toast('Терминал агента откроется в панели', { tone: 'info' }),
      onNewProject: () => setNewProject(true),
    }),
    [failOnce, patch, projects],
  );

  const listChats = listOf(chats);
  const chat = chats.find((item) => item.id === currentId) ?? null;
  const parentChat = chats.find((item) => item.id === chat?.parentId) ?? null;
  const fromChats = notificationsFromChats(listChats);
  const notifications: readonly INotification[] | null = fromChats && [
    ...notificationsFromMachines(MACHINE_NOTICES),
    ...fromChats,
  ];
  const [machineReveal, setMachineReveal] = useState(0);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = THEME;
    root.classList.toggle('dark', THEME === 'dark');
    root.style.colorScheme = THEME;
  }, []);

  const sidebar = (
    <Sidebar
      brand="harnessly"
      chats={listChats}
      projects={LIST === 'empty' ? [] : projects}
      favorites={favorites}
      loadError={LIST === 'error' ? 'Чаты не загрузились: машина не отвечает' : null}
      onRetryLoad={() => void toast('Пробуем ещё раз', { tone: 'info' })}
      currentId={currentId}
      view={SIDEBAR_VIEW.chat}
      now={now}
      person={{ name: 'Пётр' }}
      machine={MACHINE}
      otherMachines={OTHERS}
      machineReveal={machineReveal}
      unread={(notifications ?? []).length > 0}
      notificationsOpen={notif !== null}
      searchOpen={searchOpen}
      more={[
        { id: 'apps', icon: 'app-window', label: 'Приложения', current: false, onSelect: () => {} },
        { id: 'efficiency', icon: 'gauge', label: 'Эффективность', current: false, onSelect: () => {} },
        { id: 'stats', icon: 'chart-column', label: 'Статистика', current: false, onSelect: () => {} },
      ]}
      profile={[
        { id: 'settings', icon: 'settings-2', label: 'Настройки', current: false, onSelect: () => {} },
        {
          id: 'legacy',
          icon: 'undo-2',
          label: 'Вернуться в старый интерфейс',
          current: false,
          onSelect: () => {},
        },
      ]}
      actions={actions}
      onOpen={(id) => {
        setCurrentId(id);
        setDrawerOpen(false);
      }}
      onNewChat={() => {
        setCurrentId(null);
        setDrawerOpen(false);
      }}
      onSearch={() => {
        setDrawerOpen(false);
        setSearchOpen(true);
      }}
      onDesignly={() => toast('Designly — отдельная фича', { tone: 'info' })}
      onNotifications={(anchor) => setNotif((current) => (current ? null : { anchor, side: 'right' }))}
      onToggleRail={() => setRail((current) => !current)}
      onCloseDrawer={() => setDrawerOpen(false)}
      onPickMachine={(id) => answer(failOnce, `machine:${id}`)}
      onManageMachines={() => {}}
    />
  );

  return (
    <AppFrame
      rail={rail}
      drawerOpen={drawerOpen}
      onDrawerChange={setDrawerOpen}
      sidebar={sidebar}
      aside={
        currentId !== null ? (
          <PaneDrawer open={pane} onClose={() => setPane(false)} wide={paneMax}>
            <GalleryPane
              maxed={paneMax}
              onToggleMax={() => setPaneMax((m) => !m)}
              onClose={() => setPane(false)}
            />
          </PaneDrawer>
        ) : null
      }
      tabs={
        TABS ? (
          <TabStrip
            tabs={stripTabs}
            active={currentId ?? 'c-plan'}
            onActivate={setCurrentId}
            onClose={() => {}}
            onNew={() => setCurrentId(null)}
            onMove={(id, index) =>
              setStripTabs((current) => {
                const tab = current.find((item) => item.id === id);
                if (!tab) return current;
                const rest = current.filter((item) => item.id !== id);
                return [...rest.slice(0, index), tab, ...rest.slice(index)];
              })
            }
            onCloseOthers={() => {}}
            onCloseRight={() => {}}
          />
        ) : undefined
      }
      overlays={
        <>
          <ToastHost />
          <NotificationCenter
            open={notif !== null}
            anchor={notif?.anchor ?? null}
            side={notif?.side ?? 'right'}
            items={notifications}
            now={now}
            onClose={() => setNotif(null)}
            onOpenItem={(item) => {
              setNotif(null);
              if (item.chatId) setCurrentId(item.chatId);
              else setMachineReveal((n) => n + 1);
            }}
            onReadAll={() => answer(failOnce, 'read-all')}
          />
          <SearchDialog
            open={searchOpen}
            onClose={() => setSearchOpen(false)}
            query={query}
            onQuery={setQuery}
            scopes={CHAT_SCOPES}
            scope={scope}
            onScope={setScope}
            results={[
              ...searchChats(chats, projects, query),
              ...messageGroups(
                query.trim().length < 2
                  ? []
                  : seedMessages(now).filter((hit) =>
                      foldText(query)
                        .split(/\s+/)
                        .filter(Boolean)
                        .every((word) => foldText(hit.snippet).includes(word)),
                    ),
                chats,
                now,
              ),
            ]}
            recent={recentChats(chats, projects)}
            onPick={(row) => {
              setSearchOpen(false);
              setCurrentId(row.id);
            }}
          />
          <NewProjectDialog
            open={newProject}
            onClose={() => setNewProject(false)}
            onCreate={(name) =>
              answer(failOnce, `project:${name}`).then(() =>
                setProjects((current) => [...current, { id: `p-${current.length + 1}`, name }]),
              )
            }
          />
          <ResumeDialog
            open={resume}
            onClose={() => setResume(false)}
            sessions={sessionsOf(now)}
            now={now}
            onResume={(id) => answer(failOnce, `resume:${id}`)}
          />
        </>
      }
    >
      <ChatHeader
        chat={chat}
        parent={parentChat && { title: parentChat.title, onOpen: () => setCurrentId(parentChat.id) }}
        projects={projects}
        actions={actions}
        machine={{ name: MACHINE.name, health: MACHINE.health, issue: MACHINE.issue?.text ?? null }}
        drawerOpen={drawerOpen}
        onOpenDrawer={() => setDrawerOpen(true)}
        onNewChat={() => setCurrentId(null)}
        terminal={null}
        pane={{ open: pane, onToggle: () => setPane((current) => !current) }}
        background={{
          running: background ? 0 : 2,
          total: 3,
          recap: false,
          open: background,
          onToggle: () => setBackground((current) => !current),
        }}
        bell={{
          unread: (notifications ?? []).length > 0,
          open: notif !== null,
          onToggle: (anchor) => setNotif((current) => (current ? null : { anchor, side: 'bottom' })),
        }}
      />
      {currentId === null && <Greeting name="Пётр" onResume={() => setResume(true)} />}
      {currentId !== null && (
        <ChatStage key={currentId} paneOpen={pane}>
          {chat?.status === CHAT_STATUS.thinking ? null : (
            <div className="hx-chat">
              <div className="hx-feed">
                <div className="hx-feed__inner">
                  {FEED_TEXT.map((text) => (
                    <p key={text}>{text}</p>
                  ))}
                </div>
              </div>
            </div>
          )}
        </ChatStage>
      )}
      {currentId !== null && TERM && (
        <AgentTerminal
          open
          url="ws://127.0.0.1:9/terminal"
          cwd="/home/coder/workspace/harnessly"
          title={chat?.title ?? ''}
          provider="claude"
          onClose={() => undefined}
        />
      )}
    </AppFrame>
  );
};

const ShellGallery = () => <Gallery />;

export default ShellGallery;
