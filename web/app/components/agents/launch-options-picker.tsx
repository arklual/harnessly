import {
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type SyntheticEvent,
} from 'react';
import { Sparkles } from 'lucide-react';
import { useShallow } from 'zustand/react/shallow';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  CHIP_TONE,
  CHOICE_ACTIVATION,
  POPOVER_SIDE,
  TwButton,
  TwChip,
  TwPopover,
  TwRadioList,
  TwSegmented,
  TwSpinner,
  twPopoverTriggerProps,
  type ITwChoice,
} from '@/components/agents/ui';
import type { IUseLaunchOptions } from '@/hooks/use-launch-options';
import { requestLaunchChange, useLaunchChangeStore } from '@/hooks/use-launch-change-queue';
import { shownValue } from '@/lib/launch-change-queue';
import {
  CLAUDE_LAUNCH_MODELS,
  EFFORT_BARS,
  EFFORT_DESCRIPTIONS,
  OPEN_LAUNCH_PICKER_EVENT,
  PROVIDER_TITLES,
  checkChoiceAgainst,
  chooseModel,
  effortLabel,
  effortsFor,
  findLaunchModel,
  isKnownEffort,
  launchChipLabel,
  type ILaunchOptions,
  type IOpenLaunchPickerDetail,
} from '@/lib/hl-launch-options';
import { claudeFamilyOf, prettyModelName } from '@/lib/hl-models';
import { RUN_SLASH_COMMAND_EVENT, type IRunSlashCommandDetail } from '@/lib/hl-command-filter';
import {
  AUTO_LAUNCH,
  CLAUDE_EFFORTS,
  ULTRACODE_EFFORT,
  type ILaunchChoice,
  type TAgentProvider,
  type TClaudeEffort,
} from '@/types/hl-launch';
import { locale, t } from '@/lib/i18n';

/** Radio value of «Auto»: `·` never passes MODEL_ID_RE, so no model or level can collide with it. */
const AUTO_VALUE = '·auto';
const toValue = (id: string | null) => id ?? AUTO_VALUE;
const fromValue = (value: string) => (value === AUTO_VALUE ? null : value);

/** Four rising bars, filled by level (tw-effort-bars in hl-launch.css). */
export const EffortBars = ({ effort }: { effort: string }) => (
  <span
    className="tw-effort-bars"
    data-level={isKnownEffort(effort) ? EFFORT_BARS[effort] : 0}
    data-ultra={effort === ULTRACODE_EFFORT || undefined}
    aria-hidden="true"
  >
    <i />
    <i />
    <i />
    <i />
  </span>
);

const useOpenRequest = (matches: (detail: IOpenLaunchPickerDetail) => boolean, open: () => void) => {
  const matchesRef = useRef(matches);
  const openRef = useRef(open);
  useEffect(() => {
    matchesRef.current = matches;
    openRef.current = open;
  });
  useEffect(() => {
    const onRequest = (event: Event) => {
      if (matchesRef.current((event as CustomEvent<IOpenLaunchPickerDetail>).detail ?? {})) openRef.current();
    };
    window.addEventListener(OPEN_LAUNCH_PICKER_EVENT, onRequest);
    return () => window.removeEventListener(OPEN_LAUNCH_PICKER_EVENT, onRequest);
  }, []);
};

const autoModelHint = (options: ILaunchOptions) => {
  const title = PROVIDER_TITLES[options.provider];
  const model = options.cliDefault.model;
  return model
    ? t('agents.launchOptionsPicker.autoModelFromSettings', {
        title,
        model: findLaunchModel(options, model)?.label ?? model,
      })
    : t('agents.launchOptionsPicker.autoModelUpTo', { title });
};

const autoEffortHint = (options: ILaunchOptions, model: string | null) => {
  if (options.cliDefault.effort)
    return t('agents.launchOptionsPicker.autoEffortFromSettings', {
      effort: effortLabel(options.cliDefault.effort),
    });
  const own = findLaunchModel(options, model ?? options.cliDefault.model)?.defaultEffort;
  return own
    ? t('agents.launchOptionsPicker.autoEffortModelDefault', { effort: effortLabel(own) })
    : t('agents.launchOptionsPicker.autoEffortUpToModel');
};

const modelChoices = (options: ILaunchOptions): ITwChoice[] => [
  { value: AUTO_VALUE, label: t('agents.launchOptionsPicker.auto'), description: autoModelHint(options) },
  ...options.models.map((model) => ({
    value: model.id,
    label: model.label,
    description: model.hint ?? undefined,
  })),
];

