// The search of everything (⌘K) on made-up data (`?dev=search`), for the eye and for screenshots: every tab, every
// kind of preview, the answer of the model as it is written, the loading and the empty states.
//
//   ?dev=search                    the dialog open on «Всё», nothing typed
//   &scope=chats|designs|docs|apps|settings|conn:tracker|conn:code
//   &q=<words>                     typed already (a question gets the answer)
//   &sel=<n>                       the row selected
//   &state=loading|empty           the tab on its way, or with nothing to list
//   &answer=stream|ready|none      the answer being written, written, or no model to write it
//   &theme=light|dark&lang=en|ru
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { DETAIL_ACT, NO_DETAIL, type IDetailSource, type ISearchDetail } from '@/next/features/search/detail';
import SearchDialog, {
  ANSWER,
  ROW_GO,
  ROW_WIDGET,
  type ISearchGroup,
  type ISearchRow,
  type TSearchAnswer,
} from '@/next/features/search/search-dialog';
import { SCOPE, connScope, countIn, inScope, scopeEmpty } from '@/next/features/search/smart-rows';
import { matching, termsOf } from '@/next/features/search/terms';
import { isQuestion } from '@/next/features/search/terms';
import { pinLocale, t } from '@/next/i18n';
import { recentChats, searchChats } from '@/next/features/search/search-chats';
import type { ISidebarChat } from '@/next/features/sidebar/types';
import { PortalContext } from '@/next/ui/portal';
import { ToastHost, toast } from '@/next/ui/toast';
import '@/next/styles/order';

const params = new URLSearchParams(window.location.search);
pinLocale(params.get('lang') === 'en' ? 'en' : 'ru');
const STATE = params.get('state');
const ANSWER_AS = params.get('answer');
const DETAIL_MS = 500;
/** A drawn stand-in for a board's thumbnail. */
const COVER = `data:image/svg+xml,${encodeURIComponent(
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 320 200"><rect width="320" height="200" fill="#1b1c2b"/><rect x="20" y="20" width="120" height="14" rx="4" fill="#3a3c58"/><rect x="20" y="52" width="180" height="56" rx="8" fill="#2a2c44"/><rect x="20" y="120" width="180" height="56" rx="8" fill="#2a2c44"/><rect x="216" y="52" width="84" height="124" rx="8" fill="#34365a"/></svg>',
)}`;

const row = (
  id: string,
  kind: string,
  icon: string,
  title: string,
  more: Partial<ISearchRow> = {},
): ISearchRow => ({ id, kind, icon, title, meta: '', ...more });

const NOW = Date.now();
const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY_MS = 24 * HOUR;
const seedChat = (id: string, title: string, ago: number, parentId?: string): ISidebarChat => ({
  id,
  title,
  status: 'none',
  pinned: false,
  projectId: null,
  createdAt: NOW - ago,
  lastPromptAt: NOW - ago,
  lastActivityAt: NOW - ago,
  terminal: false,
  canStopTerminal: false,
  ...(parentId ? { parentId } : {}),
});

/** The person's chats over two months and two an agent delegated (the search leaves those out): ordered and parted by use. */
const CHAT_SEED: readonly ISidebarChat[] = [
  seedChat('tab-a1', 'Корзина: скидка не применяется к подарочным наборам', 12 * MIN),
  seedChat(
    'tab-a5',
    'Проверить скидку на наборах в мобильной корзине (подчат — в поиске не виден)',
    40 * MIN,
    'tab-a1',
  ),
  seedChat('tab-a2', 'Экспорт отчёта в PDF: поля и нумерация страниц', 3 * HOUR),
  seedChat(
    'tab-a3',
    'Очень длинное название чата, которое не помещается в строку списка и обрезается многоточием, а справа видно целиком',
    DAY_MS + 2 * HOUR,
  ),
  seedChat('tab-a4', 'Миграция таблицы заказов', 3 * DAY_MS),
  seedChat('tab-a6', 'Аналитика v2 — фильтры по периодам и сравнение с прошлым годом', 4 * DAY_MS),
  seedChat('tab-a7', 'Тесты оплаты: повторный платёж после отказа банка', 5 * DAY_MS, 'tab-a4'),
  seedChat('tab-a8', 'Скидка по промокоду не суммируется с бонусами', 9 * DAY_MS),
  seedChat('tab-a9', 'Онбординг: письмо после регистрации', 20 * DAY_MS),
  seedChat('tab-a10', 'Ревью вёрстки страницы заказа', 45 * DAY_MS),
  seedChat('tab-a11', 'Справочник складов и остатков', 400 * DAY_MS),
];
const CHATS = recentChats(CHAT_SEED, true, NOW);

