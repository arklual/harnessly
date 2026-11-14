// A thing a turn made that is a link (a stand, a dev server, a merge request or a pull request, a commit, a task of the
// tracker, a document, a board, a page) as its card in the feed shows it: which service it is, the item the link
// previews read its live title and state by (meta-store.ts, GET …/link-meta), and the line under the title. What
// counts as a stand, the tracker, GitLab, the documents and the boards is the page's config, which the runtime takes
// from the organization's settings in the hub or from the machine's environment. No React here.
import { cardKindOf } from '@/next/features/chat/link-card';
import {
  ARTIFACT_KIND,
  LOOK,
  PIPELINE,
  TONE,
  toneOfIssue,
  type IArtifactItem,
  type TLook,
  type TMeta,
  type TPipeline,
} from '@/next/features/artifacts/model';
import { t } from '@/next/i18n';
import type { TurnOutput } from '@/next/protocol/v2';
import type { IAppConfig } from '@/platform/config';

export const LINK = {
  stand: 'stand',
  server: 'server',
  mr: 'mr',
  pr: 'pr',
  commit: 'commit',
  task: 'task',
  doc: 'doc',
  board: 'board',
  page: 'page',
} as const;
export type TLink = (typeof LINK)[keyof typeof LINK];

/** The icon of each service: what the card is, at a glance. */
export const LINK_ICON = {
  stand: 'rocket',
  server: 'monitor',
  mr: 'gitlab',
  pr: 'github',
  commit: 'git-commit-horizontal',
  task: 'square-check-big',
  doc: 'file-text',
  board: 'workflow',
  page: 'book-open',
} as const satisfies Record<TLink, string>;

export interface ILinkOutput {
  kind: TLink;
  url: string;
  /** As the card names its place: «stand-1.preview.example.com», «harnessly !80», «HRN-1426». */
  key: string;
  /** What the link previews are asked by; null for what they do not read (a dev server, a claude.ai page). */
  item: IArtifactItem | null;
  /** A dev server's port. */
  port: number | null;
  /** Whether a new tab of the person's browser can show it: a server of the machine cannot. */
  newTab: boolean;
}

type TLinkConfig = Pick<IAppConfig, 'artifactsUrl' | 'boardsUrl' | 'readerUrl' | 'githubHost'>;

const LOCAL = /^(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])$/;
const ISSUE_KEY = /\/issue\/([A-Z][A-Z0-9]{1,15}-\d{1,7})/;
const COMMIT_SHA = /\/commit\/([0-9a-f]{7,40})\/?$/;

