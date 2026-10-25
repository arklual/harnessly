// What the Artifacts tool shows: the links of the conversation as plain items, the title and state the
// runtime read for each of them (GET …/link-meta), and the row each item becomes. No React, no network: the store that
// fetches is meta-store.ts, the view is artifacts-view.tsx.

export const ARTIFACT_KIND = {
  doc: 'doc',
  board: 'board',
  task: 'task',
  mr: 'mr',
  link: 'link',
} as const;
export type TArtifactKind = (typeof ARTIFACT_KIND)[keyof typeof ARTIFACT_KIND];

/** One link of the conversation, as the bridge to Legacy hands it over (legacy/artifacts.ts). */
export interface IArtifactItem {
  url: string;
  kind: TArtifactKind;
  /** A task's key, a merge request's «repo !12»; null for the rest. */
  key: string | null;
  /** The agent's own words for the link, when it gave any. */
  label: string | null;
  /** What to call it when nothing better is known: «Документ», «Доска». */
  fallback: string;
  /** Where it lives, short: the host and the path. */
  where: string;
  /** The link is on GitHub: its title comes from the GitHub route. */
  github: boolean;
  /** The runtime can read its title and state; false for what only has an address. */
  readable: boolean;
  /** When the agent last mentioned it. */
  at: number;
}

export interface IMetaPerson {
  name: string;
  avatar: string | null;
}

export type TMeta =
  | {
      kind: 'issue';
      key: string;
      title: string;
      state: string | null;
      resolved: boolean;
      assignee: IMetaPerson | null;
    }
  | { kind: 'mr'; title: string; state: string; project: string; iid: number; draft: boolean }
  | { kind: 'artifact'; title: string; description: string | null };

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;
const text = (v: unknown): string | null => (typeof v === 'string' && v.trim() !== '' ? v : null);

const readPerson = (v: unknown): IMetaPerson | null => {
  if (!isRecord(v)) return null;
  const name = text(v.name);
  return name ? { name, avatar: text(v.avatar) } : null;
};

/** The runtime's answer, read strictly; null for a shape that is not one of the three (nothing is guessed). */
export const readMeta = (v: unknown): TMeta | null => {
  if (!isRecord(v)) return null;
  const title = text(v.title);
  if (!title) return null;
  if (v.kind === 'artifact') return { kind: 'artifact', title, description: text(v.description) };
  if (v.kind === 'mr')
    return {
      kind: 'mr',
      title,
      state: text(v.state) ?? 'opened',
      project: text(v.project) ?? '',
      iid: typeof v.iid === 'number' ? v.iid : 0,
      draft: v.draft === true,
    };
  if (v.kind === 'issue')
    return {
      kind: 'issue',
      key: text(v.key) ?? '',
      title,
      state: text(v.state),
      resolved: v.resolved === true,
      assignee: readPerson(v.assignee),
    };
  return null;
};

export const TONE = { done: 'done', progress: 'progress', review: 'review', open: 'open' } as const;
export type TTone = (typeof TONE)[keyof typeof TONE];

// Tracker state names as the tracker spells them, in English or Russian: data, not page text.
const REVIEW_STATES = /test|review|verif|провер|ревью|тест/i;
const PROGRESS_STATES = /progress|work|develop|работ/i;

export const toneOfIssue = (state: string | null, resolved: boolean): TTone => {
  if (resolved) return TONE.done;
  if (!state) return TONE.open;
  if (REVIEW_STATES.test(state)) return TONE.review;
  if (PROGRESS_STATES.test(state)) return TONE.progress;
  return TONE.open;
};

const MR_TONE: Record<string, TTone> = {
  opened: TONE.progress,
  merged: TONE.done,
  closed: TONE.open,
  locked: TONE.open,
};

export interface IMrLabels {
  opened: string;
  merged: string;
  closed: string;
  draft: string;
}

/** What a row says: the title (null while it is still being read), the line under it and the tone of its dot. */
export interface IArtifactRow {
  item: IArtifactItem;
  title: string | null;
  sub: string;
  tone: TTone | null;
  person: IMetaPerson | null;
  /** The title and state are still on their way. */
  pending: boolean;
  /** The task is done, the merge request merged: the row rests. */
  rest: boolean;
}

export const LOOK = { wait: 'wait', got: 'got', none: 'none' } as const;
/** wait: asked, no answer yet; got: the meta is there; none: asked, and the runtime does not know it (or did not answer). */
export type TLook = (typeof LOOK)[keyof typeof LOOK];

export const rowOf = (item: IArtifactItem, meta: TMeta | null, look: TLook, mr: IMrLabels): IArtifactRow => {
  const pending = look === LOOK.wait && meta === null && item.readable;
  const base = { item, person: null, pending, rest: false, tone: null as TTone | null };
  const own = item.label ?? item.fallback;
  if (meta?.kind === 'issue') {
    const tone = toneOfIssue(meta.state, meta.resolved);
    return {
      ...base,
      title: meta.title,
      sub: [item.key ?? meta.key, meta.state].filter(Boolean).join(' · '),
      tone,
      person: meta.assignee,
      rest: tone === TONE.done,
    };
  }
  if (meta?.kind === 'mr') {
    const state =
      meta.draft && meta.state === 'opened' ? mr.draft : (mr[meta.state as keyof IMrLabels] ?? mr.closed);
    return {
      ...base,
      title: meta.title,
      sub: [item.key, state].filter(Boolean).join(' · '),
      tone: MR_TONE[meta.state] ?? TONE.open,
      rest: meta.state === 'merged',
    };
  }
  if (meta?.kind === 'artifact') return { ...base, title: meta.title, sub: meta.description ?? item.where };
  if (pending) return { ...base, title: null, sub: item.key ?? item.where };
  return { ...base, title: own, sub: item.key ? item.key : item.where };
};

export const SECTION = { docs: 'docs', tasks: 'tasks', mrs: 'mrs', links: 'links' } as const;
export type TSection = (typeof SECTION)[keyof typeof SECTION];

const SECTION_OF: Record<TArtifactKind, TSection> = {
  doc: SECTION.docs,
  board: SECTION.docs,
  task: SECTION.tasks,
  mr: SECTION.mrs,
  link: SECTION.links,
};

export const SECTION_ORDER: readonly TSection[] = [SECTION.docs, SECTION.tasks, SECTION.mrs, SECTION.links];

export const sectionsOf = (items: readonly IArtifactItem[]): { id: TSection; items: IArtifactItem[] }[] =>
  SECTION_ORDER.map((id) => ({ id, items: items.filter((item) => SECTION_OF[item.kind] === id) })).filter(
    (section) => section.items.length > 0,
  );

/** The items that have a title to read, in the order the rows show them: documents, tasks, merge requests. */
export const readableInOrder = (items: readonly IArtifactItem[]): IArtifactItem[] =>
  sectionsOf(items).flatMap((section) => section.items.filter((item) => item.readable));
