// The smart notice on made-up events (`?dev=notifs`), for the eye and for screenshots: the real plate with buttons
// that make each kind of waiting, and a field at the bottom it stands over.
//
//   ?dev=notifs                    an empty page and the buttons
//   &set=pile                      a code, a question and a permission at once; &set=one a question
//   &open=1                        the card open
//   &place=top-right|…             where it stands (the middle of the bottom edge by default)
//   &settings=1                    Settings → Notifications under the buttons
//   &theme=light|dark&lang=en|ru
import { useEffect, useState } from 'react';
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
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/base.css';
import '@/next/styles/settings.css';
import '@/next/styles/code-field.css';
import '@/next/dev/notifs-gallery.css';

const params = new URLSearchParams(window.location.search);
const LANG: TLang = params.get('lang') === 'en' ? 'en' : 'ru';
pinLocale(LANG);
const PLACE: TNoticePlace = NOTICE_PLACES.find((p) => p === params.get('place')) ?? 'bottom-center';

type TSample = 'question' | 'permission' | 'code' | 'machine' | 'done' | 'failed';

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
    icon: 'memory-stick',
    title: 'Моя ВМ',
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
  done: {
    kind: SMART_KIND.done,
    chatId: 'tab-d',
    icon: 'circle-check',
    title: 'Меню на сайте',
    text: 'Добавил раздел «Сезонное» и пометку у трёх позиций',
    detail: { k: 'plain' },
  },
  failed: {
    kind: SMART_KIND.failed,
    chatId: 'tab-e',
    icon: 'circle-alert',
    title: 'Отчёт за октябрь',
    text: 'Ход прервался',
    detail: { k: 'plain' },
  },
};

const RANK: Record<TSmartKind, number> = {
  code: 0,
  permission: 1,
  machine: 2,
  question: 2,
  plan: 2,
  failed: 3,
  limit: 3,
  done: 4,
};

let seq = 0;

type TGalleryItem = ISmartItem & { sample: TSample };

const made = (sample: TSample): TGalleryItem => {
  seq += 1;
  const at = Date.now();
  const { detail: _, ...rest } = SAMPLES[sample];
  return { ...rest, sample, key: `${sample}:${seq}`, at, until: sample === 'code' ? at + 120_000 : null };
};

const byRank = (list: readonly TGalleryItem[]) => [...list].sort((a, b) => RANK[a.kind] - RANK[b.kind]);

/** What the page starts with: `&set=pile` a code, a question and a permission, `&set=one` a question. */
const firstItems = (): readonly TGalleryItem[] => {
  const set = params.get('set');
  if (set === 'pile') return byRank((['question', 'permission', 'code'] as const).map(made));
  if (set === 'one') return [made('question')];
  return [];
};

const Gallery = () => {
  const [items, setItems] = useState<readonly TGalleryItem[]>(firstItems);
  const [open, setOpen] = useState(params.get('open') === '1');
  const [mainKey, setMainKey] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

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

  const main = items.find((i) => i.key === mainKey) ?? items[0] ?? null;
  const settle = async (item: ISmartItem, said: string) => {
    await new Promise((resolve) => setTimeout(resolve, 400));
    toast(item.title, { description: said });
    setTimeout(() => drop(item.key), 900);
  };

  return (
    <div className="hx-root hx-ng" data-testid="gallery">
      <div className="hx-ng__row">
        {(Object.keys(SAMPLES) as TSample[]).map((sample) => (
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
        <button type="button" className="hx-ng__btn" data-testid="clear" onClick={() => setItems([])}>
          clear
        </button>
      </div>
      {params.get('settings') === '1' && (
        <div className="hx-ng__settings">
          <NoticesPage />
        </div>
      )}
      <div className="hx-ng__field hx-composer">Сообщение агенту</div>
      <SmartNotice
        items={items}
        mainKey={main?.key ?? null}
        detail={main ? SAMPLES[main.sample].detail : { k: 'plain' }}
        open={open && items.length > 0}
        place={PLACE}
        anchor={PLACE === 'bottom-center' ? { x: window.innerWidth / 2, bottom: 112 } : null}
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
        onHide={(item) => {
          drop(item.key);
          toast('Скрыто', { description: 'Чат всё ещё ждёт ответа' });
        }}
        onMute={(sound) => toast('Звук выключен', { description: sound })}
        onSettings={() => toast('settings')}
        announceMs={4000}
        returnFocus={() => undefined}
        focusOnOpen={false}
      />
      <ToastHost />
    </div>
  );
};

const NotifsGallery = () => <Gallery />;

export default NotifsGallery;
