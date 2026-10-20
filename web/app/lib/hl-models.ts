import { CLAUDE_LAUNCH_MODELS } from '@/lib/hl-launch-options';

export interface IClaudeModel {
  alias: string;
  label: string;
  hint: string;
}

/** Aliases Claude Code takes in `--model` and `/model`; one list with the launch chip (fable, opus, sonnet, haiku). */
export const CLAUDE_MODELS: IClaudeModel[] = CLAUDE_LAUNCH_MODELS.map(({ id, label, hint }) => ({
  alias: id,
  label,
  hint,
}));

/** `claude-opus-5-5` → `opus`; null for non-Claude models. */
export const claudeFamilyOf = (model: string | null | undefined): string | null => {
  const match = model?.toLowerCase().match(/opus|sonnet|haiku|fable/);
  return match ? match[0] : null;
};

/** `claude-opus-5-5` → `Opus 5.5`, `claude-haiku-4-5-20251001` → `Haiku 4.5`; display names and other vendors pass through. */
export const prettyModelName = (model: string | null | undefined): string | null => {
  if (!model) return null;
  const match = model.match(/^claude-([a-z]+)((?:-\d+)*)(?:-\d{8})?$/i);
  if (!match) return model;
  const version = match[2]
    .split('-')
    .filter((part) => part && part.length < 8)
    .join('.');
  return `${match[1][0].toUpperCase()}${match[1].slice(1)}${version ? ` ${version}` : ''}`;
};

/** `/model sonnet` typed into the session → `Sonnet`; null for any other prompt. */
export const modelSwitchOf = (text: string): string | null => {
  const match = text.trim().match(/^\/model\s+([\w.-]+)$/);
  if (!match) return null;
  const known = CLAUDE_MODELS.find((item) => item.alias === match[1].toLowerCase());
  return known?.label ?? prettyModelName(match[1]);
};
