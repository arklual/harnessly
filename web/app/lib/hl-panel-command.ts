import { parsePortInput } from '@/lib/hl-preview-client';
import { stripControlChars } from '@/lib/terminal-safe-text';
import { isPaneDocTarget, paneDocTargetOf } from '@/lib/hl-doc-path';
import { gitlabRefOf } from '@/lib/hl-gitlab-ref';
import { isTaskKey } from '@/lib/hl-task';
import { standUrlOf } from '@/lib/hl-stand-url';
import { t } from '@/lib/i18n';

/**
 * What an agent may ask of the right pane of its own tab, and what the interface answers. The pane is a set of
 * tabs (kinds below); no browser or node imports.
 */

export const PANEL_COMMAND_EVENT = 'hl:panel-command' as const;

export const PANEL_MAX_ARG = 512;
export const PANEL_MAX_MESSAGE = 400;

/** The kinds of tab the right pane can hold. */
export const PANE_TAB_KINDS = [
  'overview',
  'tools',
  'browser',
  'terminal',
  'files',
  'disk',
  'changes',
  'artifacts',
  'gitlab',
  'subagents',
  'economics',
  'plan',
  'peers',
  'design',
  'side-chat',
  'gallery',
  'task',
] as const;
export type TPaneTabKind = (typeof PANE_TAB_KINDS)[number];

export const isPaneTabKind = (value: unknown): value is TPaneTabKind =>
  typeof value === 'string' && (PANE_TAB_KINDS as readonly string[]).includes(value);

/**
 * What a tab may be told to show: a port for the browser, a path for files and changes, a document or a board of
 * a document or board link for the artifacts, a merge request or a commit of a configured GitLab for GitLab, the key of a task for the task,
 * nothing for the others.
 */
type TTargetKind = 'port' | 'path' | 'doc' | 'gitlab' | 'task';
const TARGET_KIND: Partial<Record<TPaneTabKind, TTargetKind>> = {
  browser: 'port',
  files: 'path',
  changes: 'path',
  artifacts: 'doc',
  gitlab: 'gitlab',
  task: 'task',
};

export type TPanelCommand =
  | { kind: 'open'; tab: TPaneTabKind; target?: string }
  | { kind: 'show' }
  | { kind: 'hide' }
  | { kind: 'status' };

export type TPanelCommandParse = { ok: true; command: TPanelCommand } | { ok: false; error: string };

