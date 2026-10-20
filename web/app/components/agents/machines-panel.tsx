import { useState } from 'react';
import { Check, Server, TriangleAlert } from 'lucide-react';
import ClaudeLogo from '@/components/icons/claude-logo';
import OpenAIIcon from '@/components/icons/openai-icon';
import { METER_TONE, TwMeter } from '@/components/agents/ui';
import { LimitForecast, useClock } from '@/components/agents/limit-forecast';
import { useHlMachines, type IFleetMachine } from '@/hooks/use-hl-machines';
import { limitWarning } from '@/hooks/use-machine-limits';
import { cn } from '@/lib/utils';
import type { TForecastWindowKind } from '@/lib/hl-limits-forecast';
import type { ILimitWindow, IMachine, IProviderLimits } from '@/types/server/hl-machines';
import { t } from '@/lib/i18n';

export type { IFleetMachine } from '@/hooks/use-hl-machines';

const OPEN_KEY = 'hl:machines-open';

const untilReset = (resetsAt: number) => {
  const secs = Math.max(0, Math.round(resetsAt - Date.now() / 1000));
  const d = Math.floor(secs / 86400);
  const h = Math.floor((secs % 86400) / 3600);
  const m = Math.floor((secs % 3600) / 60);
  if (d) return t('agents.machinesPanel.daysHours', { d, h });
  if (h) return t('agents.machinesPanel.hoursMinutes', { h, m });
  return t('agents.machinesPanel.minutes', { m });
};

const level = (pct: number) => {
  if (pct >= 85) return 'high';
  if (pct >= 60) return 'mid';
  return 'low';
};

const LimitRow = ({
  label,
  kind,
  providerName,
  window,
}: {
  label: string;
  kind: TForecastWindowKind;
  providerName: string;
  window: ILimitWindow | null;
}) => {
  if (!window) return null;
  const used = Math.round(window.usedPct);
  const left = Math.max(0, 100 - used);
  return (
    <>
      <div
        className={cn('tw-limit', `tw-limit-${level(window.usedPct)}`)}
        title={t('agents.machinesPanel.usedTitle', { used, resetIn: untilReset(window.resetsAt) })}
      >
        <span className="tw-limit-label">{label}</span>
        <TwMeter
          value={Math.min(100, window.usedPct)}
          max={100}
          label={t('agents.machinesPanel.meterLabel', { provider: providerName, window: label })}
          valueText={t('agents.machinesPanel.meterValue', { left, resetIn: untilReset(window.resetsAt) })}
          tone={METER_TONE.auto}
          warnAt={0.6}
          dangerAt={0.85}
          className="tw-limit-meter"
        />
        <span className="tw-limit-value">{left}%</span>
        <span className="tw-limit-reset">{untilReset(window.resetsAt)}</span>
      </div>
      <LimitForecast
        window={window}
        kind={kind}
        providerName={providerName}
        withPace={false}
        withCurve={false}
        className="tw-limit-forecast"
      />
    </>
  );
};

const ProviderLimits = ({
  icon,
  name,
  limits,
}: {
  icon: React.ReactNode;
  name: string;
  limits: IProviderLimits | null;
}) => {
  if (!limits || (!limits.fiveHour && !limits.sevenDay)) return null;
  return (
    <div className="tw-limits">
      <div className="tw-limits-head">
        {icon}
        <span>{limits.plan ?? name}</span>
        <small>{t('agents.machinesPanel.columns')}</small>
      </div>
      <LimitRow
        label={t('agents.machinesPanel.fiveHours')}
        kind="five_hour"
        providerName={name}
        window={limits.fiveHour}
      />
      <LimitRow
        label={t('agents.machinesPanel.sevenDays')}
        kind="seven_day"
        providerName={name}
        window={limits.sevenDay}
      />
      {limits.error && (
        <p className="tw-limit-error" title={limits.error}>
          {limits.error}
        </p>
      )}
    </div>
  );
};

const machineTitle = (m: IMachine) => {
  const who = `${m.user}@${m.host}`;
  if (!m.release) return who;
  return m.release.commit
    ? t('agents.machinesPanel.titleWithCommit', {
        who,
        version: m.release.version,
        commit: m.release.commit.slice(0, 8),
      })
    : t('agents.machinesPanel.titleWithRuntime', { who, version: m.release.version });
};

const worst = (m: IMachine) =>
  Math.max(m.limits.claude?.fiveHour?.usedPct ?? 0, m.limits.claude?.sevenDay?.usedPct ?? 0);

const selectLabel = (machine: IFleetMachine, selected: boolean) => {
  if (machine.available === false) return t('agents.machinesPanel.unavailable');
  if (selected) return t('agents.machinesPanel.selected');
  return t('agents.machinesPanel.select');
};

