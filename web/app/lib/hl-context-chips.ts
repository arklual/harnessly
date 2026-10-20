import { issueUrlOf } from '@/lib/site';
import { GITLAB_COM_HOST, type TGitlabHost } from '@/lib/gitlab-hosts';
import { t } from '@/lib/i18n';
import { MR_STATE_LABEL, type TMrState } from '@/lib/link-meta';

export const TRACKER_ISSUE_URL = (key: string): string => issueUrlOf(key) ?? '';
export const GITLAB_MR_URL = (host: TGitlabHost, project: string, iid: number): string =>
  `https://${host}/${project}/-/merge_requests/${iid}`;

export interface IConversationChipExcerpt {
  role: 'user' | 'assistant';
  text: string;
}

export interface IConversationChipData {
  title: string;
  /** Null when the conversation could not be resolved to an open-able id (a stale search hit). */
  url: string | null;
  excerpts: IConversationChipExcerpt[];
}

export interface ITaskChipData {
  key: string;
  title: string;
  state: string | null;
}

export interface IMrChipData {
  /** Absent on a chip saved before merge requests of other hosts were supported, when every one lived on gitlab.com. */
  host?: TGitlabHost;
  project: string;
  iid: number;
  title: string;
  state: TMrState;
}

export interface IFileChipData {
  /** Relative to the conversation's cwd; the agent resolves and reads it itself. */
  path: string;
}

export type TContextChip =
  | { id: string; kind: 'conversation'; label: string; data: IConversationChipData }
  | { id: string; kind: 'task'; label: string; data: ITaskChipData }
  | { id: string; kind: 'mr'; label: string; data: IMrChipData }
  | { id: string; kind: 'file'; label: string; data: IFileChipData };

const EXCERPT_MAX = 160;

const shorten = (text: string): string => {
  const flat = text.replace(/\s+/g, ' ').trim();
  return flat.length > EXCERPT_MAX ? `${flat.slice(0, EXCERPT_MAX).trimEnd()}…` : flat;
};

const EXCERPT_ROLE_KEY: Record<IConversationChipExcerpt['role'], string> = {
  user: 'lib.hlContextChips.roleUser',
  assistant: 'lib.hlContextChips.roleAgent',
};

const expandOne = (chip: TContextChip): string => {
  if (chip.kind === 'conversation') {
    const { title, url, excerpts } = chip.data;
    const head = url
      ? t('lib.hlContextChips.conversationWithUrl', { title, url })
      : t('lib.hlContextChips.conversation', { title });
    const lines = excerpts
      .slice(-3)
      .map((excerpt) => `  ${t(EXCERPT_ROLE_KEY[excerpt.role])}: ${shorten(excerpt.text)}`);
    return [head, ...lines].join('\n');
  }
  if (chip.kind === 'task') {
    const { key, title, state } = chip.data;
    return t('lib.hlContextChips.task', {
      key,
      title: `${title}${state ? ` (${state})` : ''}`,
      url: TRACKER_ISSUE_URL(key),
    });
  }
  if (chip.kind === 'mr') {
    const { host = GITLAB_COM_HOST, project, iid, title, state } = chip.data;
    const repo = project.split('/').at(-1) ?? project;
    return `MR ${repo}!${iid}: ${title} (${MR_STATE_LABEL[state] ?? state}) — ${GITLAB_MR_URL(host, project, iid)}`;
  }
  return t('lib.hlContextChips.file', { path: chip.data.path });
};

/** Compact, deterministic block appended to the prompt on send; empty for no chips. */
export const buildContextBlock = (chips: readonly TContextChip[]): string => {
  if (!chips.length) return '';
  const body = chips.map((chip, index) => `${index + 1}. ${expandOne(chip)}`).join('\n');
  return `\n\n---\n${t('lib.hlContextChips.contextHeading')}\n${body}`;
};

export const appendContextBlock = (text: string, chips: readonly TContextChip[]): string => {
  const block = buildContextBlock(chips);
  return block ? `${text}${block}` : text;
};
