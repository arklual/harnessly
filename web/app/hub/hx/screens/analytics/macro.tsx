// The blocks of the «Аналитика» board in its own markup (`an-*`): the strip of figures, «Требует внимания», «Что меняли
// и что это дало», the waste rows and the heat table. They only draw what the hub answered; a figure the hub has no
// number for is not drawn.
import type { ReactNode } from 'react';

import type { Change, Kpi, KpiId, Peers, Signal, Waste } from '../../../org/api/analytics';
import { goSignal } from '../../go';
import { Icon } from '../../ui';
import {
  changeSize,
  DASH,
  deltaOf,
  formatEffect,
  formatKpi,
  heatOf,
  money,
  peerCells,
  ru,
  wasText,
  type Heat,
  type Tone,
} from './calc';

/** The board's card: `card(x)`. */
export const MCard = ({ cls, part, children }: { cls?: string; part?: string; children: ReactNode }) => (
  <div className={`t-card viz-card dash-tile hd-card${cls ? ` ${cls}` : ''}`} data-hx-part={part}>
    {children}
  </div>
);

const Help = ({ hint }: { hint: string }) => (
  <span className="an-qi" title={hint}>
    <Icon i="info" size="sm" />
  </span>
);

export interface StripCell {
  id: string;
  label: string;
  value: string;
  /** Null: there is nothing to tell the change by. */
  delta: { text: string; tone: Tone } | null;
  sub: string | null;
  hint: string;
  /** The metric whose chart the cell opens; null: the cell only reads. */
  go: KpiId | null;
  empty?: boolean;
}

/** «+13,5%», «−5 п.»: the change of a tile as the board writes it. */
const signed = (kpi: Kpi): { text: string; tone: Tone } | null => {
  const size = changeSize(kpi);
  const d = deltaOf(kpi);
  if (size === null) return null;
  if (d.tone === 'flat') return { text: 'как раньше', tone: 'flat' };
  const sign = size > 0 ? '+' : '−';
  const abs = Math.abs(size);
  const body =
    kpi.delta === 'rel' ? `${ru(abs, 0, 1)}%` : kpi.delta === 'pts' ? `${ru(abs, 0, 0)} п.` : ru(abs, 0, 1);
  return { text: `${sign}${body}`, tone: d.tone };
};

export const kpiCell = (k: Kpi): StripCell => ({
  id: k.id,
  label: k.title,
  value: formatKpi(k.format, k.value),
  delta: signed(k),
  sub: k.value === null ? null : wasText(k),
  hint: k.hint,
  go: k.id,
  empty: k.value === null,
});

export const Strip = ({ cells, onGo }: { cells: readonly StripCell[]; onGo: (id: KpiId) => void }) => {
  if (cells.length === 0) return null;
  return (
    <div className="t-card hd-card an-strip">
      {cells.map((c) => {
        const Tag = c.go ? 'button' : 'div';
        const tone = c.delta?.tone;
        return (
          <Tag
            key={c.id}
            className="an-kc"
            title={c.go ? `${c.hint} Нажмите — к графику.` : c.hint}
            {...(c.go ? { type: 'button' as const, onClick: () => c.go && onGo(c.go) } : {})}
          >
            <span className="an-kc__l">
              {c.label}
              <Icon i="info" size="sm" />
            </span>
            <b className={`an-kc__v num${tone === 'good' ? ' is-ok' : tone === 'bad' ? ' is-bad' : ''}`}>
              {c.value}
            </b>
            <span className="an-kc__d">
              {c.delta && (
                <span
                  className={`an-d ${c.delta.tone === 'good' ? 'ok' : c.delta.tone === 'bad' ? 'bad' : ''}`}
                >
                  {c.delta.text}
                </span>
              )}
              {c.sub && (
                <span className="mut2">
                  {c.delta ? ' · ' : ''}
                  {c.sub}
                </span>
              )}
            </span>
          </Tag>
        );
      })}
    </div>
  );
};

export const AttentionCard = ({ signals, base }: { signals: readonly Signal[]; base: string }) => {
  if (signals.length === 0) return null;
  return (
    <MCard cls="an-attc">
      <div className="ch">
        <h3>
          Требует внимания <span className="an-cnt">{signals.length}</span>
        </h3>
      </div>
      <div className="an-att">
        {signals.map((s) => {
          const go = s.action;
          const Tag = go ? 'button' : 'div';
          return (
            <Tag
              key={s.id}
              className="an-att__i"
              title={s.text}
              {...(go ? { type: 'button' as const, onClick: () => goSignal(base, go) } : {})}
            >
              <span className={`an-att__dot is-${s.tone === 'high' ? 'bad' : 'warn'}`} />
              <span className="an-att__t">
                <b>{s.title}</b>
                <span className="mut2 sm">{s.text}</span>
              </span>
              {go && (
                <span className="an-att__go">
                  {go.label} <Icon i="arrow-up-right" size="sm" />
                </span>
              )}
            </Tag>
          );
        })}
      </div>
    </MCard>
  );
};

const KIND_ICON = { tool: 'plug', model: 'cpu', plugins: 'layers' } as const;
const KIND_WORD = { tool: 'Инструмент', model: 'Модель', plugins: 'Плагины' } as const;
const VERDICT = {
  better: ['ok', 'помогло'],
  worse: ['bad', 'навредило'],
  mixed: ['warn', 'и лучше, и хуже'],
  noise: ['', 'в пределах шума'],
} as const;
const dayMonth = (at: string): string => `${at.slice(8, 10)}.${at.slice(5, 7)}`;