const effortChoices = (options: ILaunchOptions, model: string | null): ITwChoice[] => [
  {
    value: AUTO_VALUE,
    label: t('agents.launchOptionsPicker.auto'),
    description: autoEffortHint(options, model),
    icon: <Sparkles />,
  },
  ...effortsFor(options, model).map((effort) => ({
    value: effort,
    label: effortLabel(effort),
    description: isKnownEffort(effort) ? EFFORT_DESCRIPTIONS[effort] : undefined,
    icon: <EffortBars effort={effort} />,
  })),
];

const fetchedAtLabel = (value: string) => {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleDateString(locale() === 'ru' ? 'ru-RU' : 'en-US', {
        day: 'numeric',
        month: 'long',
        year: 'numeric',
      });
};

interface ILaunchChoicePanelProps {
  options: ILaunchOptions;
  value: ILaunchChoice;
  pending: ILaunchChoice | null;
  onChange: (choice: ILaunchChoice) => void;
}

/** Two radiogroups (model, then effort) over one machine's catalog; every pick is saved by the caller. */
const LaunchChoicePanel = ({ options, value, pending, onChange }: ILaunchChoicePanelProps) => {
  const modelHeadingId = useId();
  const effortHeadingId = useId();
  const [dropped, setDropped] = useState<{ effort: string; model: string } | null>(null);
  const shown = pending ?? value;
  const efforts = effortsFor(options, shown.model);
  const pendingModel = pending && pending.model !== value.model ? toValue(pending.model) : null;
  const pendingEffort =
    pending && pending.model === value.model && pending.effort !== value.effort
      ? toValue(pending.effort)
      : null;
  const invalid = checkChoiceAgainst(options, value);
  const title = PROVIDER_TITLES[options.provider];

  const pickModel = (next: string) => {
    const result = chooseModel(options, value, fromValue(next));
    setDropped(
      result.dropped
        ? {
            effort: result.dropped,
            model:
              findLaunchModel(options, result.choice.model)?.label ??
              t('agents.launchOptionsPicker.thisModel'),
          }
        : null,
    );
    onChange(result.choice);
  };

  return (
    <div className="tw-launch-panel">
      {!invalid.ok && (
        <p className="tw-launch-alert" role="alert">
          {t('agents.launchOptionsPicker.savedInvalid', { error: invalid.error })}
        </p>
      )}
      <section className="tw-launch-section">
        <h3 id={modelHeadingId} className="tw-launch-heading">
          {t('agents.launchOptionsPicker.model')}
        </h3>
        {options.models.length === 0 && (
          <p className="tw-launch-note">{t('agents.launchOptionsPicker.modelsUnavailable')}</p>
        )}
        <TwRadioList
          aria-labelledby={modelHeadingId}
          options={modelChoices(options)}
          value={toValue(shown.model)}
          onChange={pickModel}
          pendingValue={pendingModel}
          activation={CHOICE_ACTIVATION.manual}
        />
      </section>
      <section className="tw-launch-section">
        <h3 id={effortHeadingId} className="tw-launch-heading">
          {t('agents.launchOptionsPicker.effort')}
        </h3>
        {efforts.length === 0 ? (
          <p className="tw-launch-note">{t('agents.launchOptionsPicker.effortNotSelectable')}</p>
        ) : (
          <TwRadioList
            aria-labelledby={effortHeadingId}
            options={effortChoices(options, shown.model)}
            value={toValue(shown.effort)}
            onChange={(next) => {
              setDropped(null);
              onChange({ model: value.model, effort: fromValue(next) });
            }}
            pendingValue={pendingEffort}
            activation={CHOICE_ACTIVATION.manual}
          />
        )}
        {dropped && (
          <p className="tw-launch-note" role="status">
            {t('agents.launchOptionsPicker.effortDropped', {
              model: dropped.model,
              effort: effortLabel(dropped.effort),
            })}
          </p>
        )}
      </section>
      <footer className="tw-launch-footer">
        <p>{t('agents.launchOptionsPicker.rememberNote', { title })}</p>
        {options.fetchedAt && (
          <p>
            {t('agents.launchOptionsPicker.codexModelsUpdated', { when: fetchedAtLabel(options.fetchedAt) })}
          </p>
        )}
        {options.warnings.map((warning) => (
          <p key={warning} className="tw-launch-warning">
            {warning}
          </p>
        ))}
      </footer>
    </div>
  );
};

