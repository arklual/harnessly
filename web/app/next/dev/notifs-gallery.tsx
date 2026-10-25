// The notices at the top of the window on made-up events (`?dev=notifs`), for the eye and for screenshots: the real
// stack on the real store (this browser's list), with buttons that fire each kind of event.
//
//   ?dev=notifs                    an empty page and the buttons
//   &set=pile                      three notices at once; &set=one a single one
//   &auto=1                        events one after another (a question, an answer, a failure, a limit)
//   &settings=1                    Settings → Notifications under the buttons
//   &theme=light|dark&lang=en|ru
import { useEffect, useState } from 'react';
import NoticeStack from '@/next/features/notifications/notice-stack';
import { playChime, setIconDot, titleWithCount, unlockSound } from '@/next/features/notifications/ping';
import { NoticesPage } from '@/next/bindings/settings-notices';
import { pinLocale, type TLang } from '@/next/i18n';
import {
  NOTICE_KIND,
  clearChatNotices,
  dismissAllNotices,
  dismissNotice,
  pushNotice,
  setNoticesPerson,
  useNoticeList,
  type INotice,
  type TNoticeKind,
} from '@/next/stores/notices';
import { ToastHost, toast } from '@/next/ui/toast';
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/base.css';
import '@/next/styles/settings.css';
import '@/next/dev/notifs-gallery.css';

const params = new URLSearchParams(window.location.search);
const LANG: TLang = params.get('lang') === 'en' ? 'en' : 'ru';
pinLocale(LANG);
setNoticesPerson('gallery');

const SAMPLES: Record<TNoticeKind | 'perm', Omit<INotice, 'key' | 'at'>> = {
  waiting: {
    chatId: 'tab-a',
    kind: NOTICE_KIND.waiting,
    icon: 'circle-question-mark',
    title: 'Миграция базы на новый кластер',
    text: 'Ждёт вашего ответа',
  },
  perm: {
    chatId: 'tab-b',
    kind: NOTICE_KIND.waiting,
    icon: 'shield-question',
    title: 'Деплой витрины на стенд',
    text: 'Ждёт разрешения',
  },
  done: {
    chatId: 'tab-c',
    kind: NOTICE_KIND.done,
    icon: 'circle-check',
    title: 'Разобрать падение сборки на SSR',
    text: 'Причина — двойная гидратация шапки: поправил порядок провайдеров, сборка зелёная, тесты проходят.',
  },
  failed: {
    chatId: 'tab-d',
    kind: NOTICE_KIND.failed,
    icon: 'circle-alert',
    title: 'Отчёт по продажам за неделю',
    text: 'Ход прервался',
  },
  limit: {
    chatId: 'tab-e',
    kind: NOTICE_KIND.limit,
    icon: 'gauge',
    title: 'Ревью большого MR',
    text: 'Упёрся в лимит подписки',
  },
};

let seq = 0;
const fire = (sample: keyof typeof SAMPLES) => {
  seq += 1;
  pushNotice({ ...SAMPLES[sample], key: `${SAMPLES[sample].chatId}:${seq}`, at: Date.now() - seq * 1000 });
};

const Gallery = () => {
  const notices = useNoticeList();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const root = document.documentElement;
    const theme = params.get('theme') === 'light' ? 'light' : 'dark';
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => unlockSound(), []);
  useEffect(() => {
    document.title = titleWithCount(document.title, notices.length);
    setIconDot(notices.length > 0);
  }, [notices.length]);
  useEffect(() => {
    dismissAllNotices();
    const set = params.get('set');
    if (set === 'pile') (['done', 'perm', 'waiting'] as const).forEach(fire);
    if (set === 'one') fire('waiting');
    if (params.get('auto') !== '1') return;
    const order = ['waiting', 'done', 'failed', 'limit'] as const;
    const timers = order.map((sample, index) => setTimeout(() => fire(sample), 400 + index * 900));
    return () => timers.forEach(clearTimeout);
  }, []);

  return (
    <div className="hx-root hx-ng" data-testid="gallery">
      <div className="hx-ng__row">
        {(Object.keys(SAMPLES) as (keyof typeof SAMPLES)[]).map((sample) => (
          <button
            key={sample}
            type="button"
            className="hx-ng__btn"
            data-testid={`fire-${sample}`}
            onClick={() => {
              fire(sample);
              playChime();
            }}
          >
            {sample}
          </button>
        ))}
        <button type="button" className="hx-ng__btn" data-testid="clear" onClick={dismissAllNotices}>
          clear
        </button>
      </div>
      {params.get('settings') === '1' && (
        <div className="hx-ng__settings">
          <NoticesPage />
        </div>
      )}
      <NoticeStack
        notices={notices}
        now={now}
        onOpen={(notice) => {
          clearChatNotices(notice.chatId);
          toast(notice.title, { description: 'open' });
        }}
        onDismiss={dismissNotice}
        onDismissAll={dismissAllNotices}
      />
      <ToastHost />
    </div>
  );
};

const NotifsGallery = () => <Gallery />;

export default NotifsGallery;
