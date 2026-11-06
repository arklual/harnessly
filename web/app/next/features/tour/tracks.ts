// The lessons of the hands-on tour: Sinopia on the demo projects of Relay, each step a real action.
// Targets are the product's own elements; a step inside the board itself (a click on the page, a text edited there)
// is not seen by the shell, so it ends with the person's «Done».
import type { ITourEnv, ITourStep, ITourTrack } from '@/next/features/tour/model';

const SIDEBAR_SINOPIA = '.hx-nav__group[data-i="2"] .hx-nav__item';
const SITE_FRAME = '.hx-site__iframe';
const DECK_FRAME = '.hx-deck__frame[data-on="1"]';
const VIDEO_FRAME = '.hx-video__frame';
// The design's field: folded into «Спросить агента», open, or the plain composer.
const COMPOSER = ['.hx-island__mini', '.hx-island textarea', '.hx-composer'] as const;
const MODE = '.hx-modeseg';
const PICK = '.hx-browser__tool[aria-label="Выделить"], .hx-browser__tool[aria-label="Pick"]';
const NEXT_SLIDE = '[aria-label="Следующий слайд"], [aria-label="Next slide"]';

const card = (id: string) => `.hx-dl-card[data-id="${id}"]`;
const at = (id: string) => (env: ITourEnv) => new RegExp(`/designly/${id}(?:[/?#]|$)`).test(env.path);
const signal = (name: string) => (env: ITourEnv) => env.signals.has(name);
const signalLike = (test: (s: string) => boolean) => (env: ITourEnv) => [...env.signals].some(test);
const chatStarted = (env: ITourEnv) => /[?&]chat=/.test(env.path);