interface ILaunchOptionsPickerProps {
  provider: TAgentProvider;
  launch: IUseLaunchOptions;
}

/** «Opus · High ▾» in the welcome composer: the choice new conversations of this agent start with on this machine. */
export const LaunchOptionsPicker = ({ provider, launch }: ILaunchOptionsPickerProps) => {
  const [open, setOpen] = useState(false);
  const chipRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const { options, error, pending, save, retry } = launch;
  const value = options?.saved ?? AUTO_LAUNCH;
  const loading = options === null && error === null;
  const invalid = options !== null && !checkChoiceAgainst(options, value).ok;
  const warn = invalid || (options === null && error !== null);
  const title = PROVIDER_TITLES[provider];

  useOpenRequest(
    (detail) => !detail.sessionName,
    () => setOpen(true),
  );

  let label = launchChipLabel(options, pending ?? value);
  if (loading) label = t('agents.launchOptionsPicker.modelLoading');
  else if (!options) label = t('agents.launchOptionsPicker.auto');

  return (
    <>
      <TwChip
        ref={chipRef}
        icon={<Sparkles />}
        expandable
        pending={loading || pending !== null}
        tone={warn ? CHIP_TONE.warning : CHIP_TONE.neutral}
        aria-label={t('agents.launchOptionsPicker.chipAria', { title, label })}
        title={error ?? undefined}
        className="tw-launch-chip"
        {...twPopoverTriggerProps(open, popoverId)}
        onClick={() => setOpen((current) => !current)}
      >
        {label}
      </TwChip>
      <TwPopover
        id={popoverId}
        open={open}
        onClose={() => setOpen(false)}
        anchorRef={chipRef}
        side={POPOVER_SIDE.top}
        aria-label={t('agents.launchOptionsPicker.popoverAria', { title })}
        className="tw-launch-popover"
      >
        {options && (
          <LaunchChoicePanel
            options={options}
            value={value}
            pending={pending}
            onChange={(choice) => void save(choice)}
          />
        )}
        {!options && error && (
          <div className="tw-launch-error" role="alert">
            <p>{t('agents.launchOptionsPicker.loadError', { error, title })}</p>
            <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.secondary} onClick={retry}>
              {t('agents.launchOptionsPicker.retry')}
            </TwButton>
          </div>
        )}
        {loading && (
          <p className="tw-launch-loading">
            <TwSpinner /> {t('agents.launchOptionsPicker.loadingModels')}
          </p>
        )}
      </TwPopover>
    </>
  );
};

const stopBubbling = (event: SyntheticEvent) => event.stopPropagation();

const CLAUDE_SESSION_OPTIONS = CLAUDE_LAUNCH_MODELS.map((model) => ({
  value: model.id,
  label: model.label,
  description: model.hint,
}));

const SESSION_EFFORT_OPTIONS: ITwChoice[] = CLAUDE_EFFORTS.map((effort) => ({
  value: effort,
  label: effortLabel(effort),
  description: EFFORT_DESCRIPTIONS[effort],
  icon: <EffortBars effort={effort} />,
}));

const PENDING_LABEL = {
  applying: t('agents.launchOptionsPicker.pendingApplying'),
  queued: t('agents.launchOptionsPicker.pendingQueued'),
} as const;

const PendingMark = ({ pending }: { pending: 'applying' | 'queued' | null }) =>
  pending ? <span className="tw-launch-pending">{PENDING_LABEL[pending]}</span> : null;

const useShownLaunchValue = (sessionName: string, kind: 'model' | 'effort', reported: string | null) => {
  const shown = useLaunchChangeStore(
    useShallow((state) => shownValue(state.queue, sessionName, kind, reported)),
  );
  const prune = useLaunchChangeStore((state) => state.prune);
  useEffect(() => prune(sessionName, kind, reported), [prune, sessionName, kind, reported]);
  return shown;
};

interface ISessionChipProps {
  sessionName: string;
  /** From the session's statusline or last turn_context; null until the agent reports. */
  model: string | null;
  effort: string | null;
  side?: (typeof POPOVER_SIDE)[keyof typeof POPOVER_SIDE];
}

/**
 * Claude's model of a running conversation: instant, the switch (`/model x` through switch-model) goes to the agent
 * in the background, or after the turn when it is busy.
 */
