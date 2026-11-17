// The feed of the day: the address an app's stage view names (`view.feed`) answers with the person's day, the page asks
// it itself with the person's cookies (no bridge, no SDK). GET {feed} is the day, GET {feed}/items/{id} one thing to do,
// POST {feed}/items/{id}/{action} what the person decided about it. Every field but the calendar is optional: a feed
// that says less draws less.

export type TItemId = string | number;

/** A link inside a line of text: to a thing to do, to a slot of the calendar, or down the page to a block. */
export interface IRichLink {
  t: string;
  item?: TItemId;
  slot?: string;
  jump?: 'pri' | 'cal';
}

export type TRichPart = string | IRichLink;

export interface IDayMaterial {
  icon?: string;
  label: string;
  meta?: string;
  url?: string;
}

export interface IDayColumn {
  title: string;
  tone?: 'good' | 'bad' | 'plan';
  lines: string[];
}

/** What the agent gathered for a meeting or an answer: shown in the card over the calendar. */
export interface IDayPrep {
  ctx?: string[];
  quote?: string;
  draft?: string;
  cols?: IDayColumn[];
  note?: { label: string; text: string };
  mats?: IDayMaterial[];
}

export const SLOT = { meet: 'meet', focus: 'focus' } as const;
export type TSlotKind = (typeof SLOT)[keyof typeof SLOT];

export interface IDaySlot {
  id?: string;
  kind: TSlotKind;
  title: string;
  short?: string;
  meta?: string;
  start: string;
  end: string;
  place?: string;
  url?: string;
  itemId?: TItemId;
  prep?: IDayPrep;
  /** A meeting the agent proposes: drawn dashed until the person says yes or no; `why` is its reason. */
  offer?: boolean;
  why?: string;
}

export const FLAG_WHO = { me: 'me', agent: 'agent' } as const;
export type TFlagWho = (typeof FLAG_WHO)[keyof typeof FLAG_WHO];

export interface IDayFlag {
  itemId?: TItemId;
  who: TFlagWho;
  /** `reply`: somebody waits for the person's answer; `promise`, `run`: a thing promised or being done. */
  kind: string;
  title: string;
  short?: string;
  tip?: string;
  meta?: string;
  due?: string;
  where?: string;
  /** `mail` for an answer waited for by mail; the host picks the rest. */
  icon?: string;
  prep?: IDayPrep;
}

export interface IDayCalendar {
  from: string;
  to: string;
  off?: boolean;
  slots?: IDaySlot[];
  flags?: IDayFlag[];
}

export interface IDayPriority {
  itemId: TItemId;
  title: string;
  /** Whose the thing is now: the person's (`you`), the agent's (`agent`), or done (`done`). */
  group?: string;
  /** The feed's section: `prepared` the agent made it ready, `running` it works now, `queue` it will take it, `wait`. */
  section?: string;
  /** The agent's chat about the thing, once an agent worked on it: opening the thing goes there. */
  runUrl?: string;
  due?: string;
  soon?: boolean;
  sub?: string;
  points?: string[];
  cta?: string;
  done?: boolean;
  doneAt?: string;
}

export interface IDayRow {
  time?: string;
  title: string;
  sub?: string;
  itemId?: TItemId;
}

export interface IDay {
  tz?: string;
  name?: string;
  headline?: { text?: string; itemId?: TItemId; rich?: TRichPart[] };
  digest?: { text?: string; rich?: TRichPart[] };
  /** The app can replay a past day for this person and tune itself on it: the page offers «Настроить». */
  tune?: boolean;
  tomorrow?: {
    title?: string;
    text?: string;
    rich?: TRichPart[];
    meetings?: IDayRow[];
    promises?: IDayRow[];
  };
  calendar: IDayCalendar & { days?: IDayCalendar[] };
  priorities?: IDayPriority[];
}

export const CHIP = {
  send: 'send',
  ask: 'ask',
  say: 'say',
  take: 'take',
  done: 'done',
  notMine: 'not-mine',
  open: 'open',
} as const;
export type TChipKind = (typeof CHIP)[keyof typeof CHIP];

export interface IDayChip {
  label: string;
  kind: TChipKind;
  say?: string;
  url?: string;
}

/** A piece of the thing's widget: formatted text and the small widgets that stand between its paragraphs. */
export type TDayPart =
  | { k: 'rich'; v: (['h' | 'p', string] | ['ul', string[]])[] }
  | { k: 'text'; h?: string; icon?: string; src?: string; ps?: string[]; items?: string[] }
  | { k: 'chart'; h: string; src?: string; v: [string, number, boolean?][]; note?: string }
  | { k: 'kpi'; h: string; src?: string; v: [string, string, string?][] }
  | {
      k: 'data';
      h: string;
      src?: string;
      cols: string[];
      rows: { c: string[]; hot?: boolean }[];
      note?: string;
    }
  | { k: 'doc'; title: string; meta?: string; act?: string; url?: string; body: ['h' | 'p', string][] }
  | { k: 'file'; icon?: string; name: string; meta?: string; act?: string; url?: string }
  | {
      k: 'slides';
      h: string;
      src?: string;
      url?: string;
      v: [string, 'title' | 'chart' | 'num' | 'text', boolean?][];
    }
  | {
      k: 'design';
      h: string;
      src?: string;
      url?: string;
      v: [string, 'table' | 'cards' | 'wizard', string][];
    }
  | {
      k: 'mr';
      h: string;
      src?: string;
      url?: string;
      sum?: string;
      files: [string, number, number, string?][];
      ci?: [string, 'ok' | 'fail'][];
    }
  | { k: 'review'; h: string; src?: string; items: [string, string, string, string?][] }
  | { k: 'message'; h: string; icon?: string; src?: string; from?: string; quote?: string; draft: string };

