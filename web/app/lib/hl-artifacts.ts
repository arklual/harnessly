import { issueUrlOf } from '@/lib/site';
import { issueKeyRe } from '@/lib/tracker-keys';
import { parseUserSegments } from '@/lib/message-wrappers';
import { presentLink, type LinkKind } from '@/lib/presented-link';
import { docSlugOf } from '@/lib/hl-doc-path';
import type { ITimelineEntry } from '@/types/timeline';

export type TArtifactGroup = 'artifact' | 'mr' | 'issue' | 'other';

export interface IArtifactLink {
  url: string;
  label: string | null;
  timestamp: number;
  count: number;
  kind: LinkKind;
  group: TArtifactGroup;
}

const MARKDOWN_LINK = /\[([^\]\n]{1,200})\]\((https?:\/\/[^)\s]+)\)/g;
const BARE_URL = /https?:\/\/[^\s<>()"'`\]]+/g;
const TRAILING = /[.,;:!?»”’]+$/;

const cleanUrl = (url: string) => url.replace(TRAILING, '');

const matchCache = new WeakMap<RegExp, WeakMap<object, string[][]>>();

/**
 * `text.matchAll(re)` as plain arrays, remembered per timeline entry: the panels re-read the whole timeline on every
 * message, and only the new entries need their text scanned.
 */
const matchesOf = (entry: object, re: RegExp, text: string): string[][] => {
  let byEntry = matchCache.get(re);
  if (!byEntry) matchCache.set(re, (byEntry = new WeakMap()));
  let found = byEntry.get(entry);
  if (!found) byEntry.set(entry, (found = [...text.matchAll(re)].map((match) => [...match])));
  return found;
};

export const artifactGroupOf = (kind: LinkKind): TArtifactGroup =>
  kind === 'artifact' || kind === 'claude-artifact'
    ? 'artifact'
    : kind === 'mr'
      ? 'mr'
      : kind === 'issue'
        ? 'issue'
        : 'other';

const identity = (url: string, link: ReturnType<typeof presentLink>) => {
  if (link.kind === 'issue' && link.key) return { id: `issue:${link.key}`, url: issueUrlOf(link.key) ?? url };
  if (link.kind === 'mr' && link.project) return { id: `mr:${link.project}!${link.iid}`, url };
  // A document is one thing whether it was named by its portal address, with or without a key, or by the reader's.
  const slug = link.kind === 'artifact' ? docSlugOf(url) : null;
  if (slug) return { id: `doc:${slug}`, url };
  return { id: url.split('#')[0], url };
};

export interface IArtifactCollector {
  add: (entries: readonly ITimelineEntry[]) => void;
  links: () => IArtifactLink[];
}

interface IFoundLink {
  id: string;
  url: string;
  label: string | null;
  kind: LinkKind;
}

const findLink = (rawUrl: string, label: string | null): IFoundLink | null => {
  const cleaned = cleanUrl(rawUrl);
  try {
    const host = new URL(cleaned).hostname;
    if (host === 'localhost' || host === '127.0.0.1' || host === '0.0.0.0') return null;
  } catch {
    return null;
  }
  const presented = presentLink(cleaned);
  const { id, url } = identity(cleaned, presented);
  return { id, url, label, kind: presented.kind };
};

let foundLinks = new WeakMap<ITimelineEntry, IFoundLink[]>();
let foundWithKeys = '';

/**
 * Links of one answer in the order they appear; remembered per entry, as the timeline is re-read on every message.
 * Forgotten when the tracker's project keys arrive, since they decide which bare keys are issues.
 */
const linksOf = (entry: ITimelineEntry & { type: 'assistant-message' }, issueKeys: RegExp): IFoundLink[] => {
  if (issueKeys.source !== foundWithKeys) {
    foundLinks = new WeakMap();
    foundWithKeys = issueKeys.source;
  }
  const cached = foundLinks.get(entry);
  if (cached) return cached;
  const text = entry.markdown ?? '';
  const found: (IFoundLink | null)[] = [];
  const labelled = new Set<string>();
  for (const match of text.matchAll(MARKDOWN_LINK)) {
    labelled.add(cleanUrl(match[2]));
    found.push(findLink(match[2], match[1].trim()));
  }
  const unlabelled = text.replace(MARKDOWN_LINK, ' ');
  for (const match of unlabelled.matchAll(BARE_URL)) {
    if (!labelled.has(cleanUrl(match[0]))) found.push(findLink(match[0], null));
  }
  for (const match of unlabelled.replace(BARE_URL, ' ').matchAll(issueKeys)) {
    const issue = issueUrlOf(match[1]);
    if (issue) found.push(findLink(issue, null));
  }
  const links = found.filter((link): link is IFoundLink => link !== null);
  foundLinks.set(entry, links);
  return links;
};

/**
 * Accumulates links over entries fed in order, so a server can index a transcript chunk by chunk with
 * exactly the rules the timeline uses.
 */
export const createArtifactCollector = (): IArtifactCollector => {
  const links = new Map<string, IArtifactLink>();

  const add = (entries: readonly ITimelineEntry[]) => {
    const issueKeys = issueKeyRe();
    for (const entry of entries) {
      if (entry.type !== 'assistant-message') continue;
      for (const { id, url, label, kind } of linksOf(entry, issueKeys)) {
        const existing = links.get(id);
        links.set(id, {
          url: existing?.url ?? url,
          label: label ?? existing?.label ?? null,
          timestamp: Math.max(entry.timestamp, existing?.timestamp ?? 0),
          count: (existing?.count ?? 0) + 1,
          kind,
          group: artifactGroupOf(kind),
        });
      }
    }
  };

  return { add, links: () => [...links.values()].sort((a, b) => b.timestamp - a.timestamp) };
};

/** Work products and references mentioned in assistant answers, newest first; files the agent edited live in Changes. */
export const collectArtifacts = (entries: readonly ITimelineEntry[]): IArtifactLink[] => {
  const collector = createArtifactCollector();
  collector.add(entries);
  return collector.links();
};

/** Number of work products (artifact pages, merge requests, tracker issues) in the conversation. */
export interface IPeerMessage {
  id: string;
  direction: 'in' | 'out';
  peer: string;
  from?: string;
  text: string;
  timestamp: number;
}

/** Messages exchanged with other agent sessions — plans and handoffs live there, not in links. Newest first. */
export const collectPeerMessages = (entries: ITimelineEntry[]): IPeerMessage[] =>
  entries
    .flatMap((entry): IPeerMessage[] => {
      if (entry.type === 'tool-call' && entry.peerMessage) {
        return [
          {
            id: entry.id,
            direction: 'out',
            peer: entry.peerMessage.to,
            text: entry.peerMessage.text,
            timestamp: entry.timestamp,
          },
        ];
      }
      if (entry.type === 'user-message') {
        return parseUserSegments(entry.text).flatMap((segment, i): IPeerMessage[] =>
          segment.kind === 'peer'
            ? [
                {
                  id: `${entry.id}:${i}`,
                  direction: 'in',
                  peer: segment.name,
                  from: segment.from,
                  text: segment.text,
                  timestamp: entry.timestamp,
                },
              ]
            : [],
        );
      }
      return [];
    })
    .reverse();

export const countArtifacts = (entries: ITimelineEntry[]) =>
  collectArtifacts(entries).filter((link) => link.group !== 'other' && link.group !== 'mr').length;

const ABSOLUTE_PATH = /(?:^|[\s'"=(])(\/(?:[\w.@+-]+\/)*[\w.@+-]+)/g;

const HIDDEN_WORK_PATH = /^\/(proc|sys|dev|tmp\/claude-[^/]+\/[^/]+\/tasks)\b/;

/** Adds the filesystem locations of entries to `paths` in the order they appear. */
export const addWorkPaths = (paths: Set<string>, entries: readonly ITimelineEntry[]) => {
  for (const entry of entries) {
    if (entry.type === 'tool-call') {
      if (entry.filePath) paths.add(entry.filePath);
      if (entry.toolName === 'Bash')
        for (const m of matchesOf(entry, ABSOLUTE_PATH, entry.summary)) paths.add(m[1]);
    } else if (entry.type === 'patch-apply') {
      for (const file of entry.files) paths.add(file.path);
    } else if (entry.type === 'exec-command-stream') {
      if (entry.cwd) paths.add(entry.cwd);
      for (const m of matchesOf(entry, ABSOLUTE_PATH, entry.command)) paths.add(m[1]);
    }
  }
};

/** Newest first, without system and scratch task paths. */
export const workPathList = (paths: Iterable<string>): string[] =>
  [...paths].filter((p) => !HIDDEN_WORK_PATH.test(p)).reverse();

/** Filesystem locations the agent worked with: tool targets, paths in shell commands and the working directory. */
export const collectWorkPaths = (entries: readonly ITimelineEntry[], cwd: string | null): string[] => {
  const paths = new Set<string>();
  if (cwd) paths.add(cwd);
  addWorkPaths(paths, entries);
  return workPathList(paths);
};

export interface ITouchedFile {
  path: string;
  action: 'created' | 'edited';
  timestamp: number;
  count: number;
}

/** Files the agent wrote or edited through its tools, newest first. */
export const collectTouchedFiles = (entries: readonly ITimelineEntry[]): ITouchedFile[] => {
  const files = new Map<string, ITouchedFile>();
  const add = (path: string, action: ITouchedFile['action'], timestamp: number) => {
    if (!path) return;
    const existing = files.get(path);
    files.set(path, {
      path,
      action: existing?.action === 'created' ? 'created' : action,
      timestamp: Math.max(timestamp, existing?.timestamp ?? 0),
      count: (existing?.count ?? 0) + 1,
    });
  };
  for (const entry of entries) {
    if (
      entry.type === 'tool-call' &&
      entry.filePath &&
      ['Write', 'Edit', 'MultiEdit', 'NotebookEdit'].includes(entry.toolName)
    ) {
      add(
        entry.filePath,
        entry.toolName === 'Write' || /^Create /.test(entry.summary) ? 'created' : 'edited',
        entry.timestamp,
      );
    } else if (entry.type === 'patch-apply') {
      for (const file of entry.files)
        add(
          file.path,
          file.status === 'add' || file.status === 'added' ? 'created' : 'edited',
          entry.timestamp,
        );
    }
  }
  return [...files.values()].sort((a, b) => b.timestamp - a.timestamp);
};

/** Folds the files the entries wrote or edited into `edited`, keeping the time of the latest write of each. */
export const addEditedPaths = (edited: Map<string, number>, entries: readonly ITimelineEntry[]) => {
  for (const file of collectTouchedFiles(entries))
    edited.set(file.path, Math.max(file.timestamp, edited.get(file.path) ?? 0));
};

export type TFileKind = 'image' | 'video' | 'markdown' | 'html' | 'pdf' | 'text';

export interface IViewableFile {
  path: string;
  kind: TFileKind;
  origin: 'created' | 'edited' | 'viewed' | 'mentioned';
  timestamp: number;
}

const EXT_KIND: Record<string, TFileKind> = {
  png: 'image',
  jpg: 'image',
  jpeg: 'image',
  gif: 'image',
  webp: 'image',
  svg: 'image',
  mp4: 'video',
  m4v: 'video',
  webm: 'video',
  mov: 'video',
  md: 'markdown',
  markdown: 'markdown',
  html: 'html',
  htm: 'html',
  pdf: 'pdf',
};
const SHOWCASE = new Set<TFileKind>(['image', 'video', 'markdown', 'html', 'pdf']);
// The lookbehind skips paths glued to a shell variable or a relative prefix (`$S/report.md`, `./a.md`) — those don't resolve as written.
const FILE_IN_TEXT =
  /(?<![\w$}~./-])(\/(?:[\w.@+-]+\/)*[\w.@+-]+\.(?:png|jpe?g|gif|webp|svg|md|markdown|html?|pdf))\b/gi;

export const fileKindOf = (path: string): TFileKind =>
  EXT_KIND[path.split('.').pop()?.toLowerCase() ?? ''] ?? 'text';

/** Pictures and documents the agent wrote, looked at or produced — code edits live in the changes island. */
export const collectViewableFiles = (entries: ITimelineEntry[]): IViewableFile[] => {
  const files = new Map<string, IViewableFile>();
  const add = (path: string, origin: IViewableFile['origin'], timestamp: number) => {
    const kind = fileKindOf(path);
    if (!SHOWCASE.has(kind)) return;
    const existing = files.get(path);
    const rank = { created: 3, edited: 2, viewed: 1, mentioned: 0 };
    files.set(path, {
      path,
      kind,
      origin: existing && rank[existing.origin] > rank[origin] ? existing.origin : origin,
      timestamp: Math.max(timestamp, existing?.timestamp ?? 0),
    });
  };
  for (const entry of entries) {
    if (entry.type !== 'tool-call') continue;
    if (entry.filePath && ['Write', 'Edit', 'MultiEdit', 'NotebookEdit'].includes(entry.toolName)) {
      add(
        entry.filePath,
        entry.toolName === 'Write' || /^Create /.test(entry.summary) ? 'created' : 'edited',
        entry.timestamp,
      );
    } else if (entry.sentFiles) {
      for (const path of entry.sentFiles.paths) add(path, 'created', entry.timestamp);
    } else if (entry.filePath && entry.toolName === 'Read') {
      add(entry.filePath, 'viewed', entry.timestamp);
    } else if (entry.toolName === 'Bash') {
      for (const match of matchesOf(entry, FILE_IN_TEXT, entry.summary))
        add(match[1], 'mentioned', entry.timestamp);
    }
  }
  return [...files.values()].sort((a, b) => b.timestamp - a.timestamp);
};
