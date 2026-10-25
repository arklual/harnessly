// The rows of the agents' pages: which options of a CLI's settings file the Settings screen offers, in which groups, as
// what (a switch, one of a few side by side, one of a list) and with which values. The file is the machine's and the
// CLI's own: the list of options is the runtime's (`options`, each set or null); what this file adds is how each is
// drawn and what the CLI does while the file says nothing.
import { plural, t } from '@/next/i18n';
import type {
  AgentModel,
  AgentOption,
  AgentOptionKey,
  AgentOptionValue,
  AgentSettings,
  Provider,
} from '@/next/protocol/v2';

export const ROW_KIND = { switch: 'switch', seg: 'seg', menu: 'menu' } as const;
export type TRowKind = (typeof ROW_KIND)[keyof typeof ROW_KIND];

/** Where a menu's choices come from: the CLI's models on the machine, or a fixed list. */
export const CHOICES = { models: 'models', list: 'list' } as const;

export interface IOptionRow {
  key: AgentOptionKey;
  kind: TRowKind;
  /** Menu only: the models of the machine or the fixed values. */
  choices?: (typeof CHOICES)[keyof typeof CHOICES];
  /** The values a segment or a fixed menu offers, in order. */
  values?: readonly (string | number)[];
  /** What the CLI does while the file sets nothing, where that is one fixed answer; the row shows it. */
  fallback?: AgentOptionValue;
  /** A menu whose «not set» is a choice of its own, with this word. */
  unset?: 'model' | 'fallback';
}

export interface IOptionGroup {
  id: string;
  rows: readonly IOptionRow[];
}

const EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'] as const;
const CODEX_EFFORTS = ['low', 'medium', 'high', 'xhigh'] as const;

/** The groups of the Claude Code page, as the design lists them. `baseUrl` is on «Models and keys». */
const CLAUDE_GROUPS: readonly IOptionGroup[] = [
  {
    id: 'model',
    rows: [
      { key: 'model', kind: ROW_KIND.menu, choices: CHOICES.models, unset: 'model' },
      { key: 'fallbackModel', kind: ROW_KIND.menu, choices: CHOICES.models, unset: 'fallback' },
      { key: 'effort', kind: ROW_KIND.seg, values: EFFORTS },
      { key: 'ultracode', kind: ROW_KIND.switch, fallback: false },
      { key: 'thinking', kind: ROW_KIND.switch, fallback: false },
      {
        key: 'outputStyle',
        kind: ROW_KIND.menu,
        choices: CHOICES.list,
        values: ['default', 'Explanatory', 'Learning'],
        fallback: 'default',
      },
    ],
  },
  {
    id: 'permissions',
    rows: [
      {
        key: 'permissionMode',
        kind: ROW_KIND.menu,
        choices: CHOICES.list,
        values: ['default', 'acceptEdits', 'plan'],
        fallback: 'default',
      },
      { key: 'sandbox', kind: ROW_KIND.switch, fallback: false },
      { key: 'projectMcp', kind: ROW_KIND.switch, fallback: false },
    ],
  },
  {
    id: 'context',
    rows: [
      { key: 'compactWindow', kind: ROW_KIND.seg, values: [200000, 400000, 1000000] },
      { key: 'memory', kind: ROW_KIND.switch, fallback: true },
      { key: 'keepDays', kind: ROW_KIND.seg, values: [30, 90, 365], fallback: 30 },
      { key: 'hooks', kind: ROW_KIND.switch, fallback: true },
    ],
  },
  {
    id: 'git',
    rows: [
      { key: 'coauthor', kind: ROW_KIND.switch, fallback: true },
      { key: 'updates', kind: ROW_KIND.seg, values: ['stable', 'latest'], fallback: 'latest' },
    ],
  },
];

