// The smart notice on made-up events (`?dev=notifs`), for the eye and for screenshots: the real plate with buttons
// that make each kind of waiting, and a field at the bottom it stands over.
//
//   ?dev=notifs                    a mock window of the app (chats, a chat, its field) and the buttons in its side
//   &set=pile                      a code, a question and a permission at once; &set=one a question; &set=many ten
//                                  The side lists the metrics' events as the chat would send them.
//   &set=drops                     every kind put away into the drops of the right column, long names among them
//   &open=1                        the card open
//   &place=top-right|…             where it stands (the middle of the top edge by default); the side switches it
//   &settings=1                    Settings → Notifications under the buttons
//   &theme=light|dark&lang=en|ru
import { useCallback, useEffect, useLayoutEffect, useState } from 'react';
import { playSound, unlockSound } from '@/next/features/notifications/ping';
import { NoticesPage } from '@/next/bindings/settings-notices';
import SmartNotice, {
  SMART_CODE,
  SMART_KIND,
  type ISmartItem,
  type TSmartDetail,
  type TSmartKind,
} from '@/next/features/smart-notice/smart-notice';
import { pinLocale, type TLang } from '@/next/i18n';
import { NOTICE_PLACES, type TNoticePlace } from '@/next/stores/notices';
import { ToastHost, toast } from '@/next/ui/toast';
import Drops from '@/next/features/smart-notice/drops';
import { fly, snap } from '@/next/features/smart-notice/fly';
import { looksSecret } from '@/next/features/smart-notice/looks-secret';
import {
  SLOT_ANSWER,
  slotIcon,
  slotWaits,
  type ISlotNotice,
  type ISlotSource,
} from '@/next/features/smart-notice/slot-body';
import { KIND_RANK, useFront, useNewsClock } from '@/next/features/smart-notice/flow';
import {
  NOTICE_ACTION,
  noticeEvent,
  setNoticeSink,
  type IUiEvent,
} from '@/next/features/smart-notice/metrics';
// The chat's own stylesheets in the chat's order: a class of another feature that happens to share a name shows here
// as it does in a chat (composer.css once laid its file-drop hint over the drops).
import '@/next/styles/order';
import '@/next/dev/notifs-gallery.css';

const params = new URLSearchParams(window.location.search);
const LANG: TLang = params.get('lang') === 'en' ? 'en' : 'ru';
pinLocale(LANG);
const FIRST_PLACE: TNoticePlace = NOTICE_PLACES.find((p) => p === params.get('place')) ?? 'top-center';
/** The places to compare by eye: as Dynamic Island, in the corner, over the field. */
const PLACES: readonly { place: TNoticePlace; label: string }[] = [
  { place: 'top-center', label: 'Сверху по центру' },
  { place: 'top-right', label: 'Справа сверху' },
  { place: 'bottom-center', label: 'Над полем' },
];
/** News lives this long here too (bindings/smart-notice.tsx NEWS_MS), its clock paused while held. */
const NEWS_MS = 7000;
const NEWS: ReadonlySet<TSmartKind> = new Set([
  SMART_KIND.done,
  SMART_KIND.failed,
  SMART_KIND.limit,
  SMART_KIND.info,
]);
const twoFrames = (run: () => void) => requestAnimationFrame(() => requestAnimationFrame(run));
const CHATS = ['Лендинг кофейни', 'Миграция базы', 'Публикация пакета', 'Меню на сайте', 'Отчёт за октябрь'];

type TChatSample = 'question' | 'permission' | 'code' | 'machine' | 'long' | 'done' | 'failed';
/** What apps and agents ask through the open slot, one of each kind. */
type TSlotSample =
  | 'slot-choice'
  | 'slot-confirm'
  | 'slot-field'
  | 'slot-code'
  | 'slot-progress'
  | 'slot-status'
  | 'slot-custom';
type TSample = TChatSample | TSlotSample;

