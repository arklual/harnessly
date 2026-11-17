// The voice's hands on the whole page: a task the voice handed over goes to the runtime's router
// (internal/voice/router) with what the page knows — the screen, every chat with its question or permission, and what
// the page can do (pages, settings sections, themes, settings, models) — and the plan that comes back is carried out
// here: a page opened, the theme or the sidebar changed, a message sent to another chat, an agent's question answered,
// a card put on the voice's stage. Work for the chat's own agent comes back as an "agent" action and goes the old way.
import { answerQuestion, createConversation, decidePermission, sendMessage, setModel } from '@/next/commands';
import { settled } from '@/next/bindings/outcome';
import { hub } from '@/next/core/realtime/hub';
import { launchModel } from '@/next/features/composer/agents';
import { permissionOption } from '@/next/features/voice-live/permission';
import { reading } from '@/next/features/voice-live/reader';
import { setVoiceMood, type VoiceMood } from '@/next/features/voice-live/voice-mood';
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import type { Conversation, ConvId, Effort, MachineId, Permission, Question } from '@/next/protocol/v2';
import { goTo, HOME, ROUTE, SETTINGS_SECTIONS, type SettingsSection } from '@/next/routes/routes';
import { toggleRail } from '@/next/bindings/rail';
import { chatPanes, chatPaneOf } from '@/next/stores/pane-state';
import { conversationsByActivity } from '@/next/stores/conversations';
import { gatesStore, openGates } from '@/next/stores/conversation';
import { meStore, patchSettings } from '@/next/stores/me';
import { sendModeNow } from '@/next/stores/chat-state';
import { ACT, addAct, updateAct } from '@/next/features/voice-live/voice-acts';
import { t } from '@/next/i18n';

export interface IRouteAction {
  do: string;
  chat?: string;
  text?: string;
  agent?: string;
  model?: string;
  effort?: string;
  orchestrate?: boolean;
  option?: number;
  allow?: boolean;
  page?: string;
  section?: string;
  value?: string;
  open?: boolean;
  tab?: string;
  key?: string;
  what?: string;
  source?: string;
  path?: string;
}

export interface IRoutePlan {
  say: string;
  mood?: string;
  actions: IRouteAction[];
  recalled?: string[];
  ms: number;
}

const routePath = (): `/${string}` => platformPath('/voice/route');

export const routeVoice = (m: MachineId, body: unknown) =>
  runtimeJson<IRoutePlan>(m, routePath(), postJson(body));

// What the person may call a page or a section by; the model matches their words to these.
const PAGES: readonly [string, string][] = [
  ['home', 'Главная, новый чат / Home, new chat'],
  ['settings', 'Настройки / Settings'],
  ['tasks', 'Задачи и MR / Tasks and merge requests'],
  ['apps', 'Приложения / Apps'],
  ['designly', 'Sinopia Design, дизайн / Design'],
  ['manage', 'Управление / Manage'],
  ['stats', 'Статистика / Stats'],
  ['efficiency', 'Эффективность / Efficiency'],
  ['review', 'Ревью / Review'],
  ['learn', 'Обучение / Learn'],
  ['help', 'Помощь / Help'],
];

const SECTION_LABEL: Partial<Record<SettingsSection, string>> = {
  general: 'Общие',
  appearance: 'Внешний вид, тема',
  keys: 'Клавиши',
  notifications: 'Уведомления',
  voice: 'Голос',
  claude: 'Claude Code',
  codex: 'Codex',
  accounts: 'Аккаунты, подписки',
  smart: 'Фоновые модели',
  skills: 'Скиллы',
  safe: 'Сейф, секреты',
  browser: 'Браузер',
  machines: 'Машины',
  apps: 'Приложения',
  connections: 'Подключения, трекеры',
  design: 'Дизайн',
  extensions: 'Расширения',
  plugins: 'Плагины',
  beta: 'Бета',
};

