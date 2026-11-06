// The four cards under the chart: what changed in the environment and what it did, what needs attention, where the
// money goes for nothing, the teams side by side. A bubble on hover tells where a number comes from; the bubbles are
// drawn by CSS, nothing here re-renders while the pointer moves.
import type { CSSProperties, ReactNode } from 'react';

import type { Change, DetailRow, Peers, Signal, Waste } from '../../../org/api/analytics';
import { goSignal } from '../../go';
import { useLive } from '../../live';
import { nextSort, readSort, sortRows, useParams } from '../../sort';
import { Icon } from '../../ui';
import {
  barWidths,
  changeTone,
  DASH,
  formatEffect,
  HEAT_NOTE,
  heatOf,
  money,
  peerCells,
  VERDICT_HINT,
  VERDICT_TONE,
  VERDICT_WORD,
  wasteLead,
  wasteView,
} from './calc';
import { teamHex, WASTE_HEX } from './colors';

const idx = (n: number): CSSProperties => ({ '--i': n }) as CSSProperties;

export const Card = ({
  title,
  count,
  lead,
  className,
  children,
}: {
  title: string;
  count?: number;
  lead?: string;
  className?: string;
  children: ReactNode;
}) => (
  <div className={`t-card viz-card dash-tile dash-span-12${className ? ` ${className}` : ''}`} role="region">
    <header className="viz-card__header">
      <div className="viz-card__heading">
        <div className="viz-card__title-row">
          <h3 className="viz-card__title">
            {count === undefined ? (
              <span className="mx-card-title">{title}</span>
            ) : (
              <span className="at-title">
                {title} <span className="at-title__count">{count}</span>
              </span>
            )}
          </h3>
        </div>
        {lead && <p className="viz-card__description">{lead}</p>}
      </div>
    </header>
    <div className="viz-card__body">{children}</div>
  </div>
);

/** A hover bubble: a title and rows of «label — value». */
const Bubble = ({
  title,
  rows,
  note,
  className,
}: {
  title: string;
  rows: readonly DetailRow[];
  note?: string;
  className?: string;
}) => (
  <span className={className ? `anx-pop ${className}` : 'anx-pop'}>
    <span className="viz-tip">
      <span className="viz-tip__title">{title}</span>
      {rows.map((r) => (
        <span key={r.label} className="viz-tip__row">
          <span />
          <span>{r.label}</span>
          <b>{r.value}</b>
        </span>
      ))}
      {note && <span className="viz-tip__note">{note}</span>}
    </span>
  </span>
);

const KIND_ICON = { tool: 'plug', model: 'cpu', plugins: 'layers' } as const;
const dayMonth = (at: string): string => `${at.slice(8, 10)}.${at.slice(5, 7)}`;

export const Changes = ({ changes }: { changes: readonly Change[] }) => (
  <Card
    title="Изменения и их эффект"
    lead="Что меняли в окружении и как сдвинулись метрики: неделя до против недели после"
  >
    {changes.length === 0 ? (
      <p className="an-empty">За этот период в окружении ничего не меняли</p>
    ) : (
      <ol className="ancs">
        {changes.map((c, n) => (
          <li key={c.id} className="anc" data-tone={changeTone(c.effects)} style={idx(n)}>
            <span className="anc__rail">
              <span className="anc__ic">
                <Icon i={KIND_ICON[c.kind]} size="sm" />
              </span>
            </span>
            <div className="anc__b">
              <div className="anc__h">
                <b>{c.title}</b>
                <span>{`${dayMonth(c.at)} · ${c.meta}`}</span>
              </div>
              <div className="anc__es">
                {c.effects.map((e) => {
                  const tone = VERDICT_TONE[e.verdict];
                  const [w0, w1] = barWidths(e.before, e.after);
                  return (
                    <span key={e.kpi} className="anc-e" data-tone={tone}>
                      <span className="anc-e__n">{e.title}</span>
                      <span className="anc-e__bars">
                        <i style={{ '--w': `${w0}%` } as CSSProperties} />
                        <i style={{ '--w': `${w1}%` } as CSSProperties} />
                      </span>
                      <span className="anc-e__v">
                        {formatEffect(e.format, e.before)} <span className="anc-e__arr">→</span>{' '}
                        <b>{formatEffect(e.format, e.after)}</b>
                      </span>
                      <span className="an-chip anc-chip" data-st={tone} title={VERDICT_HINT[e.verdict]}>
                        {VERDICT_WORD[e.verdict]}
                      </span>
                    </span>
                  );
                })}
              </div>
            </div>
          </li>
        ))}
      </ol>
    )}
  </Card>
);