const CI: ISlotSource = { kind: 'app', id: 'ci', name: 'Сборки' };
const DESIGN: ISlotSource = { kind: 'app', id: 'design', name: 'Дизайн' };
const MAIL: ISlotSource = { kind: 'app', id: 'mail', name: 'Почта' };
const MIGRATION: ISlotSource = { kind: 'agent', id: 'tab-b', name: 'Миграция базы' };
const PUBLISH: ISlotSource = { kind: 'agent', id: 'tab-c', name: 'Публикация пакета' };

const COVERS = `<div class="ink-grid">
  <button class="ink-tile" data-answer='{"cover":"dark"}'>Тёмная</button>
  <button class="ink-tile" data-answer='{"cover":"light"}'>Светлая</button>
  <button class="ink-tile" data-answer='{"cover":"photo"}'>Фото</button>
</div>
<p class="ink-muted">Обложка встанет на все письма рассылки.</p>`;

const slotSample = (
  id: TSlotSample,
  source: ISlotSource,
  chatId: string | null,
  n: Omit<ISlotNotice, 'id' | 'source' | 'createdAt' | 'expiresAt'>,
): Omit<ISmartItem, 'key' | 'at' | 'until'> & { detail: TSmartDetail } => {
  const notice: ISlotNotice = { ...n, id, source, createdAt: 0, expiresAt: 0 };
  return {
    kind: slotWaits(n.kind) ? SMART_KIND.ask : SMART_KIND.info,
    chatId,
    icon: slotIcon(notice),
    title: source.name,
    text: n.title,
    source,
    detail: { k: 'slot', notice },
  };
};

const SAMPLES: Record<TSample, Omit<ISmartItem, 'key' | 'at' | 'until'> & { detail: TSmartDetail }> = {
  question: {
    kind: SMART_KIND.question,
    chatId: 'tab-a',
    icon: 'message-square-text',
    title: 'Лендинг кофейни',
    text: 'Какой вариант шапки оставить?',
    detail: {
      k: 'question',
      text: 'Какой вариант шапки оставить на главной?',
      options: ['Тёмная, с фото зёрен', 'Светлая, только логотип', 'Как в макете'],
    },
  },
  permission: {
    kind: SMART_KIND.permission,
    chatId: 'tab-b',
    icon: 'shield-question',
    title: 'Миграция базы',
    text: 'Ждёт разрешения',
    detail: { k: 'permission', command: 'psql -f migrations/0042_orders.sql "$DATABASE_URL"' },
  },
  code: {
    kind: SMART_KIND.code,
    chatId: 'tab-c',
    icon: 'key-round',
    title: 'Публикация пакета',
    text: 'Нужен код',
    detail: {
      k: 'code',
      why: 'Код из приложения-аутентификатора: npm просит его, чтобы выпустить новую версию.',
      digits: 6,
    },
  },
  machine: {
    kind: SMART_KIND.machine,
    chatId: null,
    icon: 'hard-drive',
    title: 'dev-machine',
    text: 'Машина не справляется',
    detail: {
      k: 'machine',
      text: 'Машине не хватает памяти — чаты могут отвечать медленно.',
      level: 0.92,
      heavy: [
        { key: 'agents', icon: 'messages-square', name: 'Агенты', text: '3 чата работают', stoppable: false },
        {
          key: 'vite',
          icon: 'square-terminal',
          name: 'vite',
          text: 'сборка или дев-сервер',
          stoppable: true,
        },
        { key: 'chrome', icon: 'app-window', name: 'chrome', text: 'программа', stoppable: true },
      ],
    },
  },
  long: {
    kind: SMART_KIND.question,
    chatId: 'tab-f',
    icon: 'message-square-text',
    title: 'Синхронизация файлов с машины в облачное хранилище и обратно',
    text: 'Какие папки держать в синхронизации, а какие оставить только на машине?',
    detail: {
      k: 'question',
      text: 'Какие папки держать в синхронизации, а какие оставить только на машине? В node_modules и .cache — сотни тысяч файлов.',
      options: ['Всё, кроме node_modules и .cache', 'Только исходники', 'Спросить по каждой папке'],
    },
  },
  done: {
    kind: SMART_KIND.done,
    chatId: 'tab-d',
    icon: 'circle-check',
    title: 'Меню на сайте',
    text: 'Добавил раздел «Сезонное» и пометку у трёх позиций',
    detail: {
      k: 'done',
      summary:
        'Добавил раздел «Сезонное» с тремя позициями, пометил их в общем меню и обновил цены. Проверил вёрстку на телефоне — всё на месте. Осталось решить, показывать ли раздел зимой.',
      canReply: true,
    },
  },
  failed: {
    kind: SMART_KIND.failed,
    chatId: 'tab-e',
    icon: 'circle-alert',
    title: 'Отчёт за октябрь',
    text: 'Ход прервался',
    detail: { k: 'plain' },
  },
  'slot-choice': slotSample('slot-choice', CI, null, {
    kind: 'choice',
    icon: 'git-branch',
    title: 'Куда выкатить сборку 482?',
    options: ['На стенд', 'В тест', 'Никуда'],
    own: true,
  }),
  'slot-confirm': slotSample('slot-confirm', MIGRATION, 'tab-b', {
    kind: 'confirm',
    icon: 'triangle-alert',
    title: 'Удалить 3 старые таблицы?',
    text: 'orders_2023, orders_tmp, orders_bak — данные в них не восстановить.',
    yes: 'Удалить',
    danger: true,
  }),
  'slot-field': slotSample('slot-field', PUBLISH, 'tab-c', {
    kind: 'field',
    title: 'Как назвать релиз?',
    text: 'Попадёт в заголовок на странице пакета.',
    placeholder: 'Например, «Весна»',
  }),
  'slot-code': slotSample('slot-code', MAIL, null, {
    kind: 'code',
    icon: 'mail',
    title: 'Код из письма',
    text: 'Ящик прислал код, чтобы подключить рассылку.',
    digits: 6,
  }),
  'slot-progress': slotSample('slot-progress', CI, null, {
    kind: 'progress',
    title: 'Собираю сборку 482',
    text: 'Тесты · 128 из 200',
    progress: 0.64,
  }),
  'slot-status': slotSample('slot-status', CI, null, {
    kind: 'status',
    icon: 'circle-check',
    title: 'Сборка 482 готова',
  }),
  'slot-custom': slotSample('slot-custom', DESIGN, null, {
    kind: 'custom',
    icon: 'palette',
    title: 'Какую обложку взять?',
    view: COVERS,
  }),
};