const verdictOf = (c: Change): keyof typeof VERDICT => {
  const b = c.effects.some((e) => e.verdict === 'better');
  const w = c.effects.some((e) => e.verdict === 'worse');
  return b && w ? 'mixed' : w ? 'worse' : b ? 'better' : 'noise';
};

export const ChangesCard = ({
  changes,
  selected,
  onPick,
  onAll,
  limit = 6,
}: {
  changes: readonly Change[];
  selected?: string | null;
  onPick?: (id: string) => void;
  onAll: () => void;
  limit?: number;
}) => (
  <MCard cls="an-chgc" part="changes">
    <div className="ch">
      <h3 className="an-q">
        Что меняли и что это дало
        <Help hint="Модели, версии Claude Code, плагины, память, правила. Неделя до против недели после." />
      </h3>
      <div className="hd-r">
        {onPick && <span className="mut2 sm an-hide-s">нажмите строку — график покажет до и после</span>}
        <button type="button" className="md-link hd-link" onClick={onAll}>
          Все изменения
        </button>
      </div>
    </div>
    {changes.length === 0 ? (
      <p className="mut2 sm">За этот период в окружении ничего не меняли</p>
    ) : (
      <div className="an-tl">
        {changes.slice(0, limit).map((c) => {
          const v = verdictOf(c);
          const f = c.effects[0];
          const Tag = onPick ? 'button' : 'div';
          return (
            <Tag
              key={c.id}
              className={`an-tl__i${selected === c.id ? ' is-sel' : ''}`}
              {...(onPick ? { type: 'button' as const, onClick: () => onPick(c.id) } : {})}
            >
              <span className={`an-tl__ic an-mk--${v}`}>
                <Icon i={KIND_ICON[c.kind]} size="sm" />
              </span>
              <span className="an-tl__w">
                <span className="mut2 sm">
                  {KIND_WORD[c.kind]} · {dayMonth(c.at)}
                </span>
                <b>{c.title}</b>
                <span className="mut sm">{c.meta}</span>
              </span>
              <span className="an-tl__m">
                {f && (
                  <>
                    <span className="mut2 sm">{f.title}</span>
                    <span className="num">
                      {formatEffect(f.format, f.before)} → {formatEffect(f.format, f.after)}
                    </span>
                  </>
                )}
              </span>
              <span className={`pill ${VERDICT[v][0]}`}>{VERDICT[v][1]}</span>
            </Tag>
          );
        })}
      </div>
    )}
  </MCard>
);

const WASTE_COLOR = { violet: '#8968f4', rose: '#e05a7a', amber: '#e0a13a', teal: '#25a897' } as const;

export const WasteRows = ({ waste }: { waste: Waste }) => (
  <MCard cls="an-waste">
    <div className="ch">
      <h3 className="an-q">
        Что с этим делать
        <Help hint="Куда уходят деньги впустую: у каждой причины своя доля расхода." />
      </h3>
      <div className="hd-r">
        <span className="mut2 sm">
          {money(waste.wasteUsd)} из {money(waste.totalUsd)} за период
        </span>
      </div>
    </div>
    {waste.items.length === 0 ? (
      <p className="mut2 sm">Заметных потерь за период нет</p>
    ) : (
      <div className="an-rows">
        {waste.items.map((w) => (
          <div
            key={w.id}
            className="an-row an-row--w"
            title={w.detail?.map((d) => `${d.label}: ${d.value}`).join('\n')}
          >
            <span className="an-sw" style={{ background: WASTE_COLOR[w.tone] }} />
            <span className="an-row__n">
              <b>{w.title}</b>
              <span className="mut2 sm">{w.text}</span>
            </span>
            <span className="num an-row__v">
              {money(w.usd)}
              <span className="mut2 sm">
                {waste.totalUsd > 0 ? Math.round((w.usd / waste.totalUsd) * 100) : 0}% расхода
              </span>
            </span>
          </div>
        ))}
      </div>
    )}
  </MCard>
);

const LEVEL: Record<Heat, number> = { g2: 2, g1: 1, n: 0, r1: -1, r2: -2 };

export const HeatCard = ({
  peers,
  kpis,
  onGo,
}: {
  peers: Peers;
  kpis: readonly Kpi[];
  onGo: (id: KpiId) => void;
}) => (
  <MCard cls="an-heatc">
    <div className="ch">
      <h3 className="an-q">
        {peers.title}
        <Help hint={`${peers.sub} Клетка — значение за период против среднего по организации.`} />
      </h3>
    </div>
    <div className="an-heatw">
      <table className="an-heat">
        <thead>
          <tr>
            <th />
            {peers.columns.map((c) => (
              <th key={c}>{c}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {peers.rows.map((r) => (
            <tr key={r.id}>
              <th>
                <span className="an-hrow">{r.name}</span>
              </th>
              {peerCells(r, peers.columns).map((cell, i) => {
                const col = peers.columns[i] ?? DASH;
                const l = LEVEL[heatOf(cell.score)];
                const kpi = kpis.find((k) => k.title === col);
                return (
                  <td key={col}>
                    <button
                      type="button"
                      className="an-cell"
                      data-l={l}
                      title={`${r.name} · ${col}: ${cell.text}, среднее ${cell.mean ?? DASH}`}
                      onClick={kpi ? () => onGo(kpi.id) : undefined}
                      style={kpi ? undefined : { cursor: 'default' }}
                    >
                      {cell.text}
                    </button>
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
    <div className="an-scale mut2 sm">
      хуже среднего
      <i data-l="-2" />
      <i data-l="-1" />
      <i data-l="0" />
      <i data-l="1" />
      <i data-l="2" />
      лучше среднего
    </div>
  </MCard>
);
