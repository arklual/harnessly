// The Machine tool (.hx-mach, design Главная.dc.html «Машина»): how loaded the machine is, as tiles, an hour of load and
// network, the cores, the disk and the heaviest processes, each with a button to stop it unless the machine needs it.
// The figures come from the machine every two seconds; this view only draws them.
import { Fragment, useMemo } from 'react';
import { t } from '@/next/i18n';
import { Skeleton } from '@/next/ui/skeleton';
import { useLeavingList } from '@/next/ui/presence';
import type { TPending } from '@/next/ui/use-pending-list';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import Swap from '@/next/ui/swap';
import type { MachineProc, MachineStats } from '@/next/protocol/v2';
import { procKey } from '@/next/features/pane/machine/api';
import { slot, type IKpi, type IPlot } from '@/next/features/pane/machine/charts';
import { buildMachine, procRow } from '@/next/features/pane/machine/model';
import { vars } from '@/next/features/pane/machine/vars';
import '@/next/styles/charts.css';

/** A stopped process leaves its row this long. */
const ROW_EXIT_MS = 220;
const SKELETON_TILES = 4;

export interface IMachineViewProps {
  /** The machine's name, said beside the level. */
  name: string;
  /** null while it loads. */
  stats: MachineStats | null;
  error: string | null;
  onRetry: () => void;
  /** The processes being stopped, by key. */
  pending: ReadonlyMap<string, TPending>;
  /** Stopped processes the machine may still list for a moment. */
  hidden: ReadonlySet<string>;
  onStop: (proc: MachineProc) => void;
}

const Kpi = ({ k }: { k: IKpi }) => (
  <div className="hx-ch-kpi" ref={vars(k.vars)}>
    <span className="hx-ch-kpi__label">{k.label}</span>
    <span className="hx-ch-kpi__value">
      {k.value}
      <small>{k.unit}</small>
    </span>
    <span className="hx-ch-kpi__foot">
      {k.delta && (
        <span className="hx-ch-delta" data-tone={k.delta.tone} data-dir={k.delta.dir}>
          {k.delta.text}
        </span>
      )}
      <span>{k.sub}</span>
    </span>
    {k.spark && (
      <div className="hx-ch-sline">
        <svg viewBox="0 0 120 32" preserveAspectRatio="none" aria-hidden="true">
          <path className="hx-ch-sline__area" d={k.spark.areaD} />
          <path className="hx-ch-sline__line" d={k.spark.lineD} />
        </svg>
        <i className="hx-ch-sline__end" ref={vars(k.spark.end)} />
      </div>
    )}
  </div>
);

const Legend = ({ items }: { items: readonly { label: string; slot: number }[] }) => (
  <div className="hx-ch-legend">
    {items.map((x) => (
      <span key={x.label} className="hx-ch-legend__item" ref={vars({ c: slot(x.slot) })}>
        <i />
        <span className="hx-ch-legend__l">{x.label}</span>
      </span>
    ))}
  </div>
);

const Plot = ({ p, small }: { p: IPlot; small?: boolean }) => (
  <div className="hx-ch-frame" data-size={small ? 'sm' : undefined} ref={vars({ rows: p.rows })}>
    <div className="hx-ch-frame__y">
      {p.ys.map((y, i) => (
        <span key={i}>{y}</span>
      ))}
    </div>
    <div className="hx-ch-frame__body">
      <div className="hx-ch-plot" role="img" aria-label={p.layers.map((x) => x.label).join(', ')}>
        {p.refs.map((r) => (
          <div key={r.label} className="hx-ch-plot__ref" ref={vars({ y: r.y })}>
            <span>{r.label}</span>
          </div>
        ))}
        {p.layers.map((l) => (
          <Fragment key={l.label}>
            <svg
              className="hx-ch-plot__fill"
              viewBox="0 0 600 160"
              preserveAspectRatio="none"
              ref={vars({ c: l.c })}
            >
              <path d={l.areaD} />
            </svg>
            <svg
              className="hx-ch-plot__line"
              viewBox="0 0 600 160"
              preserveAspectRatio="none"
              ref={vars({ c: l.c })}
            >
              <path d={l.lineD} />
            </svg>
          </Fragment>
        ))}
        {p.cols.map((c) => (
          <div key={c.key} className="hx-ch-hit" data-side={c.side} ref={vars(c.vars)}>
            {c.dots.map((d, i) => (
              <i key={i} className="hx-ch-hit__dot" ref={vars(d)} />
            ))}
            <span className="hx-ch-tip" role="tooltip">
              <b className="hx-ch-tip__t">{c.title}</b>
              {c.rows.map((r) => (
                <span key={r.label} className="hx-ch-tip__row" ref={vars({ c: r.c })}>
                  <i />
                  <span>{r.label}</span>
                  <b>{r.value}</b>
                </span>
              ))}
            </span>
          </div>
        ))}
        {p.cols.length === 0 && <div className="hx-ch-empty">{t('next.machine.noHistory')}</div>}
      </div>
    </div>
    <div className="hx-ch-frame__x">
      {p.xs.map((x) => (
        <span key={x}>{x}</span>
      ))}
    </div>
  </div>
);