export interface IDayCard {
  itemId: TItemId;
  title: string;
  icon?: string;
  kind?: string;
  where?: string;
  whenText?: string;
  people?: string[];
  lead?: string;
  quote?: string;
  draft?: string;
  cols?: IDayColumn[];
  go?: { icon?: string; title: string; meta?: string; button?: string; url?: string; chat?: string } | null;
  /** The agent's chat about the thing, once an agent took it: the opened thing shows it under its widget. */
  run?: { chat?: string; url?: string } | null;
  parts?: TDayPart[];
  chips?: IDayChip[];
}

export const ACTION = { take: 'take', ask: 'ask', send: 'send', feedback: 'feedback' } as const;
export type TFeedAction = (typeof ACTION)[keyof typeof ACTION];

/** What an action answers: the agent's chat about the thing, by its id in the hub or by its address. */
export interface IActResult {
  chat?: string;
  url?: string;
  run?: { chat?: string; url?: string };
}

export const FEED_STATUS = { login: 401, forbidden: 403, secret: 409 } as const;

/** Why a feed read through the machine did not answer: the app's secret has no value, or the feed refused it. */
export const SECRET_NEED = { missing: 'secret-missing', refused: 'secret-refused' } as const;
export type TSecretNeed = (typeof SECRET_NEED)[keyof typeof SECRET_NEED];

export class FeedError extends Error {
  readonly status: number;
  /** The code the answer named in its `error` field, when it was JSON with one. */
  readonly code: string | undefined;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = 'FeedError';
    this.status = status;
    this.code = code;
  }
}

/** A feed read through the machine's runtime (`feedPath`): a path of this origin, signed in by the machine. */
export const throughMachine = (feed: string): boolean => feed.startsWith('/');

/**
 * The feed's own site does not know the person: only a feed the page reads itself can be signed in to from here. Through
 * the machine a 401 is the host's own session, which a window of the feed's site would not help.
 */
export const isLogin = (error: unknown, feed: string): boolean =>
  error instanceof FeedError && error.status === FEED_STATUS.login && !throughMachine(feed);

/** The machine has no token for the feed, or the feed refused it: the app's card is where it is put. */
export const secretNeed = (error: unknown): TSecretNeed | null => {
  // A feed answers 409 of its own too (the thing is already being done): only the machine's codes are about the secret.
  if (!(error instanceof FeedError) || error.status !== FEED_STATUS.secret) return null;
  if (error.code === SECRET_NEED.refused) return SECRET_NEED.refused;
  return error.code === SECRET_NEED.missing ? SECRET_NEED.missing : null;
};

/** An address under the feed: the feed's own may end in a query (the machine a runtime path names), which stays last. */
export const under = (feed: string, sub: string): string => {
  const at = feed.search(/[?#]/);
  const path = at < 0 ? feed : feed.slice(0, at);
  return `${path.replace(/\/+$/, '')}${sub}${at < 0 ? '' : feed.slice(at)}`;
};

const codeOf = (text: string): string | undefined => {
  try {
    const body: unknown = JSON.parse(text);
    if (typeof body !== 'object' || body === null || !('error' in body)) return undefined;
    return typeof body.error === 'string' ? body.error : undefined;
  } catch {
    return undefined;
  }
};

const call = async <T>(url: string, init?: RequestInit): Promise<T> => {
  const res = await fetch(url, { credentials: 'include', ...init });
  const text = await res.text();
  if (!res.ok) throw new FeedError(res.status, text.slice(0, 200) || res.statusText, codeOf(text));
  return (text === '' ? {} : JSON.parse(text)) as T;
};

export const loadDay = (feed: string): Promise<IDay> => call<IDay>(feed);

export const loadCard = (feed: string, id: TItemId): Promise<IDayCard> =>
  call<IDayCard>(under(feed, `/items/${encodeURIComponent(String(id))}`));

export const actOnItem = (
  feed: string,
  id: TItemId,
  action: TFeedAction,
  body: Record<string, string> = {},
): Promise<IActResult> =>
  call<IActResult>(under(feed, `/items/${encodeURIComponent(String(id))}/${action}`), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'harnessly' },
    body: JSON.stringify(body),
  });

/** The sign-in of the feed's own site, in a window of its own: the page loads the day again when it closes. */
export const loginUrl = (feed: string): string => `${new URL(feed).origin}/auth/login?popup=1`;

/** An address the feed gave, kept only when it is a web one: nothing else is opened or linked. */
export const webUrl = (url: string | undefined): string | undefined => {
  if (url === undefined) return undefined;
  try {
    const { protocol } = new URL(url);
    return protocol === 'https:' || protocol === 'http:' ? url : undefined;
  } catch {
    return undefined;
  }
};

export const sameItem = (a: TItemId | undefined, b: TItemId | undefined): boolean =>
  a !== undefined && b !== undefined && String(a) === String(b);
