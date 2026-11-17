// Simple commands without the router («простые команды без задержки»): the theme, the sidebar, the right pane,
// a page or a settings section, a chat by its name, «продолжи» a paused reading, «заверши разговор» — the person's whole
// phrase matched here and carried out at once, without the 2–5 s of the model. Anything longer, unclear or with a second
// part goes to the router (voice-router.ts) as before: a miss here costs only the router's time, a wrong hit costs the
// person's trust, so the patterns are narrow and the phrase must be short.
import type { IRoutePlan } from '@/next/bindings/voice-router';
import type { SettingsSection } from '@/next/routes/routes';

export interface IQuickChat {
  id: string;
  title: string;
}

export interface IQuickContext {
  chats: readonly IQuickChat[];
  /** A long reading waits, paused («продолжи» goes on with it). */
  readingPaused: boolean;
  /** A board or a file is open for quick edits: «тёмная тема» may be the board's, not the app's. */
  overBoard: boolean;
}

/** Longer phrases carry more than one command, or a task: the router takes them. */
const MAX_WORDS = 7;

/** Lower case, «ё» as «е», no punctuation, single spaces. */
export const norm = (text: string): string =>
  text
    .toLowerCase()
    .replace(/ё/g, 'е')
    .replace(/[^\p{L}\p{N}\s-]/gu, ' ')
    .replace(/\s+/g, ' ')
    .trim();

const plan = (say: string, actions: IRoutePlan['actions']): IRoutePlan => ({ say, actions, ms: 0 });

// Courtesy around a command: «пожалуйста, открой настройки», «давай включи тёмную тему».
const POLITE = /^(?:(?:пожалуйста|давай|ну|так|а|слушай|можешь|можно)\s+)+|\s+(?:пожалуйста|please)$/u;

const OPEN = '(?:открой|откройте|покажи|перейди|переходи|зайди|иди|вернись|переключись|go to|open|show)';
const IN = '(?:\\s+(?:в|во|на|к|to))?';

const SECTION_WORDS: readonly [RegExp, SettingsSection][] = [
  [/голос|voice/, 'voice'],
  [/тем|внешн|вид|appearance|theme/, 'appearance'],
  [/клавиш|горяч|сочетан|keys/, 'keys'],
  [/уведомл|notif/, 'notifications'],
  [/сейф|секрет|safe/, 'safe'],
  [/аккаунт|подписк|account/, 'accounts'],
  [/машин|machine/, 'machines'],
  [/подключ|трекер|connect/, 'connections'],
  [/скилл|навык|skill/, 'skills'],
  [/плагин|plugin/, 'plugins'],
  [/браузер|browser/, 'browser'],
  [/приложен|apps?$/, 'apps'],
  [/бета|beta/, 'beta'],
  [/общи|general/, 'general'],
  [/дизайн|design/, 'design'],
  [/расширен|extension/, 'extensions'],
  [/фонов|smart/, 'smart'],
  [/клод|claude/, 'claude'],
  [/кодекс|codex/, 'codex'],
];

const PAGE_WORDS: readonly [RegExp, string, string][] = [
  [/^(?:главн\S*|домой|home|начал\S*|стартов\S*)(?:\s+(?:страниц\S*|экран\S*|page))?$/, 'home', 'Главная'],
  [/^(?:задач\S*|мр\S*|merge requests?|tasks?)$/, 'tasks', 'Задачи'],
  [/^(?:приложени\S*|apps?)$/, 'apps', 'Приложения'],
  [/^(?:дизайн\S*|синопи\S*|sinopia|design)$/, 'designly', 'Дизайн'],
  [/^(?:статистик\S*|stats)$/, 'stats', 'Статистика'],
  [/^(?:эффективност\S*)$/, 'efficiency', 'Эффективность'],
  [/^(?:ревью|review)$/, 'review', 'Ревью'],
  [/^(?:обучени\S*|уроки|learn)$/, 'learn', 'Обучение'],
  [/^(?:помощ\S*|справк\S*|help)$/, 'help', 'Помощь'],
  [/^(?:управлени\S*|manage)$/, 'manage', 'Управление'],
];

const PANE_TABS: readonly [RegExp, string, string][] = [
  [/^(?:терминал\S*|terminal)$/, 'terminal', 'Терминал'],
  [/^(?:браузер\S*|browser)$/, 'browser', 'Браузер'],
  [/^(?:файл\S*|files)$/, 'files', 'Файлы'],
  [/^(?:изменени\S*|дифф?\S*|changes|diff)$/, 'changes', 'Изменения'],
  [/^(?:план|plan)$/, 'plan', 'План'],
];

/** Two forms of one word: «доску», «доска», «доской» share all but their last two letters. */
const sameWord = (a: string, b: string) => {
  let n = 0;
  while (n < a.length && n < b.length && a[n] === b[n]) n++;
  return n >= Math.max(3, Math.min(a.length, b.length) - 2);
};
const STOP = new Set([
  'чат',
  'чата',
  'чате',
  'про',
  'где',
  'тот',
  'там',
  'который',
  'наш',
  'мой',
  'с',
  'в',
  'на',
  'по',
  'и',
]);

/** The one chat whose title holds every word of `name`; null when none or several do. */
export const chatByName = (name: string, chats: readonly IQuickChat[]): IQuickChat | null => {
  const words = norm(name)
    .split(' ')
    .filter((w) => w.length >= 2 && !STOP.has(w));
  if (words.length === 0) return null;
  const hits = chats.filter((c) => {
    const title = norm(c.title).split(' ');
    return words.every((w) => title.some((x) => sameWord(w, x)));
  });
  return hits.length === 1 ? (hits[0] ?? null) : null;
};