const AGENTS: readonly [string, string][] = [
  ['claude:opus', 'Claude Opus — самый сильный'],
  ['claude:sonnet', 'Claude Sonnet — сильный и быстрее'],
  ['claude:haiku', 'Claude Haiku — самый быстрый'],
  ['codex:default', 'Codex (ChatGPT) — модель по умолчанию'],
];

const EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'];
const PANE_TABS = ['browser', 'terminal', 'files', 'changes', 'artifacts', 'plan'];

const stateOf = (c: Conversation): string => {
  switch (c.state.k) {
    case 'working':
    case 'starting':
      return 'working';
    case 'waiting-question':
      return 'asks';
    case 'waiting-permission':
      return 'needs_permission';
    case 'launch-failed':
      return 'failed';
    case 'stopped':
    case 'gone':
      return 'stopped';
    default:
      return c.lastTurn?.status === 'failed' ? 'failed' : 'idle';
  }
};

const whenOf = (ms: number, now: number): string => {
  const d = new Date(ms);
  const today = new Date(now);
  const hm = d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' });
  const days = Math.round(
    (new Date(today.toDateString()).getTime() - new Date(d.toDateString()).getTime()) / 86_400_000,
  );
  if (days === 0) return `сегодня ${hm}`;
  if (days === 1) return `вчера ${hm}`;
  return `${d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' })} ${hm}`;
};

const gatesOf = (id: ConvId) => openGates(gatesStore.get(id) ?? { questions: [], permissions: [] });

const askOf = (q: Question | undefined) => {
  const item = q?.items[0];
  if (!item) return undefined;
  return { question: item.question || item.header, options: item.options.map((o) => o.label) };
};

const permissionText = (p: Permission | undefined): string =>
  p ? [p.tool, p.command].filter(Boolean).join(': ') || p.kind : '';

/** Chats that wait for the person have their channels opened during a call, so their questions are known. */
export const watchWaiting = (): (() => void) => {
  const open = new Map<ConvId, () => void>();
  const sync = () => {
    const waiting = conversationsByActivity().filter(
      (c) => c.state.k === 'waiting-question' || c.state.k === 'waiting-permission',
    );
    const want = new Set(waiting.map((c) => c.id));
    for (const c of waiting) if (!open.has(c.id)) open.set(c.id, hub.openConversation(c.machineId, c.id));
    for (const [id, release] of open)
      if (!want.has(id)) {
        release();
        open.delete(id);
      }
  };
  sync();
  const timer = window.setInterval(sync, 3000);
  return () => {
    window.clearInterval(timer);
    for (const release of open.values()) release();
    open.clear();
  };
};

export interface IRouteContext {
  machine: MachineId;
  conv: ConvId | null;
  screen: string;
  history: string[];
  theme: string;
  sidebarFolded: boolean;
}

const readingLine = (): string => {
  const r = reading();
  if (!r) return '';
  return `reading aloud: ${r.paused ? 'PAUSED' : 'on'} at part ${r.at + 1} of ${r.parts.length}`;
};

