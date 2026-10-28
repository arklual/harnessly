// The conversation's states on mock data, for the eye and for screenshots: ?dev=chat with the installation's
// next-ui feature. Each scene is one state of the feed, the questions, the queue and the field, including the eight of the
// design's «chat state» tweak. ?scene=<id>&theme=light|dark&lang=en|ru pick one; &chrome=0 hides the toolbar. Answers
// come after 700 ms, a scene that shows a failure makes its action fail. Never part of a production path.
import '@/next/dev/chat-gallery-config';
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/chat.css';
import '@/next/styles/composer.css';
import '@/next/styles/questions.css';

import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useRouter } from '@/platform/router';
import type { HandoffDraft, LimitAction, LimitHold } from '@/next/protocol/v2';
import { pinLocale, t } from '@/next/i18n';
import { runsByCall, WorkflowRuns, type TWorkflowTask } from '@/next/features/workflows/run';
import { sampleWorkflow } from '@/next/dev/workflow-sample';
import Feed from '@/next/features/chat/feed';
import { limitPlaceholder } from '@/next/features/chat/limit';
import LimitCard from '@/next/features/chat/limit-card';
import { MessagePictures, type IMessagePictures } from '@/next/features/chat/pictures';
import { RewindActions, type IRewindActions } from '@/next/features/chat/rewind';
import { DelegatedChats } from '@/next/features/chat/delegation';
import type { Conversation } from '@/next/protocol/v2';
import SelBar from '@/next/features/chat/selbar';
import { ChatLinks, type IChatLinks } from '@/next/features/chat/handoff';
import HandoffSheet, { SHEET_MODE, type TSheetMode } from '@/next/features/chat/handoff-sheet';
import type {
  AutoMark,
  Entry,
  FeedDensity,
  FeedItem,
  Message,
  Ms,
  Now,
  OutgoingItem,
  RefInfo,
  RewoundItem,
  SayEntry,
  ToolEntry,
  Turn,
  TurnItem,
  UserEntry,
} from '@/next/features/chat/types';
import type { ConvId } from '@/next/protocol/v2';
import Composer, { type IComposerQuote } from '@/next/features/composer/composer';
import type { IPickerAgent } from '@/next/features/composer/agents';
import DockBar from '@/next/features/composer/dock-bar';
import Queue from '@/next/features/composer/queue';
import type {
  ComposerAttachment,
  ComposerMode,
  DockNotice,
  QueuePause,
  QueueRow,
  Recording,
} from '@/next/features/composer/types';
import GateCard from '@/next/features/questions/gate-card';
import QuestionCard from '@/next/features/questions/question-card';
import type { Permission, PermissionScope, Question } from '@/next/features/questions/types';
import { quoteLabel, quoteTitle } from '@/next/stores/quotes';
import { useFeedKinds } from '@/next/stores/feed-kinds';
import { bindLaunch } from '@/next/ui/flight';
import { PortalContext } from '@/next/ui/portal';
import Segmented from '@/next/ui/segmented';
import { toast, ToastHost } from '@/next/ui/toast';

const DEVICE = 'this-browser';
const ANSWER_MS = 700;
/** A message put in the queue is handed to the agent after this long (the motion of the hand-over shows then). */
const HAND_OVER_MS = 2600;

const later = (ok = true, ms = ANSWER_MS): Promise<void> =>
  new Promise((resolve, reject) => setTimeout(() => (ok ? resolve() : reject(new Error('mock'))), ms));

// ---- mock builders

let seq = 0;
const nextId = (prefix: string) => `${prefix}${++seq}`;
const MIN = 60_000;
const CONV = 'tab-gallery' as ConvId;
/** The Workflow call of the workflow scene: its step in the feed draws the sample's run. */
const WORKFLOW_CALL = 'toolu_gallery_workflow';

const userEntry = (
  text: string,
  at: Ms,
  attachments: UserEntry['attachments'] = [],
  clientId: UserEntry['clientId'] = null,
): UserEntry => ({
  id: nextId('u'),
  turnId: null,
  at,
  rev: 1,
  t: 'user',
  text,
  attachments,
  clientId,
});
const say = (md: string, at: Ms): SayEntry => ({ id: nextId('s'), turnId: null, at, rev: 1, t: 'say', md });
const think = (text: string, at: Ms): Entry => ({
  id: nextId('k'),
  turnId: null,
  at,
  rev: 1,
  t: 'think',
  text,
});
const tool = (
  name: string,
  summary: string,
  at: Ms,
  ok: boolean | null = true,
  out: string | null = null,
): ToolEntry => ({
  id: nextId('t'),
  turnId: null,
  at,
  rev: 1,
  t: 'tool',
  toolUseId: nextId('tu'),
  tool: name,
  summary,
  ok,
  output: out === null ? null : { text: out, truncated: null },
});

interface ITurnSpec {
  prompt: string | null;
  at: Ms;
  minutes?: number;
  steps?: Entry[] | null;
  answer?: string | null;
  status?: Turn['status'];
  failure?: Turn['failure'];
  files?: Turn['files'];
  outputs?: Turn['outputs'];
  now?: Omit<Now, 'turnId'> | null;
  wait?: TurnItem['wait'];
  endNote?: string | null;
  role?: string | null;
  attachments?: UserEntry['attachments'];
  /** The prompt is the page's own message (the runtime matched it to the outbox). */
  clientId?: UserEntry['clientId'];
  /** The runtime went on by itself with the prompt. */
  auto?: AutoMark;
}

const turnOf = (spec: ITurnSpec): TurnItem => {
  const prompt =
    spec.prompt === null ? null : userEntry(spec.prompt, spec.at, spec.attachments, spec.clientId ?? null);
  const id = prompt?.id ?? nextId('turn');
  const steps = spec.steps === undefined ? [] : spec.steps;
  const status = spec.status ?? 'done';
  const live = status === 'live';
  const durationMs = live ? null : (spec.minutes ?? 2) * MIN;
  const answer = spec.answer ? say(spec.answer, spec.at + (durationMs ?? 0)) : null;
  const tools = (steps ?? []).filter((entry) => entry.t === 'tool').length;
  return {
    kind: 'turn',
    key: id,
    turn: {
      id,
      author: { provider: 'claude', role: spec.role ?? null },
      startedAt: spec.at,
      endedAt: durationMs === null ? null : spec.at + durationMs,
      durationMs,
      toolCount: steps === null ? 7 : tools,
      stepCount: steps === null ? 9 : steps.length,
      status,
      failure: spec.failure ?? null,
      answerEntryId: answer?.id ?? null,
      files: spec.files ?? [],
      outputs: spec.outputs,
    },
    prompt,
    woke: null,
    peer: null,
    note: null,
    steps,
    answer,
    now: spec.now ? { ...spec.now, turnId: id } : null,
    wait: spec.wait ?? null,
    endNote: spec.endNote ?? null,
    ...(spec.auto && { auto: spec.auto }),
  };
};

const outgoing = (
  text: string,
  state: Message['state'],
  createdAt: Ms,
  stateSince = createdAt,
): OutgoingItem => {
  const clientId = nextId('c');
  return {
    kind: 'outgoing',
    key: clientId,
    message: { clientId, text, attachments: [], mode: 'queue', createdAt, stateSince, state },
  };
};

const queued = (
  text: string,
  state: QueueRow['state'] = { k: 'queued', position: 1, reason: 'agent-busy' },
) => ({
  clientId: nextId('q'),
  text,
  attachments: [],
  state,
});

/** A design's notes as the field sends them (features/designly/annotation.ts): the raw blocks the agent reads. */
const NOTES_PAGE = `/a/ostrovok-dossier/p/${encodeURIComponent('Главная')}.html`;
const annotationBlock = (n: number, selector: string, text: string, comment: string, page = NOTES_PAGE) =>
  [
    `<annotation n="${n}" url="${page}" target="port:4100">`,
    `element: ${selector} — «${text}»`,
    `html: &lt;span class=&quot;hero__title&quot;&gt;${text}&lt;/span&gt;`,
    `comment: ${comment}`,
    '</annotation>',
  ].join('\n');
const ANNOTATED = [
  'Поправь шапку по заметкам',
  annotationBlock(1, 'main > section.hero > span', 'Острова Петербурга', 'Крупнее и жирнее, как в макете'),
  annotationBlock(
    2,
    'nav > a:nth-child(3)',
    'Контакты',
    'Убрать пункт',
    `/a/ostrovok-dossier/p/${encodeURIComponent('О проекте')}.html`,
  ),
].join('\n\n');

const ANSWER_MD = `Нашёл: после вчерашнего выката ABC-3101 форма оплаты ждёт \`orderId\`, а виджет Т-Банка шлёт \`order_id\`. Поправил маппинг в web/app/pay/form.ts и добавил тест, MR !214.

Что сделал:

1. Переименовал поле в адаптере ответа.
2. Добавил тест на оба варианта имени.
3. Прогнал e2e на телефоне — **зелёные**.

\`\`\`ts
export const toOrder = (raw: TbankAnswer): Order => ({
  id: raw.order_id ?? raw.orderId,
  amount: raw.amount / 100,
});
\`\`\`

| Проверка | Было | Стало |
| --- | --- | --- |
| Оплата с телефона | падает | проходит |
| Возврат | проходит | проходит |`;

