// The notification centre on made-up chats (`?dev=notifcenter`), for the eye, for screenshots and for the video. The
// bell at the bottom of the left column opens it, as in the app; the answers come after a short wait as the machine's
// would. Keys while it is open: N — a new event comes, R — everything back as it was.
//
//   &set=full (default) | empty | loading | failed | one
//   &theme=light|dark&lang=ru|en
//   A narrow window (≤ 900 px) shows the phone's sheet: the same list.
import { useEffect, useRef, useState } from 'react';
import NotificationCenter, {
  NOTIFICATION_KIND,
  type INotification,
} from '@/next/features/notifications/notification-center';
import WaitRow, { type TWaitDetail } from '@/next/features/notifications/wait-row';
import { pinLocale, type TLang } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { PortalContext } from '@/next/ui/portal';
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/dev/notifcenter-gallery.css';

const params = new URLSearchParams(window.location.search);
const LANG: TLang = params.get('lang') === 'en' ? 'en' : 'ru';
pinLocale(LANG);
const THEME = params.get('theme') === 'dark' ? 'dark' : 'light';
const SET = params.get('set') ?? 'full';
const ANSWER_MS = 700;
const MIN = 60_000;

const later = <T,>(value: T): Promise<T> =>
  new Promise((done) => window.setTimeout(() => done(value), ANSWER_MS));

const NOW = Date.now();

const SAMPLE: INotification[] = [
  {
    id: 'w1',
    kind: NOTIFICATION_KIND.waiting,
    icon: 'message-square-text',
    title: 'Лендинг кофейни: шапка и первый экран',
    text: 'Ждёт ответа на вопрос',
    at: NOW - 2 * MIN,
    unread: true,
    chatId: 'c1',
  },
  {
    id: 'w2',
    kind: NOTIFICATION_KIND.waiting,
    icon: 'square-terminal',
    title: 'Публикация пакета',
    text: 'Просит разрешения',
    at: NOW - 6 * MIN,
    unread: true,
    chatId: 'c2',
  },
  {
    id: 'w3',
    kind: NOTIFICATION_KIND.waiting,
    icon: 'list-checks',
    title: 'Миграция базы на новую схему',
    text: 'Ждёт утверждения плана',
    at: NOW - 11 * MIN,
    unread: true,
    chatId: 'c3',
  },
  {
    id: 'm1',
    kind: NOTIFICATION_KIND.machine,
    icon: 'memory-stick',
    title: 'Моя ВМ',
    text: 'Память на исходе: свободно 1,2 ГБ из 24',
    at: NOW - 4 * MIN,
    unread: true,
    chatId: null,
  },
  {
    id: 'f1',
    kind: NOTIFICATION_KIND.failed,
    icon: 'gauge',
    title: 'Отчёт за октябрь',
    text: 'Закончился лимит подписки — продолжит в 18:00',
    at: NOW - 48 * MIN,
    unread: true,
    chatId: 'c4',
  },
  {
    id: 'd1',
    kind: NOTIFICATION_KIND.done,
    icon: 'circle-check',
    title: 'Приложения: загрузка файлов и превью в правой панели',
    text: 'Загрузка работает: файл до 50 МБ уходит частями, превью картинок и PDF открывается справа.',
    at: NOW - 9 * MIN,
    unread: true,
    chatId: 'c5',
  },
  {
    id: 'd1h',
    kind: NOTIFICATION_KIND.done,
    icon: 'check',
    title: 'Приложения: загрузка файлов и превью в правой панели',
    text: 'Ответ из уведомления: да, частями по 5 МБ',
    at: NOW - 31 * MIN,
    unread: false,
    chatId: 'c5',
  },
  {
    id: 'd2',
    kind: NOTIFICATION_KIND.done,
    icon: 'circle-check',
    title: 'Меню на сайте',
    text: 'Добавил раздел «Сезонное» и пометку «новинка» у трёх позиций, превью справа.',
    at: NOW - 22 * MIN,
    unread: true,
    chatId: 'c6',
  },
  {
    id: 'd3',
    kind: NOTIFICATION_KIND.done,
    icon: 'circle-check',
    title: 'Сравнение тарифов хостинга',
    text: 'Дешевле всего выходит второй тариф: 1 900 ₽ в месяц, диск вдвое больше.',
    at: NOW - 70 * MIN,
    unread: false,
    chatId: 'c7',
  },
  {
    id: 'd4',
    kind: NOTIFICATION_KIND.done,
    icon: 'circle-check',
    title: 'Тесты оплаты',
    text: 'Все 48 тестов зелёные; починил таймаут в проверке возврата.',
    at: NOW - 140 * MIN,
    unread: false,
    chatId: 'c8',
  },
];

const DETAILS: Record<string, TWaitDetail> = {
  w1: {
    k: 'question',
    text: 'Какой вариант шапки оставить на главной?',
    options: ['Тёмная, с фото зёрен', 'Светлая, только логотип', 'Как в макете'],
  },
  w2: {
    k: 'permission',
    command:
      'npm publish --access public && git tag v2.4.0 && git push origin v2.4.0 && curl -X POST https://hooks.example.com/release -d \'{"version":"2.4.0","channel":"stable"}\' && rm -rf dist/.cache',
  },
  w3: { k: 'plain', text: 'Ждёт утверждения плана: 6 шагов, начиная с бэкапа' },
};