/** Folded by default: the footer chip shows the count and the tightest limit, the list opens above it. */
export const useMachinesOpen = () => {
  const [open, setOpen] = useState(() => {
    try {
      return localStorage.getItem(OPEN_KEY) === '1';
    } catch {
      return false;
    }
  });
  const toggle = () =>
    setOpen((v) => {
      try {
        localStorage.setItem(OPEN_KEY, v ? '0' : '1');
      } catch {}
      return !v;
    });
  return [open, toggle] as const;
};

interface IMachinesChipProps {
  open: boolean;
  onToggle: () => void;
  controls: string;
}

/** Footer chip: machines count, the nearest forecast of a limit running out, else what is left of Claude's 5-hour window. */
export const MachinesChip = ({ open, onToggle, controls }: IMachinesChipProps) => {
  const { data, error } = useHlMachines();
  const machines = data?.machines ?? null;
  const clock = useClock();
  if (!machines) return null;
  const warning =
    machines
      .flatMap((m) =>
        [limitWarning(m.limits.claude, 'claude'), limitWarning(m.limits.codex, 'codex')].map((w) =>
          w ? { ...w, machineName: m.name } : null,
        ),
      )
      .filter((w) => w !== null)
      .sort((a, b) => a.at - b.at)[0] ?? null;
  const claude = machines[0]?.limits.claude?.fiveHour ? machines[0].limits.claude : null;
  const hint = [
    t('agents.machinesPanel.count', { count: machines.length }),
    error && t('agents.machinesPanel.staleError', { error }),
    warning &&
      t('agents.machinesPanel.limitWarning', {
        machine: warning.machineName,
        provider: warning.provider === 'codex' ? 'Codex' : 'Claude',
      }),
  ]
    .filter(Boolean)
    .join(' · ');
  return (
    <button
      type="button"
      className={cn('tw-machines-chip', error && 'tw-machines-chip-stale')}
      aria-expanded={open}
      aria-controls={controls}
      title={hint}
      aria-label={t('agents.machinesPanel.chipLabel', { hint })}
      onClick={onToggle}
    >
      <Server aria-hidden="true" />
      <span className="tw-machines-count">{machines.length}</span>
      {warning && (
        <span className="tw-machines-warning">
          <TriangleAlert aria-hidden="true" /> {clock(warning.at, warning.kind === 'seven_day')}
        </span>
      )}
      {!warning && claude?.fiveHour && (
        <span className={cn('tw-machines-peek', `tw-limit-${level(worst(machines[0]))}`)}>
          <ClaudeLogo /> {Math.max(0, 100 - Math.round(claude.fiveHour.usedPct))}%
        </span>
      )}
    </button>
  );
};

interface IMachinesPanelProps {
  id: string;
  selectedMachineId?: string;
  onSelectMachine: (id: string) => void;
}

const MachinesPanel = ({ id, selectedMachineId, onSelectMachine }: IMachinesPanelProps) => {
  const { data, error } = useHlMachines();
  const machines = data?.machines ?? null;
  if (!machines) return null;
  return (
    <section id={id} className="tw-machines" aria-label={t('agents.machinesPanel.title')}>
      <header className="tw-machines-head">
        <span>{t('agents.machinesPanel.title')}</span>
        {error && (
          <span className="tw-machines-stale" title={error}>
            {t('agents.machinesPanel.stale')}
          </span>
        )}
      </header>
      <div className="tw-machines-list">
        {machines.map((m) => (
          <article
            key={m.id}
            className={cn('tw-machine', selectedMachineId === m.id && 'tw-machine-selected')}
          >
            <header className="tw-machine-head" title={machineTitle(m)}>
              <span className="tw-machine-icon">
                <Server />
              </span>
              <span className="tw-machine-name">
                <strong>{m.name}</strong>
                <small>
                  {m.user}@{m.host.split('.')[0]}
                </small>
              </span>
              <span className="tw-machine-stats">
                <span title={t('agents.machinesPanel.cpu')}>CPU {m.cpuPct}%</span>
                <span title={t('agents.machinesPanel.memory', { total: m.memTotalGb })}>
                  RAM {m.memUsedPct}%
                </span>
                {m.release && (
                  <span title={t('agents.machinesPanel.runtimeVersion')}>{m.release.version}</span>
                )}
              </span>
            </header>
            <button
              type="button"
              className="tw-machine-select"
              aria-pressed={selectedMachineId === m.id}
              disabled={m.available === false}
              onClick={() => onSelectMachine(m.id)}
            >
              {selectedMachineId === m.id && <Check />}
              {selectLabel(m, selectedMachineId === m.id)}
            </button>
            <ProviderLimits
              icon={<ClaudeLogo className="tw-limits-claude" />}
              name="Claude"
              limits={m.limits.claude}
            />
            <ProviderLimits
              icon={<OpenAIIcon className="tw-limits-codex" />}
              name="Codex"
              limits={m.limits.codex}
            />
          </article>
        ))}
        <p className="tw-machine-note">{t('agents.machinesPanel.note')}</p>
      </div>
    </section>
  );
};

export default MachinesPanel;