/** The router's request for a task. */
export const routeRequest = (task: string, said: string | undefined, ctx: IRouteContext) => {
  const now = Date.now();
  const convs = conversationsByActivity().filter((c) => !c.test);
  const settings = meStore.get();
  const s = settings.k === 'ready' ? settings.me.settings : null;
  const paneOpen = ctx.conv ? chatPaneOf(ctx.conv).open : false;
  return {
    task,
    said,
    screen: [
      ctx.conv ? `route: chat (id ${ctx.conv})` : 'route: no chat open',
      `theme: ${ctx.theme}`,
      `sidebar: ${ctx.sidebarFolded ? 'folded' : 'open'}`,
      `pane: ${paneOpen ? 'open' : 'closed'}`,
      readingLine(),
      ctx.screen,
    ]
      .filter(Boolean)
      .join('\n'),
    history: ctx.history.slice(-8),
    chats: convs.slice(0, 40).map((c) => {
      const g = gatesOf(c.id);
      return {
        id: c.id,
        title: c.title || '(без названия)',
        current: c.id === ctx.conv,
        state: stateOf(c),
        agent: c.provider,
        updated: whenOf(c.lastActivityAt, now),
        last: c.recap?.text ?? c.lastTurn?.snippet ?? '',
        ask: c.state.k === 'waiting-question' ? askOf(g.questions[0]) : undefined,
        permission: c.state.k === 'waiting-permission' ? permissionText(g.permissions[0]) : undefined,
      };
    }),
    catalog: {
      pages: PAGES.map(([id, label]) => ({ id, label })),
      sections: SETTINGS_SECTIONS.map((id) => ({ id, label: SECTION_LABEL[id] ?? id })),
      themes: [
        { id: 'dark', label: 'Тёмная / dark' },
        { id: 'light', label: 'Светлая / light' },
        { id: 'system', label: 'Как в системе / system' },
      ],
      settings: [
        {
          key: 'enterSends',
          label: 'Enter отправляет сообщение',
          values: ['on', 'off'],
          now: s ? (s.enterSends ? 'on' : 'off') : '',
        },
        {
          key: 'voiceMascot',
          label: 'Маскот голоса: h — знак H, face — человечек с лицом',
          values: ['h', 'face'],
          now: s?.voiceMascot ?? '',
        },
        { key: 'textSize', label: 'Размер текста', values: ['s', 'm', 'l'], now: s?.textSize ?? '' },
      ],
      agents: AGENTS.map(([id, label]) => ({ id, label })),
      efforts: EFFORTS,
      pane_tabs: PANE_TABS,
    },
  };
};

export interface IPlanHands {
  machine: MachineId;
  conv: ConvId | null;
  /** A ChatGPT talk is current (its title): «agent» is that talk writing, not a new chat. */
  chatHere?: string | null;
  /** The new chat's folder: the current chat's, or the start page's. */
  folder: string | null;
  setTheme: (theme: string) => void;
  sidebarFolded: boolean;
  /** Work for the current chat's agent (the old way: quick edits, the agent's turn, the progress said). */
  toAgent: (text: string) => void;
  show: (what: string, action: IRouteAction) => void;
  readAloud: (action: IRouteAction) => void;
  stop: () => void;
  /** A chat the voice gave work to (a message, a new chat): its answer is told when its agent is done. */
  follow?: (chat: ConvId, act: number) => void;
}

const MOODS = new Set(['neutral', 'joy', 'sad', 'thinking', 'surprised']);

/** Carries a plan out. Answers false when nothing of it could be done (the task then goes to the agent). */
export const runPlan = async (plan: IRoutePlan, hands: IPlanHands): Promise<boolean> => {
  if (plan.mood && MOODS.has(plan.mood)) setVoiceMood(plan.mood as VoiceMood, 5000);
  let done = 0;
  for (const a of plan.actions) {
    try {
      if (await runAction(a, hands)) done++;
    } catch {
      // One action failing does not stop the others; the voice says the plan's line anyway.
    }
  }
  return done > 0 || plan.actions.length === 0;
};

const chatOf = (id: string | undefined) => conversationsByActivity().find((c) => c.id === id) ?? null;

const titleOf = (c: Conversation | null) => c?.title || t('next.voice.cards.untitled');
const short = (text: string, n = 48) => (text.length > n ? `${text.slice(0, n - 1).trimEnd()}…` : text);
const PAGE_NAME = (page: string | undefined) =>
  PAGES.find(([id]) => id === page)?.[1].split(' / ')[0] ?? page ?? '';