const LONG_CODE = `Вот весь адаптер:

\`\`\`ts
${Array.from({ length: 22 }, (_, index) => `const field${index + 1} = read(raw, 'field_${index + 1}');`).join('\n')}
\`\`\``;

const REFS: Record<string, RefInfo> = {
  'ABC-3101': {
    kind: 'task',
    label: 'ABC-3101',
    title: 'Новая форма оплаты заказа',
    status: 'Testing',
    tone: 'accent',
    meta: 'Иван',
  },
  '!214': {
    kind: 'mr',
    label: '!214',
    title: 'fix(pay): order_id из виджета',
    status: 'Open',
    tone: 'warn',
    meta: null,
  },
};

const workSteps = (at: Ms): Entry[] => [
  think('Сначала посмотрю, что пишет форма в консоль, потом — что изменилось во вчерашнем выкате.', at),
  tool(
    'Bash',
    'git log --oneline -5 web/app/pay',
    at,
    true,
    'a1f3c2e fix: price rounding\n9be12d0 feat(pay): new widget',
  ),
  tool('Read', 'web/app/pay/form.ts', at),
  tool('Grep', 'orderId', at),
  tool('Read', 'web/app/pay/tbank.ts', at),
  {
    id: nextId('sub'),
    turnId: null,
    at,
    rev: 1,
    t: 'subagent',
    agentId: 'a1',
    agentType: 'Explore',
    description: 'Ищет все места, где читается ответ виджета',
    status: 'done',
    model: 'sonnet',
    durationMs: 41_000,
    output: {
      text: 'Нашёл 3 места, где ответ виджета читается без проверки поля `status`: **form.ts**, `tbank.ts`, `widget.ts`. Ни одно не отличает отказ банка от сетевой ошибки.\n\n- `form.ts`: читает `result.status` сразу\n- `tbank.ts`: падает на пустом теле\n- `widget.ts`: глотает исключение\n\n```ts\nconst status = result.status;\n```',
      truncated: null,
    },
  },
  {
    id: nextId('sub'),
    turnId: null,
    at,
    rev: 1,
    t: 'subagent',
    agentId: 'a2',
    agentType: 'ci',
    description: 'Ускоряет сборку CI',
    status: 'running',
    model: 'opus',
    durationMs: null,
    output: null,
  },
  tool('Edit', 'web/app/pay/form.ts', at),
  tool(
    'Bash',
    'npx vitest run tests/pay',
    at,
    false,
    'FAIL tests/pay/form.test.ts\n  expected 1200 to be 12',
  ),
  tool('Edit', 'web/app/pay/form.ts', at),
  tool('Bash', 'npx vitest run tests/pay', at, true, '✓ 14 tests passed'),
  {
    id: nextId('n'),
    turnId: null,
    at,
    rev: 1,
    t: 'notice',
    level: 'warn',
    code: 'slow-tests',
    detail: 'Тесты шли 4 минуты — дольше обычного',
  },
  {
    id: nextId('cp'),
    turnId: null,
    at,
    rev: 1,
    t: 'compaction',
    tokensBefore: 182_000,
    tokensAfter: 41_000,
    keep: [
      'Задача: оплата падает после выката',
      'Причина: order_id против orderId',
      'Сделано: маппинг и тест',
    ],
  },
];

const history = (now: Ms): FeedItem[] => [
  turnOf({
    prompt: 'Найди, почему после вчерашнего выката падает оплата заказа',
    at: now - 42 * MIN,
    minutes: 6,
    steps: workSteps(now - 42 * MIN),
    answer: ANSWER_MD,
    files: [
      { path: 'web/app/pay/form.ts', op: 'edited' },
      { path: 'tests/pay/form.test.ts', op: 'created' },
    ],
    outputs: [
      {
        kind: 'artifact',
        op: 'created',
        title: 'Разбор: оплата после выката',
        ref: 'https://docs.example.com/a/pay-razbor',
      },
      { kind: 'design', op: 'changed', title: 'checkout-v2', ref: 'checkout-v2' },
      { kind: 'doc', op: 'created', title: 'payments', ref: 'docs/payments.md' },
      {
        kind: 'mr',
        op: 'created',
        title: 'Оплата: маппинг order_id',
        ref: 'https://gitlab.example.com/ly/client/-/merge_requests/412',
      },
      {
        kind: 'stand',
        op: 'created',
        title: 'abc-3156.preview.example.com',
        ref: 'https://abc-3156.preview.example.com/',
      },
    ],
  }),
  turnOf({
    prompt: 'Покажи весь адаптер',
    at: now - 30 * MIN,
    minutes: 1,
    steps: [tool('Read', 'web/app/pay/tbank.ts', now - 30 * MIN)],
    answer: LONG_CODE,
  }),
];

/** What came after the history in the rewind scenes: turns to go back over. */
const laterTurns = (now: Ms): FeedItem[] => [
  turnOf({
    prompt: 'Переименуй orderId в paymentOrderId по всему модулю',
    at: now - 20 * MIN,
    minutes: 3,
    steps: null,
    answer: 'Переименовал в 6 файлах, тесты зелёные.',
    files: [
      { path: 'web/app/pay/form.ts', op: 'edited' },
      { path: 'web/app/pay/tbank.ts', op: 'edited' },
    ],
  }),
  turnOf({
    prompt: 'И добавь логирование ответа банка',
    at: now - 12 * MIN,
    minutes: 2,
    steps: null,
    answer: 'Добавил `log.debug` с телом ответа без карточных данных.',
  }),
];

const LIVE_NOW: Omit<Now, 'turnId'> = {
  thought: 'Тесты отправки зелёные, осталось проверить свёртку на длинном чате.',
  action: { tool: 'Bash', summary: 'Запускаю тесты', command: 'npx vitest run tests/unit/delivery' },
  word: 'Проверяю',
  at: Date.now(),
};

const liveTurn = (now: Ms, extra: Partial<ITurnSpec> = {}): TurnItem =>
  turnOf({
    prompt: 'Свёртку хода — как в Codex, и проверь на чате из 300 сообщений',
    at: now - 3 * MIN - 12_000,
    status: 'live',
    steps: [
      think('Свёртка уже есть в ленте, проверю её на длинном чате.', now - 3 * MIN),
      tool('Read', 'web/app/next/features/chat/turn.tsx', now - 3 * MIN),
      tool('Edit', 'web/app/next/features/chat/turn.tsx', now - 2 * MIN),
      // The machine's "now" carries the turn's latest thought, which is a step too: the tail must not show it twice.
      think(LIVE_NOW.thought ?? '', now - MIN - 2000),
      tool('Bash', 'npx vitest run tests/unit/delivery', now - MIN, null),
    ],
    now: LIVE_NOW,
    ...extra,
  });

const QUESTION = (now: Ms): Question => ({
  id: 'q1',
  turnId: 't1',
  askedAt: now,
  state: { k: 'open' },
  items: [
    {
      header: 'Раскладка',
      question: 'Как показывать шаги оплаты?',
      multi: false,
      options: [
        {
          label: 'По шагам',
          description: 'Каждый шаг — отдельный экран',
          preview: '[1] Данные\n[2] Карта\n[3] Готово',
        },
        {
          label: 'Одним экраном',
          description: 'Всё сразу, прокруткой',
          preview: '[Данные · Карта · Готово]',
        },
      ],
    },
    {
      header: 'Устройство',
      question: 'С чего начать?',
      multi: false,
      options: [
        { label: 'Сначала телефон', description: '87% покупок — с iPhone', preview: null },
        { label: 'Сначала десктоп', description: null, preview: null },
        { label: 'Оба сразу', description: null, preview: null },
      ],
    },
    {
      header: 'Отказ',
      question: 'Что показать при отказе банка?',
      multi: true,
      options: [
        { label: 'Причину от банка', description: null, preview: null },
        { label: 'Причину от организатора', description: null, preview: null },
        { label: 'Кнопку «Другая карта»', description: null, preview: null },
      ],
    },
  ],
});

const PERMISSION = (now: Ms): Permission => ({
  id: 'p1',
  kind: 'tool',
  askedAt: now,
  tool: 'Bash',
  command: 'kubectl rollout restart deploy/pay -n prod',
  cwd: '~/projects/pay',
  planMd: null,
  options: [
    { id: 'o1', label: 'Разрешить', scope: 'once' },
    { id: 'o2', label: 'Всегда для kubectl', scope: 'always' },
    { id: 'o3', label: 'Отказать', scope: 'deny' },
  ],
  state: { k: 'open' },
});

const PLAN = (now: Ms): Permission => ({
  id: 'p2',
  kind: 'plan',
  askedAt: now,
  tool: null,
  command: null,
  cwd: null,
  planMd: '1. Переименовать поле в адаптере.\n2. Добавить тест на оба имени.\n3. Прогнать e2e на телефоне.',
  options: [
    { id: 'r', label: 'Доработать', scope: 'revise' },
    { id: 'a', label: 'Спрашивать', scope: 'plan-ask' },
    { id: 'b', label: 'Без вопросов', scope: 'plan-auto' },
  ],
  state: { k: 'open' },
});

// ---- scenes

