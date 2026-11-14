// What the search (⌘K) reads besides the chats: the catalog of the settings, the team's documents, what the connected
// systems find (merge requests, issues, pages), and the model's answer to a question in words
// (cmd/runtime/routes_search.go).
import { deviceId } from '@/next/core/device';
import { getPage } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { answer, list, num, obj, opt, str } from '@/next/features/settings/platform-answer';
import { DEVICE_HEADER, type MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;
const CONNECTION_TIMEOUT_MS = 20_000;

const url = (machine: MachineId, path: `/${string}`) => runtimePath(platformPath(path), machine);

export const SETTING_KIND = { enum: 'enum', bool: 'bool', number: 'number' } as const;
export const SETTING_ON = 'on';
export const SETTING_OFF = 'off';

export interface ISettingOption {
  value: string;
  label: string;
}

/** A setting of the catalog (internal/settingsctl): what it is, what it takes and what it is now. */
export interface ICatalogSetting {
  key: string;
  section: string;
  title: string;
  description: string;
  kind: string;
  options: readonly ISettingOption[];
  value: string;
  valueLabel: string;
}

const readOption = (v: unknown): ISettingOption => {
  const o = obj(v, 'option');
  return { value: str(o.value, 'option.value'), label: str(o.label, 'option.label') };
};

const readSetting = (v: unknown): ICatalogSetting => {
  const o = obj(v, 'setting');
  return {
    key: str(o.key, 'key'),
    section: str(o.section, 'section'),
    title: str(o.title, 'title'),
    description: str(o.description, 'description'),
    kind: str(o.kind, 'kind'),
    options: list(o.options, 'options', readOption),
    value: str(o.value, 'value'),
    valueLabel: str(o.valueLabel, 'valueLabel'),
  };
};

export const loadSettingsCatalog = (machine: MachineId) =>
  getPage(
    url(machine, '/settings-catalog'),
    answer((o) => list(o.settings, 'settings', readSetting)),
    TIMEOUT_MS,
  );

/** A document of the team's catalog. */
export interface ICatalogDoc {
  slug: string;
  title: string;
  url: string;
  /** ISO time of the last change; '' when the catalog does not say. */
  changedAt: string;
  /** Who keeps it; '' when the catalog does not say. */
  owner: string;
  /** «board» for a board, else a document. */
  kind: string;
  /** Discussions not closed yet. */
  openThreads: number;
}

const nameOf = (v: unknown): string => {
  if (v === undefined || v === null) return '';
  const o = obj(v, 'person');
  return opt(o.name) || opt(o.displayName) || opt(o.login) || opt(o.email);
};

const readDoc = (v: unknown): ICatalogDoc => {
  const o = obj(v, 'doc');
  const slug = str(o.slug, 'slug');
  return {
    slug,
    title: opt(o.title) || slug,
    url: str(o.url, 'url'),
    changedAt: opt(o.lastActivityAt),
    owner: nameOf(o.owner),
    kind: opt(o.kind),
    openThreads: typeof o.openThreads === 'number' ? o.openThreads : 0,
  };
};

export const loadCatalogDocs = (machine: MachineId) =>
  getPage(
    url(machine, '/docs'),
    answer((o) => list(o.docs, 'docs', readDoc)),
    TIMEOUT_MS,
  );

/** One published version of a document, the newest first. */
export interface IDocVersion {
  n: number;
  at: string;
  author: string;
  note: string;
}

const readVersion = (v: unknown): IDocVersion => {
  const o = obj(v, 'version');
  return { n: num(o.n, 'n'), at: opt(o.createdAt), author: opt(o.author), note: opt(o.note) };
};

export const loadDocVersions = (machine: MachineId, slug: string) =>
  getPage(
    url(machine, `/docs/${encodeURIComponent(slug)}/versions`),
    answer((o) => list(o.versions, 'versions', readVersion)),
    TIMEOUT_MS,
  );

export const FOUND_KIND = { mr: 'mr', issue: 'issue', page: 'page' } as const;
export type TFoundKind = (typeof FOUND_KIND)[keyof typeof FOUND_KIND];

/** One thing a connected system found. What a kind does not have is ''. */
export interface IFound {
  kind: TFoundKind;
  title: string;
  url: string;
  /** Where it is: the project of a merge request, the key of an issue, the space of a page. */
  place: string;
  /** The state in the system's own word (merged, opened, Done); '' when it has none. */
  state: string;
  /** ISO time of the last change. */
  updated: string;
  /** An issue's assignee, a page's author. */
  person: string;
  /** A merge request's source branch. */
  branch: string;
  /** The state of a merge request's pipeline, in the system's own word. */
  pipeline: string;
  draft: boolean;
  /** An issue's description, the words of a page around the match. */
  text: string;
  /** An issue's type and priority, in the system's own words. */
  type: string;
  priority: string;
}

const NO_MORE = {
  updated: '',
  person: '',
  branch: '',
  pipeline: '',
  draft: false,
  text: '',
  type: '',
  priority: '',
} as const;

const readMr = (v: unknown): IFound => {
  const o = obj(v, 'mr');
  num(o.iid, 'iid');
  const pipeline = o.pipeline === undefined || o.pipeline === null ? null : obj(o.pipeline, 'pipeline');
  return {
    ...NO_MORE,
    kind: FOUND_KIND.mr,
    title: str(o.title, 'title'),
    url: str(o.url, 'url'),
    place: opt(o.project),
    state: opt(o.state),
    updated: opt(o.updated),
    branch: opt(o.sourceBranch),
    pipeline: pipeline === null ? '' : opt(pipeline.status),
    draft: o.draft === true,
  };
};

const readIssue = (v: unknown): IFound => {
  const o = obj(v, 'issue');
  const status = o.status === undefined || o.status === null ? null : obj(o.status, 'status');
  return {
    ...NO_MORE,
    kind: FOUND_KIND.issue,
    title: str(o.title, 'title'),
    url: str(o.url, 'url'),
    place: opt(o.key),
    state: status === null ? '' : opt(status.name),
    updated: opt(o.updated),
    person: nameOf(o.assignee),
    text: opt(o.description),
    type: opt(o.type),
    priority: opt(o.priority),
  };
};

const readPage = (v: unknown): IFound => {
  const o = obj(v, 'page');
  return {
    ...NO_MORE,
    kind: FOUND_KIND.page,
    title: str(o.title, 'title'),
    url: str(o.url, 'url'),
    place: opt(o.space),
    state: '',
    updated: opt(o.updated),
    person: nameOf(o.author),
    text: opt(o.excerpt),
  };
};

const FOUND_PER_KIND = 5;
/** How many of the person's own a connection's tab lists before anything is typed. */
const MINE_PER_KIND = 12;

const READERS = {
  [FOUND_KIND.mr]: { path: 'mrs', read: answer((o) => list(o.mrs, 'mrs', readMr)) },
  [FOUND_KIND.issue]: { path: 'issues', read: answer((o) => list(o.issues, 'issues', readIssue)) },
  [FOUND_KIND.page]: { path: 'pages', read: answer((o) => list(o.pages, 'pages', readPage)) },
} as const;

/** What one connection finds of one kind for the words. */
export const searchConnection = (machine: MachineId, conn: string, kind: TFoundKind, query: string) =>
  getPage(
    url(
      machine,
      `/connections/${encodeURIComponent(conn)}/${READERS[kind].path}?q=${encodeURIComponent(query)}&limit=${FOUND_PER_KIND}`,
    ),
    READERS[kind].read,
    CONNECTION_TIMEOUT_MS,
  );

/** What a connection's tab lists before anything is typed: the person's open issues, the open merge requests. */
export const listConnection = (
  machine: MachineId,
  conn: string,
  kind: typeof FOUND_KIND.mr | typeof FOUND_KIND.issue,
) =>
  getPage(
    url(
      machine,
      kind === FOUND_KIND.issue
        ? `/connections/${encodeURIComponent(conn)}/issues?mine=1&open=1&limit=${MINE_PER_KIND}`
        : `/connections/${encodeURIComponent(conn)}/mrs?state=opened&limit=${MINE_PER_KIND}`,
    ),
    READERS[kind].read,
    CONNECTION_TIMEOUT_MS,
  );

/** One issue whole, with its description. */
export const loadIssue = (machine: MachineId, conn: string, key: string) =>
  getPage(
    url(machine, `/connections/${encodeURIComponent(conn)}/issue?key=${encodeURIComponent(key)}`),
    answer((o) => readIssue(o.issue)),
    CONNECTION_TIMEOUT_MS,
  );

/** A found thing as the model is shown it. */
export interface IAnswerSource {
  kind: string;
  title: string;
  text: string;
}

export interface ISearchAnswer {
  text: string;
  /** The sources the answer rests on, by their place in what was sent. */
  cited: readonly number[];
}

/** The refusal of an answer when no model is picked for «Answers in the search» and no provider can stand in. */
export const ANSWER_NO_MODEL = 'no-model';

export const ANSWER_EVENT = { delta: 'delta', done: 'done', error: 'error' } as const;

export type TAnswerEvent =
  /** A piece of the answer as the model writes it; it may carry the marks of the sources ([2]). */
  | { type: typeof ANSWER_EVENT.delta; text: string }
  | { type: typeof ANSWER_EVENT.done; answer: ISearchAnswer }
  /** `code` is the machine's reason (ANSWER_NO_MODEL among them), `message` its words for the person ('' for none). */
  | { type: typeof ANSWER_EVENT.error; code: string; message: string };

/** The marks of the sources in the text the model writes: the page shows the sources as links, not as numbers. */
export const withoutMarks = (text: string): string =>
  text.replace(/\s*\[\d{1,2}\]/g, '').replace(/\s*\[\d?$/, '');

const eventOf = (name: string, data: string): TAnswerEvent | null => {
  let doc: unknown;
  try {
    doc = JSON.parse(data);
  } catch {
    return null;
  }
  if (typeof doc !== 'object' || doc === null) return null;
  const o = doc as Record<string, unknown>;
  if (name === ANSWER_EVENT.delta) return { type: ANSWER_EVENT.delta, text: opt(o.text) };
  if (name === ANSWER_EVENT.error)
    return { type: ANSWER_EVENT.error, code: opt(o.error) || 'failed', message: opt(o.message) };
  if (name === ANSWER_EVENT.done && typeof o.text === 'string')
    return {
      type: ANSWER_EVENT.done,
      answer: {
        text: o.text,
        cited: Array.isArray(o.cited) ? o.cited.filter((x): x is number => typeof x === 'number') : [],
      },
    };
  return null;
};

/** Splits what came so far into whole events (`event:` and `data:` lines, then a blank one) and what is left. */
export const readAnswerFrames = (buffer: string): { events: TAnswerEvent[]; rest: string } => {
  const frames = buffer.split(/\r?\n\r?\n/);
  const rest = frames.pop() ?? '';
  const events = frames.flatMap((frame) => {
    const lines = frame.split(/\r?\n/);
    const name =
      lines
        .find((line) => line.startsWith('event:'))
        ?.slice(6)
        .trim() ?? '';
    const data = lines
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trim())
      .join('');
    return eventOf(name, data) ?? [];
  });
  return { events, rest };
};

const failedEvent = (code: string, message = ''): TAnswerEvent => ({
  type: ANSWER_EVENT.error,
  code,
  message,
});

/**
 * Asks the model and reports the answer as it is written: pieces, then the whole answer with its sources, or why
 * there is none. A machine that does not stream answers at once, which is reported the same way. Every outcome but
 * the caller's own abort ends in done or error.
 */
export const askSearchStream = async (
  machine: MachineId,
  query: string,
  sources: readonly IAnswerSource[],
  signal: AbortSignal,
  onEvent: (event: TAnswerEvent) => void,
): Promise<void> => {
  let res: Response;
  try {
    res = await fetch(url(machine, '/search/answer'), {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        [DEVICE_HEADER]: deviceId(),
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
      },
      body: JSON.stringify({ query, sources }),
      signal,
    });
  } catch {
    if (!signal.aborted) onEvent(failedEvent('network'));
    return;
  }
  const streamed = (res.headers.get('Content-Type') ?? '').includes('text/event-stream');
  if (!streamed || !res.body) {
    const doc: unknown = await res.json().catch(() => null);
    if (signal.aborted) return;
    const o = typeof doc === 'object' && doc !== null ? (doc as Record<string, unknown>) : {};
    if (res.ok && typeof o.text === 'string') {
      const done = eventOf(ANSWER_EVENT.done, JSON.stringify(o));
      onEvent(done ?? failedEvent('failed'));
    } else onEvent(failedEvent(opt(o.error) || 'failed', opt(o.message)));
    return;
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let rest = '';
  let ended = false;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      const parsed = readAnswerFrames(rest + decoder.decode(value, { stream: true }));
      rest = parsed.rest;
      for (const event of parsed.events) {
        if (event.type !== ANSWER_EVENT.delta) ended = true;
        onEvent(event);
      }
    }
  } catch {
    if (!signal.aborted) onEvent(failedEvent('interrupted'));
    return;
  }
  if (!ended && !signal.aborted) onEvent(failedEvent('interrupted'));
};
