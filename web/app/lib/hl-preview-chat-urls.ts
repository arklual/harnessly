import { encodePath, type IPreviewTarget } from '@/lib/hl-preview-client';
import type { IPreviewPort, TPortGroup } from '@/lib/hl-preview/ports-shared';
import { standUrlPattern, standUrlOf } from '@/lib/hl-stand-url';
import type { ITimelineEntry } from '@/types/timeline';

/** A dev-server address the agent printed or wrote in the conversation. */
export interface IChatUrl {
  port: number;
  path: string;
  at: number;
}

const LOCAL_URL =
  /\bhttps?:\/\/(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]):(\d{1,5})(\/[^\s"'`<>()[\]{}]*)?/gi;
const TRAILING_PUNCTUATION = /[.,;:!?]+$/;

const pathOf = (raw: string | undefined): string => {
  const path = (raw ?? '/').replace(TRAILING_PUNCTUATION, '') || '/';
  try {
    return encodePath(path);
  } catch {
    return '/';
  }
};

type TFoundUrl = Pick<IChatUrl, 'port' | 'path'>;

// The whole timeline is re-read on every message (the browser tab, auto-open): only new entries get their text scanned.
const foundUrls = new WeakMap<ITimelineEntry, TFoundUrl[]>();

const urlsIn = (entry: ITimelineEntry, text: string): TFoundUrl[] => {
  let found = foundUrls.get(entry);
  if (!found) {
    found = [];
    for (const match of text.matchAll(LOCAL_URL)) {
      const port = Number(match[1]);
      if (port >= 1 && port <= 65535) found.push({ port, path: pathOf(match[2]) });
    }
    foundUrls.set(entry, found);
  }
  return found;
};

/** Assistant text and what Bash (or a Codex command) printed; the person's own messages and other tools do not count. */
const urlsOf = (entries: readonly ITimelineEntry[]): Array<{ urls: TFoundUrl[]; at: number }> => {
  const bashCalls = new Set(
    entries.flatMap((entry) =>
      entry.type === 'tool-call' && entry.toolName === 'Bash' ? [entry.toolUseId] : [],
    ),
  );
  return entries.flatMap((entry) => {
    if (entry.type === 'assistant-message')
      return [{ urls: urlsIn(entry, entry.markdown), at: entry.timestamp }];
    if (entry.type === 'tool-result' && !entry.isError && bashCalls.has(entry.toolUseId))
      return [{ urls: urlsIn(entry, entry.summary), at: entry.timestamp }];
    if (entry.type === 'exec-command-stream')
      return [{ urls: urlsIn(entry, `${entry.stdout}\n${entry.stderr ?? ''}`), at: entry.timestamp }];
    return [];
  });
};

/** Local addresses in the conversation, one per port with its last mention, newest first. */
export const extractLocalUrls = (entries: readonly ITimelineEntry[]): IChatUrl[] => {
  const byPort = new Map<number, IChatUrl>();
  for (const { urls, at } of urlsOf(entries)) {
    for (const { port, path } of urls) {
      byPort.delete(port);
      byPort.set(port, { port, path, at });
    }
  }
  return [...byPort.values()].sort((a, b) => b.at - a.at);
};

/** A feature stand the agent named: its address with the last mention. */
export interface IChatStand {
  url: string;
  host: string;
  at: number;
}

const foundStands = new WeakMap<ITimelineEntry, string[]>();

const standsIn = (entry: ITimelineEntry, text: string): string[] => {
  let found = foundStands.get(entry);
  if (!found) {
    found = [...text.matchAll(standUrlPattern())].flatMap((match) => {
      const url = standUrlOf(match[0].replace(TRAILING_PUNCTUATION, ''));
      return url ? [url] : [];
    });
    foundStands.set(entry, found);
  }
  return found;
};

/** Stands the conversation named (assistant text, Bash output), one per host with its last mention, newest first. */
export const extractStandUrls = (entries: readonly ITimelineEntry[]): IChatStand[] => {
  const bashCalls = new Set(
    entries.flatMap((entry) =>
      entry.type === 'tool-call' && entry.toolName === 'Bash' ? [entry.toolUseId] : [],
    ),
  );
  const byHost = new Map<string, IChatStand>();
  for (const entry of entries) {
    let text: string | null = null;
    if (entry.type === 'assistant-message') text = entry.markdown;
    else if (entry.type === 'tool-result' && !entry.isError && bashCalls.has(entry.toolUseId))
      text = entry.summary;
    else if (entry.type === 'exec-command-stream') text = `${entry.stdout}\n${entry.stderr ?? ''}`;
    if (text === null) continue;
    for (const url of standsIn(entry, text)) {
      const host = new URL(url).hostname;
      byHost.delete(host);
      byHost.set(host, { url, host, at: entry.timestamp });
    }
  }
  return [...byHost.values()].sort((a, b) => b.at - a.at);
};

/** A listed port with what the conversation said about it; `listening: false` is an address nobody serves any more. */
export interface IPortRow extends IPreviewPort {
  path: string;
  mentionedAt: number | null;
  listening: boolean;
}

const GROUP_ORDER: Record<TPortGroup, number> = { chat: 0, agent: 1, other: 2 };

export const mergePortRows = (ports: readonly IPreviewPort[], urls: readonly IChatUrl[]): IPortRow[] => {
  const mentioned = new Map(urls.map((url) => [url.port, url]));
  const rows: IPortRow[] = ports.map((port) => {
    const url = mentioned.get(port.port);
    // A server the chat talks about but started outside its worktrees (a temp dir) still belongs to the chat.
    const group = url && port.group === 'other' ? 'chat' : port.group;
    return { ...port, group, path: url?.path ?? '/', mentionedAt: url?.at ?? null, listening: true };
  });
  const listening = new Set(ports.map((port) => port.port));
  for (const url of urls) {
    if (listening.has(url.port)) continue;
    rows.push({
      port: url.port,
      pid: null,
      command: null,
      cwd: null,
      group: 'chat',
      owner: null,
      kind: 'unknown',
      responding: false,
      path: url.path,
      mentionedAt: url.at,
      listening: false,
    });
  }
  return rows.sort((a, b) => GROUP_ORDER[a.group] - GROUP_ORDER[b.group] || a.port - b.port);
};

export interface IAutoOpenInput {
  rows: readonly IPortRow[];
  /** Ports already seen answering as sites that still listen (`trackSeenSites(...).before`); null before the first answer. */
  previousPorts: ReadonlySet<number> | null;
  /** No preview is open or opening in the tab: auto-open never replaces what the person opened. */
  empty: boolean;
  /** The island is shown for the tab for the first time. */
  firstShow: boolean;
  /** Newest chat address already acted on (opened or passed over). */
  seenAt: number;
  now: number;
  /** The port the window shows, when the island opened it by itself, and when. */
  autoOpened?: { port: number; at: number } | null;
}

/** An address older than this is history, not a server the agent has just started. */
export const FRESH_MENTION_MS = 30 * 60_000;
/** An auto-opened MFE remote gives way to a lower shell that comes up this soon after it. */
export const SHELL_SWITCH_MS = 60_000;

/**
 * Ports seen answering as sites, kept while they keep listening. A dev server listens before it compiles, and while it
 * rebuilds the probe may miss its answer for a poll: «new» means answering now and never seen answering since it
 * started listening, so a rebuild is never new, a first answer is, and a server that stopped listening starts over.
 * `before` is what the previous answers saw, pruned to the ports listening now (null before the first answer); `after`
 * adds the current answering sites.
 */
export const trackSeenSites = (
  seen: ReadonlySet<number> | null,
  ports: readonly IPreviewPort[],
): { before: Set<number> | null; after: Set<number> } => {
  const listening = new Set(ports.map((port) => port.port));
  const before = seen === null ? null : new Set([...seen].filter((port) => listening.has(port)));
  const answering = ports.filter((port) => port.responding && port.kind === 'site').map((port) => port.port);
  return { before, after: new Set([...(before ?? []), ...answering]) };
};

/**
 * What the island opens by itself. On first show: the freshest answering chat address, else the lowest site of the
 * chat (an MFE shell sits below its remotes). Later: an address newer than `seenAt` (the agent has just started a
 * server), else the lowest chat site that was not an answering site in the previous answer. An address that does not
 * answer yet stays unseen so it opens once the server is up; an address older than 30 minutes and an API are never
 * opened by themselves. A window the island filled by itself less than a minute ago moves to a lower chat site that
 * has just come up; a window the person filled is never touched.
 */
export const pickAutoOpen = ({
  rows,
  previousPorts,
  empty,
  firstShow,
  seenAt,
  now,
  autoOpened = null,
}: IAutoOpenInput): { target: IPreviewTarget | null; seenAt: number } => {
  const answering = rows.filter(
    (row) => row.responding && row.mentionedAt !== null && row.mentionedAt > seenAt,
  );
  const newest = Math.max(seenAt, ...answering.map((row) => row.mentionedAt!));
  const fresh = answering
    .filter((row) => row.kind !== 'api' && row.mentionedAt! >= now - FRESH_MENTION_MS)
    .sort((a, b) => b.mentionedAt! - a.mentionedAt!)[0];
  const chatSites = rows
    .filter((row) => row.group === 'chat' && row.kind === 'site' && row.responding)
    .sort((a, b) => a.port - b.port);
  const appeared = previousPorts === null ? [] : chatSites.filter((row) => !previousPorts.has(row.port));
  const targetOf = (row: IPortRow | undefined) => (row ? { port: row.port, path: row.path } : null);
  if (!empty) {
    const switching = autoOpened !== null && now - autoOpened.at <= SHELL_SWITCH_MS;
    return {
      target: switching ? targetOf(appeared.find((row) => row.port < autoOpened.port)) : null,
      seenAt: newest,
    };
  }
  return { target: targetOf(fresh ?? (firstShow ? chatSites[0] : appeared[0])), seenAt: newest };
};

const MAX_WORK_ROOTS = 32;
const WORKTREE = /^(.*\/\.worktrees\/[^/]+\/[^/]+)(?:\/|$)/;
const WORKTREES_FOLDER = /\/\.worktrees(?:\/[^/]+)?\/?$/;

/**
 * Paths the runtime groups «this chat» by, the session cwd first, then agent worktrees, then the rest. A file in a
 * worktree stands for the worktree; the worktrees folder, a repository folder in it and anything above a worktree
 * (the workspace root a chat starts in) are dropped, or every agent's server would be «this chat». The runtime
 * normalises again by git toplevel; this keeps the list short and meaningful before the cut.
 */
export const previewWorkRoots = (paths: readonly string[], cwd: string | null): string[] => {
  const mapped = [...new Set(paths.map((path) => WORKTREE.exec(path)?.[1] ?? path))];
  const worktrees = mapped.filter((root) => WORKTREE.test(root));
  const holdsWorktree = (path: string) => {
    const base = path.replace(/\/+$/, '');
    return worktrees.some((worktree) => worktree.startsWith(`${base}/`));
  };
  const keep = (path: string) =>
    !WORKTREES_FOLDER.test(path) && (WORKTREE.test(path) || !holdsWorktree(path));
  const first = cwd !== null && keep(cwd) ? [WORKTREE.exec(cwd)?.[1] ?? cwd] : [];
  const rest = mapped.filter((path) => !WORKTREE.test(path) && keep(path));
  return [...new Set([...first, ...worktrees, ...rest])].slice(0, MAX_WORK_ROOTS);
};