export const SessionModelPicker = ({ sessionName, model, side = POPOVER_SIDE.bottom }: ISessionChipProps) => {
  const [open, setOpen] = useState(false);
  const chipRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const headingId = useId();
  const reportedAlias = claudeFamilyOf(model);
  const shown = useShownLaunchValue(sessionName, 'model', reportedAlias);
  const text = shown.pending
    ? CLAUDE_LAUNCH_MODELS.find((item) => item.id === shown.value)?.label
    : prettyModelName(model);

  useOpenRequest(
    (detail) => detail.sessionName === sessionName && (!detail.section || detail.section === 'model'),
    () => setOpen(true),
  );

  return (
    <span className="tw-launch-session" onClick={stopBubbling} onKeyDown={stopBubbling}>
      <TwChip
        ref={chipRef}
        expandable
        data-pending={shown.pending ?? undefined}
        aria-busy={shown.pending ? true : undefined}
        aria-label={`${t('agents.launchOptionsPicker.modelAria', { value: text ?? t('agents.launchOptionsPicker.unknown') })}${shown.pending ? `, ${PENDING_LABEL[shown.pending]}` : ''}`}
        className="tw-launch-chip tw-launch-chip-session"
        {...twPopoverTriggerProps(open, popoverId)}
        onClick={() => setOpen((current) => !current)}
      >
        <span className="tw-launch-model">{text ?? t('agents.launchOptionsPicker.model')}</span>
        <PendingMark pending={shown.pending} />
      </TwChip>
      <TwPopover
        id={popoverId}
        open={open}
        onClose={() => setOpen(false)}
        anchorRef={chipRef}
        side={side}
        aria-label={t('agents.launchOptionsPicker.model')}
        className="tw-launch-popover"
      >
        <div className="tw-launch-panel">
          <section className="tw-launch-section">
            <h3 id={headingId} className="tw-launch-heading">
              {t('agents.launchOptionsPicker.model')}
            </h3>
            <TwRadioList
              aria-labelledby={headingId}
              options={CLAUDE_SESSION_OPTIONS}
              value={shown.value}
              onChange={(alias) => {
                setOpen(false);
                requestLaunchChange(sessionName, { kind: 'model', value: alias, from: reportedAlias });
              }}
              activation={CHOICE_ACTIVATION.manual}
            />
          </section>
          <footer className="tw-launch-footer">
            <p>{t('agents.launchOptionsPicker.midConversationNote')}</p>
            <p>{t('agents.launchOptionsPicker.claudeRemembersNote')}</p>
          </footer>
        </div>
      </TwPopover>
    </span>
  );
};

/** Claude's reasoning effort of a running conversation (`/effort x` through the command runner); Haiku has none. */
export const SessionEffortPicker = ({
  sessionName,
  model,
  effort,
  side = POPOVER_SIDE.bottom,
}: ISessionChipProps) => {
  const [open, setOpen] = useState(false);
  const chipRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const headingId = useId();
  const shownModel = useShownLaunchValue(sessionName, 'model', claudeFamilyOf(model));
  const shown = useShownLaunchValue(sessionName, 'effort', effort);
  const effortless = shownModel.value === 'haiku';
  const text = shown.value ? effortLabel(shown.value) : null;

  useOpenRequest(
    (detail) => detail.sessionName === sessionName && detail.section === 'effort',
    () => {
      if (!effortless) setOpen(true);
    },
  );

  if (effortless) return null;

  return (
    <span className="tw-launch-session" onClick={stopBubbling} onKeyDown={stopBubbling}>
      <TwChip
        ref={chipRef}
        expandable
        data-pending={shown.pending ?? undefined}
        aria-busy={shown.pending ? true : undefined}
        aria-label={`${t('agents.launchOptionsPicker.effortAria', { value: text ?? t('agents.launchOptionsPicker.unknown') })}${shown.pending ? `, ${PENDING_LABEL[shown.pending]}` : ''}`}
        className="tw-launch-chip tw-launch-chip-session tw-launch-chip-effort"
        {...twPopoverTriggerProps(open, popoverId)}
        onClick={() => setOpen((current) => !current)}
      >
        {shown.value && isKnownEffort(shown.value) && <EffortBars effort={shown.value} />}
        <span className="tw-launch-effort" data-ultra={shown.value === ULTRACODE_EFFORT || undefined}>
          {text ?? t('agents.launchOptionsPicker.effortShort')}
        </span>
        <PendingMark pending={shown.pending} />
      </TwChip>
      <TwPopover
        id={popoverId}
        open={open}
        onClose={() => setOpen(false)}
        anchorRef={chipRef}
        side={side}
        aria-label={t('agents.launchOptionsPicker.effort')}
        className="tw-launch-popover"
      >
        <div className="tw-launch-panel">
          <section className="tw-launch-section">
            <h3 id={headingId} className="tw-launch-heading">
              {t('agents.launchOptionsPicker.effort')}
            </h3>
            <TwRadioList
              aria-labelledby={headingId}
              options={SESSION_EFFORT_OPTIONS}
              value={shown.value && isKnownEffort(shown.value) ? shown.value : null}
              onChange={(level) => {
                setOpen(false);
                requestLaunchChange(sessionName, { kind: 'effort', value: level, from: effort });
              }}
              activation={CHOICE_ACTIVATION.manual}
            />
            {!shown.value && (
              <p className="tw-launch-note">{t('agents.launchOptionsPicker.effortNotReported')}</p>
            )}
          </section>
        </div>
      </TwPopover>
    </span>
  );
};