const SHEET_KIND = { handoff: 'handoff', merge: 'merge' } as const;
type TSheetKind = (typeof SHEET_KIND)[keyof typeof SHEET_KIND];

/** A context block as the runtime writes it (internal/handoff): the feed draws it as a card. */
const contextBlock = (tag: 'handoff' | 'fork-result', attrs: Record<string, string>, text: string): string =>
  `<${tag}${Object.entries(attrs)
    .map(([k, v]) => ` ${k}="${v}"`)
    .join('')}>\n<instructions>Read it and wait for the person.</instructions>\n\n${text}\n</${tag}>`;

const HANDOFF_NOTE =
  '## Goal\nПочинить падение оплаты заказа после вчерашнего выката.\n\n' +
  '## Decisions\n- Таймаут шлюза вынесен в конфиг, по умолчанию 15 с.\n- Повтор только для сетевых ошибок.\n\n' +
  '## Current state\nФикс в ветке, юнит-тесты зелёные, e2e не гонялся.\n\n' +
  '## Next steps\n1. Прогнать e2e оплаты.\n2. Открыть MR.\n\n' +
  '## Files\n- `internal/pay/gateway.go`\n- `internal/pay/gateway_test.go`\n\n' +
  '## Verify\n`go test ./internal/pay/...`\n\n## Gotchas\nСтенд отвечает медленно по утрам: таймаут не трогать.';

const SHEET_DRAFTS = {
  [SHEET_MODE.excerpt]: {
    mode: 'excerpt',
    text:
      '### The person (first request)\nНайди, почему после вчерашнего выката падает оплата заказа\n\n' +
      '_… 4 turns in between are left out …_\n\n### The person\nА повтор запроса нужен?\n\n' +
      '### Claude\nТолько для сетевых ошибок: на 4xx повтор опасен.\n\nCommands:\n- `go test ./internal/pay/...` → ok',
    tokens: 2140,
    turns: 3,
  },
  [SHEET_MODE.note]: { mode: 'note', text: HANDOFF_NOTE, tokens: 160, turns: 0 },
  [SHEET_MODE.result]: {
    mode: 'result',
    text: 'Сделано: повтор запроса к шлюзу только для сетевых ошибок.\nФайлы: `internal/pay/retry.go`.\nОсталось: e2e.',
    tokens: 40,
    turns: 0,
  },
  [SHEET_MODE.history]: {
    mode: 'history',
    tokens: 0,
    turns: 0,
    artifact: { id: '0123456789abcdef0123456789abcdef', bytes: 292_552_704 },
  },
} satisfies Record<TSheetMode, HandoffDraft>;

const CODEX_AGENT: IPickerAgent = {
  provider: 'codex',
  name: 'Codex',
  short: 'Codex',
  canSwitch: true,
  available: true,
  status: 'ready',
  models: [
    {
      id: 'gpt-5.5',
      label: 'GPT-5.5',
      hint: 'Codex',
      efforts: ['low', 'medium', 'high', 'xhigh'],
      defaultEffort: 'high',
    },
  ],
};

const GALLERY_LINKS: IChatLinks = { has: () => true, open: (id) => toast(id) };
/** What the runtime does with the hold for each answer: goes on now (the card folds away), or the time it goes on moves. */
const afterDecision = (scene: IScene, action: LimitAction): IScene => {
  const hold = scene.limited;
  if (!hold) return scene;
  if (action === 'now') return { ...scene, limited: null, queue: [], waitingLimit: false };
  return { ...scene, limited: { ...hold, continueAt: action === 'auto' ? hold.resetsAt : null } };
};

const limitHold = (now: Ms, patch: Partial<LimitHold> = {}): LimitHold => ({
  provider: 'claude',
  window: 'session',
  at: now - 2 * MIN,
  resetsAt: now + 134 * MIN,
  continueAt: now + 134 * MIN,
  ...patch,
});

/** The turn the usage limit cut short, the last one in a held chat. */
const limitedTurn = (now: Ms) =>
  turnOf({
    prompt: 'Собери сверку ещё и за сентябрь',
    at: now - 6 * MIN,
    minutes: 4,
    steps: [tool('Read', 'reports/august.csv', now)],
    status: 'failed',
    failure: { code: 'rate-limit', detail: null, retryAt: null },
  });

interface IScene {
  items: FeedItem[] | null;
  /** The pictures the messages name (ConvSnapshot.pictures), with their bytes. */
  pictures?: IMessagePictures;
  failed?: boolean;
  hasOlder?: boolean;
  density?: FeedDensity;
  offline?: boolean;
  machine?: string;
  question?: Question | null;
  gates?: Permission[];
  queue?: QueueRow[];
  paused?: QueuePause | null;
  waitingLimit?: boolean;
  /** The chat is held on its agent's usage limit: the card under the feed, the hint in the field. */
  limited?: LimitHold | null;
  notice?: DockNotice | null;
  mode?: ComposerMode;
  draft?: string;
  attachments?: ComposerAttachment[];
  /** Words picked in the answers (the «Annotate» button) and waiting in the field. */
  quotes?: IComposerQuote[];
  recording?: Recording | null;
  /** What the voice recognizes while the scene records: the words come into the field one by one. */
  dictate?: string[];
  /** Answers to the question and the gates fail. */
  answerFails?: boolean;
  /** The person's messages can be edited to go on from them; busy: the agent works and is stopped first. */
  rewind?: 'idle' | 'busy' | 'no-snapshot';
  /** The handoff's preview (a model of the other agent picked) or a fork's «Влить обратно», open from the start. */
  sheet?: TSheetKind | null;
  /** The chats the scene's agent delegated (their cards stand under the turns that started them). */
  delegated?: readonly Conversation[];
  /** The chat's dynamic workflows (its background tasks of kind workflow); the running ones are waited for. */
  workflows?: TWorkflowTask[];
  /** Changes over time: the scene's own script, run once it is on screen. */
  script?: (set: (patch: Partial<IScene>) => void, now: Ms) => () => void;
}

const machineNotice: DockNotice = { kind: 'machine', machine: 'runner-2', onStop: () => later() };
const limitNotice = (now: Ms, waiting: boolean): DockNotice => ({
  kind: 'limit',
  provider: 'claude',
  until: now + 74 * MIN,
  waiting,
  switchTo: 'Sonnet 5.5',
  onSwitch: () => later(),
  onUseKey: () => later(),
  onWait: () => {},
});

