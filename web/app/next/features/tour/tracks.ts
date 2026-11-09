// The lessons of the hands-on tour: Sinopia on the demo projects of Relay, each step a real action.
// Targets are the product's own elements; a step inside the board itself (a click on the page, a text edited there)
// is not seen by the shell, so it ends with the person's «Done».
import type { ITourEnv, ITourStage, ITourStep, ITourTrack } from '@/next/features/tour/model';

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
/** The design is in its editing mode: the address keeps it (`?edit=1`). */
const editing = (env: ITourEnv) => /[?&]edit=1(?:&|$)/.test(env.path);
const found =
  (...selectors: string[]) =>
  (env: ITourEnv) =>
    selectors.some((sel) => env.find(sel) !== null);
/** No agent on this machine can answer: a request of the lesson is passed over. */
const noAgent = (env: ITourEnv) => env.facts.has('agent:none');

/** The library, and a design of it, as the steps' places and the ways back to them. */
const LIBRARY = /\/designly\/?(?:[?#]|$)/;
const LIBRARY_PATH = '/designly';
const IN = (id: string) => ({ where: new RegExp(`/designly/${id}(?:[/?#]|$)`), back: `/designly/${id}` });

// The pick switch on (the bar's «Выделить» pressed), its card open over a block, a note pinned to a block.
const PICK_ON = [
  '.hx-browser__tool[aria-label="Выделить"][aria-pressed="true"]',
  '.hx-browser__tool[aria-label="Pick"][aria-pressed="true"]',
];
const NOTE_CARD = '.hx-ann';
const NOTE_PIN = '.hx-npin';
// The send button of the design's chat docked beside the page, else the folded field's.
const SEND = '.hx-site__chat .hx-round:has(.hx-icon[data-i="arrow-up"])';

const openSinopia: ITourStep = {
  id: 'sinopia',
  target: [SIDEBAR_SINOPIA],
  done: (env) => /\/designly(?:[/?#]|$)/.test(env.path),
};

/** What hides a demo project in the library, to be set aside: the search, a chip, a tab, the trash. */
const HIDING = [
  '.hx-dl__trash button.hx-dl__crumb',
  '.hx-dl__ibtn[data-on]:has(.hx-icon[data-i="sliders-horizontal"])',
  '.hx-dl__search[data-filled="true"]',
  '.hx-dl__tab[aria-selected="true"]:not(:first-child)',
  '.hx-dl__toolbar',
];
const FOLDER_NAME = (id: string) => `.hx-dl-folder[data-drop-folder="${id}"] .hx-dl-folder__name`;

/** The library's way to the project, click by click (the library says it in facts: library-model.ts routeToCard). */
const wayTo =
  (id: string) =>
  (env: ITourEnv): ITourStage | null => {
    const prefix = `lib:route:${id}:`;
    const fact = [...env.facts].find((f) => f.startsWith(prefix))?.slice(prefix.length);
    if (!fact || fact === 'card') return null;
    if (fact === 'gone')
      return { id: 'gone', target: ['.hx-dl__demo', '.hx-dl__body'], action: 'restoreDemo' };
    if (fact === 'reset') return { id: 'reset', target: HIDING, action: 'clear' };
    if (fact === 'up') return { id: 'up', target: ['.hx-dl__crumb[data-drop-folder=""]'] };
    const folder = fact.replace(/^folder:/, '');
    return {
      id: 'folder',
      target: [`.hx-dl-folder[data-drop-folder="${folder}"] .hx-dl-folder__open`],
      vars: { folder: env.find(FOLDER_NAME(folder))?.textContent ?? '' },
    };
  };

const open = (id: string): ITourStep => ({
  id: 'open',
  target: [card(id)],
  done: at(id),
  where: LIBRARY,
  back: LIBRARY_PATH,
  stage: wayTo(id),
});

/** The agent's request of a lesson: on the cheapest model, in the chat beside the design, passed over with no agent. */
const ask = (id: string, prompt: string | undefined, frame: string): ITourStep => ({
  id: 'ask',
  // Where the field is folded away (a deck or a video looked at, not edited), the ring is on the mode switch.
  target: [SEND, ...COMPOSER, MODE, frame],
  // The card keeps off the whole field: the note and the request in it are what is sent.
  avoid: ['.hx-site__chat .hx-composer'],
  prompt,
  done: chatStarted,
  cheap: true,
  sideChat: true,
  skip: noAgent,
  ...IN(id),
});

const site: ITourTrack = {
  id: 'site',
  steps: [
    openSinopia,
    open('relay-site'),
    // The flags of the hero's code: the ring stands on them inside the page.
    { id: 'click', target: [SITE_FRAME], frame: '.flags', ...IN('relay-site') },
    { id: 'edit', target: [MODE], done: signal('site:edit'), sideChat: true, ...IN('relay-site') },
    {
      id: 'pick',
      target: [PICK],
      done: found(...PICK_ON, NOTE_CARD, NOTE_PIN),
      sideChat: true,
      ...IN('relay-site'),
    },
    // A click on the heading opens its note: the request is written right there, about that block.
    {
      id: 'heading',
      target: [SITE_FRAME],
      frame: 'h1',
      done: found(NOTE_CARD, NOTE_PIN),
      ...IN('relay-site'),
    },
    {
      id: 'note',
      target: ['.hx-ann .hx-note__input', NOTE_CARD],
      note: 'Сделай заголовок короче и ярче',
      done: (env) => env.signals.has('site:note') || env.find(NOTE_PIN) !== null,
      needs: { test: found(NOTE_CARD, NOTE_PIN), back: 'pick' },
      ...IN('relay-site'),
    },
    { ...ask('relay-site', undefined, SITE_FRAME), needs: { test: found(NOTE_PIN), back: 'pick' } },
    {
      id: 'page',
      target: ['.hx-strip__tabs', SITE_FRAME],
      done: signalLike((s) => s.startsWith('site:page:Цены')),
      ...IN('relay-site'),
    },
    { id: 'year', target: [SITE_FRAME], frame: 'label[for="yr"]', ...IN('relay-site') },
    { id: 'device', target: ['.hx-browser__dev'], done: signal('site:device:ip14'), ...IN('relay-site') },
  ],
};

const slides: ITourTrack = {
  id: 'slides',
  steps: [
    openSinopia,
    open('relay-deck'),
    {
      id: 'next',
      target: [NEXT_SLIDE, DECK_FRAME],
      done: signalLike((s) => /^deck:slide:[1-9]/.test(s)),
      ...IN('relay-deck'),
    },
    { id: 'edit', target: [MODE, DECK_FRAME], done: editing, sideChat: true, ...IN('relay-deck') },
    {
      id: 'comment',
      target: ['.hx-deck__bar [data-k="comment"]', DECK_FRAME],
      done: signal('deck:comment'),
      ...IN('relay-deck'),
    },
    {
      id: 'show',
      target: ['.hx-deck__bar [data-k="full"]', '.hx-deck__bar [data-k="show"]'],
      done: (env) => env.signals.has('deck:full') || env.signals.has('deck:screen:show'),
      ...IN('relay-deck'),
    },
    ask('relay-deck', 'Добавь после «Решения» слайд с отзывом клиента', DECK_FRAME),
  ],
};

const video: ITourTrack = {
  id: 'video',
  steps: [
    openSinopia,
    open('relay-video'),
    { id: 'play', target: [VIDEO_FRAME], done: signal('video:play'), ...IN('relay-video') },
    {
      id: 'scene',
      target: ['[aria-label="Следующая сцена"]', '[aria-label="Next scene"]', '.hx-video__timeline'],
      done: signalLike((s) => /^video:scene:[1-9]/.test(s)),
      ...IN('relay-video'),
    },
    { id: 'timeline', target: ['.hx-video__timeline'], ...IN('relay-video') },
    ask('relay-video', 'В сцене «Откат за секунду» замени подпись на «Откат — одним кликом»', VIDEO_FRAME),
    {
      id: 'download',
      target: ['[data-k="download"]'],
      done: (env) => env.find('[role=menu]') !== null,
      ...IN('relay-video'),
    },
  ],
};

const ds: ITourTrack = {
  id: 'ds',
  steps: [
    openSinopia,
    open('relay-ds'),
    { id: 'look', target: [SITE_FRAME], look: true, ...IN('relay-ds') },
    { id: 'back', target: [SIDEBAR_SINOPIA], done: (env) => LIBRARY.test(env.path) },
    {
      id: 'pick',
      target: ['.hx-deck-chip'],
      done: (env) =>
        env.find('.hx-deck-set__ds [role=radio]:not(:first-child)[aria-checked="true"]') !== null,
      where: LIBRARY,
      back: LIBRARY_PATH,
    },
    {
      id: 'make',
      target: [...COMPOSER, '.hx-dl__hero'],
      prompt: 'Страница «О компании» для Relay',
      done: at('[^/?#]+'),
      cheap: true,
      skip: noAgent,
      where: LIBRARY,
      back: LIBRARY_PATH,
    },
  ],
};

// The basics, in the person's own chats: the composer, the agent at work, the right pane, the safe, several agents.
const NAV = (i: number) => `.hx-nav__group[data-i="${i}"] .hx-nav__item`;
const NEW_CHAT = NAV(0);
const SEARCH = NAV(1);
const BELL = '.hx-user__bell';
// «Ещё» is the last of the sidebar's places: the lessons live in its menu.
const MORE = NAV(4);
const FIELD = '.hx-composer__layer';
const ROUND = (icon: string) => `.hx-composer__bar .hx-round:has(.hx-icon[data-i="${icon}"])`;
const TOOLS = '.hx-header__tools';
// The same tools on the start page, where there is no chat yet to carry them in its header.
const HOME_TOOLS = '.hx-hometools';
const TURN = '.hx-work__head';
const SETTINGS_ITEM = (id: string) => `.hx-set__nav-item[data-id="${id}"]`;

const has = (selector: string) => (env: ITourEnv) => env.find(selector) !== null;
const typed = (env: ITourEnv) => {
  const field = env.find('.hx-composer__input');
  return field instanceof HTMLTextAreaElement && field.value.trim().length > 0;
};
const inChat = (env: ITourEnv) => /\/c\/[^/?#]+/.test(env.path);
/** A step in an open chat: elsewhere the card says to come back to it. */
const CHAT = { where: /\/c\/[^/?#]+/ };
/** A step of the agent at work: with no agent on the machine it is passed over. */
const AGENT = { skip: noAgent };
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
    look('pane', [TOOLS, HOME_TOOLS]),
    look('sinopia', [SIDEBAR_SINOPIA]),
    look('learn', [MORE]),
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
    { id: 'send', target: [ROUND('arrow-up'), FIELD], done: inChat, cheap: true },
    { id: 'turn', target: [TURN], done: has(`${TURN}[aria-expanded="true"]`), ...CHAT, ...AGENT },
  ],
};

const work: ITourTrack = {
  id: 'work',
  steps: [
    {
      id: 'send',
      target: [ROUND('arrow-up'), FIELD],
      done: (env) => inChat(env) && env.find('.hx-work[data-live]') !== null,
      cheap: true,
      ...AGENT,
    },
    { ...look('list', ['.hx-sidebar__list']), ...AGENT },
    { id: 'turn', target: [TURN], done: has(`${TURN}[aria-expanded="true"]`), ...CHAT, ...AGENT },
    {
      id: 'queue',
      target: [ROUND('arrow-up'), FIELD],
      ...CHAT,
      ...AGENT,
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
    { id: 'term', target: ['.hx-header__term'], done: has('.hx-header__term[aria-pressed="true"]'), ...CHAT },
    { id: 'open', target: [TOOLS], done: has('.hx-pane[data-state="open"]'), ...CHAT },
    { ...look('tools', ['.hx-pane']), ...CHAT },
  ],
};

const safe: ITourTrack = {
  id: 'safe',
  steps: [
    { id: 'settings', target: ['.hx-user__open'], done: inSettings() },
    {
      id: 'safe',
      target: [SETTINGS_ITEM('safe')],
      done: inSettings('safe'),
      where: /\/settings(?:[/?#]|$)/,
      back: '/settings',
    },
    {
      id: 'conn',
      target: [SETTINGS_ITEM('connections')],
      done: inSettings('connections'),
      where: /\/settings(?:[/?#]|$)/,
      back: '/settings',
    },
  ],
};

const agents: ITourTrack = {
  id: 'agents',
  steps: [
    goHome,
    { id: 'send', target: [ROUND('arrow-up'), FIELD], done: inChat, cheap: true },
    look('list', ['.hx-sidebar__list']),
    { id: 'subs', target: [TOOLS, '.hx-pane'], done: has('.hx-tv[data-tool="subagents"]'), ...CHAT },
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

/** The buttons that draw the sidebar out on a phone, one on each kind of page header. */
const BURGER = [
  '.hx-header__menu',
  '.hx-learn__menu',
  '.hx-appmain__menu',
  '.hx-site__menu',
  '.hx-help__menu',
].map((c) => `.hx-head-btn${c}`);
const PHONE_QUERY = '(max-width: 900px)';

/** On a phone the sidebar is a drawer that lies shut: a step about its items first shows the button that opens it. */
const viaDrawer = (step: ITourStep): ITourStep => {
  if (step.stage || step.target.length === 0 || !step.target.every((s) => s.startsWith('.hx-nav')))
    return step;
  return {
    ...step,
    stage: (env) =>
      window.matchMedia?.(PHONE_QUERY).matches && env.find('.hx-app[data-drawer="closed"]')
        ? { id: 'drawer', target: BURGER }
        : null,
  };
};
const withDrawer = (track: ITourTrack): ITourTrack => ({ ...track, steps: track.steps.map(viaDrawer) });

export const TOUR_TRACKS: Readonly<Record<string, ITourTrack>> = Object.fromEntries(
  Object.entries({
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
  }).map(([id, track]) => [id, withDrawer(track)]),
);
/** The track of this id; a key the object only inherits (`constructor`, from an address or storage) is none. */
export const trackOf = (id: string | null): ITourTrack | null =>
  id !== null && Object.hasOwn(TOUR_TRACKS, id) ? (TOUR_TRACKS[id] ?? null) : null;

/** The Sinopia lessons, listed on its own page. */
export const TOUR_ORDER = ['site', 'slides', 'video', 'ds'] as const;