interface ISessionLaunchPickerProps extends ISessionChipProps {
  provider: TAgentProvider;
}

/** Claude: two chips, model and effort. Codex: one chip that opens its own `/model` menu, mirrored above the composer. */
export const SessionLaunchPicker = ({
  provider,
  sessionName,
  model,
  effort,
  side,
}: ISessionLaunchPickerProps) => {
  if (provider === 'claude') {
    return (
      <>
        <SessionModelPicker sessionName={sessionName} model={model} effort={effort} side={side} />
        <SessionEffortPicker sessionName={sessionName} model={model} effort={effort} side={side} />
      </>
    );
  }
  if (!model) return null;
  const effortText = effort ? effortLabel(effort) : null;
  return (
    <span className="tw-launch-session" onClick={stopBubbling} onKeyDown={stopBubbling}>
      <TwChip
        expandable
        aria-label={t('agents.launchOptionsPicker.codexChipAria', {
          value: effortText ? `${model} · ${effortText}` : model,
        })}
        title={t('agents.launchOptionsPicker.codexChipTitle')}
        className="tw-launch-chip tw-launch-chip-session"
        onClick={() => {
          const detail: IRunSlashCommandDetail = { sessionName, line: '/model' };
          window.dispatchEvent(new CustomEvent(RUN_SLASH_COMMAND_EVENT, { detail }));
        }}
      >
        <span className="tw-launch-model">{model}</span>
        {effortText && <span className="tw-launch-effort"> · {effortText}</span>}
      </TwChip>
    </span>
  );
};

const MODEL_SEGMENTS: ITwChoice[] = CLAUDE_LAUNCH_MODELS.map((model) => ({
  value: model.id,
  label: model.label,
}));

interface IEffortSliderProps {
  value: TClaudeEffort | null;
  onChange: (value: TClaudeEffort) => void;
}

/** Effort as a track of stops, the way ChatGPT sets it: tap a stop or drag along; arrows step on a keyboard. */
const EffortSlider = ({ value, onChange }: IEffortSliderProps) => {
  const [dragged, setDragged] = useState<number | null>(null);
  const index = dragged ?? (value ? CLAUDE_EFFORTS.indexOf(value) : -1);
  const last = CLAUDE_EFFORTS.length - 1;
  const stopAt = (event: ReactPointerEvent<HTMLDivElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    const share = Math.min(1, Math.max(0, (event.clientX - box.left) / box.width));
    return Math.round(share * last);
  };
  const commit = (next: number) => {
    setDragged(null);
    const effort = CLAUDE_EFFORTS[next];
    if (effort !== value) onChange(effort);
  };
  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const step = { ArrowRight: 1, ArrowUp: 1, ArrowLeft: -1, ArrowDown: -1 }[event.key];
    if (step === undefined) return;
    event.preventDefault();
    commit(Math.min(last, Math.max(0, (index < 0 ? 0 : index) + step)));
  };

  return (
    <div
      role="slider"
      tabIndex={0}
      aria-label={t('agents.launchOptionsPicker.effort')}
      aria-valuemin={0}
      aria-valuemax={last}
      aria-valuenow={index < 0 ? undefined : index}
      aria-valuetext={
        index < 0 ? t('agents.launchOptionsPicker.unknown') : effortLabel(CLAUDE_EFFORTS[index])
      }
      className="tw-effort-slider"
      data-level={index < 0 ? undefined : index}
      onKeyDown={onKeyDown}
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        setDragged(stopAt(event));
      }}
      onPointerMove={(event) => {
        if (dragged !== null) setDragged(stopAt(event));
      }}
      onPointerUp={(event) => commit(stopAt(event))}
      onPointerCancel={() => setDragged(null)}
    >
      <span className="tw-effort-slider-track" aria-hidden="true">
        <span className="tw-effort-slider-fill" />
        {CLAUDE_EFFORTS.map((effort, stop) => (
          <span key={effort} className="tw-effort-slider-stop" data-on={stop <= index ? '' : undefined} />
        ))}
        <span className="tw-effort-slider-thumb" />
      </span>
      <span className="tw-effort-slider-ends" aria-hidden="true">
        <span>{effortLabel(CLAUDE_EFFORTS[0])}</span>
        <span>{effortLabel(CLAUDE_EFFORTS[last])}</span>
      </span>
    </div>
  );
};