const FRESH: Omit<INotification, 'id' | 'at'>[] = [
  {
    kind: NOTIFICATION_KIND.done,
    icon: 'circle-check',
    title: 'Сборка мобильного приложения',
    text: 'Собрал под iOS и Android, оба билда лежат в «Файлах».',
    unread: true,
    chatId: 'c9',
  },
  {
    kind: NOTIFICATION_KIND.failed,
    icon: 'triangle-alert',
    title: 'Перевод документации на английский',
    text: 'Ход упал: сеть пропала на середине ответа',
    unread: true,
    chatId: 'c10',
  },
  {
    kind: NOTIFICATION_KIND.waiting,
    icon: 'message-square-text',
    title: 'Логотип для кофейни',
    text: 'Ждёт ответа на вопрос',
    unread: true,
    chatId: 'c11',
  },
];

const initial = (): INotification[] | null => {
  if (SET === 'loading' || SET === 'failed') return null;
  if (SET === 'empty') return [];
  if (SET === 'one') return SAMPLE.filter((i) => i.id === 'd2');
  return SAMPLE;
};

const Gallery = () => {
  const bell = useRef<HTMLButtonElement>(null);
  const portal = useRef<HTMLDivElement>(null);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [open, setOpen] = useState(true);
  const [items, setItems] = useState<INotification[] | null>(initial);
  const [extra, setExtra] = useState<Record<string, TWaitDetail>>({});
  const fresh = useRef(0);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 15_000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = THEME;
    root.dataset.hxTheme = THEME;
    root.classList.toggle('dark', THEME === 'dark');
    setAnchor(bell.current);
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement) return;
      if (e.key === 'n' || e.key === 'т') {
        const next = FRESH[fresh.current % FRESH.length] as Omit<INotification, 'id' | 'at'>;
        fresh.current += 1;
        const id = `n${fresh.current}`;
        if (next.kind === NOTIFICATION_KIND.waiting)
          setExtra((was) => ({
            ...was,
            [id]: {
              k: 'question',
              text: 'Какой цвет оставить для логотипа?',
              options: ['Чёрный', 'Кофейный'],
            },
          }));
        setItems((was) => [{ ...next, id, at: Date.now() }, ...(was ?? [])]);
      }
      if (e.key === 'r' || e.key === 'к') setItems(initial());
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const take = (list: readonly INotification[]) => {
    const ids = new Set(list.map((i) => i.id));
    setItems((was) => (was ?? []).filter((i) => !ids.has(i.id)));
  };
  /** An answer: the row leaves «Ждёт вас» and stands under «Готово» as what was said. */
  const answered = (item: INotification, said: string) =>
    setItems((was) => [
      {
        id: `${item.id}:done`,
        kind: NOTIFICATION_KIND.done,
        icon: 'check',
        title: item.title,
        text: said,
        at: Date.now(),
        unread: false,
        chatId: item.chatId,
      },
      ...(was ?? []).filter((i) => i.id !== item.id),
    ]);

  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-ncg">
        <aside className="hx-ncg__side">
          <div className="hx-ncg__proj">кофейня</div>
          {[
            'Меню на сайте',
            'Лендинг кофейни: шапка и первый экран',
            'Публикация пакета',
            'Отчёт за октябрь',
          ].map((title, i) => (
            <div key={title} className="hx-ncg__chat" data-on={i === 0 || undefined}>
              {title}
            </div>
          ))}
          <span className="hx-ncg__sp" />
          <button
            ref={bell}
            type="button"
            className="hx-ncg__bell"
            aria-label="Уведомления"
            onClick={() => setOpen(true)}
          >
            <Icon name="bell" size="sm" />
            Уведомления
          </button>
        </aside>
        <main className="hx-ncg__main">
          <p>
            <b>?dev=notifcenter</b> · колокольчик внизу слева · N — новое событие · R — сначала ·
            &set=empty|loading|failed|one · узкое окно — телефон
          </p>
        </main>
        <NotificationCenter
          open={open}
          anchor={anchor}
          side="right"
          items={items}
          now={now}
          onClose={() => setOpen(false)}
          onOpenItem={() => setOpen(false)}
          onRead={(list) => {
            const ids = new Set(list.map((i) => i.id));
            setItems((was) => (was ?? []).map((i) => (ids.has(i.id) ? { ...i, unread: false } : i)));
          }}
          onRemove={take}
          onReadAll={async () => {
            await later(null);
            setItems((was) =>
              (was ?? []).map((i) =>
                i.kind === NOTIFICATION_KIND.done || i.kind === NOTIFICATION_KIND.failed
                  ? { ...i, unread: false }
                  : i,
              ),
            );
          }}
          failed={
            SET === 'failed'
              ? { reason: 'Машина не ответила за 12 секунд', onRetry: () => setItems(SAMPLE) }
              : null
          }
          renderWait={(item, isOpen, toggle) => {
            const detail = extra[item.id] ?? DETAILS[item.id] ?? { k: 'loading' };
            return (
              <WaitRow
                detail={detail}
                open={isOpen}
                onToggle={toggle}
                onOpenChat={() => setOpen(false)}
                onChoose={async (index) => {
                  await later(null);
                  answered(item, `Ответ: ${detail.k === 'question' ? detail.options[index] : ''}`);
                }}
                onText={async (text) => {
                  await later(null);
                  answered(item, `Ответ: ${text}`);
                }}
                onDecide={async (allow) => {
                  await later(null);
                  answered(item, allow ? 'Разрешено' : 'Запрещено');
                }}
              />
            );
          }}
        />
        <div ref={portal} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

const NotifCenterGallery = () => <Gallery />;

export default NotifCenterGallery;
