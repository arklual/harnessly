import { t } from '@/lib/i18n';
import { CLAUDE_EFFORTS, MODEL_ID_RE, type ILaunchChoice, type TAgentProvider } from '@/types/hl-launch';

/**
 * Model and reasoning effort of an agent launch: catalog shapes, labels and the
 * CLI flags built from a choice. Isomorphic: the welcome composer, the context bar and the server
 * launch builders share it, so a choice is typed into a shell only through the formatters below.
 */

/** Every reasoning level the CLIs know, weakest first (Codex models support a subset each). */
export const EFFORT_ORDER = [
  'none',
  'minimal',
  'low',
  'medium',
  'high',
  'xhigh',
  'max',
  'ultra',
  'ultracode',
] as const;
export type TEffort = (typeof EFFORT_ORDER)[number];

export const isKnownEffort = (value: unknown): value is TEffort =>
  typeof value === 'string' && (EFFORT_ORDER as readonly string[]).includes(value);

export const EFFORT_LABELS = {
  none: t('lib.hlLaunchOptions.effortNone'),
  minimal: 'Minimal',
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  xhigh: 'Extra high',
  max: 'Max',
  ultra: 'Ultra',
  ultracode: 'Ultracode',
} as const satisfies Record<TEffort, string>;

export const EFFORT_DESCRIPTIONS = {
  none: t('lib.hlLaunchOptions.effortNoneDescription'),
  minimal: t('lib.hlLaunchOptions.effortMinimalDescription'),
  low: t('lib.hlLaunchOptions.effortLowDescription'),
  medium: t('lib.hlLaunchOptions.effortMediumDescription'),
  high: t('lib.hlLaunchOptions.effortHighDescription'),
  xhigh: t('lib.hlLaunchOptions.effortXhighDescription'),
  max: t('lib.hlLaunchOptions.effortMaxDescription'),
  ultra: t('lib.hlLaunchOptions.effortUltraDescription'),
  ultracode: t('lib.hlLaunchOptions.effortUltracodeDescription'),
} as const satisfies Record<TEffort, string>;

/** Filled bars (of four) in the effort icon. */
export const EFFORT_BARS = {
  none: 0,
  minimal: 1,
  low: 1,
  medium: 2,
  high: 3,
  xhigh: 4,
  max: 4,
  ultra: 4,
  ultracode: 4,
} as const satisfies Record<TEffort, number>;

export const effortLabel = (effort: string): string =>
  isKnownEffort(effort) ? EFFORT_LABELS[effort] : effort;

export interface ILaunchModel {
  id: string;
  label: string;
  hint: string | null;
  /** Levels the model accepts; empty means the effort flag is not offered. */
  efforts: string[];
  defaultEffort: string | null;
}

export interface ILaunchOptions {
  provider: TAgentProvider;
  source: 'static' | 'models-cache';
  /** `fetched_at` of ~/.codex/models_cache.json; null for Claude. */
  fetchedAt: string | null;
  models: ILaunchModel[];
  /** What the CLI uses when no flag is passed: ~/.claude/settings.json or ~/.codex/config.toml. */
  cliDefault: ILaunchChoice;
  /** The machine's remembered choice for new conversations (the runtime config's launch defaults key). */
  saved: ILaunchChoice | null;
  warnings: string[];
}

export type TLaunchDefaults = Partial<Record<TAgentProvider, ILaunchChoice>>;

/** Aliases from `claude --help` and the 2.1.282 binary; Haiku takes `--effort` silently and ignores it. */
export const CLAUDE_LAUNCH_MODELS = [
  {
    id: 'fable',
    label: 'Fable',
    hint: t('lib.hlLaunchOptions.aliasHint', { model: 'Fable' }),
    efforts: [...CLAUDE_EFFORTS],
    defaultEffort: null,
  },
  {
    id: 'opus',
    label: 'Opus',
    hint: t('lib.hlLaunchOptions.aliasHint', { model: 'Opus' }),
    efforts: [...CLAUDE_EFFORTS],
    defaultEffort: null,
  },
  {
    id: 'sonnet',
    label: 'Sonnet',
    hint: t('lib.hlLaunchOptions.aliasHint', { model: 'Sonnet' }),
    efforts: [...CLAUDE_EFFORTS],
    defaultEffort: null,
  },
  {
    id: 'haiku',
    label: 'Haiku',
    hint: t('lib.hlLaunchOptions.noEffortHint'),
    efforts: [],
    defaultEffort: null,
  },
] as const satisfies readonly ILaunchModel[];

export const PROVIDER_TITLES = { claude: 'Claude', codex: 'Codex' } as const satisfies Record<
  TAgentProvider,
  string
>;

/** Opens the model and effort chip: the welcome composer (no sessionName) or a session's context bar. */
export const OPEN_LAUNCH_PICKER_EVENT = 'hl:open-launch-picker';
export interface IOpenLaunchPickerDetail {
  sessionName?: string | null;
  section?: 'model' | 'effort';
}

export const findLaunchModel = (
  options: Pick<ILaunchOptions, 'models'>,
  id: string | null,
): ILaunchModel | null => (id ? options.models.find((model) => model.id === id) : undefined) ?? null;

/**
 * Levels offered for a model choice. The "Auto" model means the CLI default model: its levels when the
 * catalog knows it, Claude's own list otherwise (every Claude alias but Haiku takes all of them).
 */
