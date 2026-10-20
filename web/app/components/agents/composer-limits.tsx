import { useContext, useId, useRef, useState } from 'react';
import { Gauge } from 'lucide-react';
import {
  POPOVER_SIDE,
  TOOLTIP_SIDE,
  TwMeter,
  TwPopover,
  TwTooltip,
  METER_TONE,
  twPopoverTriggerProps,
} from '@/components/agents/ui';
import { LimitForecast, formatAgo, useClock } from '@/components/agents/limit-forecast';
import useMinuteClock from '@/components/agents/sidebar/use-minute-clock';
import { RuntimeMachineContext } from '@/hooks/use-fleet-status';
import { useHlMachines, type IFleetMachine } from '@/hooks/use-hl-machines';
import { currentMachineId } from '@/lib/app-path';
import { formatDuration, type TForecastWindowKind } from '@/lib/hl-limits-forecast';
import type { ILimitWindow, IProviderLimits } from '@/types/server/hl-machines';
import { t } from '@/lib/i18n';

const PROVIDER_NAME = { claude: 'Claude', codex: 'Codex' } as const;

type TProvider = keyof typeof PROVIDER_NAME;

const WINDOWS: readonly {
  kind: TForecastWindowKind;
  key: 'fiveHour' | 'sevenDay';
  label: string;
  short: string;
}[] = [
  {
    kind: 'five_hour',
    key: 'fiveHour',
    label: t('agents.composerLimits.session'),
    short: t('agents.composerLimits.sessionShort'),
  },
  {
    kind: 'seven_day',
    key: 'sevenDay',
    label: t('agents.composerLimits.week'),
    short: t('agents.composerLimits.weekShort'),
  },
];

const LEVEL = { ok: 'ok', low: 'low', out: 'out' } as const;

const levelOf = (left: number) => (left <= 5 ? LEVEL.out : left <= 25 ? LEVEL.low : LEVEL.ok);

const leftOf = (window: ILimitWindow) => Math.max(0, Math.min(100, Math.round(100 - window.usedPct)));

/**
 * The machine the conversation runs on. A runtime frame asks its own runtime and gets only itself; the shell of
 * the cluster draws the default machine's conversations in place and gets every machine of the person from the
 * gateway, so the machine is named by the conversation's context, then the frame's address, then the default.
 */
export const pickMachine = (
  machines: readonly IFleetMachine[] | undefined,
  ids: readonly (string | null | undefined)[],
  defaultMachineId?: string | null,
): IFleetMachine | null => {
  if (!machines?.length) return null;
  for (const id of [...ids, defaultMachineId]) {
    const machine = id ? machines.find((item) => item.id === id) : undefined;
    if (machine) return machine;
  }
  return machines.length === 1 ? machines[0] : null;
};

const useLocalLimits = (
  provider: TProvider,
): { limits: IProviderLimits | null; machineName: string | null } => {
  const data = useHlMachines().data;
  const contextMachineId = useContext(RuntimeMachineContext);
  const machine = pickMachine(data?.machines, [contextMachineId, currentMachineId()], data?.defaultMachineId);
  return { limits: machine?.limits[provider] ?? null, machineName: machine?.name ?? null };
};

interface IWindowRowProps {
  label: string;
  kind: TForecastWindowKind;
  window: ILimitWindow;
  provider: TProvider;
  now: number;
}

const WindowRow = ({ label, kind, window, provider, now }: IWindowRowProps) => {
  const clock = useClock();
  const left = leftOf(window);
  const resetIn = formatDuration(window.resetsAt - now / 1000);
  return (
    <div className="tw-climits-window" data-level={levelOf(left)}>
      <div className="tw-climits-head">
        <span>{label}</span>
        <strong>{t('agents.composerLimits.left', { left })}</strong>
      </div>
      <TwMeter
        value={Math.min(100, window.usedPct)}
        max={100}
        label={`${PROVIDER_NAME[provider]}, ${label}`}
        valueText={t('agents.composerLimits.meterValue', { left, resetIn })}
        tone={METER_TONE.auto}
        warnAt={0.75}
        dangerAt={0.95}
        projected={window.forecast?.usedAtReset ?? undefined}
      />
      <span className="tw-climits-reset">
        {t('agents.composerLimits.resetsIn', {
          resetIn,
          clock: clock(window.resetsAt, kind === 'seven_day'),
        })}
      </span>
      <LimitForecast window={window} kind={kind} providerName={PROVIDER_NAME[provider]} withPace={false} />
    </div>
  );
};

