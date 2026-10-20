import type { ICommandItem, TCommandSource, TCommandTrigger } from '@/lib/hl-command-catalog/types';
import type { TAgentProvider } from '@/types/hl-launch';
import { t } from '@/lib/i18n';

/**
 * Which command the composer text is asking for, and the ranked list for it.
 * Pure: shared by the conversation composer and the welcome composer.
 */

export const MAX_LISTED_COMMANDS = 50;

/** Asks the composer of `sessionName` to run a slash line through its usual path (the Codex model chip → `/model`). */
export const RUN_SLASH_COMMAND_EVENT = 'hl:run-slash-command';
export interface IRunSlashCommandDetail {
  sessionName: string;
  line: string;
}

export type TCommandQuery =
  /** The text starts with `/` and the caret is still in the first word: the list is open. */
  | { kind: 'list'; trigger: TCommandTrigger; query: string; start: number }
  /** A `/name args…` line: the list is closed, the argument hint shows. */
  | { kind: 'args'; trigger: '/'; name: string; args: string };

const SLASH_RE = /^\/([^\s]*)$/;
const SLASH_ARGS_RE = /^\/([^\s]+)\s([\s\S]*)$/;
const SKILL_WORD_RE = /(^|\s)\$([^\s$]*)$/;

/**
 * `/co` → list for `co`; `/compact now` → args of `compact`; at Codex, `fix $tick` → `$` skills for `tick`.
 * The caret is taken to be at the end of the text (where typing happens in the composer).
 */
export const parseCommandQuery = (text: string, provider: TAgentProvider | null): TCommandQuery | null => {
  const slash = text.match(SLASH_RE);
  if (slash) return { kind: 'list', trigger: '/', query: slash[1], start: 0 };
  const args = text.match(SLASH_ARGS_RE);
  if (args) return { kind: 'args', trigger: '/', name: args[1], args: args[2] };
  if (provider === 'codex') {
    const skill = text.match(SKILL_WORD_RE);
    if (skill && skill.index !== undefined)
      return { kind: 'list', trigger: '$', query: skill[2], start: skill.index + skill[1].length };
  }
  return null;
};

/** The composer text after picking `item` for a list query: `/name ` or `…$name `. */
export const completeCommand = (
  text: string,
  query: Extract<TCommandQuery, { kind: 'list' }>,
  item: ICommandItem,
): string => `${text.slice(0, query.start)}${item.trigger}${item.name} `;

/** Picking with Enter runs it right away, unless it needs an argument first (then Enter only completes it). */
export const needsArgument = (item: ICommandItem): boolean =>
  item.argumentHint !== null && /^</.test(item.argumentHint);

const SOURCE_ORDER = {
  quick: 0,
  builtin: 1,
  project: 2,
  user: 3,
  skill: 4,
  plugin: 5,
} as const satisfies Record<TCommandSource, number>;

const words = (text: string) =>
  text
    .toLowerCase()
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean);

/** 0 exact name, 1 prefix (also after `plugin:`), 2 substring, 3 every query word in the description; null no match. */
export const matchTier = (item: ICommandItem, query: string): number | null => {
  const q = query.toLowerCase();
  if (!q) return 3;
  const names = [item.name, ...item.aliases].map((name) => name.toLowerCase());
  if (names.includes(q)) return 0;
  if (names.some((name) => name.startsWith(q) || name.split(':').some((part) => part.startsWith(q))))
    return 1;
  if (names.some((name) => name.includes(q))) return 2;
  const queryWords = words(q);
  const described = words(`${item.description} ${item.plugin ?? ''}`);
  if (
    queryWords.length &&
    queryWords.every((word) => described.some((candidate) => candidate.startsWith(word)))
  )
    return 3;
  return null;
};

export interface IRankOptions {
  trigger: TCommandTrigger;
  /** «All commands»: terminal-only commands are listed too. An exact name always is. */
  showHidden?: boolean;
  /** The welcome composer lists only what may start a conversation (and the model and effort chips). */
  welcome?: boolean;
  limit?: number;
}

export interface IRankedCommands {
  items: ICommandItem[];
  /** Matches beyond the limit: «N more: refine the query». */
  more: number;
  /** Hidden terminal-only matches, for the «All commands» switch. */
  hiddenMatches: number;
}

const welcomeUsable = (item: ICommandItem) =>
  item.welcome || item.execution === 'model' || item.execution === 'effort';

/** Tier, then usage, then built-ins, then source order and name. At most `limit` rows. */
export const rankCommands = (
  catalog: readonly ICommandItem[],
  query: string,
  options: IRankOptions,
): IRankedCommands => {
  const limit = options.limit ?? MAX_LISTED_COMMANDS;
  const q = query.toLowerCase();
  let hiddenMatches = 0;
  const scored: { item: ICommandItem; tier: number }[] = [];
  for (const item of catalog) {
    if (item.trigger !== options.trigger) continue;
    if (options.welcome && !welcomeUsable(item)) continue;
    const tier = matchTier(item, q);
    if (tier === null) continue;
    if (item.hidden && !options.showHidden && tier !== 0) {
      hiddenMatches += 1;
      continue;
    }
    scored.push({ item, tier });
  }
  scored.sort(
    (a, b) =>
      a.tier - b.tier ||
      (b.item.usage?.count ?? 0) - (a.item.usage?.count ?? 0) ||
      SOURCE_ORDER[a.item.source] - SOURCE_ORDER[b.item.source] ||
      a.item.name.localeCompare(b.item.name),
  );
  return {
    items: scored.slice(0, limit).map(({ item }) => item),
    more: Math.max(0, scored.length - limit),
    hiddenMatches,
  };
};

/** Execution badge shown next to the source; null for a plain command. */
export const executionBadge = (item: ICommandItem): string | null => {
  if (item.execution === 'model' || item.execution === 'effort')
    return t('lib.hlCommandFilter.badgeWebPicker');
  if (item.execution === 'menu') return t('lib.hlCommandFilter.badgeWebMenu');
  if (item.execution === 'terminal') return t('lib.hlCommandFilter.badgeTerminal');
  if (item.execution === 'restart') return t('lib.hlCommandFilter.badgeRestart');
  if (item.execution === 'confirm') return t('lib.hlCommandFilter.badgeConfirm');
  if (item.execution === 'blocked') return t('lib.hlCommandFilter.badgeBlocked');
  return null;
};