const DESIGNS: ISearchRow[] = [
  row('design:shop', 'board', 'pen-tool', 'Магазин — корзина и оплата', {
    meta: '12 нояб.',
    go: { k: ROW_GO.design, id: 'shop' },
    preview: { kind: 'Макет', text: 'Проект в библиотеке макетов.' },
  }),
  row('design:deck', 'board', 'pen-tool', 'Квартальный отчёт', {
    meta: '9 нояб.',
    go: { k: ROW_GO.design, id: 'deck' },
    preview: { kind: 'Макет', text: 'Проект в библиотеке макетов.' },
  }),
];

const DOCS: ISearchRow[] = [
  row('doc:cart-spec', 'doc', 'file-text', 'Корзина: правила скидок и подарочных наборов', {
    meta: '11 нояб.',
    go: { k: ROW_GO.link, url: 'https://docs.example.com/a/cart-spec' },
    preview: { kind: 'Документ', text: '' },
  }),
  row('doc:pdf', 'doc', 'file-text', 'Экспорт в PDF — разбор', {
    meta: '8 нояб.',
    go: { k: ROW_GO.link, url: 'https://docs.example.com/a/pdf' },
    preview: { kind: 'Документ', text: '' },
  }),
];

const APPS: ISearchRow[] = [
  row('appopen:tasks', 'app', 'layout-grid', 'Задачи', {
    snippet: 'Доска задач команды рядом с чатом',
    meta: 'Открыть приложение',
    preview: { kind: 'Приложение', text: 'Доска задач команды рядом с чатом' },
  }),
  row('appcmd:tab-a1|/tasks:new ', 'app', 'square-slash', '/new', {
    snippet: 'Завести задачу из чата',
    meta: 'Задачи',
    preview: { kind: 'Команда приложения', text: 'Завести задачу из чата' },
  }),
];

const settingRows = (enterOn: boolean, stop: string): ISearchRow[] => [
  row('setting:idle.stop', 'setting', 'sliders-horizontal', 'Останавливать простаивающие терминалы', {
    snippet: 'Основные · Через сколько часов простоя терминал агента останавливается',
    widget: {
      kind: ROW_WIDGET.pick,
      value: stop,
      label: stop === 'off' ? 'Выкл' : `${stop} ч`,
      options: [
        { value: 'off', label: 'Выкл' },
        { value: '1', label: '1 ч' },
        { value: '2', label: '2 ч' },
        { value: '6', label: '6 ч' },
      ],
    },
    go: { k: ROW_GO.setting, key: 'idle.stop', section: 'general' },
    preview: { kind: 'Настройка', text: 'Через сколько часов простоя терминал агента останавливается' },
  }),
  row('setting:limit.continue', 'setting', 'sliders-horizontal', 'Продолжать после сброса лимита', {
    snippet: 'Основные · Чат сам продолжит ход, когда лимит сбросится',
    widget: { kind: ROW_WIDGET.toggle, on: enterOn },
    go: { k: ROW_GO.setting, key: 'limit.continue', section: 'general' },
    preview: { kind: 'Настройка', text: 'Чат сам продолжит ход, когда лимит сбросится' },
  }),
  row('page-setting:enter', 'setting', 'sliders-horizontal', 'Enter отправляет', {
    snippet: 'Основные',
    go: { k: ROW_GO.section, section: 'general' },
    preview: { kind: 'Настройка', text: 'Меняется в «Настройках» → «Основные».' },
  }),
  row('section:keys', 'setting', 'settings', 'Клавиши', {
    snippet: 'Настройки',
    go: { k: ROW_GO.section, section: 'keys' },
    preview: { kind: 'Раздел настроек', text: 'Откроет этот раздел настроек.' },
  }),
];