/** The plan for a simple command, or null when it is not one (the router takes it). */
export const quickPlan = (spoken: string, ctx: IQuickContext): IRoutePlan | null => {
  let s = norm(spoken);
  for (let i = 0; i < 3; i++) s = s.replace(POLITE, '').trim();
  if (!s || s.split(' ').length > MAX_WORDS) return null;

  // The theme: «включи тёмную тему», «тёмная тема», «светлый режим», «как в системе».
  const theme =
    /^(?:(?:включи|поставь|верни|переключи|смени|сделай)(?:\s+(?:на|мне))?\s+)?(темн\S*|светл\S*|ночн\S*|дневн\S*|системн\S*|dark|light)(?:\s+(?:тем\S*|режим\S*|оформлени\S*|mode|theme))?(?:\s+(?:интерфейс\S*|приложени\S*))?$/u.exec(
      s,
    ) ?? /^(?:тем\S*|оформлени\S*)\s+(?:как в системе|системн\S*)$/u.exec(s);
  if (theme) {
    const word = theme[1] ?? 'системн';
    // Over a board «сделай тёмную тему» is about the board unless the interface is named.
    if (ctx.overBoard && !/интерфейс|приложени/.test(s)) return null;
    const value = /^(?:темн|ночн|dark)/.test(word)
      ? 'dark'
      : /^(?:светл|дневн|light)/.test(word)
        ? 'light'
        : 'system';
    const say =
      value === 'dark' ? 'Тёмная тема.' : value === 'light' ? 'Светлая тема.' : 'Тема как в системе.';
    return plan(say, [{ do: 'theme', value }]);
  }

  // The sidebar: «скрой боковую панель», «покажи сайдбар», «сверни список чатов».
  const side =
    /^(скрой|спрячь|закрой|сверни|убери|покажи|открой|разверни|верни|hide|show)\s+(?:боков\S*\s+(?:панел\S*|меню)|сайдбар\S*|sidebar|список чатов|левую панель|панель слева)$/u.exec(
      s,
    );
  if (side) {
    const open = /^(покажи|открой|разверни|верни|show)/.test(side[1] ?? '');
    return plan(open ? 'Боковая панель открыта.' : 'Боковая панель скрыта.', [{ do: 'sidebar', open }]);
  }

  // The right pane: «закрой правую панель», «открой терминал».
  const pane = /^(закрой|скрой|спрячь|убери|открой|покажи)\s+(?:правую панель|панель справа)$/u.exec(s);
  if (pane) {
    const open = /^(открой|покажи)/.test(pane[1] ?? '');
    return plan(open ? 'Открыл панель.' : 'Закрыл панель.', [{ do: 'pane', open }]);
  }
  const tabWords = new RegExp(`^(?:открой|покажи)\\s+(?:мне\\s+)?(.+)$`, 'u').exec(s)?.[1];
  const tab = tabWords ? PANE_TABS.find(([re]) => re.test(tabWords)) : undefined;
  if (tab) return plan(`${tab[2]} открыт.`, [{ do: 'pane', open: true, tab: tab[1] }]);

  // Settings and their sections: «открой настройки», «настройки голоса», «открой сейф в настройках».
  const settings = new RegExp(
    `^(?:${OPEN}${IN}\\s+)?(?:(.*)\\s+)?(?:настройк\\S*|settings)(?:\\s+(.*))?$`,
    'u',
  ).exec(s);
  if (settings) {
    const rest = `${settings[1] ?? ''} ${settings[2] ?? ''}`.trim();
    const section = rest ? SECTION_WORDS.find(([re]) => re.test(rest))?.[1] : undefined;
    if (rest && !section) return null;
    return plan('Открываю настройки.', [{ do: 'open', page: 'settings', ...(section ? { section } : {}) }]);
  }

  // A chat by its name: «открой чат про доску», «перейди в чат синопия».
  const chat = new RegExp(
    `^${OPEN}${IN}\\s+(?:чат\\S*|разговор\\S*|chat)\\s+(?:про\\s+|с\\s+|about\\s+)?(.+)$`,
    'u',
  ).exec(s);
  if (chat?.[1]) {
    const c = chatByName(chat[1], ctx.chats);
    if (!c) return null;
    return plan(`Открываю «${c.title}».`, [{ do: 'open_chat', chat: c.id }]);
  }

  // Pages: «открой задачи», «на главную», «домой».
  const page = new RegExp(`^(?:${OPEN}${IN}\\s+)?(.+)$`, 'u').exec(s);
  const pageWords = page?.[1]?.replace(/^(?:на|в|во)\s+/, '').replace(/\s+(?:страниц\S*|раздел\S*)$/u, '');
  const hasVerb = page?.[1] !== s;
  const target = pageWords ? PAGE_WORDS.find(([re]) => re.test(pageWords)) : undefined;
  // Without a verb only «домой» and «на главную»: a bare «задачи» may be the start of another phrase.
  if (target && (hasVerb || target[1] === 'home'))
    return plan(`${target[2]}.`, [{ do: 'open', page: target[1] }]);

  // A paused reading goes on.
  if (
    ctx.readingPaused &&
    /^(?:продолж\S*|дальше|читай дальше|continue|go on)(?:\s+(?:читать|чтение))?$/u.test(s)
  )
    return plan('Продолжаю.', [{ do: 'read_aloud', source: 'resume' }]);

  // The end of the talk.
  if (
    /^(?:(?:заверши|закончи|выключи|останови|прекрати)\s+(?:разговор|звонок|голос\S*(?:\s+режим)?|беседу)|отбой|end call|hang up)$/u.test(
      s,
    )
  )
    return plan('До связи.', [{ do: 'stop' }]);

  return null;
};