const runAction = async (a: IRouteAction, h: IPlanHands): Promise<boolean> => {
  switch (a.do) {
    case 'agent':
      if (!a.text) return false;
      if (!h.conv && !h.chatHere) return newChat({ ...a, do: 'new_chat' }, h);
      h.toAgent(a.text);
      if (h.conv)
        h.follow?.(h.conv, addAct(t('next.voice.acts.agent', { text: short(a.text) }), ACT.waiting, h.conv));
      return true;
    case 'send': {
      const c = chatOf(a.chat);
      if (!c || !a.text) return false;
      sendMessage(c.machineId, {
        conversationId: c.id,
        text: a.text,
        attachments: [],
        mode: sendModeNow(c.id),
      });
      h.follow?.(c.id, addAct(t('next.voice.acts.send', { title: titleOf(c) }), ACT.waiting, c.id));
      return true;
    }
    case 'open_chat': {
      const c = chatOf(a.chat);
      if (!c) return false;
      goTo({ kind: ROUTE.chat, id: c.id });
      addAct(t('next.voice.acts.openChat', { title: titleOf(c) }), ACT.done, c.id);
      return true;
    }
    case 'answer': {
      const c = chatOf(a.chat);
      const q = c ? gatesOf(c.id).questions[0] : undefined;
      if (!c || !q) return false;
      const n = (a.option ?? 0) - 1;
      const valid = n >= 0 && n < (q.items[0]?.options.length ?? 0);
      const answers = valid ? [{ i: 0, choices: [n] }] : [{ i: 0, choices: [], text: a.text ?? '' }];
      if (!valid && !a.text) return false;
      await settled(answerQuestion(c.machineId, c.id, q.id, answers));
      const said = valid ? (q.items[0]?.options[n]?.label ?? '') : (a.text ?? '');
      h.follow?.(
        c.id,
        addAct(
          t('next.voice.acts.answer', { title: titleOf(c), answer: short(said, 32) }),
          ACT.waiting,
          c.id,
        ),
      );
      return true;
    }
    case 'permit': {
      const c = chatOf(a.chat);
      const p = c ? gatesOf(c.id).permissions[0] : undefined;
      if (!c || !p) return false;
      const option = permissionOption(p, a.allow === true);
      if (!option) return false;
      await settled(decidePermission(c.machineId, c.id, p.id, option));
      const key = a.allow === true ? 'next.voice.acts.allowed' : 'next.voice.acts.denied';
      h.follow?.(c.id, addAct(t(key, { title: titleOf(c) }), ACT.waiting, c.id));
      return true;
    }
    case 'new_chat':
      return newChat(a, h);
    case 'model': {
      // The chat's model or depth (the current one unless named): it takes at the chat's next safe moment.
      const c = chatOf(a.chat ?? h.conv ?? undefined);
      if (!c) return false;
      const name = (a.model ?? '').replace(/^.*:/, '');
      const model = name && name !== 'default' ? name : c.model?.id;
      if (!model) return false;
      const effort = (
        EFFORTS.includes(a.effort ?? '') ? a.effort : (c.model?.effort ?? null)
      ) as Effort | null;
      await settled(setModel(c.machineId, c.id, model, effort, false));
      addAct(
        t('next.voice.acts.model', { title: titleOf(c), model: [model, effort].filter(Boolean).join(' · ') }),
        ACT.done,
        c.id,
      );
      return true;
    }
    case 'open': {
      if (a.page === 'home') goTo(HOME);
      else if (a.page === 'settings') {
        const section = SETTINGS_SECTIONS.find((s) => s === a.section) ?? null;
        goTo({ kind: ROUTE.settings, section });
      } else {
        const kind = PAGES.find(([id]) => id === a.page)?.[0];
        if (!kind) return false;
        goTo({ kind } as Parameters<typeof goTo>[0]);
      }
      addAct(
        t('next.voice.acts.open', {
          name:
            a.page === 'settings' && a.section
              ? `${PAGE_NAME('settings')} · ${SECTION_LABEL[a.section as SettingsSection] ?? a.section}`
              : PAGE_NAME(a.page),
        }),
      );
      return true;
    }
    case 'theme':
      if (!a.value) return false;
      h.setTheme(a.value);
      addAct(t(`next.voice.acts.theme.${a.value === 'dark' || a.value === 'light' ? a.value : 'system'}`));
      return true;
    case 'sidebar': {
      const wantFolded = a.open === false;
      if (wantFolded !== h.sidebarFolded) toggleRail();
      addAct(t(wantFolded ? 'next.voice.acts.sidebarFolded' : 'next.voice.acts.sidebarOpen'));
      return true;
    }
    case 'pane': {
      if (!h.conv) return false;
      const conv = h.conv;
      chatPanes.update(conv, h.machine, (pane) => ({ ...pane, open: a.open !== false }));
      if (a.open !== false && a.tab) {
        const request =
          a.tab === 'browser' || a.tab === 'terminal' || a.tab === 'files' || a.tab === 'plan'
            ? ({ k: a.tab, target: null, reload: false } as const)
            : ({ k: 'legacy', kind: a.tab, target: null, reload: false } as const);
        chatPanes.queue(conv, h.machine, request as Parameters<typeof chatPanes.queue>[2]);
        // A queued tab is taken when the chat is shown: show it again.
        goTo({ kind: ROUTE.chat, id: conv }, { replace: true });
      }
      addAct(
        t(a.open === false ? 'next.voice.acts.paneClosed' : 'next.voice.acts.paneOpen', { tab: a.tab ?? '' }),
      );
      return true;
    }
    case 'setting': {
      const ok = await changeSetting(a);
      if (ok) addAct(t('next.voice.acts.setting', { key: a.key ?? '', value: a.value ?? '' }));
      return ok;
    }
    case 'show':
      h.show(a.what ?? 'text', a);
      return true;
    case 'read_aloud':
      h.readAloud(a);
      return true;
    case 'stop':
      h.stop();
      return true;
    default:
      return false;
  }
};

