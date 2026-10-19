import { Badge, Button, Dot, Meter, Popup, Skeleton } from '@kit';
import { Cpu } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';

import { machine } from '../api/endpoints';
import { HttpError } from '../api/http';
import type { LimitWindow, MachineLoad, MachineSnapshot, ProviderLimits } from '../api/types';
import { formatBytes, formatPct, formatUntil, formatUptime } from '../lib/format';
import { LEVEL_LABEL, LEVEL_TONE, loadPerCore, meterSteps, stopOutcome } from '../lib/machine-load';
import { reportError } from '../stores/runtime';

const TONE_TO_DOT = { success: 'success', warning: 'warning', danger: 'danger' } as const;

function usePoll<T>(fn: () => Promise<T>, everyMs: number, enabled = true) {
  const [value, setValue] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    const load = () =>
      fn().then(
        (v) => alive && (setValue(v), setError(null)),
        (e: unknown) => alive && setError(e instanceof Error ? e.message : String(e)),
      );
    void load();
    const t = setInterval(load, everyMs);
    return () => {
      alive = false;
      clearInterval(t);
    };
    // fn is a stable module function
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [everyMs, enabled, tick]);
  return { value, error, refresh: () => setTick((t) => t + 1) };
}

function LimitRow({ title, w }: { title: string; w: LimitWindow | null | undefined }) {
  if (!w || w.usedPct === null) return null;
  const exhausts = w.forecast?.status === 'exhausts_before_reset';
  return (
    <Meter
      label={title}
      value={w.usedPct / 100}
      detail={`${formatPct(w.usedPct)}${w.resetsAt ? ` · resets ${formatUntil(w.resetsAt)}` : ''}${exhausts ? ' · runs out before' : ''}`}
    />
  );
}

function Limits({ name, limits }: { name: string; limits: ProviderLimits | null }) {
  if (!limits) return null;
  return (
    <div className="machine__group">
      <div className="machine__subhead">
        {name} {limits.plan && <Badge look="outline">{limits.plan}</Badge>}
      </div>
      {limits.error && <div className="machine__note">{limits.error}</div>}
      <LimitRow title="5 hours" w={limits.fiveHour} />
      <LimitRow title="7 days" w={limits.sevenDay} />
    </div>
  );
}

function Panel({ load, onStopped }: { load: MachineLoad | null; onStopped: () => void }) {
  const snap = usePoll(() => machine.snapshot(), 30_000);
  const me: MachineSnapshot | undefined = snap.value?.machines[0];
  const [stopping, setStopping] = useState<string | null>(null);

  const stop = async (pid: number, starttime: number) => {
    setStopping(`${pid}:${starttime}`);
    try {
      await machine.stop(pid, starttime);
      onStopped();
    } catch (err) {
      const o = err instanceof HttpError ? stopOutcome(err.status, err.body) : null;
      if (o?.kind === 'refresh') onStopped();
      reportError(o && o.kind !== 'stopped' ? new Error(o.message) : err);
    } finally {
      setStopping(null);
    }
  };

  return (
    <div className="machine" data-testid="machine-panel">
      <div className="machine__head">
        <div>
          <div className="machine__title">{me?.name ?? 'This machine'}</div>
          <div className="machine__note">
            {me ? `${me.user}@${me.host} · up ${formatUptime(me.uptimeSec)}` : <Skeleton width="10rem" />}
          </div>
        </div>
        {load && (
          <Badge
            tone={
              LEVEL_TONE[load.level] === 'success'
                ? 'success'
                : LEVEL_TONE[load.level] === 'warning'
                  ? 'warning'
                  : 'danger'
            }
          >
            {LEVEL_LABEL[load.level]}
          </Badge>
        )}
      </div>
      {load ? (
        <div className="machine__group">
          <Meter
            label="CPU load"
            value={Math.min(loadPerCore(load) / 3, 1)}
            warnAt={0.5}
            dangerAt={1}
            detail={`${loadPerCore(load).toFixed(2)} per core · ${load.cpu.cores} cores · ${formatPct(load.cpu.pct)}`}
          />
          <Meter
            label="Memory"
            value={load.mem.usedPct / 100}
            {...meterSteps('ram')}
            detail={`${formatPct(load.mem.usedPct)}${me ? ` of ${me.memTotalGb} GB` : ''}${load.mem.swapUsedBytes ? ` · swap ${formatBytes(load.mem.swapUsedBytes)}` : ''}`}
          />
          <Meter
            label="Disk"
            value={load.disk.usedPct / 100}
            {...meterSteps('disk')}
            detail={`${formatPct(load.disk.usedPct)} · ${load.disk.path}`}
          />
        </div>
      ) : (
        <div className="machine__group">
          <Skeleton height="2rem" />
          <Skeleton height="2rem" />
        </div>
      )}
      {me && (me.limits.claude || me.limits.codex) && (
        <>
          <Limits name="Claude" limits={me.limits.claude} />
          <Limits name="Codex" limits={me.limits.codex} />
        </>
      )}
      {load && load.top.length > 0 && (
        <div className="machine__group">
          <div className="machine__subhead">Heaviest processes</div>
          <ul className="procs">
            {load.top.slice(0, 8).map((p) => (
              <li key={`${p.pid}:${p.starttime}`} className="proc">
                <div className="proc__main">
                  <span className="proc__cmd" title={p.command}>
                    {p.command}
                  </span>
                  <span className="proc__meta">
                    {p.kind}
                    {p.port ? ` · :${p.port}` : ''} · CPU {formatPct(p.cpuPct)} · {formatBytes(p.rssBytes)}
                  </span>
                </div>
                {p.stoppable && (
                  <Button
                    size="xs"
                    appearance="ghost"
                    loading={stopping === `${p.pid}:${p.starttime}`}
                    onClick={() => void stop(p.pid, p.starttime)}
                  >
                    Stop
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
      {me?.release && <div className="machine__note">Runtime {me.release.version}</div>}
    </div>
  );
}

/** The machine's load next to the header actions; it opens the machine panel. */
export function MachineButton() {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLButtonElement>(null);
  const load = usePoll(() => machine.load(), open ? 5000 : 15_000);
  const l = load.value;
  return (
    <>
      <button
        ref={anchor}
        type="button"
        className="machine-btn"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        data-testid="machine-button"
      >
        <Cpu size={14} />
        {l ? (
          <>
            <Dot tone={TONE_TO_DOT[LEVEL_TONE[l.level]]} />
            <span>RAM {formatPct(l.mem.usedPct)}</span>
          </>
        ) : (
          <span>{load.error ? 'Load unknown' : 'Machine'}</span>
        )}
      </button>
      <Popup
        anchor={anchor}
        open={open}
        onClose={() => setOpen(false)}
        placement={{ side: 'bottom', align: 'end' }}
        role="dialog"
        className="machine-popup"
      >
        <Panel load={l} onStopped={load.refresh} />
      </Popup>
    </>
  );
}
