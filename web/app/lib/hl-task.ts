import {
  GITHUB_KEY_RE,
  githubKeyOf,
  githubRefOf,
  isTrackerHost,
  taskKeyRe as siteTaskKeyRe,
} from '@/lib/site';
import { stripControlChars } from '@/lib/terminal-safe-text';
import { t } from '@/lib/i18n';

/** A task of the tracker as a tab of the right panel: the key it is named by and what the tab shows of it. */

/** A task key of the configured tracker (RUNTIME_TASK_KEY_RE). */
export const taskKeyRe = (): RegExp => siteTaskKeyRe();
export const TASK_DESCRIPTION_MAX = 20 * 1024;
export const TASK_COMMENT_MAX = 4 * 1024;
export const TASK_COMMENTS_LIMIT = 10;
export const TASK_CHILDREN_LIMIT = 50;
const TASK_TEXT_MAX = 300;

export const isTaskKey = (value: unknown): value is string =>
  typeof value === 'string' && (siteTaskKeyRe().test(value) || GITHUB_KEY_RE.test(value));

const ISSUE_PATH = /^\/(?:youtrack\/)?issue\/([A-Z][A-Z0-9]*-\d+)(?:\/|$)/;

/** The key of a link to a task of the tracker; a task of another space stays a link to the tracker. */
export const taskKeyOf = (href: string): string | null => {
  const github = githubRefOf(href);
  if (github && !github.pull) return githubKeyOf(github);
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.port || !isTrackerHost(url.hostname)) return null;
  const key = url.pathname.match(ISSUE_PATH)?.[1];
  return isTaskKey(key) ? key : null;
};

/** Untrusted text bound for the page: controls out (ESC stays visible), line ends unified, clipped to `max` with an ellipsis. */
export const cleanTaskText = (value: unknown, max: number): string => {
  if (typeof value !== 'string') return '';
  const text = stripControlChars(value.replace(/\r\n?/g, '\n'));
  return text.length > max ? `${text.slice(0, max - 1)}…` : text;
};

export interface ITaskPerson {
  name: string;
  avatar: string | null;
}

export interface ITaskChild {
  key: string;
  title: string;
  state: string | null;
  resolved: boolean;
}

export interface ITaskComment {
  author: string;
  at: number | null;
  text: string;
}

export interface ITask {
  key: string;
  title: string;
  project: string | null;
  type: string | null;
  state: string | null;
  priority: string | null;
  resolved: boolean;
  assignee: ITaskPerson | null;
  parent: { key: string; title: string } | null;
  children: ITaskChild[];
  description: string;
  comments: ITaskComment[];
  updated: number | null;
}

export type TTaskError = 'not-found' | 'no-token' | 'tracker';

/** `fields` of the one request that brings a whole task. */
export const TASK_FIELDS = [
  'idReadable',
  'summary',
  'description',
  'resolved',
  'updated',
  'project(shortName)',
  'parent(issues(idReadable,summary))',
  'subtasks(issues(idReadable,summary,resolved,customFields(name,value(name))))',
  'customFields(name,value(name,login,fullName,avatarUrl))',
  'comments(author(login,fullName),created,text)',
].join(',');

type TRecord = Record<string, unknown>;

const isRecord = (value: unknown): value is TRecord =>
  typeof value === 'object' && value !== null && !Array.isArray(value);
const records = (value: unknown): TRecord[] => (Array.isArray(value) ? value.filter(isRecord) : []);
const short = (value: unknown): string | null =>
  cleanTaskText(value, TASK_TEXT_MAX).replace(/\s+/g, ' ').trim() || null;
const timestamp = (value: unknown): number | null =>
  typeof value === 'number' && Number.isFinite(value) ? value : null;

const fieldValue = (fields: unknown, name: string): TRecord | null => {
  const value = records(fields).find((field) => field.name === name)?.value;
  return isRecord(value) ? value : null;
};

const firstIssue = (link: unknown): TRecord | null => records(isRecord(link) ? link.issues : null)[0] ?? null;

/** What the tracker answers for one issue, reduced to what the tab shows; null when it is not an issue of a known space. */
export const parseTaskIssue = (
  raw: unknown,
  avatarOf: (login: string, source: string | null) => string | null,
): ITask | null => {
  if (!isRecord(raw) || !isTaskKey(raw.idReadable)) return null;
  const title = short(raw.summary);
  if (!title) return null;
  const nameOf = (field: string) => short(fieldValue(raw.customFields, field)?.name);
  const person = fieldValue(raw.customFields, 'Assignee');
  const login = person && typeof person.login === 'string' ? person.login : null;
  const assigneeName = person ? (short(person.fullName) ?? short(person.name) ?? login) : null;
  const parent = firstIssue(raw.parent);
  const parentKey = parent?.idReadable;
  return {
    key: raw.idReadable,
    title,
    project: isRecord(raw.project) ? short(raw.project.shortName) : null,
    type: nameOf('Type'),
    state: nameOf('State'),
    priority: nameOf('Priority'),
    resolved: raw.resolved != null,
    assignee: assigneeName
      ? {
          name: assigneeName,
          avatar: login
            ? avatarOf(login, typeof person?.avatarUrl === 'string' ? person.avatarUrl : null)
            : null,
        }
      : null,
    parent:
      parent && isTaskKey(parentKey) ? { key: parentKey, title: short(parent.summary) ?? parentKey } : null,
    children: records(isRecord(raw.subtasks) ? raw.subtasks.issues : null)
      .filter((child) => isTaskKey(child.idReadable))
      .slice(0, TASK_CHILDREN_LIMIT)
      .map((child) => ({
        key: child.idReadable as string,
        title: short(child.summary) ?? (child.idReadable as string),
        state: short(fieldValue(child.customFields, 'State')?.name),
        resolved: child.resolved != null,
      })),
    description: cleanTaskText(raw.description, TASK_DESCRIPTION_MAX),
    comments: records(raw.comments)
      .slice(-TASK_COMMENTS_LIMIT)
      .map((comment) => ({
        author:
          (isRecord(comment.author)
            ? (short(comment.author.fullName) ?? short(comment.author.login))
            : null) ?? t('lib.hlTask.noName'),
        at: timestamp(comment.created),
        text: cleanTaskText(comment.text, TASK_COMMENT_MAX),
      })),
    updated: timestamp(raw.updated),
  };
};