const CODEX_GROUPS: readonly IOptionGroup[] = [
  {
    id: 'model',
    rows: [
      { key: 'model', kind: ROW_KIND.menu, choices: CHOICES.models, unset: 'model' },
      { key: 'effort', kind: ROW_KIND.seg, values: CODEX_EFFORTS },
      {
        key: 'summary',
        kind: ROW_KIND.seg,
        values: ['auto', 'concise', 'detailed', 'none'],
        fallback: 'auto',
      },
      { key: 'fast', kind: ROW_KIND.switch, fallback: false },
    ],
  },
  {
    id: 'sandbox',
    rows: [
      {
        key: 'sandboxMode',
        kind: ROW_KIND.menu,
        choices: CHOICES.list,
        values: ['read-only', 'workspace-write', 'danger-full-access'],
      },
      { key: 'network', kind: ROW_KIND.switch, fallback: false },
      { key: 'approval', kind: ROW_KIND.seg, values: ['untrusted', 'on-failure', 'on-request', 'never'] },
    ],
  },
  {
    id: 'tools',
    rows: [
      { key: 'search', kind: ROW_KIND.seg, values: ['disabled', 'cached', 'live'] },
      { key: 'history', kind: ROW_KIND.switch, fallback: true },
    ],
  },
];

export const groupsOf = (provider: Provider): readonly IOptionGroup[] =>
  provider === 'claude' ? CLAUDE_GROUPS : CODEX_GROUPS;

/** The value of an option in the settings read from the file; undefined while the CLI has no such option. */
export const optionValue = (settings: AgentSettings, key: AgentOptionKey): AgentOptionValue | undefined =>
  settings.options.find((option: AgentOption) => option.key === key)?.value;

/** What the row shows: the file's value, else what the CLI does without one (null: nothing is chosen). */
export const shownValue = (row: IOptionRow, settings: AgentSettings): AgentOptionValue =>
  optionValue(settings, row.key) ?? row.fallback ?? null;

export interface IChoice {
  value: string | number | null;
  label: string;
  /** A short line under the label in a menu. */
  sub: string | null;
}

/** A text of the dictionary when it has one for the key. */
const maybe = (key: string): string | null => {
  const text = t(key);
  return text === key ? null : text;
};

/** «200K», «1M»: a context window in tokens. */
const windowLabel = (tokens: number): string =>
  tokens >= 1_000_000 && tokens % 1_000_000 === 0
    ? `${tokens / 1_000_000}M`
    : `${Math.round(tokens / 1000)}K`;

const DAYS_IN_YEAR = 365;

/** The word of a value: the CLI's own if the dictionary has one (Codex's top effort is «Max», Claude's is above it), else the shared one. */
const choiceText = (
  provider: Provider,
  key: AgentOptionKey,
  value: string | number,
  suffix = '',
): string | null =>
  maybe(`next.settings.choice.${provider}.${key}.${value}${suffix}`) ??
  maybe(`next.settings.choice.${key}.${value}${suffix}`);

const choiceLabel = (provider: Provider, key: AgentOptionKey, value: string | number): string => {
  if (key === 'compactWindow' && typeof value === 'number') return windowLabel(value);
  if (key === 'keepDays' && typeof value === 'number')
    return value === DAYS_IN_YEAR
      ? t('next.settings.choice.keepDays.year')
      : plural(value, 'next.settings.choice.keepDays.days');
  return choiceText(provider, key, value) ?? String(value);
};

const choiceSub = (provider: Provider, key: AgentOptionKey, value: string | number): string | null =>
  choiceText(provider, key, value, '.sub');

const modelChoices = (models: readonly AgentModel[]): IChoice[] =>
  models.map((model) => ({ value: model.id, label: model.label, sub: model.hint || null }));

/**
 * The choices of a row: the list the row names (a CLI's models come from the machine), «not set» where it is a choice,
 * and the value the file holds if the list does not have it, so the row never hides what is written.
 */
export const choicesOf = (
  provider: Provider,
  row: IOptionRow,
  models: readonly AgentModel[],
  current: AgentOptionValue,
): IChoice[] => {
  const list: IChoice[] =
    row.choices === CHOICES.models
      ? modelChoices(models)
      : (row.values ?? []).map((value) => ({
          value,
          label: choiceLabel(provider, row.key, value),
          sub: choiceSub(provider, row.key, value),
        }));
  if (row.unset) list.unshift({ value: null, label: t(`next.settings.choice.${row.key}.unset`), sub: null });
  const known = list.some((choice) => choice.value === current);
  if (!known && current !== null && typeof current !== 'boolean')
    list.push({
      value: current,
      label: choiceLabel(provider, row.key, current),
      sub: choiceSub(provider, row.key, current),
    });
  return list;
};