const RANK = KIND_RANK;

let seq = 0;

type TGalleryItem = ISmartItem & { sample: TSample };

const made = (sample: TSample): TGalleryItem => {
  seq += 1;
  const at = Date.now();
  const { detail: _, ...rest } = SAMPLES[sample];
  return { ...rest, sample, key: `${sample}:${seq}`, at, until: sample === 'code' ? at + 120_000 : null };
};

const byRank = (list: readonly TGalleryItem[]) => [...list].sort((a, b) => RANK[a.kind] - RANK[b.kind]);

/** Every kind that waits, put away into a drop: the long names show how a line ends. */
const DROP_SAMPLES: readonly TSample[] = ['machine', 'code', 'permission', 'question', 'long'];
const allDrops = () => byRank(DROP_SAMPLES.map(made));
/** Ten at once: what a busy afternoon of several chats brings. */
const MANY_SAMPLES: readonly TSample[] = [
  'code',
  'permission',
  'question',
  'question',
  'long',
  'machine',
  'failed',
  'done',
  'done',
  'done',
];
const many = () => byRank(MANY_SAMPLES.map(made));
/** A chat long enough to scroll under the plate: the feed must keep its scroll. */
const FEED_LINES = Array.from({ length: 36 }, (_, i) =>
  i % 3 === 0
    ? `Шаг ${i / 3 + 1}: собрал блок, проверил вёрстку на телефоне и на широком экране.`
    : 'Поправил отступы по сетке 8pt, сверил цвета с макетом, прогнал проверку ссылок.',
);

/** What the page starts with: `&set=pile` a code, a question and a permission, `&set=one` a question, `&set=drops`
 *  every waiting kind in the drops. */
const firstItems = (): readonly TGalleryItem[] => {
  const set = params.get('set');
  if (set === 'pile') return byRank((['question', 'permission', 'code'] as const).map(made));
  if (set === 'drops') return allDrops();
  if (set === 'one') return [made('question')];
  if (set === 'many') return many();
  return [];
};

