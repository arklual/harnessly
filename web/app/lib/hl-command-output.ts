import type { ITimelineEntry, ITimelineSlashCommand } from '@/types/timeline';
import { effortLabel, isKnownEffort } from '@/lib/hl-launch-options';
import { modelSwitchOf } from '@/lib/hl-models';

export const COMMAND_OUTPUT_LIMIT = 20_000;
/** The name a `!` line (Claude Code's bash mode) takes among the timeline's commands. */
export const SHELL_COMMAND_NAME = '!';

export const isShellCommand = (entry: Pick<ITimelineSlashCommand, 'name'>): boolean =>
  entry.name === SHELL_COMMAND_NAME;

/** A single-line prompt that starts with `!` runs in the agent's shell instead of reaching the model. */
export const shellCommandOf = (text: string): string | null => {
  const trimmed = text.trim();
  if (!trimmed.startsWith(SHELL_COMMAND_NAME) || trimmed.includes('\n')) return null;
  const command = trimmed.slice(SHELL_COMMAND_NAME.length).trim();
  return command || null;
};
/** An output record attaches to a command only this close behind it; farther away it is shown on its own. */
export const COMMAND_OUTPUT_LOOKBACK = 5;

// CSI (colors, cursor), OSC (titles, links; BEL or ST terminated) and two-byte escapes.
const ANSI_RE = /\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]/g;

export const cleanCommandOutput = (text: string): string => {
  const plain = text.replace(ANSI_RE, '').replace(/\r\n?/g, '\n').replace(/^\n+/, '').trimEnd();
  return plain.length > COMMAND_OUTPUT_LIMIT ? `${plain.slice(0, COMMAND_OUTPUT_LIMIT - 1)}…` : plain;
};

/** Index of the command a trailing output belongs to: the newest command without output among the last few entries. */
export const findCommandForOutput = (entries: readonly ITimelineEntry[]): number => {
  const stop = Math.max(0, entries.length - COMMAND_OUTPUT_LOOKBACK);
  for (let i = entries.length - 1; i >= stop; i--) {
    const entry = entries[i];
    if (entry.type === 'slash-command' && entry.output === undefined) return i;
  }
  return -1;
};

export const withCommandOutput = (
  command: ITimelineSlashCommand,
  output: string,
  isError: boolean,
): ITimelineSlashCommand => ({ ...command, output, ...(isError ? { isError: true } : {}) });

// Claude Code 2.x: "Set model to `Opus 5 (1M context)` and saved as your default…", "Kept model as `Haiku 4.5`";
// older builds print the name without backticks.
const MODEL_SET_RE = /^Set model to\s+(?:`([^`]+)`|(.+?))(?:\s+and saved\b.*)?$/;
const MODEL_KEPT_RE = /^Kept model as\s+(?:`([^`]+)`|(.+))$/;

export interface ISlashModel {
  label: string;
  switched: boolean;
}

/** What a `/model` command did to the model, or null while it is running or when its output is unknown. */
export const slashCommandModel = (entry: ITimelineSlashCommand): ISlashModel | null => {
  if (entry.name !== 'model' || entry.isError) return null;
  if (entry.output === undefined) {
    if (entry.pending || !entry.args) return null;
    const label = modelSwitchOf(`/model ${entry.args}`);
    return label ? { label, switched: true } : null;
  }
  const firstLine = entry.output.split('\n')[0].trim();
  const set = firstLine.match(MODEL_SET_RE);
  if (set) return { label: (set[1] ?? set[2]).trim(), switched: true };
  const kept = firstLine.match(MODEL_KEPT_RE);
  if (kept) return { label: (kept[1] ?? kept[2]).trim(), switched: false };
  return null;
};

/** What a successful `/effort` set, for the quiet line in the timeline. */
export const slashCommandEffort = (entry: ITimelineSlashCommand): { label: string } | null => {
  if (entry.name !== 'effort' || entry.isError || !isKnownEffort(entry.args)) return null;
  return { label: effortLabel(entry.args) };
};

const EFFORT_SET_RE = /^(?:Set effort level|Ultracode on)\b/i;

/** `/model` output saying the model did not change: nothing to draw a switch line for. */
export const isKeptModelOutput = (output: string): boolean =>
  MODEL_KEPT_RE.test(output.split('\n')[0].trim());

/** Output of `/model` or `/effort` that lost its command: the quiet line in the timeline already says it. */
export const isSwitchOutput = (output: string): boolean => {
  const firstLine = output.split('\n')[0].trim();
  return MODEL_SET_RE.test(firstLine) || MODEL_KEPT_RE.test(firstLine) || EFFORT_SET_RE.test(firstLine);
};
