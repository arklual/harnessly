/**
 * Slash commands a composer can offer for one agent on one machine.
 * Isomorphic: the catalog route builds it, the composer ranks and executes it.
 */
import type { TAgentProvider } from '@/types/hl-launch';
import { t } from '@/lib/i18n';

export const COMMAND_SOURCES = ['quick', 'builtin', 'project', 'user', 'skill', 'plugin'] as const;
export type TCommandSource = (typeof COMMAND_SOURCES)[number];

export const COMMAND_SOURCE_LABELS = {
  quick: t('lib.commandCatalogTypes.sourceQuick'),
  builtin: t('lib.commandCatalogTypes.sourceBuiltin'),
  project: t('lib.commandCatalogTypes.sourceProject'),
  user: t('lib.commandCatalogTypes.sourceUser'),
  skill: t('lib.commandCatalogTypes.sourceSkill'),
  plugin: t('lib.commandCatalogTypes.sourcePlugin'),
} as const satisfies Record<TCommandSource, string>;

export const COMMAND_EXECUTIONS = [
  /** Type the line; the result lands in the transcript or the reply. */
  'send',
  /** The web model or effort chip; with an argument the line is run. */
  'model',
  'effort',
  /** The existing restart of the conversation (/clear, /new). */
  'restart',
  /** Show the whole list. */
  'help',
  /** Switch the list to Codex `$` skills. */
  'skills',
  /** The CLI opens a menu or a panel: mirrored in the web. */
  'menu',
  /** Confirmed in the web first, then run. */
  'confirm',
  /** Only usable in the terminal: hidden by default, running it opens the terminal. */
  'terminal',
  /** Refused from the web. */
  'blocked',
] as const;
export type TCommandExecution = (typeof COMMAND_EXECUTIONS)[number];

/** Where the result shows up: the transcript (Claude local commands), the model's reply, the terminal screen. */
export type TCommandOutput = 'transcript' | 'reply' | 'screen' | 'none';

export type TCommandTrigger = '/' | '$';

export interface ICommandItem {
  /** `${source}:${plugin ?? ''}:${name}`, unique within a catalog. */
  id: string;
  /** Without the trigger: 'compact', 'commit-commands:commit'. */
  name: string;
  trigger: TCommandTrigger;
  aliases: string[];
  /** Up to 240 characters; in the page's language for built-ins. */
  description: string;
  argumentHint: string | null;
  source: TCommandSource;
  /** 'built-in' | 'mine' | 'project' | 'skill' | 'plugin · tracker' | 'quick', in the page's language. */
  sourceLabel: string;
  plugin: string | null;
  execution: TCommandExecution;
  output: TCommandOutput;
  /** Allowed as the first message of a new conversation. */
  welcome: boolean;
  /** Listed only under «All commands». */
  hidden: boolean;
  /** Why a command is blocked, or the question a confirm command asks. */
  note: string | null;
  /** A confirm command that destroys something: red button, «for good». */
  danger: boolean;
  usage?: { count: number; lastAt: number };
}

export type TCommandCounts = Record<TCommandSource, number>;

export interface ICommandCatalog {
  provider: TAgentProvider;
  cwd: string;
  generatedAt: number;
  items: ICommandItem[];
  counts: TCommandCounts;
  /** Sources that could not be read, said out loud instead of silently missing. */
  warnings: string[];
  /** True when the item limit cut the list. */
  truncated: boolean;
}

/** One row of a curated built-in table. */
export interface IBuiltinCommand {
  name: string;
  aliases?: string[];
  description: string;
  argumentHint?: string;
  execution: TCommandExecution;
  output?: TCommandOutput;
  welcome?: boolean;
  hidden?: boolean;
  note?: string;
  danger?: boolean;
}

export const MAX_CATALOG_ITEMS = 2000;
export const MAX_DESCRIPTION_LENGTH = 240;
