// Settings → Notifications → Telegram on made-up bots (`?dev=notifybots`), for the eye and for screenshots. The actions
// answer after a short wait as the machine would: a token is taken unless it holds «bad», a new bot starts its binding,
// and the binding finds its chat after a few seconds.
//
//   &set=list (default) | empty | binding | loading | failed
//   &open=<bot id>                 that bot's card open (nb_1, nb_2, nb_3)
//   &theme=light|dark&lang=en|ru
import { useEffect, useState } from 'react';
import type { INotifyBot } from '@/next/features/notify-bots/api';
import NotifyBotsSection from '@/next/features/settings/notify-bots-section';
import { pinLocale, type TLang } from '@/next/i18n';
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
const SET = params.get('set') ?? 'list';
const ANSWER_MS = 600;
const BOUND_AFTER_MS = 4000;

const waitingBinding = (username: string): INotifyBot['binding'] => ({
  state: 'waiting',
  code: '5f2c9a1e07b3d4c8',
  link: `https://t.me/${username}?start=5f2c9a1e07b3d4c8`,
  command: `/start@${username} 5f2c9a1e07b3d4c8`,
  expiresAt: Date.now() + 600_000,
  reason: '',
});

const SAMPLE: INotifyBot[] = [
  {
    id: 'nb_1',
    name: 'Личные',
    username: 'sergey_harnessly_bot',
    title: 'Harnessly',
    tokenSet: true,
    tokenTail: 'x9Qk',
    chat: { title: 'Анна', type: 'private' },
    instruction: 'Пиши сюда, когда закончил долгую задачу или нужен мой ответ',
    binding: null,
  },
  {
    id: 'nb_2',
    name: 'Релизы',
    username: 'acme_release_bot',
    title: 'Release',
    tokenSet: true,
    tokenTail: 'a7Lm',
    chat: { title: 'Команда · релизы', type: 'supergroup' },
    instruction: 'Только когда что-то уехало в прод или выкат упал.\nКоротко: что, куда, ссылка на MR.',
    binding: null,
  },
  {
    id: 'nb_3',
    name: 'Ночные проверки',
    username: 'night_checks_bot',
    title: 'Night checks',
    tokenSet: true,
    tokenTail: 'Zt04',
    chat: null,
    instruction: '',
    binding: SET === 'binding' ? waitingBinding('night_checks_bot') : null,
  },
];

const later = <T,>(value: T) => new Promise<T>((resolve) => setTimeout(() => resolve(value), ANSWER_MS));

const Gallery = () => {
  const [bots, setBots] = useState<INotifyBot[] | null>(() => {
    if (SET === 'loading' || SET === 'failed') return null;
    return SET === 'empty' ? [] : SAMPLE;
  });
  const [open, setOpen] = useState<string | null>(params.get('open'));

  useEffect(() => {
    const root = document.documentElement;
    const theme = params.get('theme') === 'light' ? 'light' : 'dark';
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, []);

  const update = (id: string, change: Partial<INotifyBot>) =>
    setBots((now) => (now ?? []).map((bot) => (bot.id === id ? { ...bot, ...change } : bot)));

  // A waiting binding finds its chat a few seconds later, as if the person pressed Start.
  const waiting = (bots ?? [])
    .filter((bot) => bot.binding?.state === 'waiting')
    .map((bot) => bot.id)
    .join(',');
  useEffect(() => {
    if (waiting === '') return;
    const ids = waiting.split(',');
    const timer = setTimeout(
      () =>
        setBots((now) =>
          (now ?? []).map((bot) =>
            ids.includes(bot.id) ? { ...bot, binding: null, chat: { title: 'Анна', type: 'private' } } : bot,
          ),
        ),
      BOUND_AFTER_MS,
    );
    return () => clearTimeout(timer);
  }, [waiting]);

  return (
    <div className="hx-root hx-ng" data-testid="gallery">
      <div className="hx-ng__settings">
        <NotifyBotsSection
          bots={bots}
          failed={SET === 'failed' ? { reason: 'Машина не ответила', onRetry: () => setBots(SAMPLE) } : null}
          open={open}
          onOpen={setOpen}
          onAdd={async (token) => {
            if (token.includes('bad')) return later('Telegram не принял токен. Проверьте его у @BotFather');
            await later(null);
            const bot: INotifyBot = {
              id: `nb_${Date.now()}`,
              name: 'Новый бот',
              username: 'new_notify_bot',
              title: 'New',
              tokenSet: true,
              tokenTail: token.slice(-4),
              chat: null,
              instruction: '',
              binding: waitingBinding('new_notify_bot'),
            };
            setBots((now) => [...(now ?? []), bot]);
            setOpen(bot.id);
            return null;
          }}
          onSave={async (id, change) => {
            await later(null);
            update(id, change);
            return null;
          }}
          onToken={async (id, token) => {
            await later(null);
            update(id, { tokenTail: token.slice(-4) });
            return null;
          }}
          onBind={async (id) => {
            await later(null);
            const bot = bots?.find((b) => b.id === id);
            update(id, { binding: waitingBinding(bot?.username ?? 'bot') });
            return null;
          }}
          onStopBind={async (id) => {
            await later(null);
            update(id, { binding: null });
            return null;
          }}
          onTest={() => later(null)}
          onRemove={async (id) => {
            await later(null);
            setBots((now) => (now ?? []).filter((bot) => bot.id !== id));
            setOpen(null);
            return null;
          }}
        />
      </div>
    </div>
  );
};

const NotifyBotsGallery = () => <Gallery />;

export default NotifyBotsGallery;
