// SPDX-License-Identifier: Apache-2.0
import type { ReactNode } from 'react';

import { X } from 'lucide-react';

import { classNames } from '../internal/cx';
import { VizIconButton } from '../internal/icon-button';
import { VizBadge, type VizBadgeTone } from '../internal/badge';
import { formatDelta, formatNumber, formatValue, type VizUnit } from '../model/format';
import type { VizZone } from '../model/types';
import { VizSwatch } from './primitives';

import '../viz.css';

/** Изменение со стрелкой и знаком; цвет — только подкрепление, смысл несут стрелка и знак. */
export function VizDelta({
  pct,
  suffix,
  className,
}: {
  readonly pct: number;
  readonly suffix?: string;
  readonly className?: string;
}) {
  if (!Number.isFinite(pct)) return null;
  return (
    <em className={classNames('viz-delta', pct >= 0 ? 'viz-delta--up' : 'viz-delta--down', className)}>
      {formatDelta(pct)}
      {suffix ? ` ${suffix}` : null}
    </em>
  );
}

const zoneTone = (zone: VizZone): VizBadgeTone =>
  zone.tone && zone.tone !== 'neutral' ? zone.tone : 'purple';

export interface WaveTooltipRow {
  readonly id: string;
  readonly label: string;
  readonly color: string;
  /** `NaN` — нет данных. */
  readonly value: number;
}

export interface WaveTooltipLabels {
  readonly current: string;
  readonly total: string;
  readonly empty: string;
  /** Подсказка, когда курсор на конкретной серии. */
  readonly pick: string;
  readonly more: (count: number) => string;
}

export const DEFAULT_WAVE_TOOLTIP_LABELS: WaveTooltipLabels = {
  current: 'идёт сейчас',
  total: 'Всего',
  empty: 'Нет данных за этот отрезок',
  pick: 'Клик — подробнее',
  more: (count) => `и ещё ${count}`,
};

export interface WaveTooltipProps {
  /** Отрезок: «7 окт, вторник». */
  readonly title: string;
  /** Отрезок ещё идёт. */
  readonly current?: boolean;
  readonly zone?: VizZone | null;
  readonly rows: readonly WaveTooltipRow[];
  readonly unit: VizUnit;
  /** Доли от суммы вместо значений. */
  readonly percent?: boolean;
  /** Серия под курсором — строка выделена. */
  readonly focusId?: string | null;
  /** Сумма прошлого отрезка — для «↑ +12%». */
  readonly previousTotal?: number;
  /** «к пред. дню». */
  readonly deltaSuffix?: string;
  /** Изменения внутри отрезка. */
  readonly notes?: readonly { readonly id: string; readonly label: string; readonly icon?: ReactNode }[];
  readonly maxRows?: number;
  readonly labels?: Partial<WaveTooltipLabels>;
  /** Подсказка закреплена кликом: крестик в заголовке. */
  readonly onClose?: () => void;
  readonly closeLabel?: string;
}

/** Содержимое подсказки отрезка: серии по убыванию, сумма с изменением, зона и события. */
export function WaveTooltip({
  title,
  current,
  zone,
  rows,
  unit,
  percent = false,
  focusId,
  previousTotal,
  deltaSuffix,
  notes,
  maxRows = 8,
  labels,
  onClose,
  closeLabel = 'Закрыть',
}: WaveTooltipProps) {
  const l = { ...DEFAULT_WAVE_TOOLTIP_LABELS, ...labels };
  const empty = rows.every((r) => r.value !== r.value);
  const total = rows.reduce((a, r) => a + (r.value === r.value ? r.value : 0), 0);
  const sorted = [...rows].sort((a, b) => (b.value || 0) - (a.value || 0));
  const shown = sorted.slice(0, maxRows);
  return (
    <div className="viz-tip">
      <div className="viz-tip__title">
        <span>
          {title}
          {current ? <span className="viz-tip__current"> · {l.current}</span> : null}
        </span>
        {onClose ? (
          <VizIconButton
            className="viz-tip__close"
            size="xs"
            icon={<X />}
            aria-label={closeLabel}
            onClick={onClose}
          />
        ) : null}
      </div>
      {zone ? (
        <div className="viz-tip__zone">
          <VizBadge tone={zoneTone(zone)} uppercase={false}>
            {zone.note ? `${zone.label} · ${zone.note}` : zone.label}
          </VizBadge>
        </div>
      ) : null}
      {empty ? (
        <div className="viz-tip__note">{l.empty}</div>
      ) : (
        <>
          {shown.map((r) => (
            <div
              key={r.id}
              className="viz-tip__row"
              data-zero={!(r.value > 0) || undefined}
              data-focus={r.id === focusId || undefined}
            >
              <VizSwatch color={r.color} />
              <span className="viz-tip__label">{r.label}</span>
              <b className="viz-tip__value">
                {percent
                  ? total > 0
                    ? `${formatNumber((r.value / total) * 100, 1)}%`
                    : '—'
                  : formatValue(r.value, unit, { long: true })}
              </b>
            </div>
          ))}
          {sorted.length > maxRows ? (
            <div className="viz-tip__note">{l.more(sorted.length - maxRows)}</div>
          ) : null}
          {rows.length > 1 ? (
            <div className="viz-tip__total">
              <span>{l.total}</span>
              <b>
                {formatValue(total, unit, { long: true })}
                {previousTotal && previousTotal > 0 ? (
                  <>
                    {' '}
                    <VizDelta pct={(total / previousTotal - 1) * 100} suffix={deltaSuffix} />
                  </>
                ) : null}
              </b>
            </div>
          ) : null}
        </>
      )}
      {notes?.map((n) => (
        <div key={n.id} className="viz-tip__event">
          {n.icon}
          <span>{n.label}</span>
        </div>
      ))}
      {focusId && !empty && l.pick ? <div className="viz-tip__hint">{l.pick}</div> : null}
    </div>
  );
}