/** A mock screenshot: a page of the given size, its header, a card and lines, as an SVG. */
const mockShot = (w: number, h: number, hue: number, label: string): string => {
  const card = Math.round(w * 0.82);
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">` +
    `<rect width="${w}" height="${h}" fill="hsl(${hue} 30% 96%)"/>` +
    `<rect width="${w}" height="${Math.round(h * 0.09)}" fill="hsl(${hue} 55% 42%)"/>` +
    `<rect x="${(w - card) / 2}" y="${Math.round(h * 0.18)}" width="${card}" height="${Math.round(h * 0.42)}" rx="18" fill="#fff" stroke="hsl(${hue} 20% 86%)"/>` +
    `<rect x="${(w - card) / 2 + 28}" y="${Math.round(h * 0.24)}" width="${Math.round(card * 0.5)}" height="22" rx="6" fill="hsl(${hue} 20% 80%)"/>` +
    `<rect x="${(w - card) / 2 + 28}" y="${Math.round(h * 0.24) + 44}" width="${Math.round(card * 0.7)}" height="14" rx="5" fill="hsl(${hue} 15% 88%)"/>` +
    `<rect x="${(w - card) / 2 + 28}" y="${Math.round(h * 0.24) + 72}" width="${Math.round(card * 0.6)}" height="14" rx="5" fill="hsl(${hue} 15% 88%)"/>` +
    `<rect x="${(w - card) / 2}" y="${Math.round(h * 0.7)}" width="${card}" height="${Math.round(h * 0.08)}" rx="14" fill="hsl(${hue} 60% 50%)"/>` +
    `<text x="${w / 2}" y="${Math.round(h * 0.7 + h * 0.05)}" font-family="sans-serif" font-size="${Math.round(h * 0.03)}" fill="#fff" text-anchor="middle">${label}</text>` +
    `</svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
};

const SCRATCH = '/tmp/claude-1001/-home-coder-workspace-acme/503cb9c6/scratchpad';
const PICTURE_SRC: Record<string, string> = {
  desk: mockShot(1440, 900, 220, 'Оплатить'),
  phone: mockShot(390, 844, 150, 'Оплатить'),
  paste: mockShot(800, 600, 20, 'Ошибка'),
};
const GALLERY_PICTURES: IMessagePictures = {
  pictures: [
    { id: 'paste', text: '/home/coder/.harnessly/uploads/unknown/unknown/1792894270382-e2af4dde-image.png' },
    { id: 'desk', text: `${SCRATCH}/checkout-desktop.png` },
    { id: 'phone', text: `${SCRATCH}/checkout-phone.png` },
  ],
  srcOf: (picture) => PICTURE_SRC[picture.id] ?? '',
};

// What other agents' sessions and the harness wrote: the cards a delivery of Claude Code's teammates becomes, and the
// harness's quiet notes.
const REVIEW_MD =
  'Spec ❌, quality needs fixes. Full review: `/home/coder/workspace/harnessly/task-4-review.md`\n\n' +
  '**Findings**\n' +
  '- **Important:** the retry path drops the error and reports success (`internal/outbox/retry.go:88`).\n' +
  '- **Important:** no test covers a refused retry.\n' +
  '- Minor: the log line repeats the conversation id.\n' +
  '- Minor: `peers.css` keeps an unused selector.\n\n' +
  'Fix the two important ones first; the minor ones can go with the next change.';

const peerTurn = (now: Ms): TurnItem => {
  const item = turnOf({
    prompt: null,
    at: now - 6 * MIN,
    minutes: 1,
    steps: null,
    answer: 'Беру ревью в работу: сначала путь повтора.',
  });
  item.peer = {
    id: nextId('p'),
    turnId: item.turn.id,
    at: now - 6 * MIN,
    rev: 1,
    t: 'peer',
    peers: [
      {
        from: 'rev-t4',
        kind: 'done',
        color: 'yellow',
        event: null,
        summary: null,
        sentAt: now - 6 * MIN,
        agentId: null,
        model: null,
        md: REVIEW_MD,
      },
      {
        from: 'build-codex-pane',
        kind: 'message',
        color: 'blue',
        event: null,
        summary: 'ABC-2656: панель готова',
        sentAt: now - 7 * MIN,
        agentId: null,
        model: null,
        md: 'Промежуточный отчёт: панель собрана, осталось проверить тёмную тему.',
      },
      {
        from: 'w1',
        kind: 'idle',
        color: 'green',
        event: null,
        summary: null,
        sentAt: now - 8 * MIN,
        agentId: null,
        model: null,
        md: '',
      },
      {
        from: 'lead',
        kind: 'event',
        color: null,
        event: 'shutdown_request',
        summary: null,
        sentAt: now - 9 * MIN,
        agentId: null,
        model: null,
        md: 'Работа закончена, сворачивайся.',
      },
      {
        from: 'Debug stuck delivered queue items',
        kind: 'message',
        color: null,
        event: null,
        summary: null,
        sentAt: now - 10 * MIN,
        agentId: 'a68d2e2f655d5c31e',
        model: 'sonnet',
        md: 'Root cause of the 4 stuck queue rows: they come from the browser IndexedDB `feeds` store, not from `outbox`.',
      },
    ],
  };
  return item;
};

const noteTurn = (now: Ms, detail: string): TurnItem => {
  const item = turnOf({
    prompt: null,
    at: now - 3 * MIN,
    minutes: 1,
    steps: null,
    answer: 'CI ещё идёт, жду.',
  });
  item.note = {
    id: nextId('n'),
    turnId: item.turn.id,
    at: now - 3 * MIN,
    rev: 1,
    t: 'notice',
    level: 'info',
    code: 'system-note',
    detail,
  };
  return item;
};

const firstTurnOf = (items: FeedItem[] | null): string | undefined =>
  (items ?? []).find((x) => x.kind === 'turn')?.key;

const NO_DELEGATED: readonly Conversation[] = [];

/** A chat the scene's agent delegated, as the runtime lists it. */
const delegatedChat = (
  id: string,
  title: string,
  createdAt: Ms,
  provider: Conversation['provider'],
  model: string,
  state: Conversation['state'],
): Conversation => ({
  id: `tab-${id}`,
  machineId: 'runner-2',
  projectId: null,
  cwd: '/home/coder/workspace/harnessly',
  provider,
  model: { id: model, effort: null },
  title,
  titleSource: 'user',
  pinned: false,
  createdAt,
  lastActivityAt: createdAt,
  lastPromptAt: createdAt,
  state,
  seenAt: createdAt,
  lastTurn: null,
  queue: { count: 0, paused: null },
  gate: null,
  parentId: 'tab-parent',
  rev: 1,
});

const SCENES: Record<string, { title: string; build: (now: Ms) => IScene }> = {
  rich: {
    title: 'Диаграммы и HTML',
    build: (now) => ({
      items: [
        turnOf({
          prompt: 'Нарисуй, как запрос идёт через сервисы, и покажи макет карточки',
          at: now - 6 * MIN,
          minutes: 2,
          steps: null,
          answer:
            'Запрос идёт так:\n\n```mermaid\nflowchart LR\n  A[Браузер] --> B[Шлюз]\n  B --> C{Кеш}\n  C -->|есть| D[Ответ]\n  C -->|нет| E[Сервис]\n  E --> D\n```\n\n' +
            'Макет карточки:\n\n```html\n<style>.c{padding:16px;border-radius:12px;background:#eef;max-width:320px}h3{margin:0 0 6px}</style>\n<div class="c"><h3>Концерт</h3><p>12 октября, 20:00</p><button>Купить</button></div>\n```\n\n' +
            'Если скрипт пытается достать страницу, он упирается в песочницу:\n\n```html\n<p id="o">проверка</p>\n<script>document.getElementById("o").textContent = "script ran: " + (typeof parent.document)</script>\n```\n\n' +
            'Описание лежит здесь:\n\nhttps://docs.example.com/p/payments-flow\n\nа схема тут:\n\n[Схема оплаты](https://boards.example.com/b/abc123)\n\nА это ломаный граф:\n\n```mermaid\nflowchart LR\n  A -->\n```\n',
        }),
      ],
    }),
  },
  delegated: {
    title: 'Поручения другим агентам',
    build: (now) => ({
      items: [
        turnOf({
          prompt: 'Пусть Codex посмотрит дифф сайдбара, а тесты прогонит отдельный агент',
          at: now - 6 * MIN,
          minutes: 3,
          steps: null,
          answer: 'Поручил ревью Codex и прогон тестов отдельному чату — пришлю итог, когда оба закончат.',
        }),
      ],
      delegated: [
        delegatedChat('review', 'Ревью диффа сайдбара', now - 5 * MIN, 'codex', 'gpt-5.5', {
          k: 'working',
          since: now - 5 * MIN,
          turnId: null,
          compacting: false,
        }),
        delegatedChat('tests', 'Прогон тестов сайдбара', now - 4 * MIN, 'claude', 'claude-sonnet-5-5', {
          k: 'idle',
          since: now - MIN,
        }),
      ],
    }),
  },
  pictures: {
    title: 'Картинки в сообщениях',
    build: (now) => ({
      pictures: GALLERY_PICTURES,
      items: [
        turnOf({
          prompt:
            'Вот что видит покупатель после оплаты\n/home/coder/.harnessly/uploads/unknown/unknown/1792894270382-e2af4dde-image.png',
          at: now - 6 * MIN,
          minutes: 3,
          steps: null,
          answer:
            'Поправил отступы у кнопки оплаты и проверил на телефоне и на десктопе.\n\n' +
            `Скриншот: ${SCRATCH}/checkout-desktop.png\n\nНа телефоне: ${SCRATCH}/checkout-phone.png`,
        }),
        turnOf({
          prompt: 'А этот файл? /tmp/claude-1001/gone.png',
          at: now - 2 * MIN,
          minutes: 1,
          steps: null,
          answer: 'Такого файла нет — путь остаётся текстом, карточки нет.',
        }),
      ],
    }),
  },
  annotate: {
    title: 'Аннотирование',
    build: (now) => ({
      items: [
        ...history(now),
        turnOf({
          prompt: 'Как теперь работает свёртка длинной ленты?',
          at: now - 9 * MIN,
          minutes: 2,
          steps: null,
          answer:
            'Лента держит в памяти окно из последних сорока ходов, а всё, что старше, свёрнуто в одну строку «Показать ещё». ' +
            'При прокрутке вверх окно сдвигается плавно, без скачка позиции, потому что высоты ходов запоминаются по ключу и не пересчитываются заново.\n\n' +
            'Если ход растёт прямо сейчас, окно не двигается, пока человек не отпустит прокрутку: так новый текст не уводит экран из-под пальца.',
        }),
      ],
    }),
  },
  normal: { title: 'Обычное', build: (now) => ({ items: history(now), hasOlder: true }) },
  rewind: {
    title: 'Продолжить с прежнего сообщения',
    build: (now) => ({ items: [...history(now), ...laterTurns(now)], rewind: 'idle' }),
  },
  'rewind-busy': {
    title: 'Продолжить отсюда — агент работает',
    build: (now) => ({ items: [...history(now), liveTurn(now)], rewind: 'busy', mode: 'working' }),
  },
  'rewind-nofiles': {
    title: 'Продолжить отсюда — без снимка файлов',
    build: (now) => ({ items: [...history(now), ...laterTurns(now)], rewind: 'no-snapshot' }),
  },
  rewound: {
    title: 'Откатено N ходов',
    build: (now) => {
      const [first] = history(now);
      const mark: RewoundItem = {
        kind: 'rewound',
        key: 'rewound:0',
        n: 0,
        at: now - 4 * MIN,
        turns: 3,
        files: true,
        undoUntil: now + 10_000,
      };
      return {
        items: [
          first,
          mark,
          turnOf({
            prompt: 'Покажи только маппинг полей, без всего адаптера',
            at: now - 4 * MIN,
            minutes: 1,
            steps: null,
            answer: 'Маппинг в `toOrder`: `order_id` → `orderId`, остальные поля без изменений.',
          }),
        ],
        rewind: 'idle',
      };
    },
  },
  fork: {
    title: 'Ответвление',
    build: (now) => {
      const [first, ...rest] = history(now);
      return {
        items: [
          first,
          {
            kind: 'origin',
            key: 'origin',
            at: now - 30 * MIN,
            fork: true,
            fromId: 'tab-gallery',
            title: 'Падает оплата заказа',
            from: null,
            to: null,
          },
          ...rest,
        ],
      };
    },
  },
  handoff: {
    title: 'Передача контекста',
    build: (now) => ({
      items: [
        {
          kind: 'origin',
          key: 'origin',
          at: now - 20 * MIN,
          fork: false,
          fromId: 'tab-gallery',
          title: 'Падает оплата заказа',
          from: 'Opus 5.5',
          to: 'GPT-5.5',
        },
        turnOf({
          prompt: contextBlock(
            'handoff',
            {
              from: 'tab-gallery',
              title: 'Падает оплата заказа',
              agent: 'claude',
              model: 'claude-opus-5-5',
              mode: 'note',
            },
            HANDOFF_NOTE,
          ),
          at: now - 20 * MIN,
          minutes: 1,
          steps: null,
          answer:
            'Принял: фикс таймаута в ветке, осталось прогнать e2e оплаты и открыть MR. Жду следующего сообщения.',
        }),
      ],
    }),
  },
  merged: {
    title: 'Итог ответвления',
    build: (now) => ({
      items: [
        ...history(now).slice(0, 1),
        turnOf({
          prompt: contextBlock(
            'fork-result',
            { from: 'tab-gallery', title: 'Повтор запроса', agent: 'claude' },
            SHEET_DRAFTS.result.text,
          ),
          at: now - 4 * MIN,
          minutes: 1,
          steps: null,
          answer: 'Учёл: повтор только для сетевых ошибок уже в ветке, e2e остаётся за нами.',
        }),
      ],
    }),
  },
  workflow: {
    title: 'Workflow',
    build: (now) => ({
      workflows: [sampleWorkflow(CONV, now, WORKFLOW_CALL)],
      items: [
        ...history(now).slice(0, 1),
        turnOf({
          prompt: 'Проверь оплату целиком и поправь всё, что найдут ревьюеры',
          at: now - 15 * MIN,
          minutes: 1,
          steps: [
            think(
              'Запущу workflow: три ревьюера, потом починка по каждой находке и проверка.',
              now - 15 * MIN,
            ),
            {
              ...tool(
                'Workflow',
                'Workflow checkout-review',
                now - 15 * MIN,
                true,
                'Workflow launched in background. Task ID: wq7m2kx4d',
              ),
              toolUseId: WORKFLOW_CALL,
            },
          ],
          answer:
            'Запустил workflow `wq7m2kx4d`: ревью идёт в три потока, потом починка и проверка. Напишу, когда закончится.',
        }),
      ],
    }),
  },
  'handoff-sheet': {
    title: 'Передать в Codex',
    build: (now) => ({ items: history(now), sheet: SHEET_KIND.handoff }),
  },
  'merge-sheet': {
    title: 'Влить обратно',
    build: (now) => ({ items: history(now), sheet: SHEET_KIND.merge }),
  },
  peers: {
    title: 'Сообщения сессий',
    build: (now) => ({
      items: [
        ...history(now).slice(0, 1),
        peerTurn(now),
        noteTurn(now, 'Goal check-in: background work still running'),
        noteTurn(now, 'Monitor fired\ntask-id: b9\noutput-file: /tmp/b9.out\nThe goal is not met yet.'),
      ],
    }),
  },
  full: { title: 'Полный вид', build: (now) => ({ items: history(now), density: 'full' }) },
  loading: { title: 'Загрузка', build: () => ({ items: null }) },
  'load-failed': { title: 'Не загрузилась', build: () => ({ items: null, failed: true }) },
  live: {
    title: 'Агент работает',
    build: (now) => ({
      items: [...history(now), liveTurn(now)],
      mode: 'working',
      queue: [queued('И проверь свёртку на чате из 300 сообщений'), queued('Потом обнови README')],
    }),
  },
  notes: {
    title: 'Заметки дизайна',
    build: (now) => ({
      items: [
        ...history(now),
        turnOf({ prompt: ANNOTATED, at: now - 5 * MIN, minutes: 1, answer: 'Сделал крупнее, пункт убрал.' }),
      ],
      mode: 'working',
      queue: [queued(ANNOTATED), queued(annotationBlock(1, 'footer > p', 'Подвал', 'Светлее'))],
    }),
  },
  questions: {
    title: 'Вопросы',
    build: (now) => ({
      items: [...history(now), liveTurn(now, { now: null })],
      question: QUESTION(now),
      mode: 'answer',
    }),
  },
  permission: {
    title: 'Разрешение и план',
    build: (now) => {
      const turn = liveTurn(now, { now: null });
      turn.steps = [
        ...(turn.steps ?? []),
        { id: 'g1', turnId: null, at: now, rev: 1, t: 'gate', permissionId: 'p1' },
        { id: 'g2', turnId: null, at: now, rev: 1, t: 'gate', permissionId: 'p2' },
      ];
      return {
        items: [...history(now).slice(0, 1), turn],
        gates: [PERMISSION(now), PLAN(now)],
        mode: 'working',
      };
    },
  },
  sending: {
    title: 'Отправка',
    build: (now) => ({
      items: [
        ...history(now),
        outgoing('Отправляется только что', { k: 'submitting' }, now),
        outgoing('Отправляется дольше обычного', { k: 'typing' }, now - 5000),
        outgoing('Агент ещё не принял', { k: 'in-input' }, now - 20_000),
        outgoing(
          'Не отправилось: машина отказала',
          { k: 'failed', code: 'refused', retryable: true, at: now },
          now - MIN,
        ),
      ],
    }),
  },
  delivery: {
    title: 'Доставка: не дошло, отправлено',
    build: (now) => ({
      items: [
        ...history(now),
        // Sent, and its turn does not show it: it fades away a moment after the scene opens.
        outgoing(
          'Отправлено и гаснет',
          { k: 'accepted', entryId: 'c:0f0e0d0c-0000-4000-8000-00000000aa01#0' },
          now - 8000,
        ),
        outgoing('Отправляется дольше обычного', { k: 'typing' }, now - 5000),
      ],
      // Typed, and the agent did not take it in 30 s: back in the queue for Повторить; the runtime did not know the
      // second one at all; the third waits behind them.
      queue: [
        queued('как дела', { k: 'queued', position: 1, reason: 'stalled' }),
        queued('И глянь логи оплаты', { k: 'failed', code: 'unconfirmed', retryable: true, at: now }),
        queued('Потом обнови README', { k: 'queued', position: 2, reason: 'behind' }),
      ],
    }),
  },
  offline: {
    title: 'Нет связи',
    build: (now) => ({
      items: [...history(now), outgoing('Уйдёт, когда вернётся связь', { k: 'submitting' }, now)],
      offline: true,
    }),
  },
  composer: {
    title: 'Поле: вложения',
    build: (now) => ({
      items: history(now),
      draft: 'Посмотри скриншот и лог',
      attachments: [
        { id: 'a1', name: 'скриншот-оплаты.png', kind: 'image', status: 'ready', error: null },
        { id: 'a2', name: 'лог-оплаты.txt', kind: 'file', status: 'uploading', error: null },
        { id: 'a3', name: 'запись-экрана.mov', kind: 'file', status: 'error', error: 'больше 50 МБ' },
      ],
    }),
  },
  // A message with a picture: the field loses its file row as well as its text when it sends, and the bubble brings it.
  picture: {
    title: 'Поле: картинка',
    build: (now) => ({
      items: history(now),
      draft: 'Вот как выглядит оплата',
      attachments: [{ id: 'p1', name: 'оплата.png', kind: 'image', status: 'ready', error: null }],
    }),
  },
  // Claude Code wraps a burst it takes for a paste in pasted_content whoever made it: the dictated message from this page
  // stays a plain bubble, the same wrapper typed into the agent's own terminal is a pasted block.
  dictated: {
    title: 'Диктовка и вставка',
    build: (now) => ({
      items: [
        turnOf({
          prompt: `<pasted_content id="d1">\n${DICTATED}\n</pasted_content id="d1">`,
          clientId: 'c-dictated',
          at: now - 12 * MIN,
          minutes: 1,
          steps: null,
          answer: 'Понял, убираю макеты со страницы дизайнли.',
        }),
        turnOf({
          prompt: `Вот лог:\n<pasted_content id="d2">\n${LOG}\n</pasted_content id="d2">`,
          at: now - 5 * MIN,
          minutes: 1,
          steps: null,
          answer: 'В логе упал шаг 4.',
        }),
      ],
    }),
  },
  recording: {
    title: 'Запись голоса',
    build: (now) => ({ items: history(now), recording: { startedAt: now - 7000, busy: false, level: null } }),
  },
  // The voice as it sounds: a level that rises and falls with phrases, and words that come into the field.
  voice: {
    title: 'Голос: диктовка вживую',
    build: (now) => ({
      items: history(now),
      draft: 'Посмотри ещё одну вещь.',
      recording: { startedAt: now - 3000, busy: false, level: speech },
      dictate: DICTATION,
    }),
  },
  'voice-busy': {
    title: 'Голос: распознаём',
    build: (now) => ({
      items: history(now),
      draft: 'Посмотри, почему на телефоне кнопка «Вернуть» уезжает за экран, и поправь отступы снизу.',
      recording: { startedAt: now - 21000, busy: true, level: speech },
    }),
  },
  // The eight states of the design's «chat state» tweak.
  'agent-crashed': {
    title: 'Агент упал',
    build: (now) => ({
      items: [
        ...history(now),
        turnOf({
          prompt: 'Свёртку хода — как в Codex',
          at: now - 4 * MIN,
          minutes: 3,
          steps: [
            tool('Read', 'web/app/next/features/chat/turn.tsx', now),
            tool('Bash', 'npx vitest run', now, false),
          ],
          status: 'failed',
          failure: {
            code: 'agent-exited',
            detail: 'Claude Code упал: на машине кончилась память',
            retryAt: null,
          },
        }),
      ],
    }),
  },
  stopped: {
    title: 'Остановлено вами',
    build: (now) => ({
      items: [
        ...history(now),
        turnOf({
          prompt: 'Свёртку хода — как в Codex',
          at: now - 4 * MIN,
          minutes: 3,
          steps: [
            tool('Read', 'web/app/next/features/chat/turn.tsx', now),
            tool('Edit', 'web/app/next/features/chat/turn.tsx', now),
          ],
          status: 'interrupted',
          endNote: 'вопрос снят',
        }),
      ],
      queue: [queued('И проверь свёртку на чате из 300 сообщений'), queued('Потом обнови README')],
      paused: { reason: 'user', at: now },
    }),
  },
  'machine-silent': {
    title: 'Машина не отвечает',
    build: (now) => ({
      items: [...history(now), liveTurn(now, { wait: 'machine' })],
      notice: machineNotice,
      mode: 'working',
    }),
  },
  'answer-failed': {
    title: 'Ответ не прошёл',
    build: (now) => ({
      items: [...history(now), liveTurn(now, { now: null })],
      question: QUESTION(now),
      mode: 'answer',
      answerFails: true,
    }),
  },
  'answered-elsewhere': {
    title: 'Ответ с другого устройства',
    build: (now) => ({
      items: [...history(now), liveTurn(now, { now: null })],
      question: QUESTION(now),
      mode: 'answer',
      draft: 'Сначала телефон, а таблицу для организаторов — потом',
      script: (set) => {
        const timer = setTimeout(() => {
          const answered: Question = {
            ...QUESTION(now),
            state: { k: 'answered', at: Date.now(), byDevice: 'MacBook' },
          };
          const turn = liveTurn(now, { now: null });
          turn.steps = [
            ...(turn.steps ?? []),
            {
              id: 'ans1',
              turnId: null,
              at: Date.now(),
              rev: 1,
              t: 'answers',
              questionId: 'q1',
              pairs: [
                { q: 'Раскладка', a: 'По шагам' },
                { q: 'Устройство', a: 'Сначала телефон' },
                { q: 'Отказ', a: 'Причину от организатора\nСумму возврата\nСсылку на заявку в кабинете' },
                {
                  q: 'Какой текст поставить под «Создайте аккаунт»? Сейчас вернул исходный.',
                  a: 'Про задачи, которые команда видит на одной доске, без лишних слов и обещаний',
                },
              ],
            },
          ];
          set({ question: answered, items: [...history(now), turn], mode: 'working' });
        }, 1500);
        return () => clearTimeout(timer);
      },
    }),
  },
  limit: {
    title: 'Лимит исчерпан',
    build: (now) => ({
      items: [
        ...history(now),
        turnOf({
          prompt: 'Собери сверку ещё и за сентябрь',
          at: now - 2 * MIN,
          minutes: 1,
          steps: [tool('Read', 'reports/august.csv', now)],
          status: 'failed',
          failure: { code: 'rate-limit', detail: null, retryAt: now + 74 * MIN },
        }),
      ],
      notice: limitNotice(now, false),
      queue: [queued('И добавь разбивку по банкам')],
      waitingLimit: true,
    }),
  },
  'limit-auto': {
    title: 'Лимит: продолжит сам',
    build: (now) => ({ items: [...history(now), limitedTurn(now)], limited: limitHold(now) }),
  },
  'limit-cancelled': {
    title: 'Лимит: не продолжать',
    build: (now) => ({
      items: [...history(now), limitedTurn(now)],
      limited: limitHold(now, { continueAt: null }),
      queue: [queued('И добавь разбивку по банкам', { k: 'queued', position: 1, reason: 'paused' })],
      waitingLimit: true,
    }),
  },
  'limit-unknown': {
    title: 'Лимит: время неизвестно',
    build: (now) => ({
      items: [...history(now), limitedTurn(now)],
      limited: limitHold(now, { resetsAt: null, continueAt: null }),
    }),
  },
  'limit-weekly': {
    title: 'Лимит: недельный',
    build: (now) => ({
      items: [...history(now), limitedTurn(now)],
      limited: limitHold(now, {
        window: 'weekly',
        resetsAt: now + 3 * 24 * 60 * MIN,
        continueAt: now + 3 * 24 * 60 * MIN,
      }),
    }),
  },
  'auto-limit': {
    title: 'Продолжил после сброса лимита',
    build: (now) => ({
      items: [
        ...history(now),
        limitedTurn(now),
        turnOf({
          prompt: 'Продолжай с того места, где остановился',
          at: now - 4 * MIN,
          minutes: 3,
          steps: [tool('Read', 'reports/september.csv', now)],
          answer: 'Сверка за сентябрь готова: расхождений нет.',
          auto: { why: 'limit', own: true },
        }),
        turnOf({
          prompt: 'И добавь разбивку по банкам',
          at: now - 2 * MIN,
          minutes: 1,
          answer: 'Разбивка добавлена в конец отчёта.',
          clientId: 'c-own',
          auto: { why: 'limit', own: false },
        }),
      ],
    }),
  },
  'auto-pending': {
    title: 'Сброс лимита: продолжаю',
    build: (now) => ({
      items: [
        ...history(now),
        limitedTurn(now),
        {
          ...outgoing('Продолжай с того места, где остановился', { k: 'typing' }, now),
          auto: { why: 'limit', own: true },
        },
      ],
    }),
  },
  'auto-restart': {
    title: 'Продолжил после перезапуска',
    build: (now) => ({
      items: [
        ...history(now),
        turnOf({
          prompt: 'Прогони e2e на телефоне',
          at: now - 9 * MIN,
          minutes: 2,
          status: 'interrupted',
          steps: [tool('Bash', 'npx playwright test', now, null)],
        }),
        turnOf({
          prompt: 'Продолжай — работа была прервана перезапуском',
          at: now - 5 * MIN,
          minutes: 3,
          steps: [tool('Bash', 'npx playwright test', now)],
          answer: 'Прогон закончен: 42 теста прошли.',
          auto: { why: 'restart', own: true },
        }),
      ],
    }),
  },
  'after-reload': {
    title: 'После перезагрузки',
    build: (now) => {
      const items = history(now).map((item) => (item.kind === 'turn' ? { ...item, steps: null } : item));
      return {
        items: [
          ...items,
          outgoing(
            'Собери сверку ещё и за сентябрь',
            { k: 'failed', code: 'unconfirmed', retryable: true, at: now },
            now - 10 * MIN,
          ),
        ],
        draft: 'И добавь разбивку по банкам',
        hasOlder: true,
      };
    },
  },
  'queue-failed': {
    title: 'Очередь не ушла',
    build: (now) => ({
      items: [...history(now), liveTurn(now)],
      mode: 'working',
      queue: [queued('Прогони ещё e2e на телефоне'), queued('И обнови скриншоты в MR')],
      script: (set) => {
        const first = setTimeout(() => {
          set({
            items: [
              ...history(now),
              liveTurn(now, { status: 'done', minutes: 4, answer: 'Свёртка работает и на 300 ходах.' }),
            ],
            mode: 'idle',
            queue: [
              queued('Прогони ещё e2e на телефоне', { k: 'submitting' }),
              queued('И обнови скриншоты в MR'),
            ],
          });
        }, 900);
        const second = setTimeout(() => {
          set({
            queue: [
              queued('Прогони ещё e2e на телефоне', {
                k: 'failed',
                code: 'launch-failed',
                retryable: true,
                at: Date.now(),
              }),
              queued('И обнови скриншоты в MR'),
            ],
            paused: { reason: 'launch-failed', at: Date.now() },
          });
        }, 2000);
        return () => {
          clearTimeout(first);
          clearTimeout(second);
        };
      },
    }),
  },
  long: {
    title: 'Лента на 5000 ходов',
    build: (now) => ({
      items: Array.from({ length: 5000 }, (_, index) =>
        turnOf({
          prompt: `Сообщение ${index + 1}: проверь шаг ${index + 1} сверки`,
          at: now - (5000 - index) * MIN,
          minutes: 1 + (index % 4),
          steps: null,
          answer:
            index % 3 === 0
              ? `Готово, шаг ${index + 1} сходится с ABC-${3000 + (index % 90)}.`
              : `Шаг ${index + 1} проверен.`,
        }),
      ),
      hasOlder: true,
    }),
  },
};