/** What the sync socket carries to every page of the runtime; only the page that shows `tabId` acts on it. */
export interface IPanelCommandEvent {
  type: 'panel-command';
  id: string;
  tabId: string;
  command: TPanelCommand;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

const fail = (error: string): TPanelCommandParse => ({ ok: false, error });

const textArg = (value: unknown): string | null =>
  typeof value === 'string' && value.length <= PANEL_MAX_ARG ? value : null;

/**
 * A port of this machine as one canonical text, `localhost:4400/path`, or a feature stand; any other
 * external address is refused.
 */
const portTarget = (text: string): string | null => {
  if (stripControlChars(text) !== text) return null;
  const stand = standUrlOf(text);
  if (stand) return stand;
  try {
    const parsed = parsePortInput(text);
    return parsed ? `localhost:${parsed.port}${parsed.path === '/' ? '' : parsed.path}` : null;
  } catch {
    return null;
  }
};

const pathTarget = (text: string): string | null => {
  const clean = text.trim();
  if (!clean || stripControlChars(clean) !== clean) return null;
  return clean.split('/').includes('..') ? null : clean;
};

const urlTarget = (text: string): string | null => {
  const clean = text.trim();
  return clean && stripControlChars(clean) === clean ? clean : null;
};

/**
 * The reader's own path for a document or a board; any other address is refused, so nothing foreign gets a frame.
 * The page parses the command the runtime has already parsed, so the reader's path is taken as it is.
 */
const docTarget = (text: string): string | null => {
  const url = urlTarget(text);
  if (!url) return null;
  return isPaneDocTarget(url) ? url : paneDocTargetOf(url);
};

const gitlabTarget = (text: string): string | null => {
  const url = urlTarget(text);
  return url && gitlabRefOf(url) ? url : null;
};

const taskTarget = (text: string): string | null => {
  const key = text.trim();
  return isTaskKey(key) ? key : null;
};

const TARGET_PARSER: Record<TTargetKind, (text: string) => string | null> = {
  port: portTarget,
  path: pathTarget,
  doc: docTarget,
  gitlab: gitlabTarget,
  task: taskTarget,
};

const targetHint = (tab: TPaneTabKind, wants: TTargetKind): string => {
  switch (wants) {
    case 'port':
      return t('lib.hlPanelCommand.hintPort');
    case 'doc':
      return t('lib.hlPanelCommand.hintDoc');
    case 'gitlab':
      return t('lib.hlPanelCommand.hintGitlab');
    case 'task':
      return t('lib.hlPanelCommand.hintTask');
    default:
      return t('lib.hlPanelCommand.hintPath', { tab });
  }
};

const parseOpen = (tab: unknown, target: unknown): TPanelCommandParse => {
  if (!isPaneTabKind(tab))
    return fail(t('lib.hlPanelCommand.unknownTab', { tabs: PANE_TAB_KINDS.join(', ') }));
  if (target === undefined || target === null || (typeof target === 'string' && target.trim() === ''))
    return { ok: true, command: { kind: 'open', tab } };
  const wants = TARGET_KIND[tab];
  if (!wants) return fail(t('lib.hlPanelCommand.noTarget', { tab }));
  const text = textArg(target);
  if (text === null) return fail(t('lib.hlPanelCommand.badTarget', { tab }));
  const clean = TARGET_PARSER[wants](text);
  if (clean === null) return fail(targetHint(tab, wants));
  return { ok: true, command: { kind: 'open', tab, target: clean } };
};

/** The body of a request, reduced to a command the pane knows. Unknown keys, kinds and values are refused, never passed on. */
export const parsePanelCommand = (input: unknown): TPanelCommandParse => {
  if (!isRecord(input)) return fail(t('lib.hlPanelCommand.needObject'));
  switch (input.kind) {
    case 'open':
      return parseOpen(input.tab, input.target);
    case 'show':
    case 'hide':
    case 'status':
      return { ok: true, command: { kind: input.kind } };
    default:
      return fail(t('lib.hlPanelCommand.unknownCommand'));
  }
};

/** What the pane shows, as its tab registry reports it, so the agent can check what its command did. */
export interface IPanelState {
  visible: boolean | null;
  open: TPaneTabKind[];
  active: TPaneTabKind | null;
}

export interface IPanelResult {
  ok: boolean;
  message: string;
  state?: IPanelState;
}

const cleanMessage = (value: string): string =>
  stripControlChars(value).replace(/\s+/g, ' ').trim().slice(0, PANEL_MAX_MESSAGE);

export const cleanPanelState = (value: unknown): IPanelState | undefined => {
  if (!isRecord(value)) return undefined;
  return {
    visible: typeof value.visible === 'boolean' ? value.visible : null,
    open: Array.isArray(value.open) ? value.open.filter(isPaneTabKind) : [],
    active: isPaneTabKind(value.active) ? value.active : null,
  };
};

/** The answer of a browser, which is not trusted: shape-checked, cleaned, clipped. */
export const parsePanelResult = (input: unknown): IPanelResult | null => {
  if (!isRecord(input) || typeof input.ok !== 'boolean' || typeof input.message !== 'string') return null;
  const state = cleanPanelState(input.state);
  return { ok: input.ok, message: cleanMessage(input.message), ...(state ? { state } : {}) };
};

export interface IRateLimit {
  limit: number;
  windowMs: number;
}

/** Sliding window over the times of the commands let through; a refused one is not counted. */
export const panelRateDecision = (
  stamps: readonly number[],
  now: number,
  { limit, windowMs }: IRateLimit,
): { allowed: boolean; stamps: number[] } => {
  const recent = stamps.filter((stamp) => now - stamp < windowMs);
  if (recent.length >= limit) return { allowed: false, stamps: recent };
  return { allowed: true, stamps: [...recent, now] };
};

/** The tab of the caller is the one whose own tmux session it speaks from, never one named in an argument. */
export const resolveAgentTab = (
  sources: readonly { tabId: string; tmuxSession: string }[],
  session: string,
): string | null => {
  if (!session) return null;
  return sources.find((source) => source.tmuxSession === session)?.tabId ?? null;
};