export interface VizSummaryRow {
  readonly id: string;
  readonly label: ReactNode;
  readonly value: ReactNode;
  /** Изменение к прошлому периоду, %. */
  readonly deltaPct?: number;
  /** Пояснение под значением в шите: от чего посчитано. */
  readonly hint?: ReactNode;
}

export interface VizSummaryAction {
  readonly id: string;
  readonly label: string;
  readonly onClick: () => void;
}

export interface VizSeriesSummaryProps {
  readonly color?: string;
  readonly title: ReactNode;
  /** Период сводки: «1 окт – 9 окт». */
  readonly subtitle?: ReactNode;
  readonly rows: readonly VizSummaryRow[];
  readonly actions?: readonly VizSummaryAction[];
  readonly onClose?: () => void;
  readonly closeLabel?: string;
  readonly className?: string;
}

/**
 * Закреплённая сводка по серии: что показывать, решает продукт (продажи, выручка, доля),
 * библиотека держит форму — заголовок с ключом, строки «подпись — значение», действия.
 */
export function VizSeriesSummary({
  color,
  title,
  subtitle,
  rows,
  actions,
  onClose,
  closeLabel = 'Закрыть',
  className,
}: VizSeriesSummaryProps) {
  return (
    <div className={classNames('viz-tip', 'viz-tip--summary', className)}>
      <div className="viz-tip__title viz-tip__title--summary">
        {color ? <VizSwatch color={color} /> : null}
        <span className="viz-tip__label">{title}</span>
        {onClose ? (
          <VizIconButton
            className="viz-tip__close"
            size="xs"
            icon={<X />}
            aria-label={closeLabel}
            onClick={onClose}
          />
        ) : null}
      </div>
      {subtitle ? <div className="viz-tip__subtitle">{subtitle}</div> : null}
      {rows.map((r) => (
        <div key={r.id} className="viz-tip__pair">
          <span>{r.label}</span>
          <b>
            {r.value}
            {r.deltaPct !== undefined && Number.isFinite(r.deltaPct) ? (
              <>
                {' '}
                <VizDelta pct={r.deltaPct} />
              </>
            ) : null}
          </b>
        </div>
      ))}
      {actions?.length ? (
        <div className="viz-tip__actions">
          {actions.map((a) => (
            <button key={a.id} type="button" className="viz-tip__action" onClick={a.onClick}>
              {a.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

export interface VizEffectCardRow {
  readonly id: string;
  readonly label: string;
  readonly color?: string;
  /** «12,4 → 18,1 шт/день». */
  readonly change: string;
  readonly deltaPct: number;
  /** Нет базы до изменения или ещё рано. */
  readonly status?: string;
}

export interface VizEffectCardProps {
  readonly icon?: ReactNode;
  /** «Рекламный пост · 3 окт, 14:00». */
  readonly kicker: ReactNode;
  readonly title: ReactNode;
  readonly detail?: ReactNode;
  readonly rows: readonly VizEffectCardRow[];
  /** Как считали: «Среднее в день: 3 дня до против 3 дней после». */
  readonly note?: ReactNode;
}

/** «Что сдвинулось»: изменение и его эффект на серии — до и после, со знаком. */
export function VizEffectCard({ icon, kicker, title, detail, rows, note }: VizEffectCardProps) {
  return (
    <div className="viz-tip viz-tip--effect">
      <div className="viz-tip__kicker">
        {icon}
        <span>{kicker}</span>
      </div>
      <div className="viz-tip__title">{title}</div>
      {detail ? <div className="viz-tip__subtitle">{detail}</div> : null}
      {rows.map((r) => (
        <div key={r.id} className="viz-tip__effect">
          <VizSwatch color={r.color ?? 'currentColor'} />
          <span className="viz-tip__label">{r.label}</span>
          <span className="viz-tip__change">{r.change}</span>
          {Number.isFinite(r.deltaPct) ? (
            <VizDelta pct={r.deltaPct} />
          ) : (
            <span className="viz-tip__status">{r.status}</span>
          )}
        </div>
      ))}
      {note ? <div className="viz-tip__note">{note}</div> : null}
    </div>
  );
}
