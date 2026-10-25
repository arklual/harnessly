// Tasks and files that the agent's text names become chips in place (marks in the text, no link
// cards). This finds them in plain text; links become chips in markdown.tsx — a merge request, a pull request or a commit
// only from its link: a bare `!80` names no project and stays a word (the page does not guess).
import { appConfig } from '@/platform/config';
import type { FeedItem, RefInfo, RefKind } from '@/next/features/chat/types';

export type TSegment = { text: string } | { ref: RefKind; label: string };

// A path has a slash and an extension (web/app/next/feed.tsx, feed.tsx:12 is a file only with a slash before it, so
// plain words with dots stay text).
const FILE = String.raw`(?<![\w/.:-])(?:[\w@.-]+/)+[\w@-]+(?:\.[\w-]+)+(?::\d+)?`;

let cached: { source: string; re: RegExp } | null = null;

const pattern = (): RegExp => {
  const task = appConfig().taskKeyPattern;
  if (cached?.source === task) return cached.re;
  // Named: the tracker's pattern may hold groups of its own ((ABC|EXP)-\d+), which would shift numbered ones.
  const re = new RegExp(String.raw`(?<task>\b(?:${task})\b)|(?<file>${FILE})`, 'g');
  cached = { source: task, re };
  return re;
};

/** Splits plain text into text and references, in order. */
export const splitRefs = (text: string): TSegment[] => {
  const re = pattern();
  const out: TSegment[] = [];
  let last = 0;
  re.lastIndex = 0;
  for (let match = re.exec(text); match; match = re.exec(text)) {
    if (match.index > last) out.push({ text: text.slice(last, match.index) });
    out.push({ ref: match.groups?.task ? 'task' : 'file', label: match[0] });
    last = match.index + match[0].length;
  }
  if (last < text.length) out.push({ text: text.slice(last) });
  return out;
};

// A merge request on GitLab (/-/merge_requests/80), a pull request on GitHub (/pull/80), a commit on either.
const MR_LINK = /^https?:\/\/[^/]+\/(.+?)(?:\/-)?\/(merge_requests|pull)\/(\d+)(?:[/?#].*)?$/;
const COMMIT_LINK = /^https?:\/\/[^/]+\/(.+?)(?:\/-)?\/commits?\/([0-9a-f]{7,40})(?:[/?#].*)?$/i;

/** What a link to a merge request, a pull request or a commit is called on its chip («harnessly !80», «harnessly
 *  1a2b3c4»); null for any other link. */
export const changeLinkLabel = (href: string): string | null => {
  const mr = MR_LINK.exec(href);
  // GitLab writes a merge request !80, GitHub a pull request #80.
  if (mr?.[1] && mr[2] && mr[3]) return `${mr[1].split('/').pop()} ${mr[2] === 'pull' ? '#' : '!'}${mr[3]}`;
  const commit = COMMIT_LINK.exec(href);
  if (commit?.[1] && commit[2]) return `${commit[1].split('/').pop()} ${commit[2].slice(0, 7)}`;
  return null;
};

/** What a chip knows when the page has nothing more about it. */
export const bareRef = (kind: RefKind, label: string): RefInfo => ({
  kind,
  label,
  title: null,
  status: null,
  tone: null,
  meta: null,
});

const MD_PATH = /^[\w@./~-]+\.md$/i;
const LINE_SUFFIX = /:\d+$/;

/** The Markdown files the conversation's turns touched or made (a read, a write, a document card): the only ones a
 *  path in the agent's text opens, since the page cannot tell a file that is there from one that is only named. */
export const docPathsOf = (items: readonly FeedItem[]): ReadonlySet<string> => {
  const out = new Set<string>();
  for (const item of items) {
    if (item.kind !== 'turn') continue;
    for (const file of item.turn.files) if (MD_PATH.test(file.path)) out.add(file.path);
    for (const output of item.turn.outputs ?? []) if (output.kind === 'doc') out.add(output.ref);
  }
  return out;
};

/**
 * The touched path an inline `code` of the agent's text opens, as the turn wrote it (`docs/plan.md` is
 * /home/x/app/docs/plan.md when that is the file touched); null for any other code.
 */
export const docOfCode = (code: string, docs: ReadonlySet<string> | undefined): string | null => {
  if (!docs || docs.size === 0) return null;
  const path = code.replace(LINE_SUFFIX, '');
  if (!MD_PATH.test(path)) return null;
  const rel = path.replace(/^\.\//, '');
  for (const have of docs) if (have === rel || have.endsWith(`/${rel}`)) return have;
  return null;
};