const Loading = () => (
  <div className="hx-mach" role="status" aria-label={t('next.machine.toolLoading')}>
    <>
      <Skeleton className="hx-mach__sk-line" />
      <div className="hx-ch-kpis" data-box="true">
        {Array.from({ length: SKELETON_TILES }, (_, i) => (
          <Skeleton key={i} className="hx-mach__sk-tile" />
        ))}
      </div>
      <Skeleton className="hx-mach__sk-chart" />
      <Skeleton className="hx-mach__sk-line" />
      <Skeleton className="hx-mach__sk-line" />
    </>
  </div>
);

const MachineView = ({ name, stats, error, onRetry, pending, hidden, onStop }: IMachineViewProps) => {
  const model = useMemo(() => (stats ? buildMachine(stats) : null), [stats]);
  const procs = useMemo(
    () => (stats ? stats.procs.filter((x) => !hidden.has(procKey(x))) : []),
    [stats, hidden],
  );
  const rows = useLeavingList(procs, procKey, ROW_EXIT_MS);

  const disk = model?.disk;
  const body = model && (
    <div className="hx-mach">
      <div className="hx-tv__sum">
        <span className="hx-lvl" data-tone={model.level.tone} />
        <span className="hx-tv__name">{model.level.text}</span>
        <span className="hx-spacer" />
        <span className="hx-tv__muted">{name}</span>
      </div>
      {error !== null && (
        <LoadFailed
          size={LOAD_FAILED_SIZE.inline}
          title={t('next.load.machine')}
          reason={error}
          onRetry={onRetry}
        />
      )}
      <div className="hx-ch-kpis" data-box="true">
        {model.kpis.map((k) => (
          <Kpi key={k.label} k={k} />
        ))}
      </div>
      <div className="hx-tv__label">{t('next.machine.loadHour')}</div>
      <Legend items={model.load.legend} />
      <Plot p={model.load.plot} />
      <div className="hx-tv__label">{t('next.machine.coresTitle')}</div>
      <div className="hx-ch-heat">
        {model.cores.map((c, i) => (
          <i key={i} ref={vars(c.vars)} data-tip={c.tip} />
        ))}
      </div>
      <div className="hx-tv__label">{t('next.machine.netTitle')}</div>
      <Legend items={model.net.legend} />
      <Plot p={model.net.plot} small />
      {disk && (
        <>
          <div className="hx-tv__label">{disk.title}</div>
          <div className="hx-ch-stack">
            {disk.parts.map((p) => (
              <div key={p.id} className="hx-ch-stack__part" data-free={p.free} ref={vars(p.vars)}>
                <span className="hx-ch-tip" role="tooltip">
                  <span className="hx-ch-tip__row">
                    <i />
                    <span>{p.label}</span>
                    <b>{p.text}</b>
                  </span>
                </span>
              </div>
            ))}
          </div>
          <div className="hx-ch-legend">
            {disk.parts.map((p) => (
              <span key={p.id} className="hx-ch-legend__item" data-free={p.free} ref={vars(p.vars)}>
                <i />
                <span className="hx-ch-legend__l">{p.label}</span>
                <b>{p.text}</b>
              </span>
            ))}
          </div>
          {disk.note !== null && <p className="hx-tv__note">{disk.note}</p>}
        </>
      )}
      <div className="hx-tv__label">{t('next.machine.procsTitle')}</div>
      {rows.length === 0 && <div className="hx-tv__empty">{t('next.machine.procsEmpty')}</div>}
      <div className="hx-ch-list">
        {rows.map(({ item, leaving }, i) => {
          const row = procRow(item, i);
          const busy = pending.has(row.key);
          return (
            <div
              key={row.key}
              className="hx-ch-list__row hx-mach__proc"
              data-tone={row.tone || undefined}
              data-leaving={leaving || undefined}
              ref={vars(row.vars)}
            >
              <span className="hx-ch-list__fill" />
              <span className="hx-ch-list__main">
                <span className="hx-ch-list__label">{row.label}</span>
                <span className="hx-ch-list__sub">{row.sub}</span>
              </span>
              <span className="hx-ch-list__val">{row.text}</span>
              <span className="hx-tv__num">{row.mem}</span>
              {item.guard ? (
                <span className="hx-tv__guard">
                  <Icon name="lock" size="sm" />
                  {t('next.machine.guard')}
                </span>
              ) : (
                <button
                  type="button"
                  className="hx-tv__btn"
                  aria-label={t('next.machine.stopFor', { name: row.label })}
                  disabled={busy || leaving}
                  data-busy={busy ? pending.get(row.key) : undefined}
                  onClick={() => onStop(item)}
                >
                  {busy ? t('next.machine.stopping') : t('next.machine.stop')}
                </button>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
  return (
    <div className="hx-tv" data-tool="machine">
      {stats === null && error !== null ? (
        <LoadFailed title={t('next.load.machine')} reason={error} onRetry={onRetry} />
      ) : (
        <Swap ready={model !== null} skeleton={<Loading />}>
          {body}
        </Swap>
      )}
    </div>
  );
};

export default MachineView;