const openSinopia: ITourStep = {
  id: 'sinopia',
  target: [SIDEBAR_SINOPIA],
  done: (env) => /\/designly(?:[/?#]|$)/.test(env.path),
};

const open = (id: string): ITourStep => ({ id: 'open', target: [card(id)], done: at(id) });

const ask = (prompt: string, frame: string): ITourStep => ({
  id: 'ask',
  // Where the field is folded away (a deck or a video looked at, not edited), the ring is on the mode switch.
  target: [...COMPOSER, MODE, frame],
  prompt,
  done: chatStarted,
});

const site: ITourTrack = {
  id: 'site',
  steps: [
    openSinopia,
    open('relay-site'),
    { id: 'click', target: [SITE_FRAME] },
    { id: 'edit', target: [MODE], done: signal('site:edit') },
    // A block is picked on the page and rides in the agent's field as a note: the edit is asked for that block.
    { id: 'text', target: [PICK, SITE_FRAME], done: signal('site:note'), manual: true },
    ask('Сделай этот заголовок короче и ярче', SITE_FRAME),
    {
      id: 'page',
      target: ['.hx-strip__tabs', SITE_FRAME],
      done: signalLike((s) => s.startsWith('site:page:Цены')),
    },
    { id: 'year', target: [SITE_FRAME] },
    { id: 'device', target: ['.hx-browser__dev'], done: signal('site:device:ip14') },
  ],
};

const slides: ITourTrack = {
  id: 'slides',
  steps: [
    openSinopia,
    open('relay-deck'),
    { id: 'next', target: [NEXT_SLIDE, DECK_FRAME], done: signalLike((s) => /^deck:slide:[1-9]/.test(s)) },
    { id: 'edit', target: [MODE, DECK_FRAME] },
    { id: 'comment', target: ['[aria-label^="Комментир"]', DECK_FRAME], done: signal('deck:comment') },
    {
      id: 'show',
      target: ['.hx-deck__bar [data-k="full"]', '.hx-deck__bar [data-k="show"]'],
      done: (env) => env.signals.has('deck:full') || env.signals.has('deck:screen:show'),
    },
    ask('Добавь после «Решения» слайд с отзывом клиента', DECK_FRAME),
  ],
};

const video: ITourTrack = {
  id: 'video',
  steps: [
    openSinopia,
    open('relay-video'),
    { id: 'play', target: [VIDEO_FRAME], done: signal('video:play') },
    {
      id: 'scene',
      target: ['[aria-label="Следующая сцена"]', '.hx-video__timeline'],
      done: signalLike((s) => /^video:scene:[1-9]/.test(s)),
    },
    { id: 'timeline', target: ['.hx-video__timeline'] },
    ask('В сцене «Откат за секунду» замени подпись на «Откат — одним кликом»', VIDEO_FRAME),
    { id: 'download', target: ['[data-k="download"]'], done: (env) => env.find('[role=menu]') !== null },
  ],
};

const ds: ITourTrack = {
  id: 'ds',
  steps: [
    openSinopia,
    open('relay-ds'),
    { id: 'look', target: [SITE_FRAME] },
    { id: 'back', target: [SIDEBAR_SINOPIA], done: (env) => /\/designly\/?(?:[?#]|$)/.test(env.path) },
    {
      id: 'pick',
      target: ['.hx-deck-chip'],
      done: (env) =>
        env.find('.hx-deck-set__ds [role=radio]:not(:first-child)[aria-checked="true"]') !== null,
    },
    {
      id: 'make',
      target: [...COMPOSER, '.hx-dl__hero'],
      prompt: 'Страница «О компании» для Relay',
      done: at('[^/?#]+'),
    },
  ],
};

// The basics, in the person's own chats: the composer, the agent at work, the right pane, the safe, several agents.
const NAV = (i: number) => `.hx-nav__group[data-i="${i}"] .hx-nav__item`;
const NEW_CHAT = NAV(0);
const SEARCH = NAV(1);
const BELL = NAV(4);
const FIELD = '.hx-composer__layer';
const ROUND = (icon: string) => `.hx-composer__bar .hx-round:has(.hx-icon[data-i="${icon}"])`;
const TOOLS = '.hx-header__tools';
const TURN = '.hx-work__head';
const SETTINGS_ITEM = (id: string) => `.hx-set__nav-item[data-id="${id}"]`;

const has = (selector: string) => (env: ITourEnv) => env.find(selector) !== null;
const typed = (env: ITourEnv) => {
  const field = env.find('.hx-composer__input');
  return field instanceof HTMLTextAreaElement && field.value.trim().length > 0;
};
const inChat = (env: ITourEnv) => /\/c\/[^/?#]+/.test(env.path);
const inSettings = (section?: string) => (env: ITourEnv) =>
  new RegExp(`/settings${section ? `/${section}` : ''}(?:[/?#]|$)`).test(env.path);
const look = (id: string, target: readonly string[] = []): ITourStep => ({ id, target, look: true });
const goHome: ITourStep = { id: 'home', target: [NEW_CHAT], done: has('.hx-hello') };

const welcome: ITourTrack = {
  id: 'welcome',
  steps: [
    look('field', [FIELD]),
    look('plus', ['.hx-plus-btn']),
    look('model', ['.hx-model']),
    look('list', ['.hx-sidebar__list']),
    look('pane', [TOOLS]),
    look('sinopia', [SIDEBAR_SINOPIA]),
    look('learn', [NAV(5)]),
  ],
};

const chat: ITourTrack = {
  id: 'chat',
  steps: [
    goHome,
    { id: 'type', target: [FIELD], done: typed },
    { id: 'plus', target: ['.hx-plus-btn'], done: has('.hx-plus__group') },
    { id: 'model', target: ['.hx-model'], done: has('.hx-picker') },
    { id: 'mic', target: [ROUND('mic')] },
    { id: 'send', target: [ROUND('arrow-up'), FIELD], done: inChat },
    { id: 'turn', target: [TURN], done: has(`${TURN}[aria-expanded="true"]`) },
  ],
};

const work: ITourTrack = {
  id: 'work',
  steps: [
    { id: 'send', target: [FIELD], done: (env) => inChat(env) && env.find('.hx-work[data-live]') !== null },
    look('list', ['.hx-sidebar__list']),
    { id: 'turn', target: [TURN], done: has(`${TURN}[aria-expanded="true"]`) },
    {
      id: 'queue',
      target: [FIELD],
      // In the queue over the field, or already taken by the agent: the second message of the chat.
      done: (env) => env.find('.hx-queue__row') !== null || env.count('.hx-msg[data-kind="user"]') > 1,
    },
    look('next'),
  ],
};

// The pane and the terminal hang on a chat's header: the lesson begins in a chat. The terminal's button hides while
// the pane is open, so it comes first.
const openChat: ITourStep = { id: 'chat', target: ['.hx-sidebar__list', FIELD], done: inChat };

const pane: ITourTrack = {
  id: 'pane',
  steps: [
    openChat,
    { id: 'term', target: ['.hx-header__term'], done: has('.hx-header__term[aria-pressed="true"]') },
    { id: 'open', target: [TOOLS], done: has('.hx-pane[data-state="open"]') },
    look('tools', ['.hx-pane']),
  ],
};

const safe: ITourTrack = {
  id: 'safe',
  steps: [
    { id: 'settings', target: ['.hx-user__open'], done: inSettings() },
    { id: 'safe', target: [SETTINGS_ITEM('safe')], done: inSettings('safe') },
    { id: 'conn', target: [SETTINGS_ITEM('connections')], done: inSettings('connections') },
  ],
};

const agents: ITourTrack = {
  id: 'agents',
  steps: [
    goHome,
    { id: 'send', target: [FIELD], done: inChat },
    look('list', ['.hx-sidebar__list']),
    { id: 'subs', target: [TOOLS, '.hx-pane'], done: has('.hx-tv[data-tool="subagents"]') },
  ],
};

const extra: ITourTrack = {
  id: 'extra',
  steps: [
    { id: 'search', target: [SEARCH], done: has('.hx-srch') },
    { id: 'bell', target: [BELL], done: has('.hx-notif__pane') },
    look('side', [TOOLS]),
  ],
};

export const TOUR_TRACKS: Readonly<Record<string, ITourTrack>> = {
  welcome,
  chat,
  work,
  pane,
  site,
  slides,
  video,
  ds,
  safe,
  agents,
  extra,
};
/** The track of this id; a key the object only inherits (`constructor`, from an address or storage) is none. */
export const trackOf = (id: string | null): ITourTrack | null =>
  id !== null && Object.hasOwn(TOUR_TRACKS, id) ? (TOUR_TRACKS[id] ?? null) : null;

/** The Sinopia lessons, listed on its own page. */
export const TOUR_ORDER = ['site', 'slides', 'video', 'ds'] as const;