export const Attention = ({ signals, base }: { signals: readonly Signal[]; base: string }) => (
  <Card title="На что обратить внимание" count={signals.length} className="mx-attention">
    {signals.length === 0 ? (
      <p className="an-empty">Сейчас ничего не требует внимания</p>
    ) : (
      <div className="at-grid" style={{ '--at-cols': 1 } as CSSProperties}>
        {signals.map((s, n) => {
          const go = s.action;
          const Tag = go ? 'button' : 'div'; // no action: a row to read, not a dead click
          return (
            <Tag
              key={s.id}
              {...(go ? { type: 'button' as const, onClick: () => goSignal(base, go) } : {})}
              className="at-cell anx-hov"
              data-tone={s.tone === 'high' ? 'risk' : 'warn'}
              style={{ '--i': n, '--at-span': 1, '--j': 0, '--jr': 0 } as CSSProperties}
            >
              <span className="at-cell__dot" aria-hidden="true" />
              <span className="at-cell__text">
                <b className="at-cell__title">{s.title}</b>
                <span className="at-cell__detail">{s.text}</span>
              </span>
              {go && (
                <span className="at-cell__action">
                  {go.label}
                  <Icon i="arrow-up-right" size="sm" />
                </span>
              )}
              {s.detail && s.detail.length > 0 && (
                <Bubble className="anx-pop--att" title={s.title} rows={s.detail} />
              )}
            </Tag>
          );
        })}
      </div>
    )}
  </Card>
);

export const WasteCard = ({ waste }: { waste: Waste }) => (
  <Card title="Куда уходят деньги впустую" lead={`${wasteLead(waste)}. Наведи на строку — откуда цифра`}>
    {waste.items.length === 0 ? (
      <p className="an-empty">Заметных потерь за период нет</p>
    ) : (
      <>
        <div className="anw-bar">
          {waste.items.map((w, n) => (
            <span
              key={w.id}
              className="anw-seg anx-hov"
              style={
                {
                  '--w': `${wasteView(waste, w.usd).segment.toFixed(2)}%`,
                  '--c': WASTE_HEX[w.tone],
                  '--i': n,
                } as CSSProperties
              }
            />
          ))}
        </div>
        <ul className="anw">
          {waste.items.map((w, n) => (
            <li key={w.id} className="anw-r anx-hov" style={idx(n)}>
              <span className="anw-r__sw" style={{ '--c': WASTE_HEX[w.tone] } as CSSProperties} />
              <span className="anw-r__t">
                <b>{w.title}</b>
                <small>{w.text}</small>
              </span>
              <span className="anw-r__v">
                {money(w.usd)}
                <small>{`${wasteView(waste, w.usd).share}% расхода`}</small>
              </span>
              {w.detail && w.detail.length > 0 && (
                <Bubble title={`${w.title} · ${money(w.usd)}`} rows={w.detail} />
              )}
            </li>
          ))}
        </ul>
      </>
    )}
  </Card>
);

const MEAN = 'Среднее по тенанту';

export const PeersCard = ({ peers, teamOrder }: { peers: Peers; teamOrder: readonly string[] }) => {
  const live = useLive();
  const p = useParams();
  const keys = ['name', ...peers.columns.map((_, i) => `c${i}`)];
  const sort = readSort(p.search, keys, { key: null, dir: 'desc' });
  const pick = (k: string) => {
    const n = nextSort(sort, k, k === 'name' ? 'asc' : 'desc');
    p.set({ sort: n.key, dir: n.dir });
  };
  const rows = live
    ? sortRows(peers.rows, sort, {
        name: (r) => r.name,
        ...Object.fromEntries(
          peers.columns.map((_, i) => [
            `c${i}`,
            (r: Peers['rows'][number]) => peerCells(r, peers.columns)[i]?.score ?? 0,
          ]),
        ),
      })
    : peers.rows;
  const head = (k: string, label: ReactNode) =>
    live ? (
      <button
        type="button"
        className="live-sort"
        aria-sort={sort.key === k ? (sort.dir === 'asc' ? 'ascending' : 'descending') : undefined}
        onClick={() => pick(k)}
      >
        {label}
        <i aria-hidden="true">↓</i>
      </button>
    ) : (
      label
    );
  return (
    <Card title={peers.title} lead={peers.sub}>
      <div
        className="anh"
        style={
          peers.columns.length === 8
            ? undefined
            : { gridTemplateColumns: `minmax(110px,1.2fr) repeat(${peers.columns.length},minmax(0,1fr))` }
        }
      >
        <span>{head('name', live ? 'Команда' : null)}</span>
        {peers.columns.map((c, i) => (
          <span key={c} className="anh__c">
            {head(`c${i}`, c)}
          </span>
        ))}
        {rows.flatMap((r, ri) => [
          <span key={`${r.id}-name`} className="anh__r">
            <span className="anh__sw" style={{ '--c': teamHex(teamOrder, r.name) } as CSSProperties} />
            {r.name}
          </span>,
          ...peerCells(r, peers.columns).map((cell, ci) => {
            const heat = heatOf(cell.score);
            const col = peers.columns[ci] ?? DASH;
            return (
              <span
                key={`${r.id}-${col}`}
                className="anh__x anx-hov"
                data-t={heat}
                style={idx(ri * peers.columns.length + ci)}
              >
                {cell.text}
                <Bubble
                  title={`${r.name} · ${col}`}
                  rows={[
                    { label: 'Команда', value: cell.text },
                    { label: MEAN, value: cell.mean ?? DASH },
                  ]}
                  note={HEAT_NOTE[heat]}
                />
              </span>
            );
          }),
        ])}
      </div>
      <div className="anh-leg">
        <small>хуже среднего</small>
        <span data-t="r2" />
        <span data-t="r1" />
        <span data-t="n" />
        <span data-t="g1" />
        <span data-t="g2" />
        <small>лучше среднего</small>
      </div>
    </Card>
  );
};
