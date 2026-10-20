import { useId, useRef, useState, type ReactNode } from 'react';
import { Activity } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  CHIP_TONE,
  LAYER_ALIGN,
  METER_TONE,
  TwBadge,
  TwButton,
  TwMeter,
  TwPopover,
  twPopoverTriggerProps,
} from '@/components/agents/ui';
import type { TMetric } from '@/lib/hl-machine-load';
import type { IMachineLoad, IMachineProcess } from '@/types/server/hl-machine-load-scan';
import { useMachineLoad, type IMachineLoadNotice } from '@/lib/hl-machine-load-store';
import {
  LEVEL_LABEL,
  LEVEL_TONE,
  formatBytes,
  formatPct,
  indicatorText,
  loadRatioText,
  metricText,
  metricTone,
  effectiveLevel,
  gateDecision,
  overloadQuestion,
  processKey,
  processUsage,
  swapInText,
} from '@/lib/hl-machine-load-view';
import { t } from '@/lib/i18n';

const KIND_NOTE: Record<IMachineProcess['kind'], string> = {
  dev: t('agents.machineLoad.kindDev'),
  agent: t('agents.machineLoad.kindAgent'),
  service: t('agents.machineLoad.kindService'),
  other: t('agents.machineLoad.kindOther'),
};

export interface IStopState {
  stopping: ReadonlySet<string>;
  stopped: ReadonlyMap<string, number>;
  onStop: (proc: IMachineProcess) => void;
}

interface IStopControlProps extends IStopState {
  proc: IMachineProcess;
}

/**
 * «Stop» asks once in place; while the request runs the row says so and nothing else can be pressed, and a
 * stopped process that is still listed (SIGKILL follows after 5 s) cannot be asked about again.
 */
export const ProcessStopControl = ({ proc, stopping, stopped, onStop }: IStopControlProps) => {
  const [asking, setAsking] = useState(false);
  if (stopping.has(processKey(proc)))
    return (
      <span className="tw-mload-stopping" role="status">
        {t('agents.machineLoad.stopping')}
      </span>
    );
  if (stopped.has(processKey(proc)))
    return (
      <TwButton type="button" size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.ghost} disabled>
        {t('agents.machineLoad.stopped')}
      </TwButton>
    );
  if (!asking) {
    return (
      <TwButton
        type="button"
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.ghost}
        aria-label={t('agents.machineLoad.stopProcess', { pid: proc.pid })}
        onClick={() => setAsking(true)}
      >
        {t('agents.machineLoad.stop')}
      </TwButton>
    );
  }
  return (
    <span
      className="tw-mload-ask"
      role="group"
      aria-label={t('agents.machineLoad.stopProcessAsk', { pid: proc.pid })}
    >
      <span className="tw-mload-ask-text">{t('agents.machineLoad.stopAsk')}</span>
      <TwButton
        type="button"
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.ghost}
        onClick={() => setAsking(false)}
      >
        {t('agents.machineLoad.cancel')}
      </TwButton>
      <TwButton
        type="button"
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.danger}
        onClick={() => {
          setAsking(false);
          onStop(proc);
        }}
      >
        {t('agents.machineLoad.yes')}
      </TwButton>
    </span>
  );
};

export const MachineLoadNotice = ({ notice }: { notice: IMachineLoadNotice | null }) => {
  if (!notice) return null;
  return notice.tone === 'error' ? (
    <p className="tw-mload-notice tw-mload-notice-error" role="alert">
      {notice.text}
    </p>
  ) : (
    <p className="tw-mload-notice" role="status">
      {notice.text}
    </p>
  );
};

interface IOverloadGateProps {
  load: IMachineLoad;
  onOpen: () => void;
  onCancel: () => void;
}