// ---- the voice: a level like speech (phrases of syllables with pauses) and the words it recognizes

const DICTATED =
  'Братан, ну это вот жопа, что ты сделал справа, потому что на данный момент мне вообще не нравится. То есть ты взял просто и айфрейм условно вставил сюда страницу дизайнли. Но это полная фигня. Тут именно надо просто выкинуть макеты. Всё. Что вот поиск нужно? И макеты вставить. Зачем здесь ещё одно поле ввода, что нарисуем? И так далее.';
const LOG = Array.from(
  { length: 8 },
  (_, i) => `step ${i + 1}: ${i < 3 ? 'ok' : 'FAILED'} in ${i * 7 + 12} ms`,
).join('\n');

const speech = () => {
  const t = performance.now() / 1000;
  const syllables = Math.sin(t * 9) * 0.5 + 0.5;
  const phrase = Math.max(0, Math.sin(t * 0.9 - 0.3));
  return Math.min(1, 0.03 + phrase * syllables * 0.5 + Math.random() * 0.04);
};

const DICTATION =
  'Посмотри, почему на телефоне кнопка «Вернуть» уезжает за экран, и поправь отступы снизу. Проверь на айфоне с вырезом.'.split(
    ' ',
  );

// ---- the scene on screen

const SceneView = ({ build }: { build: (now: Ms) => IScene }) => {
  const [startedAt] = useState(() => Date.now());
  const [scene, setScene] = useState<IScene>(() => build(startedAt));
  const shown = useFeedKinds();
  const [draft, setDraft] = useState(scene.draft ?? '');
  const [decided, setDecided] = useState<Record<string, PermissionScope>>({});
  const [loadingOlder, setLoadingOlder] = useState(false);
  const script = useRef(scene.script);

  useEffect(() => {
    const run = script.current;
    if (!run) return;
    return run((patch) => setScene((current) => ({ ...current, ...patch })), startedAt);
  }, [startedAt]);

  // The voice of a scene that records: its words come into the field one by one, after the text that was there.
  const dictate = scene.dictate;
  const recordedAt = scene.recording?.startedAt;
  useEffect(() => {
    if (!dictate || recordedAt === undefined) return;
    const words = dictate;
    let said = 0;
    const base = scene.draft ?? '';
    const timer = setInterval(() => {
      said += 1;
      setDraft(`${base ? `${base} ` : ''}${words.slice(0, said).join(' ')}`);
      if (said >= words.length) clearInterval(timer);
    }, 380);
    return () => clearInterval(timer);
  }, [dictate, recordedAt, scene.draft]);

  // The rewinds of the scene, on its own items: the turns from the message on fold into one line, «Вернуть» brings them
  // back.
  const branches = useRef<FeedItem[][]>([]);
  const rewindMode = scene.rewind;
  const rewindActions = useMemo<IRewindActions | null>(() => {
    if (!rewindMode) return null;
    return {
      can: (turnId) => turnId !== firstTurnOf(scene.items),
      busy: rewindMode === 'busy',
      plan: (turnId) =>
        later().then(() => ({
          entryId: turnId,
          turns: Math.max(
            1,
            (scene.items ?? []).length - (scene.items ?? []).findIndex((x) => x.key === turnId),
          ),
          files:
            rewindMode === 'no-snapshot'
              ? null
              : {
                  at: Date.now() - 20 * MIN,
                  changes: [
                    { path: 'web/app/pay/form.ts', change: 'changed' },
                    { path: 'web/app/pay/tbank.ts', change: 'changed' },
                    { path: 'web/app/pay/log.ts', change: 'removed' },
                  ],
                  more: 0,
                },
        })),
      rewind: (turnId, text, files) =>
        later().then(() =>
          setScene((current) => {
            const items = current.items ?? [];
            const at = items.findIndex((x) => x.key === turnId);
            const left = items.slice(at).filter((x) => x.kind === 'turn');
            branches.current.push(left);
            const now = Date.now();
            const mark: RewoundItem = {
              kind: 'rewound',
              key: `rewound:${now}`,
              n: branches.current.length - 1,
              at: now,
              turns: left.length,
              files,
              undoUntil: now + 10_000,
            };
            return {
              ...current,
              items: [...items.slice(0, at), mark, liveTurn(now, { prompt: text, at: now, steps: [] })],
              mode: 'working',
            };
          }),
        ),
      stop: () =>
        later().then(() => setScene((current) => ({ ...current, rewind: 'idle', mode: undefined }))),
      undo: () =>
        later().then(() =>
          setScene((current) => {
            const items = current.items ?? [];
            const at = items.findIndex((x) => x.kind === 'rewound');
            const back = branches.current.pop() ?? [];
            return { ...current, items: [...items.slice(0, at), ...back], mode: undefined };
          }),
        ),
      rewound: (n) =>
        later().then(() => {
          const turns = (branches.current[n] ?? laterTurns(startedAt)).filter((x) => x.kind === 'turn');
          return {
            turns: turns.map((x) => x.turn),
            entries: turns.flatMap((x): Entry[] => [x.prompt, x.answer].flatMap((e) => (e ? [e] : []))),
            beforeTurnId: null,
          };
        }),
    };
  }, [rewindMode, scene.items, startedAt]);

  const fails = scene.answerFails === true;
  const dropQueued = (clientId: string) =>
    later().then(() =>
      setScene((current) => ({
        ...current,
        queue: (current.queue ?? []).filter((row) => row.clientId !== clientId),
      })),
    );

  // The field sends: with the agent idle the bubble goes to the feed and is confirmed a moment later, with it working
  // the message goes to the queue and is handed over later, so the motion of both can be seen (?scene=live).
  const send = () => {
    const text = draft.trim();
    if (!text) return;
    const clientId = nextId('c');
    bindLaunch(clientId);
    setDraft('');
    // What was attached goes with the words and leaves the field.
    const files = (scene.attachments ?? [])
      .filter((file) => file.status === 'ready')
      .map((file) => ({ id: file.id, name: file.name, kind: file.kind, mime: `${file.kind}/*` }));
    if (files.length > 0) setScene((current) => ({ ...current, attachments: [] }));
    const at = Date.now();
    const message = (state: Message['state']): FeedItem => ({
      kind: 'outgoing',
      key: clientId,
      message: {
        clientId,
        text,
        attachments: files,
        mode: 'queue',
        createdAt: at,
        stateSince: Date.now(),
        state,
      },
    });
    if (scene.mode === 'working') {
      setScene((current) => ({
        ...current,
        queue: [
          ...(current.queue ?? []),
          {
            clientId,
            text,
            attachments: [],
            state: { k: 'queued', position: 1, reason: 'agent-busy', wait: null },
          },
        ],
      }));
      setTimeout(() => {
        setScene((current) => ({
          ...current,
          queue: (current.queue ?? []).filter((row) => row.clientId !== clientId),
          items: [...(current.items ?? []), message({ k: 'submitting' })],
        }));
      }, HAND_OVER_MS);
      return;
    }
    setScene((current) => ({ ...current, items: [...(current.items ?? []), message({ k: 'submitting' })] }));
    setTimeout(() => {
      setScene((current) => ({
        ...current,
        items: (current.items ?? []).map((item) =>
          item.key === clientId ? message({ k: 'accepted', entryId: null }) : item,
        ),
      }));
    }, ANSWER_MS * 2);
  };

  const editQueued = (clientId: string) =>
    later().then(() => {
      const row = (scene.queue ?? []).find((r) => r.clientId === clientId);
      setScene((current) => ({
        ...current,
        queue: (current.queue ?? []).filter((r) => r.clientId !== clientId),
      }));
      if (row) setDraft((have) => [row.text, have].filter(Boolean).join('\n\n'));
    });

  const flowRuns = useMemo(() => runsByCall(scene.workflows ?? []), [scene.workflows]);
  const flowsAtWork = useMemo(
    () => (scene.workflows ?? []).filter((x) => x.status === 'running'),
    [scene.workflows],
  );

  const gates = scene.gates ?? [];
  const renderGate = (permissionId: string): ReactNode => {
    const permission = gates.find((gate) => gate.id === permissionId);
    if (!permission) return null;
    return (
      <GateCard
        permission={permission}
        device={DEVICE}
        decided={decided[permissionId] ?? null}
        onDecide={(id, optionId) =>
          later(!fails).then(() => {
            const scope = permission.options.find((option) => option.id === optionId)?.scope ?? 'once';
            setDecided((current) => ({ ...current, [id]: scope }));
            setScene((current) => ({
              ...current,
              gates: (current.gates ?? []).map((gate) =>
                gate.id === id
                  ? { ...gate, state: { k: 'answered', at: Date.now(), byDevice: DEVICE } }
                  : gate,
              ),
            }));
          })
        }
      />
    );
  };

  const dock = (
    <>
      <QuestionCard
        question={scene.question ?? null}
        device={DEVICE}
        ownAnswer={draft}
        onAnswer={() =>
          later(!fails).then(() =>
            setScene((current) => ({
              ...current,
              question: current.question
                ? { ...current.question, state: { k: 'answered', at: Date.now(), byDevice: DEVICE } }
                : null,
            })),
          )
        }
      />
      <DockBar notice={scene.notice ?? null} />
      <Queue
        rows={scene.queue ?? []}
        paused={scene.paused ?? null}
        waitingLimit={scene.waitingLimit ?? false}

        onRemove={dropQueued}
        onEdit={editQueued}
        onResume={() => later().then(() => setScene((current) => ({ ...current, paused: null })))}
        onRetry={dropQueued}
      />
      <Composer
        value={draft}
        onChange={setDraft}
        onSubmit={send}
        mode={scene.mode ?? 'idle'}
        placeholder={scene.limited ? limitPlaceholder(scene.limited, startedAt) : undefined}
        onStop={() => later()}
        picker={{
          value: { provider: 'claude', model: 'opus', effort: 'high', ultracode: false },
          agents: [
            {
              provider: 'claude',
              name: 'Claude Code',
              short: 'Claude',
              canSwitch: true,
              available: true,
              status: 'ready',
              models: [
                {
                  id: 'opus',
                  label: 'Opus',
                  hint: 'Самая сильная',
                  efforts: ['low', 'medium', 'high', 'xhigh', 'max'],
                  defaultEffort: 'high',
                },
              ],
            },
          ],
          locked: true,
          onChange: () => {},
          handoff: {
            agents: [CODEX_AGENT],
            onPick: () => setScene((current) => ({ ...current, sheet: SHEET_KIND.handoff })),
          },
        }}
        attachments={scene.attachments ?? []}
        quotes={scene.quotes}
        onRemoveQuote={(id) =>
          setScene((current) => ({ ...current, quotes: (current.quotes ?? []).filter((q) => q.id !== id) }))
        }
        onFiles={() => {}}
        onRetryAttachment={() => {}}
        onRemoveAttachment={(id) =>
          setScene((current) => ({
            ...current,
            attachments: (current.attachments ?? []).filter((file) => file.id !== id),
          }))
        }
        contexts={[
          { id: 'project', label: 'Проект', icon: 'folder' },
          { id: 'task', label: 'Задача', icon: 'list-checks' },
        ]}
        onContext={() => {}}
        recording={scene.recording ?? null}
        onMic={() =>
          setScene((current) => ({
            ...current,
            recording: { startedAt: Date.now(), busy: false, level: speech },
          }))
        }
        onCancelRecording={() => setScene((current) => ({ ...current, recording: null }))}
        onFinishRecording={() => setScene((current) => ({ ...current, recording: null }))}
        maxText={20_000}
        contextRing={{ used: 52_000, limit: 200_000, compacting: false }}
      />
    </>
  );

  const sheet = scene.sheet ?? null;
  const closeSheet = () => setScene((current) => ({ ...current, sheet: null }));
  const draftOf = (mode: TSheetMode) =>
    later(true, mode === SHEET_MODE.note || mode === SHEET_MODE.result ? 1600 : ANSWER_MS).then(() => ({
      ok: true as const,
      draft: SHEET_DRAFTS[mode],
    }));

  return (
    <ChatLinks.Provider value={GALLERY_LINKS}>
      <MessagePictures.Provider value={scene.pictures ?? null}>
        <WorkflowRuns value={flowRuns}>
          <RewindActions value={rewindActions}>
            <SelBar
              onAdd={(text, note) => {
                const quote = { id: `q${Date.now()}`, text, note };
                setScene((current) => ({
                  ...current,
                  quotes: [
                    ...(current.quotes ?? []),
                    {
                      id: quote.id,
                      label: quoteLabel(quote),
                      title: quoteTitle(quote),
                      remark: note !== null,
                    },
                  ],
                }));
              }}
            />
            <DelegatedChats.Provider value={scene.delegated ?? NO_DELEGATED}>
              <Feed
                items={scene.items}
                failed={scene.failed}
                onRetryLoad={() => setScene((current) => ({ ...current, failed: false }))}
                hasOlder={scene.hasOlder ?? false}
                loadingOlder={loadingOlder}
                onLoadOlder={() => {
                  setLoadingOlder(true);
                  setTimeout(() => {
                    setLoadingOlder(false);
                    setScene((current) => ({
                      ...current,
                      hasOlder: false,
                      items: current.items && [...history(startedAt - 300 * MIN), ...current.items],
                    }));
                  }, ANSWER_MS);
                }}
                density={scene.density ?? 'compact'}
                offline={scene.offline ?? false}
                limit={
                  <LimitCard
                    limited={scene.limited ?? null}
                    clock={Date.now}
                    controls
                    onDecide={(action) =>
                      later().then(() => setScene((current) => afterDecision(current, action)))
                    }
                  />
                }
                enterAfter={startedAt}
                machineName={scene.machine ?? 'runner-2'}
                refs={REFS}
                onOpenRef={() => {}}
                onOpenFile={(path) => toast(path)}
                onOpenOutput={(output) => toast(`${output.kind}: ${output.ref}`)}
                shown={shown}
                onLoadSteps={(turnId) =>
                  new Promise<boolean>((done) =>
                    setTimeout(() => {
                      setScene((current) => ({
                        ...current,
                        items:
                          current.items?.map((item) =>
                            item.kind === 'turn' && item.key === turnId
                              ? { ...item, steps: workSteps(item.turn.startedAt) }
                              : item,
                          ) ?? null,
                      }));
                      done(true);
                    }, ANSWER_MS),
                  )
                }
                onContinueTurn={() => later()}
                onRetryTurn={() => later()}
                onRetryMessage={() => later(false)}
                onDeleteMessage={(clientId) =>
                  later().then(() =>
                    setScene((current) => ({
                      ...current,
                      items: current.items?.filter((item) => item.key !== clientId) ?? null,
                    })),
                  )
                }
                renderGate={renderGate}
                onForkTurn={() => later().then(() => toast('fork'))}
                dock={dock}
                waiting={
                  flowsAtWork.length > 0
                    ? { agents: flowsAtWork, onOpen: () => toast(t('next.bg.waitingTip')) }
                    : null
                }
              />
            </DelegatedChats.Provider>
          </RewindActions>
          <HandoffSheet
            open={sheet === SHEET_KIND.handoff}
            onClose={closeSheet}
            title="Передать в Codex · GPT-5.5"
            lead="Новый чат в той же папке начнёт с этого текста. Чат на Opus 5.5 останется как есть."
            modes={[SHEET_MODE.excerpt, SHEET_MODE.note, SHEET_MODE.history]}
            sendLabel="Передать"
            onDraft={draftOf}
            onSend={() => later()}
          />
          <HandoffSheet
            open={sheet === SHEET_KIND.merge}
            onClose={closeSheet}
            title="Влить обратно в «Падает оплата заказа»"
            lead="Итог этого ответвления уйдёт сообщением в исходный чат — в его очередь."
            modes={[SHEET_MODE.result]}
            sendLabel="Влить"
            onDraft={draftOf}
            onSend={() => later()}
          />
        </WorkflowRuns>
      </MessagePictures.Provider>
    </ChatLinks.Provider>
  );
};