const changeSetting = async (a: IRouteAction): Promise<boolean> => {
  if (a.key === 'enterSends') return (await patchSettings({ enterSends: a.value === 'on' })).r === 'accepted';
  if (a.key === 'voiceMascot' && (a.value === 'h' || a.value === 'face'))
    return (await patchSettings({ voiceMascot: a.value })).r === 'accepted';
  if (a.key === 'textSize' && (a.value === 's' || a.value === 'm' || a.value === 'l'))
    return (await patchSettings({ textSize: a.value })).r === 'accepted';
  return false;
};

const newChat = async (a: IRouteAction, h: IPlanHands): Promise<boolean> => {
  if (!a.text) return false;
  // «claude:opus» from the catalog, or the agent and the model apart.
  const ref = a.model?.includes(':') ? a.model : `${a.agent ?? 'claude'}:${a.model ?? ''}`;
  const [who, name = ''] = ref.split(':');
  const provider = who === 'codex' ? 'codex' : 'claude';
  const id = name && name !== 'default' ? name : provider === 'claude' ? 'sonnet' : null;
  const fallback = a.orchestrate ? 'high' : 'medium';
  const effort = (EFFORTS.includes(a.effort ?? '') ? a.effort : fallback) as Effort;
  const text = a.orchestrate
    ? `${a.text}\n\nЭто большая задача: разбей её на части, раздай их отдельным чатам (delegate_task или ` +
      '`harnessly tab create -t claude-code --prompt …`), следи за ними и собери результат; сам координируй, а не ' +
      'делай всё в одном контексте.'
    : a.text;
  const handle = createConversation(h.machine, {
    where: { cwd: h.folder ?? '' },
    provider,
    model: id ? launchModel({ provider, model: id, effort, ultracode: false }) : null,
    first: { text, attachments: [] },
  });
  const act = addAct(
    t('next.voice.acts.newChat', { text: short(a.text, 40), model: id ?? provider }),
    ACT.doing,
  );
  const conv = await handle.conversation;
  if (!conv) {
    updateAct(act, { state: ACT.failed });
    return false;
  }
  updateAct(act, { state: ACT.waiting, chat: conv });
  // The new chat is opened; the voice goes on with the person there (its chat follows the screen, voice-hands.tsx).
  goTo({ kind: ROUTE.chat, id: conv });
  h.follow?.(conv, act);
  return true;
};