/** The inline «are you sure» before a new preview on an overloaded machine. */
const OverloadGate = ({ load, onOpen, onCancel }: IOverloadGateProps) => (
  <div
    className="tw-preview-offer tw-mload-gate"
    role="group"
    aria-label={t('agents.machineLoad.overloaded')}
  >
    <span className="tw-preview-offer-text">{overloadQuestion(load)}</span>
    <TwButton type="button" size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.ghost} onClick={onCancel}>
      {t('agents.machineLoad.cancel')}
    </TwButton>
    <TwButton type="button" size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.primary} onClick={onOpen}>
      {t('agents.machineLoad.open')}
    </TwButton>
  </div>
);

interface IGated {
  owner: string;
  open: () => void;
}

/**
 * The overload gate of one preview surface (island, reader pane, floating window) of the page's machine. `guard` opens
 * at once unless the machine is overloaded (a fresh load, never a stale one) and the target is not the one shown; then
 * `gate` asks in place. The question goes away when the level leaves overload or the surface switches `owner` (a tab).
 */
export const useOverloadGate = (
  owner: string,
  enabled = true,
): { guard: (targetKey: string, shownKey: string | null, open: () => void) => void; gate: ReactNode } => {
  const snapshot = useMachineLoad('', { enabled });
  const level = effectiveLevel(snapshot);
  const [gated, setGated] = useState<IGated | null>(null);
  if (gated && level !== 'over') setGated(null);
  const guard = (targetKey: string, shownKey: string | null, open: () => void) => {
    if (gateDecision(level, targetKey, shownKey) === 'confirm') {
      setGated({ owner, open });
      return;
    }
    setGated(null);
    open();
  };
  const gate =
    gated && gated.owner === owner && snapshot.load && level === 'over' ? (
      <OverloadGate
        load={snapshot.load}
        onOpen={() => {
          setGated(null);
          gated.open();
        }}
        onCancel={() => setGated(null)}
      />
    ) : null;
  return { guard, gate };
};

interface IMetricRowProps {
  label: string;
  metric: TMetric;
  load: IMachineLoad;
  value: number;
  max: number;
  figure: string;
  note: string;
}

const MetricRow = ({ label, metric, load, value, max, figure, note }: IMetricRowProps) => (
  <div className="tw-mload-metric">
    <div className="tw-mload-metric-head">
      <span>{label}</span>
      <strong>{figure}</strong>
    </div>
    <TwMeter
      value={Math.min(value, max)}
      max={max}
      label={label}
      valueText={`${figure}, ${note}`}
      tone={METER_TONE[metricTone(metric, load)]}
    />
    <span className="tw-mload-metric-note">{note}</span>
  </div>
);

const ProcessRow = ({ proc, ...stop }: IStopControlProps) => (
  <li className="tw-mload-proc">
    <div className="tw-mload-proc-main">
      <span
        className="tw-mload-proc-command"
        title={proc.cwd ? `${proc.command}\n${proc.cwd}` : proc.command}
      >
        {proc.command}
      </span>
      <span className="tw-mload-proc-meta">
        <span>{processUsage(proc)}</span>
        {proc.port !== undefined && <span>{t('agents.machineLoad.port', { port: proc.port })}</span>}
        <span>{KIND_NOTE[proc.kind]}</span>
        {proc.stuck && <TwBadge tone={CHIP_TONE.warning}>{t('agents.machineLoad.stuck')}</TwBadge>}
      </span>
    </div>
    {proc.stoppable ? (
      <ProcessStopControl proc={proc} {...stop} />
    ) : (
      <span className="tw-mload-proc-locked" title={t('agents.machineLoad.protectedTitle')}>
        {t('agents.machineLoad.protected')}
      </span>
    )}
  </li>
);