// ---- the page

type TTheme = 'light' | 'dark';
type TLang = 'en' | 'ru';

const ChatGallery = () => {
  const router = useRouter();
  const { query } = router;
  const portalRef = useRef<HTMLDivElement>(null);
  const sceneId = typeof query.scene === 'string' && query.scene in SCENES ? query.scene : 'normal';
  const theme: TTheme = query.theme === 'dark' ? 'dark' : 'light';
  const lang: TLang = query.lang === 'en' ? 'en' : 'ru';
  const chrome = query.chrome !== '0';

  // The language is read when a text is drawn: it is pinned before the scene renders, and the scene remounts on a change.
  pinLocale(lang);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);

  const go = (patch: Record<string, string>) => {
    const params = new URLSearchParams(window.location.search);
    Object.entries(patch).forEach(([key, value]) => params.set(key, value));
    void router.replace(`${window.location.pathname}?${params.toString()}`);
  };

  const scenes = useMemo(() => Object.entries(SCENES), []);

  return (
    <PortalContext value={portalRef}>
      <div className="hx-root hx-gallery" data-chrome={chrome || undefined}>
        {chrome && (
          <nav className="hx-gallery__bar">
            <div className="hx-gallery__scenes">
              {scenes.map(([id, scene]) => (
                <button
                  key={id}
                  type="button"
                  className="hx-gallery__scene"
                  aria-pressed={id === sceneId}
                  onClick={() => go({ scene: id })}
                >
                  {scene.title}
                </button>
              ))}
            </div>
            <Segmented
              label="Theme"
              value={theme}
              options={[
                { value: 'light', label: 'Light' },
                { value: 'dark', label: 'Dark' },
              ]}
              onChange={(value) => go({ theme: value })}
            />
            <Segmented
              label="Language"
              value={lang}
              options={[
                { value: 'ru', label: 'RU' },
                { value: 'en', label: 'EN' },
              ]}
              onChange={(value) => go({ lang: value })}
            />
          </nav>
        )}
        <main className="hx-main hx-gallery__frame">
          <SceneView key={`${sceneId}-${lang}`} build={SCENES[sceneId].build} />
        </main>
        <ToastHost />
        <div ref={portalRef} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

export default ChatGallery;