const Gallery = () => {
  const [items, setItems] = useState<readonly TGalleryItem[]>(firstItems);
  const [open, setOpen] = useState(params.get('open') === '1');
  const [mainKey, setMainKey] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [place, setPlace] = useState<TNoticePlace>(FIRST_PLACE);
  const [hidden, setHidden] = useState<ReadonlySet<string>>(
    () => new Set(params.get('set') === 'drops' ? items.map((i) => i.key) : []),
  );
  const [arriving, setArriving] = useState(false);
  const [replyText, setReplyText] = useState('');
  const [held, setHeld] = useState(false);
  const [anchor, setAnchor] = useState<{ x: number; bottom: number } | null>(null);
  const onHold = useCallback((on: boolean) => setHeld(on), []);
  const pick = (next: TNoticePlace) => {
    setPlace(next);
    const url = new URL(window.location.href);
    url.searchParams.set('place', next);
    window.history.replaceState(null, '', url);
  };
  const pile = () => setItems(byRank((['question', 'permission', 'code'] as const).map(made)));
  const putAway = () => {
    const list = allDrops();
    setItems(list);
    setHidden(new Set(list.map((i) => i.key)));
  };

  // Over the field: the middle of the mock chat's field, a little above it.
  useLayoutEffect(() => {
    const measure = () => {
      const field = document.querySelector('.hx-ng__composer');
      if (!field) return;
      const r = field.getBoundingClientRect();
      setAnchor({ x: r.left + r.width / 2, bottom: window.innerHeight - r.top + 12 });
    };
    measure();
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, []);

  const fire = (sample: TSample) => {
    const item = made(sample);
    setItems((was) => byRank([...was, item]));
    if (sample === 'code' || sample === 'permission' || sample === 'machine') playSound('urgent');
    if (sample === 'question') playSound('wait');
  };
  const drop = (key: string) => setItems((was) => was.filter((i) => i.key !== key));

  useEffect(() => {
    const root = document.documentElement;
    const theme = params.get('theme') === 'dark' ? 'dark' : 'light';
    root.dataset.theme = theme;
    root.dataset.hxTheme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => unlockSound(), []);
  // The metrics' events, newest first, as the chat would send them.
  const [events, setEvents] = useState<readonly IUiEvent[]>([]);
  useEffect(() => {
    setNoticeSink((event) => setEvents((was) => [event, ...was].slice(0, 8)));
    return () => setNoticeSink(null);
  }, []);

  const keep = open || held;
  const onClock = useNewsClock(
    items.filter((i) => NEWS.has(i.kind)),
    keep,
    now,
    NEWS_MS,
  );
  const live = items.filter((i) => !hidden.has(i.key) && (!NEWS.has(i.kind) || onClock.has(i.key)));
  const drops = items
    .filter((i) => hidden.has(i.key) && !NEWS.has(i.kind))
    .map((item) => {
      const detail = SAMPLES[item.sample].detail;
      const what = detail.k === 'code' ? detail.why : detail.k === 'permission' ? detail.command : item.text;
      return { item, what };
    });
  // Put away by hand, the shape flows into its drop; a drop pressed flows back into the card, open.
  const hideItem = (item: ISmartItem, from: DOMRect | null) => {
    if (NEWS.has(item.kind)) {
      drop(item.key);
      return;
    }
    const view = from ? snap(document.querySelector('.hx-sn .hx-sn__body:not([data-ghost])')) : null;
    setHidden((was) => new Set(was).add(item.key));
    if (!from) return;
    twoFrames(() => {
      const el = document.querySelector<HTMLElement>(`[data-drop-key="${CSS.escape(item.key)}"]`);
      if (!el) return;
      const to = snap(el);
      el.dataset.arriving = '';
      void fly(
        { rect: from, radius: 24, view },
        { rect: el.getBoundingClientRect(), radius: 22, view: to },
      ).then(() => {
        delete el.dataset.arriving;
      });
    });
  };
  const backItem = (key: string, from: DOMRect | null, view: HTMLElement | null) => {
    setHidden((was) => {
      const next = new Set(was);
      next.delete(key);
      return next;
    });
    setMainKey(key);
    setOpen(true);
    if (!from) return;
    setArriving(true);
    twoFrames(() => {
      const body = document.querySelector<HTMLElement>('.hx-sn .hx-sn__body:not([data-ghost])');
      if (!body) {
        setArriving(false);
        return;
      }
      void fly(
        { rect: from, radius: 22, view },
        { rect: body.getBoundingClientRect(), radius: 28, view: snap(body) },
      ).then(() => setArriving(false));
    });
  };
  const main = useFront(live, mainKey, setMainKey, open || held);
  const closeAll = () => {
    if (live.length === 0) return;
    const was = hidden;
    const gone = live;
    setOpen(false);
    setHidden(new Set([...was, ...gone.map((i) => i.key)]));
    noticeEvent(NOTICE_ACTION.closedAll, { count: gone.length, stack: gone.length, place });
    toast('Уведомления убраны', {
      description: gone.some((i) => !NEWS.has(i.kind))
        ? 'То, что ждёт ответа, — каплями в колонке чата'
        : undefined,
      action: {
        label: 'Вернуть',
        onClick: () => {
          setHidden(was);
          noticeEvent(NOTICE_ACTION.restored, { count: gone.length });
        },
      },
    });
  };
  const settle = async (item: ISmartItem, said: string) => {
    await new Promise((resolve) => setTimeout(resolve, 400));
    toast(item.title, { description: said });
    setTimeout(() => drop(item.key), 900);
  };

  return (
    <div className="hx-root hx-ng" data-testid="gallery">
      <aside className="hx-ng__side">
        <b className="hx-ng__label">Место</b>
        <div className="hx-ng__col">
          {PLACES.map((one) => (
            <button
              key={one.place}
              type="button"
              className="hx-ng__btn"
              data-on={place === one.place || undefined}
              data-testid={`place-${one.place}`}
              onClick={() => pick(one.place)}
            >
              {one.label}
            </button>
          ))}
        </div>
        <b className="hx-ng__label">События</b>
        <div className="hx-ng__row">
          {(Object.keys(SAMPLES) as TSample[])
            .filter((sample) => !sample.startsWith('slot-'))
            .map((sample) => (
              <button
                key={sample}
                type="button"
                className="hx-ng__btn"
                data-testid={`fire-${sample}`}
                onClick={() => fire(sample)}
              >
                {sample}
              </button>
            ))}
          <button type="button" className="hx-ng__btn" data-testid="pile" onClick={pile}>
            стопка
          </button>
          <button type="button" className="hx-ng__btn" data-testid="many" onClick={() => setItems(many())}>
            10 штук
          </button>
          <button type="button" className="hx-ng__btn" data-testid="drops" onClick={putAway}>
            капли
          </button>
          <button type="button" className="hx-ng__btn" data-testid="clear" onClick={() => setItems([])}>
            clear
          </button>
        </div>
        <b className="hx-ng__label">Слот приложений и агента</b>
        <div className="hx-ng__row">
          {(Object.keys(SAMPLES) as TSample[])
            .filter((sample) => sample.startsWith('slot-'))
            .map((sample) => (
              <button
                key={sample}
                type="button"
                className="hx-ng__btn"
                data-testid={`fire-${sample}`}
                onClick={() => fire(sample)}
              >
                {sample.slice(5)}
              </button>
            ))}
        </div>
        <b className="hx-ng__label">События метрик</b>
        <ol className="hx-ng__events" data-testid="events">
          {events.map((e, i) => (
            <li key={`${e.at}:${i}`}>
              {e.name === 'harnessly_notice' ? String(e.props.action) : 'жест'}
              <small>
                {Object.entries(e.props)
                  .filter(([k]) => k !== 'action')
                  .map(([k, v]) => `${k}=${v}`)
                  .join(' ')}
              </small>
            </li>
          ))}
        </ol>
        <b className="hx-ng__label">Чаты</b>
        <ul className="hx-ng__chats">
          {CHATS.map((chat, i) => (
            <li key={chat} data-on={i === 0 || undefined}>
              {chat}
            </li>
          ))}
        </ul>
      </aside>
      <main className="hx-ng__chat">
        <header className="hx-ng__head">Лендинг кофейни</header>
        <div className="hx-ng__feed">
          <p className="hx-ng__msg" data-me="">
            Сделай главную для кофейни: шапка, меню, адрес.
          </p>
          <p className="hx-ng__msg">Собрал главную: шапка с фото, меню из трёх разделов, карта с адресом.</p>
          <p className="hx-ng__msg">Осталось выбрать вариант шапки — спросил в уведомлении.</p>
          {FEED_LINES.map((line, i) => (
            <p key={i} className="hx-ng__msg" data-me={i % 6 === 5 || undefined}>
              {line}
            </p>
          ))}
          {params.get('settings') === '1' && (
            <div className="hx-ng__settings">
              <NoticesPage />
            </div>
          )}
        </div>
        <div className="hx-ng__composer hx-composer">Сообщение агенту</div>
      </main>
      <aside className="hx-ng__rail">
        <section className="hx-ng__card">
          <b>Цель</b>
          <p>Главная кофейни: шапка, меню из трёх разделов, адрес на карте.</p>
        </section>
        <section className="hx-ng__card">
          <b>Сабагенты</b>
          <p>Проверка вёрстки · 2 мин</p>
        </section>
        <Drops drops={drops} now={now} onBack={backItem} />
      </aside>
      <SmartNotice
        items={live}
        mainKey={main?.key ?? null}
        detail={main ? SAMPLES[main.sample].detail : { k: 'plain' }}
        open={open && live.length > 0}
        place={place}
        anchor={place === 'bottom-center' ? anchor : null}
        now={now}
        onOpen={setOpen}
        onMain={setMainKey}
        onOpenChat={(item) => toast(item.title, { description: 'open chat' })}
        onAnswer={(item, answer) =>
          settle(item, 'text' in answer ? answer.text : `choice ${answer.choice + 1}`)
        }
        onDecide={(item, allow) => settle(item, allow ? 'allowed' : 'denied')}
        onCode={async (item, code) => {
          await new Promise((resolve) => setTimeout(resolve, 400));
          if (code === '000000') return SMART_CODE.bad;
          setTimeout(() => drop(item.key), 900);
          return SMART_CODE.ok;
        }}
        onCancelCode={async (item) => drop(item.key)}
        onStop={(item, heavy) => settle(item, `stopped ${heavy}`)}
        onOpenMachine={(item) => toast(item.title, { description: 'open machine' })}
        onHide={hideItem}
        onCloseAll={closeAll}
        arriving={arriving}
        reply={{
          text: replyText,
          setText: setReplyText,
          recording: null,
          onCancelRecording: () => undefined,
          onFinishRecording: () => undefined,
        }}
        onReply={async (item, text) => {
          await new Promise((resolve) => setTimeout(resolve, 300));
          toast(item.title, { description: `→ ${text}` });
          setTimeout(() => drop(item.key), 1800);
        }}
        onMute={(sound) => toast('Звук выключен', { description: sound })}
        onSlot={async (notice, value) => {
          await new Promise((resolve) => setTimeout(resolve, 300));
          const typed = typeof value === 'string' ? value : (value as { text?: unknown }).text;
          if (typeof typed === 'string' && looksSecret(typed)) return SLOT_ANSWER.secret;
          const item = live.find((i) => i.sample === notice.id);
          if (item) void settle(item, `Ответили: ${notice.title}`);
          return SLOT_ANSWER.ok;
        }}
        onSlotDismiss={async (notice) => {
          const item = live.find((i) => i.sample === notice.id);
          if (item) drop(item.key);
        }}
        onMuteSource={(item) => {
          const source = item.source;
          if (!source) return;
          setItems((was) => was.filter((i) => i.source?.id !== source.id));
          toast(`Уведомления от «${source.name}» выключены`);
        }}
        onSettings={() => toast('settings')}
        announceMs={5000}
        returnFocus={() => undefined}
        focusOnOpen={false}
        onHold={onHold}
      />
      <ToastHost />
    </div>
  );
};

const NotifsGallery = () => <Gallery />;

export default NotifsGallery;