const ISSUES: ISearchRow[] = [
  row('found:tracker:1', 'found', 'circle-dot', 'Скидка не применяется к подарочным наборам', {
    snippet: 'SHOP-412 · задача · В работе',
    meta: '12 нояб.',
    go: { k: ROW_GO.link, url: 'https://tracker.example.com/issue/SHOP-412' },
    preview: { kind: 'задача', text: '' },
  }),
  row('found:tracker:2', 'found', 'circle-dot', 'PDF: номера страниц съезжают на альбомных листах', {
    snippet: 'REP-88 · задача · Открыта',
    meta: '10 нояб.',
    go: { k: ROW_GO.link, url: 'https://tracker.example.com/issue/REP-88' },
    preview: { kind: 'задача', text: '' },
  }),
];

const MRS: ISearchRow[] = [
  row('found:code:1', 'found', 'git-pull-request', 'Корзина: скидка для подарочных наборов', {
    snippet: 'shop/web · запрос на слияние · opened',
    meta: '13 нояб.',
    go: { k: ROW_GO.review, url: 'https://code.example.com/shop/web/-/merge_requests/214' },
    preview: { kind: 'запрос на слияние', text: '' },
  }),
];

const DETAILS: Readonly<Record<string, ISearchDetail>> = {
  'tab-a1': {
    quotes: [
      {
        who: 'Агент',
        text: 'Нашёл: наборы исключались из базы скидки целиком, а не только их подарочная часть. Поправил расчёт в cart/discount.ts, добавил три теста на наборы. Открыл запрос на слияние, пайплайн зелёный.',
        when: '12 мин назад',
      },
    ],
    facts: [],
    lists: [],
    act: DETAIL_ACT.stop,
    link: 'https://app.example.com/chat/tab-a1',
  },
  'tab-a2': {
    quotes: [
      { who: 'От «Сборка отчётов»', text: 'Шаблон обновлён, можно пересобирать отчёт.', when: 'вчера' },
    ],
    facts: [],
    lists: [],
    act: DETAIL_ACT.resume,
    link: 'https://app.example.com/chat/tab-a2',
  },
  'design:shop': {
    cover: { src: COVER, alt: 'Корзина' },
    quotes: [],
    facts: [
      { label: 'Тип', value: 'Сайт' },
      { label: 'Досок', value: '6' },
      { label: 'Изменён', value: '3 часа назад' },
      { label: 'Работали', value: 'Анна, Борис' },
    ],
    lists: [
      {
        label: 'Доски',
        items: ['Корзина', 'Оплата', 'Пустая корзина'].map((title) => ({
          id: title,
          icon: 'layout-dashboard',
          title,
          go: { k: ROW_GO.design, id: 'shop' },
        })),
      },
    ],
    link: 'https://app.example.com/designly/shop',
  },
  'doc:cart-spec': {
    quotes: [],
    facts: [
      { label: 'Автор', value: 'Анна' },
      { label: 'Изменён', value: '2 дня назад' },
      { label: 'Обсуждений', value: '2' },
    ],
    lists: [
      {
        label: 'Последние версии',
        items: [
          { id: 'v3', icon: 'history', title: 'Наборы считаются отдельно', meta: 'Анна · 2 дня назад' },
          { id: 'v2', icon: 'history', title: 'Версия 2', meta: 'Борис · 5 дней назад' },
        ],
      },
    ],
    link: 'https://docs.example.com/a/cart-spec',
  },
  'setting:idle.stop': {
    quotes: [],
    text: 'Через сколько часов простоя терминал агента останавливается. Чат остаётся, агент проснётся от нового сообщения.',
    facts: [
      { label: 'Раздел', value: 'Основные' },
      { label: 'Сейчас', value: '2 ч' },
      { label: 'Варианты', value: 'Выкл, 1 ч, 2 ч, 6 ч' },
    ],
    lists: [],
  },
  'found:tracker:1': {
    quotes: [],
    text: 'Промокод на 10% не уменьшает сумму, если в корзине есть подарочный набор.\n\nОжидаем: скидка считается от всех товаров, кроме подарочной части набора.',
    facts: [
      { label: 'Статус', value: 'В работе' },
      { label: 'Номер', value: 'SHOP-412' },
      { label: 'Исполнитель', value: 'Анна' },
      { label: 'Тип', value: 'Bug · Major' },
      { label: 'Изменён', value: 'вчера' },
    ],
    lists: [],
    link: 'https://tracker.example.com/issue/SHOP-412',
  },
  'found:code:1': {
    quotes: [],
    facts: [
      { label: 'Статус', value: 'opened' },
      { label: 'Проект', value: 'shop/web' },
      { label: 'Ветка', value: 'fix/gift-set-discount' },
      { label: 'Пайплайн', value: 'success' },
      { label: 'Изменён', value: '12 мин назад' },
    ],
    lists: [],
    link: 'https://code.example.com/shop/web/-/merge_requests/214',
  },
  'appopen:tasks': {
    quotes: [],
    text: 'Доска задач команды рядом с чатом',
    facts: [{ label: 'Версия', value: '1.4.0' }],
    lists: [
      {
        label: 'Команды',
        items: [{ id: 'new', icon: 'square-slash', title: '/new', meta: 'Завести задачу из чата' }],
      },
    ],
  },
};