export const effortsFor = (
  options: Pick<ILaunchOptions, 'provider' | 'models' | 'cliDefault'>,
  model: string | null,
): string[] => {
  if (model) return findLaunchModel(options, model)?.efforts ?? [];
  const fallbackModel = findLaunchModel(options, options.cliDefault.model);
  if (fallbackModel) return fallbackModel.efforts;
  return options.provider === 'claude' ? [...CLAUDE_EFFORTS] : [];
};

export type TChoiceCheck = { ok: true; launch: ILaunchChoice } | { ok: false; error: string };

/** A choice checked against one machine's catalog, with the messages the API returns (5.2, 5.3). */
export const checkChoiceAgainst = (
  options: Pick<ILaunchOptions, 'provider' | 'models' | 'cliDefault'>,
  choice: ILaunchChoice,
): TChoiceCheck => {
  const title = PROVIDER_TITLES[options.provider];
  if (choice.model !== null && !findLaunchModel(options, choice.model)) {
    return {
      ok: false,
      error: t('lib.hlLaunchOptions.modelUnavailable', { model: choice.model, provider: title }),
    };
  }
  if (choice.effort !== null && !effortsFor(options, choice.model).includes(choice.effort)) {
    const model = choice.model ?? options.cliDefault.model;
    const effort = effortLabel(choice.effort);
    return {
      ok: false,
      error: model
        ? t('lib.hlLaunchOptions.effortUnsupported', { model, effort })
        : t('lib.hlLaunchOptions.effortUnsupportedDefault', { effort }),
    };
  }
  return { ok: true, launch: choice };
};

/**
 * Picking another model keeps the effort only when the new model supports it; otherwise the effort
 * returns to "Auto" and `dropped` names what was lost, so the UI can say so instead of changing it silently.
 */
export const chooseModel = (
  options: ILaunchOptions,
  current: ILaunchChoice,
  model: string | null,
): { choice: ILaunchChoice; dropped: string | null } => {
  const keep = current.effort === null || effortsFor(options, model).includes(current.effort);
  return { choice: { model, effort: keep ? current.effort : null }, dropped: keep ? null : current.effort };
};

/** "Opus · High", "GPT-5.4 · Auto", "Haiku", "Auto". */
export const launchChipLabel = (options: ILaunchOptions | null, choice: ILaunchChoice): string => {
  const model = choice.model
    ? (findLaunchModel(options ?? { models: [] }, choice.model)?.label ?? choice.model)
    : null;
  const effort = choice.effort ? effortLabel(choice.effort) : null;
  const auto = t('lib.hlLaunchOptions.auto');
  if (!model) return effort ? `${auto} · ${effort}` : auto;
  const offersEffort = options ? effortsFor(options, choice.model).length > 0 : true;
  return offersEffort ? `${model} · ${effort ?? auto}` : model;
};

const shellSingleQuote = (value: string): string => `'${value.replace(/'/g, `'\\''`)}'`;

/**
 * A stored choice comes from a layout file or a request body: it is re-checked here, right before it becomes
 * shell text, and a bad value stops the launch instead of being dropped.
 */
const assertLaunch = (launch: ILaunchChoice): void => {
  if (launch.model !== null && (typeof launch.model !== 'string' || !MODEL_ID_RE.test(launch.model))) {
    throw new Error('Invalid launch model');
  }
  if (launch.effort !== null && !isKnownEffort(launch.effort)) throw new Error('Invalid launch effort');
};

/** `--model 'opus' --effort high` for `claude`; goes before `--` and the prompt. */
export const claudeLaunchFlags = (launch: ILaunchChoice | null | undefined): string[] => {
  if (!launch) return [];
  assertLaunch(launch);
  const flags: string[] = [];
  if (launch.model) flags.push(`--model ${shellSingleQuote(launch.model)}`);
  if (launch.effort) flags.push(`--effort ${launch.effort}`);
  return flags;
};

/** `--model 'gpt-5.4' --effort 'xhigh'` for the Codex launcher script, which forwards them to launch-args. */
export const codexLauncherFlags = (launch: ILaunchChoice | null | undefined): string[] => {
  if (!launch) return [];
  assertLaunch(launch);
  const flags: string[] = [];
  if (launch.model) flags.push('--model', shellSingleQuote(launch.model));
  if (launch.effort) flags.push('--effort', shellSingleQuote(launch.effort));
  return flags;
};

/** argv for `codex` itself (spawned without a shell): `-m <slug> -c model_reasoning_effort="<level>"`. */
export const codexRuntimeLaunchArgs = (launch: ILaunchChoice | null | undefined): string[] => {
  if (!launch) return [];
  assertLaunch(launch);
  const args: string[] = [];
  if (launch.model) args.push('-m', launch.model);
  if (launch.effort) args.push('-c', `model_reasoning_effort="${launch.effort}"`);
  return args;
};

/** Shape-only check for values that already passed the catalog once (a resume, the Codex launcher round-trip). */
export const readLaunchShape = (
  value: { model?: unknown; effort?: unknown } | null | undefined,
): ILaunchChoice | null | 'invalid' => {
  const model = value?.model ?? null;
  const effort = value?.effort ?? null;
  if (model === null && effort === null) return null;
  if (model !== null && (typeof model !== 'string' || !MODEL_ID_RE.test(model))) return 'invalid';
  if (effort !== null && !isKnownEffort(effort)) return 'invalid';
  return { model: model as string | null, effort: effort as string | null };
};