interface ISessionLaunchControlsProps extends ISessionChipProps {
  provider: TAgentProvider;
  /** The sheet closes when the change goes to Codex's own menu. */
  onLeave: () => void;
}

/**
 * Model and effort of a running conversation as sections of the composer's settings sheet on a phone:
 * the same changes the chips make, laid out to be read and tapped instead of squeezed into the row.
 */
export const SessionLaunchControls = ({
  provider,
  sessionName,
  model,
  effort,
  onLeave,
}: ISessionLaunchControlsProps) => {
  const reportedAlias = claudeFamilyOf(model);
  const shownModel = useShownLaunchValue(sessionName, 'model', reportedAlias);
  const shownEffort = useShownLaunchValue(sessionName, 'effort', effort);

  if (provider !== 'claude') {
    const effortText = effort ? effortLabel(effort) : null;
    return (
      <section className="tw-csheet-section">
        <h3 className="tw-csheet-heading">{t('agents.launchOptionsPicker.modelAndEffort')}</h3>
        <button
          type="button"
          className="tw-csheet-row"
          onClick={() => {
            onLeave();
            const detail: IRunSlashCommandDetail = { sessionName, line: '/model' };
            window.dispatchEvent(new CustomEvent(RUN_SLASH_COMMAND_EVENT, { detail }));
          }}
        >
          <span>
            {model
              ? [model, effortText].filter(Boolean).join(' · ')
              : t('agents.launchOptionsPicker.modelUnknown')}
          </span>
          <span className="tw-csheet-row-hint">{t('agents.launchOptionsPicker.changeInCodex')}</span>
        </button>
      </section>
    );
  }

  const effortless = shownModel.value === 'haiku';
  const effortValue = CLAUDE_EFFORTS.find((level) => level === shownEffort.value) ?? null;
  return (
    <>
      <section className="tw-csheet-section">
        <div className="tw-csheet-title">
          <h3 className="tw-csheet-heading">{t('agents.launchOptionsPicker.model')}</h3>
          <PendingMark pending={shownModel.pending} />
        </div>
        <TwSegmented
          aria-label={t('agents.launchOptionsPicker.model')}
          options={MODEL_SEGMENTS}
          value={shownModel.value}
          activation={CHOICE_ACTIVATION.manual}
          className="tw-csheet-models"
          onChange={(alias) =>
            requestLaunchChange(sessionName, { kind: 'model', value: alias, from: reportedAlias })
          }
        />
      </section>
      {!effortless && (
        <section className="tw-csheet-section">
          <div className="tw-csheet-title">
            <h3 className="tw-csheet-heading">{t('agents.launchOptionsPicker.effort')}</h3>
            <span className="tw-csheet-value" data-ultra={effortValue === ULTRACODE_EFFORT || undefined}>
              {effortValue ? effortLabel(effortValue) : '—'}
            </span>
            <PendingMark pending={shownEffort.pending} />
          </div>
          <EffortSlider
            value={effortValue}
            onChange={(level) =>
              requestLaunchChange(sessionName, { kind: 'effort', value: level, from: effort })
            }
          />
          <p className="tw-csheet-note">
            {effortValue
              ? EFFORT_DESCRIPTIONS[effortValue]
              : t('agents.launchOptionsPicker.effortNotReported')}
          </p>
        </section>
      )}
    </>
  );
};
