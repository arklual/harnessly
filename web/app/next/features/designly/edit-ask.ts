// The person asked, in so many words, to edit or make a design in Sinopia: then the design the agent starts
// working on opens on the whole screen, in «Редактирование» with its chat beside it (on the phone, the design's sheet
// on the whole screen), not as a tab of the right pane. A design only named or looked at («покажи макет», «открой
// доску посмотреть») stays in the pane.
//
// Two signals together, both required: the person's last message asks to edit or make a design (words, below), and the
// agent of that message works on a design (a pane intent `open design`: its files written, a board handed over, its
// own `open design`). The words alone do not say which design; the work alone does not say the person wants to edit.
// Each message acts once on a device: a person who left the full screen is not thrown back into it by the next edit,
// only by their next such message.

/** A word for the thing: Sinopia, a design, a mockup, a board, a screen, a prototype, a landing page. */
const THING =
  /(?<!\p{L})(?:синопи\p{L}*|sinopia|дизайн\p{L}*|задизайн\p{L}*|макет\p{L}*|доск\p{L}*|экран\p{L}*|прототип\p{L}*|лендинг\p{L}*|вёрстк\p{L}*|верстк\p{L}*|design\p{L}*|mock-?ups?|boards?|wireframes?|prototypes?|landing|screens?|layouts?)(?!\p{L})/iu;

/** A verb that edits or makes: asked (правь, поправим, создай, нарисуй, edit, make…), not told of (поменял). */
const EDIT =
  /(?<!\p{L})(?:прав(?:им|ь|ьте|ка|ку|ить|лю|ишь)|поправ(?:ь|ьте|им|ить|лю|ишь)|исправ(?:ь|ьте|им|ить|лю)|отредактир\p{L}*|редактир\p{L}*|измени(?:ть|м|те)?|поменя(?:й|йте|ем|ть)|передела(?:й|йте|ем|ть)|перерису(?:й|йте|ем)|нарису(?:й|йте|ем)|рису(?:й|йте|ем)|созда(?:й|йте|дим|ть|ём|ем)|сдела(?:й|йте|ем|ть)|сверста(?:й|йте|ем|ть)|доработа(?:й|йте|ем|ть)|добав(?:ь|ьте|им|ить)|убер(?:и|ите|ём|ем)|накида(?:й|йте|ем|ть)|наброса(?:й|йте|ем|ть)|собер(?:и|ите|ём|ем)|перестро(?:й|йте|им|ить)|задизайн\p{L}*|edit|change|create|make|draw|redesign|fix|tweak|update|build|sketch|mock\s+up|design)(?!\p{L})/giu;

/** «не правь», «don't change»: a verb under a no does not ask. */
const NEGATED = /(?:(?<!\p{L})не|n't|not|never)\s+$/iu;
/** «сделай скриншот макета» asks for a picture of the design, not for an edit. */
const SHOT = /^\s*(?:скриншот\p{L}*|скрин\p{L}*|screenshots?|снимок\p{L}*|фото\p{L}*)/iu;

/** Whether the words ask to edit or make a design (pure words: no call to a model). */
export const asksDesignEdit = (text: string): boolean => {
  if (!THING.test(text)) return false;
  for (const m of text.matchAll(EDIT)) {
    const at = m.index ?? 0;
    if (NEGATED.test(text.slice(Math.max(0, at - 12), at))) continue;
    if (SHOT.test(text.slice(at + m[0].length))) continue;
    // «design» is the thing as often as the verb: as a verb it stands first or after «to», «let's», «please».
    if (
      /^design$/i.test(m[0]) &&
      !/(?:^|(?<!\p{L})(?:to|let's|lets|please|pls))\s*$/iu.test(text.slice(0, at))
    )
      continue;
    return true;
  }
  return false;
};

/** A message of the person: its entry id, its words and when it was sent. */
export interface IAsk {
  id: string;
  text: string;
  at: number;
}

/** A message older than this is history read again (a chat opened later), not the work going on now. */
export const ASK_FRESH_MS = 30 * 60_000;

/**
 * Whether the design `design` the agent works on goes to the whole screen for the message `ask`: the message asks to
 * edit a design, it is fresh, and this device did not act on it yet.
 */
export const goesFull = (
  ask: IAsk | null,
  design: string | null,
  acted: (askId: string) => boolean,
  now: number,
): boolean =>
  ask !== null &&
  design !== null &&
  now - ask.at <= ASK_FRESH_MS &&
  !acted(ask.id) &&
  asksDesignEdit(ask.text);

const KEY = 'hx:design-full-asks';
/** How many acted messages a device remembers. */
export const ACTED_LIMIT = 200;

const readActed = (): string[] => {
  try {
    const list: unknown = JSON.parse(localStorage.getItem(KEY) ?? '[]');
    return Array.isArray(list) ? list.filter((x): x is string => typeof x === 'string') : [];
  } catch {
    return [];
  }
};

/** This device already opened the full screen for the message (or chose not to): it never acts again. */
export const actedOn = (conv: string, askId: string): boolean => readActed().includes(`${conv} ${askId}`);

export const markActed = (conv: string, askId: string): void => {
  const key = `${conv} ${askId}`;
  const list = readActed().filter((x) => x !== key);
  try {
    localStorage.setItem(KEY, JSON.stringify([...list, key].slice(-ACTED_LIMIT)));
  } catch {
    // Blocked storage: the page acts once while it lives (the caller keeps its own mark too).
  }
};

/** «создай новый», «нарисуй», «new», «create»: a design not made yet, so not the one the chat worked on before. */
const NEW = /(?<!\p{L})(?:нов\p{L}*|созда\p{L}*|нарису\p{L}*|с нуля|new|create|draw|from scratch)(?!\p{L})/iu;

/** A design the chat's agent works on: its pane intent's design and when it was asked. */
export interface IDesignWork {
  design: string;
  at: number;
}

/**
 * The design a message is about: the newest one the agent took up after it, else (when the message does not ask for
 * a new one) the newest the chat worked on before — «правим макет дальше» goes on with it at once.
 */
export const designFor = (ask: IAsk | null, works: readonly IDesignWork[]): string | null => {
  if (!ask || works.length === 0) return null;
  const sorted = [...works].sort((a, b) => a.at - b.at);
  const after = sorted.filter((w) => w.at >= ask.at);
  if (after.length > 0) return after[after.length - 1].design;
  return NEW.test(ask.text) ? null : sorted[sorted.length - 1].design;
};