const parse = (ref: string): URL | null => {
  if (!/^https?:\/\//i.test(ref)) return null;
  try {
    return new URL(ref);
  } catch {
    return null;
  }
};

const where = (url: URL): string => `${url.host}${url.pathname === '/' ? '' : url.pathname}`;

/** The project of a forge address: the part before `/-/…`, `/pull/…`, `/commit/…`, its last segment. */
const projectOf = (url: URL): string => {
  const parts = url.pathname.split('/').filter(Boolean);
  const cut = parts.findIndex((p) => p === '-' || p === 'pull' || p === 'commit' || p === 'merge_requests');
  const head = cut < 0 ? parts : parts.slice(0, cut);
  return head[head.length - 1] ?? url.host;
};

const item = (
  url: URL,
  kind: IArtifactItem['kind'],
  key: string | null,
  title: string,
  extra: Partial<IArtifactItem> = {},
) => ({
  url: url.href,
  kind,
  key,
  label: null,
  fallback: title,
  where: where(url),
  github: false,
  readable: true,
  at: 0,
  ...extra,
});

/** The link a thing the turn made is, or null for one that is no link (a design, a file). */
export const linkOf = (output: TurnOutput, config: TLinkConfig): ILinkOutput | null => {
  const url = parse(output.ref);
  if (!url) return null;
  const host = url.host.toLowerCase();
  const github = url.hostname.toLowerCase() === config.githubHost.toLowerCase();
  const base = { url: url.href, port: null, newTab: true };
  switch (output.kind) {
    case 'task': {
      const key = ISSUE_KEY.exec(url.pathname)?.[1] ?? output.title;
      return { ...base, kind: LINK.task, key, item: item(url, ARTIFACT_KIND.task, key, output.title) };
    }
    case 'commit': {
      const sha = COMMIT_SHA.exec(url.pathname)?.[1]?.slice(0, 8) ?? '';
      const key = `${projectOf(url)} ${sha}`.trim();
      // GitHub's commits have no preview of their own: the card shows what the address says.
      const preview = item(url, ARTIFACT_KIND.link, key, output.title, { readable: !github });
      return { ...base, kind: LINK.commit, key, item: preview };
    }
    case 'mr': {
      const pr = github || url.pathname.includes('/pull/');
      const kind = pr ? LINK.pr : LINK.mr;
      return {
        ...base,
        kind,
        key: output.title,
        item: item(url, ARTIFACT_KIND.mr, output.title, output.title, { github: pr }),
      };
    }
    case 'stand':
      if (LOCAL.test(url.hostname)) {
        const port = Number(url.port) || null;
        return { ...base, kind: LINK.server, key: `localhost:${url.port}`, item: null, port, newTab: false };
      }
      return {
        ...base,
        kind: LINK.stand,
        key: host,
        item: item(url, ARTIFACT_KIND.link, null, output.title),
      };
    case 'artifact': {
      const card = cardKindOf(url.href, config);
      if (card === 'board')
        return {
          ...base,
          kind: LINK.board,
          key: host,
          item: item(url, ARTIFACT_KIND.board, null, output.title),
        };
      if (card === 'doc')
        return { ...base, kind: LINK.doc, key: host, item: item(url, ARTIFACT_KIND.doc, null, output.title) };
      return { ...base, kind: LINK.page, key: host, item: null };
    }
    default:
      return null;
  }
};

/** The state of a card: how it is read now, its words and its tone. */
export const STATE_TONE = { ok: 'ok', bad: 'bad', busy: 'busy', rest: 'rest' } as const;
export type TStateTone = (typeof STATE_TONE)[keyof typeof STATE_TONE];

export interface ILinkView {
  /** The live title; null while there is none (the card shows the turn's own). */
  title: string | null;
  /** The line under the title: what it is, where, its state. */
  meta: string;
  tone: TStateTone | null;
  /** Still being read: the line says so softly. */
  pending: boolean;
}

const PIPELINE_TONE: Record<TPipeline, TStateTone | null> = {
  success: STATE_TONE.ok,
  failed: STATE_TONE.bad,
  running: STATE_TONE.busy,
  other: null,
};

const pipelineText = (p: TPipeline | null): string | null =>
  p === null || p === PIPELINE.other ? null : t(`next.outputs.pipeline.${p}`);

const mrState = (state: string, draft: boolean): string => {
  if (draft && state === 'opened') return t('next.outputs.mr.draft');
  if (state === 'merged' || state === 'opened') return t(`next.outputs.mr.${state}`);
  return t('next.outputs.mr.closed');
};

const join = (...parts: (string | null)[]): string => parts.filter((p): p is string => !!p).join(' · ');

/**
 * What the card says of its link: the live title and state when the previews read them, the kind and the place while
 * they are on their way or when there are none. `up` is a dev server's: listening or not, null while unknown.
 */
export const viewOf = (link: ILinkOutput, meta: TMeta | null, look: TLook, up: boolean | null): ILinkView => {
  const kind = t(`next.outputs.link.${link.kind}`);
  const pending = link.item !== null && link.item.readable && look === LOOK.wait;
  const plain: ILinkView = { title: null, meta: join(kind, link.key), tone: null, pending };
  if (link.kind === LINK.server) {
    if (up === null) return plain;
    return {
      ...plain,
      meta: join(kind, link.key, t(up ? 'next.outputs.up' : 'next.outputs.serverDown')),
      tone: up ? STATE_TONE.ok : STATE_TONE.bad,
    };
  }
  if (meta === null) return plain;
  switch (meta.kind) {
    case 'issue': {
      const tone = toneOfIssue(meta.state, meta.resolved);
      const toneOf: Record<string, TStateTone | null> = {
        [TONE.done]: STATE_TONE.rest,
        [TONE.progress]: STATE_TONE.busy,
        [TONE.review]: STATE_TONE.busy,
        [TONE.open]: null,
      };
      return {
        ...plain,
        title: meta.title,
        meta: join(meta.key || link.key, meta.state, meta.assignee?.name ?? null),
        tone: toneOf[tone],
      };
    }
    case 'mr': {
      const pipeline = meta.state === 'opened' ? meta.pipeline : null;
      const rest = meta.state === 'merged' || meta.state === 'closed' || meta.state === 'locked';
      const tone = rest ? STATE_TONE.rest : ((pipeline && PIPELINE_TONE[pipeline]) ?? STATE_TONE.busy);
      return {
        ...plain,
        title: meta.title,
        meta: join(kind, link.key, mrState(meta.state, meta.draft), pipelineText(pipeline)),
        tone,
      };
    }
    case 'commit':
      return {
        ...plain,
        title: meta.title,
        meta: join(kind, link.key, pipelineText(meta.pipeline)),
        tone: meta.pipeline ? PIPELINE_TONE[meta.pipeline] : null,
      };
    case 'stand':
      return {
        ...plain,
        title: meta.title,
        meta: join(kind, link.key, t(meta.up ? 'next.outputs.up' : 'next.outputs.standDown')),
        tone: meta.up ? STATE_TONE.ok : STATE_TONE.bad,
      };
    case 'artifact':
      return { ...plain, title: meta.title };
  }
};