const LoadDetail = ({
  load,
  notice,
  ...stop
}: IStopState & { load: IMachineLoad; notice: IMachineLoadNotice | null }) => (
  <div className="tw-mload-detail">
    <p className="tw-mload-summary" data-tone={LEVEL_TONE[load.level]}>
      {LEVEL_LABEL[load.level]}
      {load.reason && `: ${metricText(load.reason, load)}`}
    </p>
    <div className="tw-mload-metrics">
      <MetricRow
        label="CPU"
        metric="load"
        load={load}
        value={load.cpu.pct}
        max={100}
        figure={formatPct(load.cpu.pct)}
        note={t('agents.machineLoad.cpuNote', { ratio: loadRatioText(load), cores: load.cpu.cores })}
      />
      <MetricRow
        label={t('agents.machineLoad.memory')}
        metric="ram"
        load={load}
        value={load.mem.usedPct}
        max={100}
        figure={formatPct(load.mem.usedPct)}
        note={t('agents.machineLoad.memNote', {
          used: formatBytes(load.mem.swapUsedBytes),
          swapIn: swapInText(load.mem.swapInPerSec),
        })}
      />
      <MetricRow
        label={t('agents.machineLoad.disk')}
        metric="disk"
        load={load}
        value={load.disk.usedPct}
        max={100}
        figure={formatPct(load.disk.usedPct)}
        note={load.disk.path}
      />
    </div>
    <h3 className="tw-mload-heading">{t('agents.machineLoad.heavyProcesses')}</h3>
    <MachineLoadNotice notice={notice} />
    {load.top.length === 0 ? (
      <p className="tw-inspector-muted">{t('agents.machineLoad.noHeavyProcesses')}</p>
    ) : (
      <ul className="tw-mload-procs">
        {load.top.map((proc) => (
          <ProcessRow key={processKey(proc)} proc={proc} {...stop} />
        ))}
      </ul>
    )}
  </div>
);

/**
 * The machine's load next to its name in the header: a dot and the figure that matters, the details in a popover.
 * A skeleton until the machine's first answer, a muted «no data» once the load is stale.
 */
export const MachineLoadIndicator = ({ machine }: { machine: string }) => {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const { key, load, error, stale, stopping, stopped, notice, stop } = useMachineLoad(machine, {
    notify: true,
  });
  // Open for one machine only: switching the conversation to another machine closes it.
  const [openFor, setOpenFor] = useState<string | null>(null);
  const open = openFor === key;
  if (!load && !error)
    return (
      <span
        className="tw-mload tw-mload-skeleton tw-skeleton"
        role="status"
        aria-label={t('agents.machineLoad.loading')}
      />
    );
  const fresh = load && !stale ? load : null;
  const label = fresh
    ? `${LEVEL_LABEL[fresh.level]}, ${indicatorText(fresh)}`
    : `${t('agents.machineLoad.noLoadData')}${error ? `: ${error}` : ''}`;
  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className="tw-mload"
        data-tone={fresh ? LEVEL_TONE[fresh.level] : 'unknown'}
        aria-label={t('agents.machineLoad.indicatorAria', { label })}
        title={label}
        {...twPopoverTriggerProps(open, popoverId)}
        onClick={() => setOpenFor(open ? null : key)}
      >
        <span className="tw-mload-dot" aria-hidden="true" />
        <Activity aria-hidden="true" />
        <span className="tw-mload-text">{fresh ? indicatorText(fresh) : t('agents.machineLoad.noData')}</span>
      </button>
      <TwPopover
        id={popoverId}
        open={open}
        onClose={() => setOpenFor(null)}
        anchorRef={triggerRef}
        align={LAYER_ALIGN.start}
        title={t('agents.machineLoad.popoverTitle')}
        className="tw-mload-pop"
      >
        {!fresh && (
          <p className="tw-mload-notice tw-mload-notice-error" role="alert">
            {error ?? t('agents.machineLoad.stale')}
            {load && t('agents.machineLoad.lastKnownBelow')}
          </p>
        )}
        {load && (
          <LoadDetail
            load={load}
            notice={notice}
            stopping={stopping}
            stopped={stopped}
            onStop={(proc) => void stop(proc)}
          />
        )}
      </TwPopover>
    </>
  );
};