const ANSWER_TEXT =
  'Скидка не применялась, потому что набор целиком исключался из базы расчёта. Теперь исключается только подарочная часть; правка в запросе на слияние, пайплайн зелёный.';

const SearchGallery = () => {
  const portal = useRef<HTMLDivElement>(null);
  const [query, setQuery] = useState(params.get('q') ?? '');
  const [scope, setScope] = useState(params.get('scope') ?? SCOPE.all);
  const [limitOn, setLimitOn] = useState(true);
  const [stop, setStop] = useState('2');
  const [written, setWritten] = useState(0);
  const [kept] = useState(() => new Map<string, ISearchDetail>());

  useLayoutEffect(() => {
    const root = document.documentElement;
    const theme = params.get('theme') === 'light' ? 'light' : 'dark';
    root.dataset.theme = theme;
    root.dataset.hxTheme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, []);

  const typed = query.trim() !== '';
  const ask = typed && (isQuestion(query) || ANSWER_AS !== null);
  useEffect(() => {
    if (!ask || ANSWER_AS !== 'stream') return;
    const timer = setInterval(() => setWritten((n) => Math.min(ANSWER_TEXT.length, n + 6)), 60);
    return () => clearInterval(timer);
  }, [ask]);

  const all = useMemo((): ISearchGroup[] => {
    const settings = settingRows(limitOn, stop);
    return [
      { id: SCOPE.settings, label: 'Настройки', rows: settings },
      { id: SCOPE.chats, label: '', rows: CHATS },
      { id: SCOPE.designs, label: 'Макеты', rows: DESIGNS },
      { id: SCOPE.docs, label: 'Документы', rows: DOCS },
      { id: SCOPE.apps, label: 'Приложения', rows: APPS },
      { id: connScope('tracker'), label: 'Трекер', rows: ISSUES },
      { id: connScope('code'), label: 'GitLab', rows: MRS },
    ];
  }, [limitOn, stop]);

  const found = useMemo(() => {
    const terms = termsOf(query);
    return all
      .flatMap((group) =>
        group.id === SCOPE.chats
          ? searchChats(CHAT_SEED, query, terms, NOW)
          : [{ ...group, rows: matching(group.rows, terms, (item) => [item.title, item.snippet ?? '']) }],
      )
      .filter((group) => group.rows.length > 0);
  }, [all, query]);

  const idle = useMemo((): readonly ISearchGroup[] | null => {
    if (STATE === 'loading') return null;
    if (STATE === 'empty') return [];
    if (scope === SCOPE.all)
      return [
        {
          id: 'go-on',
          label: 'Продолжить',
          rows: [...recentChats(CHAT_SEED, false, NOW).slice(0, 3), DESIGNS[0], DOCS[0]].flatMap(
            (r) => r ?? [],
          ),
        },
      ];
    const label = scope.startsWith('conn:') ? 'Открыто сейчас' : undefined;
    return inScope(all, scope).map((group) => (label ? { ...group, label } : group));
  }, [all, scope]);

  const answer = ((): TSearchAnswer | null => {
    if (!ask) return null;
    if (ANSWER_AS === 'none') return { k: ANSWER.noModel };
    if (ANSWER_AS === 'stream' && written < ANSWER_TEXT.length)
      return written === 0 ? { k: ANSWER.wait } : { k: ANSWER.stream, text: ANSWER_TEXT.slice(0, written) };
    const first = CHATS[0];
    const second = MRS[0];
    return {
      k: ANSWER.ready,
      text: ANSWER_TEXT,
      sources: [first, second].flatMap((source) => (source ? [{ row: source, label: source.title }] : [])),
    };
  })();

  const detail = useMemo(
    (): IDetailSource => ({
      now: (item) => {
        const whole = DETAILS[item.id];
        return whole ? { ...whole, quotes: [], lists: [], text: undefined, cover: undefined } : NO_DETAIL;
      },
      loads: (item) => item.id in DETAILS,
      more: (item) =>
        new Promise((resolve) => setTimeout(() => resolve(DETAILS[item.id] ?? null), DETAIL_MS)),
    }),
    [],
  );

  const count = (id: string) => (typed ? countIn(found, id) : undefined);
  const sel = Number(params.get('sel') ?? '0');
  // The dialog keeps its own selection: the gallery moves it by the keys the person would press.
  useEffect(() => {
    if (!(sel > 0)) return;
    const input = document.querySelector<HTMLInputElement>('.hx-srch__field input');
    for (let i = 0; i < sel; i += 1)
      input?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  }, [sel]);

  return (
    <div className="hx-root hx-ng" data-testid="gallery">
      <PortalContext.Provider value={portal}>
        <SearchDialog
          open
          onClose={() => toast('close')}
          query={query}
          onQuery={setQuery}
          scopes={[
            { id: SCOPE.all, label: t('next.search.scopeAll'), count: count(SCOPE.all) },
            { id: SCOPE.chats, label: t('next.search.groupChats'), count: count(SCOPE.chats) },
            { id: SCOPE.designs, label: t('next.search.groupDesigns'), count: count(SCOPE.designs) },
            { id: SCOPE.docs, label: t('next.search.groupDocs'), count: count(SCOPE.docs) },
            { id: SCOPE.apps, label: t('next.search.groupApps'), count: count(SCOPE.apps) },
            { id: SCOPE.settings, label: t('next.search.groupSettings'), count: count(SCOPE.settings) },
            {
              id: connScope('tracker'),
              label: t('next.search.tabTracker'),
              hint: 'Tracker · tracker.example.com',
              count: count(connScope('tracker')),
            },
            {
              id: connScope('code'),
              label: 'GitLab',
              hint: 'code.example.com',
              count: count(connScope('code')),
            },
          ]}
          scope={scope}
          onScope={setScope}
          results={STATE === 'loading' ? null : inScope(found, scope)}
          recent={[]}
          idle={idle}
          empty={scopeEmpty(scope)}
          onPick={(item) => toast(`open: ${item.title}`)}
          smart={{
            answer,
            onAsk: () => toast('ask'),
            onSetting: async (key, value) => {
              if (key === 'limit.continue') setLimitOn(value === 'on');
              else setStop(value);
              return true;
            },
            onAddSource: () => toast('connections'),
            onPickModel: () => toast('pick a model'),
            detail,
            kept,
            beside: true,
            onAside: (item) => toast(`aside: ${item.title}`),
            onChatAct: async (chat, what) => {
              toast(`${what}: ${chat}`);
              return true;
            },
          }}
        />
        <ToastHost />
        <div ref={portal} className="hx-portal" />
      </PortalContext.Provider>
    </div>
  );
};

export default SearchGallery;