export interface ILimitWindowItem {
  kind: TForecastWindowKind;
  label: string;
  short: string;
  window: ILimitWindow;
}

export interface IComposerLimits {
  provider: TProvider;
  windows: ILimitWindowItem[];
  limits: IProviderLimits | null;
  machineName: string | null;
  now: number;
  /** Level of the window with the least left, `null` while the machine reports none. */
  level: (typeof LEVEL)[keyof typeof LEVEL] | null;
  summary: string;
}

/** Every window the plan of this machine reports, for the limits chip and the composer's compact gauge. */
export const useComposerLimits = (provider: TProvider): IComposerLimits => {
  const now = useMinuteClock();
  const { limits, machineName } = useLocalLimits(provider);
  const windows = WINDOWS.flatMap((spec) =>
    limits?.[spec.key]
      ? [{ kind: spec.kind, label: spec.label, short: spec.short, window: limits[spec.key]! }]
      : [],
  );
  const level = windows.length ? levelOf(Math.min(...windows.map((item) => leftOf(item.window)))) : null;
  const summary = `${PROVIDER_NAME[provider]}: ${windows
    .map((item) =>
      t('agents.composerLimits.summaryItem', {
        window: item.short,
        left: leftOf(item.window),
        resetIn: formatDuration(item.window.resetsAt - now / 1000),
      }),
    )
    .join('; ')}`;
  return { provider, windows, limits, machineName, now, level, summary };
};

export const limitsTitle = ({ provider, machineName }: IComposerLimits) =>
  machineName
    ? t('agents.composerLimits.titleOnMachine', { provider: PROVIDER_NAME[provider], machine: machineName })
    : t('agents.composerLimits.title', { provider: PROVIDER_NAME[provider] });

/** The windows with their resets and forecasts, as the limits popover shows them. */
export const ComposerLimitsDetail = ({ data }: { data: IComposerLimits }) => (
  <>
    {data.windows.map((item) => (
      <WindowRow
        key={item.kind}
        label={item.label}
        kind={item.kind}
        window={item.window}
        provider={data.provider}
        now={data.now}
      />
    ))}
    <p className="tw-climits-foot">
      {[
        data.limits?.plan,
        data.limits?.updatedAt
          ? t('agents.composerLimits.updated', {
              ago: formatAgo(data.limits.updatedAt / 1000, data.now / 1000),
            })
          : null,
      ]
        .filter(Boolean)
        .join(' · ')}
    </p>
    {data.limits?.error && <p className="tw-climits-error">{data.limits.error}</p>}
  </>
);

/**
 * What is left of the subscription on this machine, beside the prompt: every window the plan has, «5 h 94%
 * wk 81%», with resets and forecasts in the popover. Hidden until the machine reports any window.
 */
const ComposerLimits = ({ provider }: { provider: TProvider }) => {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const data = useComposerLimits(provider);
  if (data.level === null) return null;

  return (
    <>
      <TwTooltip content={data.summary} side={TOOLTIP_SIDE.top}>
        <button
          ref={triggerRef}
          type="button"
          className="tw-climits"
          data-level={data.level}
          aria-label={t('agents.composerLimits.ariaLabel', { summary: data.summary })}
          {...twPopoverTriggerProps(open, popoverId)}
          onClick={() => setOpen((value) => !value)}
        >
          <Gauge aria-hidden="true" />
          {data.windows.map((item) => (
            <span key={item.kind} className="tw-climits-win" data-level={levelOf(leftOf(item.window))}>
              <small>{item.short}</small>
              {leftOf(item.window)}%
            </span>
          ))}
        </button>
      </TwTooltip>
      <TwPopover
        id={popoverId}
        open={open}
        onClose={() => setOpen(false)}
        anchorRef={triggerRef}
        side={POPOVER_SIDE.top}
        align="end"
        title={limitsTitle(data)}
        className="tw-climits-pop"
      >
        <ComposerLimitsDetail data={data} />
      </TwPopover>
    </>
  );
};

export default ComposerLimits;
