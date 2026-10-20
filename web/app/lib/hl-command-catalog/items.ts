import { CLAUDE_BUILTINS } from './builtins-claude';
import { CODEX_BUILTINS } from './builtins-codex';
import {
  COMMAND_SOURCE_LABELS,
  MAX_DESCRIPTION_LENGTH,
  type IBuiltinCommand,
  type ICommandItem,
  type TCommandSource,
  type TCommandTrigger,
} from './types';
import type { TAgentProvider } from '@/types/hl-launch';

/** Catalog items built from rows and file metadata. Pure: the composer uses the built-in tables too. */

const COMMAND_NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;

export const isCommandName = (name: string): boolean => COMMAND_NAME_RE.test(name);

export const clipDescription = (text: string | null | undefined): string => {
  const plain = (text ?? '').replace(/\s+/g, ' ').trim();
  return plain.length > MAX_DESCRIPTION_LENGTH ? `${plain.slice(0, MAX_DESCRIPTION_LENGTH - 1)}…` : plain;
};

export const builtinItem = (row: IBuiltinCommand, trigger: TCommandTrigger = '/'): ICommandItem => ({
  id: `builtin::${row.name}`,
  name: row.name,
  trigger,
  aliases: [...(row.aliases ?? [])],
  description: row.description,
  argumentHint: row.argumentHint ?? null,
  source: 'builtin',
  sourceLabel: COMMAND_SOURCE_LABELS.builtin,
  plugin: null,
  execution: row.execution,
  output: row.output ?? 'none',
  welcome: row.welcome ?? false,
  hidden: row.hidden ?? false,
  note: row.note ?? null,
  danger: row.danger ?? false,
});

interface IPromptItemInput {
  name: string;
  trigger: TCommandTrigger;
  source: TCommandSource;
  plugin: string | null;
  description: string;
  argumentHint: string | null;
}

/** A command, skill or quick prompt: typed as is, its answer is the model's reply, fine as a first message. */
export const promptItem = ({
  name,
  trigger,
  source,
  plugin,
  description,
  argumentHint,
}: IPromptItemInput): ICommandItem => ({
  id: `${source}:${plugin ?? ''}:${name}`,
  name,
  trigger,
  aliases: [],
  description: clipDescription(description),
  argumentHint,
  source,
  sourceLabel: plugin ? `${COMMAND_SOURCE_LABELS.plugin} · ${plugin}` : COMMAND_SOURCE_LABELS[source],
  plugin,
  execution: 'send',
  output: 'reply',
  welcome: true,
  hidden: false,
  note: null,
  danger: false,
});

/** The curated built-ins as items: what the composer knows before (or without) the machine's catalog. */
export const BUILTIN_COMMANDS = {
  claude: CLAUDE_BUILTINS.map((row) => builtinItem(row)),
  codex: CODEX_BUILTINS.map((row) => builtinItem(row)),
} as const satisfies Record<TAgentProvider, ICommandItem[]>;
