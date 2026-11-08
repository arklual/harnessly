// SPDX-License-Identifier: Apache-2.0
import { type ReactNode, useEffect, useLayoutEffect, useRef, useState } from 'react';

import { classNames } from '../internal/cx';
import { Hint } from '../internal/hint';
import { NavigatorEngine } from '../engine/navigator-engine';
import type { VizTimeEngine } from '../engine/time-engine';
import { formatDay, formatRange } from '../model/format';
import { layoutMarkers, pxPerDay } from '../model/markers';
import { MOSCOW_ZONE, type VizTimeZone } from '../model/time';
import type { VizBucketFrame, VizMarker, VizZone } from '../model/types';
import type { VizViewport } from '../model/viewport';
import { useVizViewportTarget } from './hooks';
import { VizAnchor, type VizAnchorPoint } from './primitives';
import type { VizChartSeries } from './wave-chart';

import '../viz.css';

export interface VizNavigatorProps<Id extends string = string, Kind extends string = string> {
  readonly viewport: VizViewport;
  readonly series: readonly VizChartSeries<Id>[];
  /** Кадр всей истории (`useVizOverviewData`). */
  readonly frame: VizBucketFrame<Id> | null;
  /** Данные есть; иначе — скелетон. */
  readonly ready: boolean;
  readonly refreshing?: boolean;
  readonly hidden?: ReadonlySet<Id>;
  readonly zones?: readonly VizZone[];
  readonly showZones?: boolean;
  readonly now?: number;
  /** Дата мероприятия и подпись к ней в будущей части полосы. */
  readonly eventAt?: number | null;
  readonly eventLabel?: string;
  /** Значки изменений над полосой: при отдалении — главные, ближе — все. */
  readonly markers?: readonly VizMarker<Id, Kind>[];
  readonly renderMarkerIcon?: (marker: VizMarker<Id, Kind>) => ReactNode;
  /** Значок выбран: окно графика переезжает к нему. */
  readonly onMarkerSelect?: (marker: VizMarker<Id, Kind>) => void;
  /** Выровнять полосу по полю графика над ней. */
  readonly alignTo?: VizTimeEngine | null;
  readonly timeZone?: VizTimeZone;
  /** Множитель длительности анимаций. */
  readonly slow?: number;
  readonly onInteract?: () => void;
  readonly className?: string;
  readonly 'aria-label'?: string;
}

const MARKER_GAP = 15;
/** Окно, в которое переезжаем к значку, не шире 10 суток. */
const MARKER_SPAN = 10 * 86_400_000;

/**
 * Полоса «весь период»: история целиком, окно графика с ручками, значки изменений.
 * Работает с тем же `VizViewport`, что и график, — двигается вместе с ним.
 */
export function VizNavigator<Id extends string = string, Kind extends string = string>({
  viewport,
  series,
  frame,
  ready,
  refreshing = false,
  hidden,
  zones,
  showZones = true,
  now = 0,
  eventAt = null,
  eventLabel = '',
  markers,
  renderMarkerIcon,
  onMarkerSelect,
  alignTo,
  timeZone = MOSCOW_ZONE,
  slow = 1,
  onInteract,
  className,
  'aria-label': ariaLabel = 'Весь период',
}: VizNavigatorProps<Id, Kind>) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const [engine, setEngine] = useState<NavigatorEngine | null>(null);
  const markerEls = useRef(new Map<string, HTMLButtonElement>());
  const [hint, setHint] = useState<{ id: string; point: VizAnchorPoint } | null>(null);
  const target = useVizViewportTarget(viewport);
  const latest = useRef({ markers, hint });
  latest.current = { markers, hint };

  useLayoutEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const created = new NavigatorEngine(canvas, viewport);
    setEngine(created);
    return () => {
      created.destroy();
      setEngine(null);
    };
  }, [viewport]);

  useEffect(() => {
    if (!engine) return;
    engine.slow = slow;
    engine.onInteract = onInteract ?? null;
  }, [engine, slow, onInteract]);
  useEffect(() => {
    engine?.setSeries(series, hidden ?? new Set());
  }, [engine, series, hidden]);
  useEffect(() => {
    engine?.setFrame(frame, ready);
  }, [engine, frame, ready]);
  useEffect(() => {
    engine?.setRefreshing(refreshing);
  }, [engine, refreshing]);
  useEffect(() => {
    engine?.setZones(zones ?? [], showZones);
  }, [engine, zones, showZones]);
  useEffect(() => {
    if (!engine) return;
    engine.now = now;
    engine.eventAt = eventAt;
    engine.eventLabel = eventLabel;
    engine.kick();
  }, [engine, now, eventAt, eventLabel]);

  // Поля — как у графика: окно полосы стоит ровно под окном графика.
  useEffect(() => {
    if (!engine || !alignTo) return;
    const sync = () => engine.setInsets({ left: alignTo.plot.left, right: alignTo.pad.right });
    sync();
    return alignTo.subscribePaint(sync);
  }, [engine, alignTo]);

  useEffect(() => {
    if (!engine) return;
    engine.onPaint = () => {
      const list = latest.current.markers;
      if (!list?.length) return;
      const strip = engine.strip;
      const extent = viewport.extent;
      const view = viewport.state.current;
      const layout = layoutMarkers(list, (t) => engine.xOf(t), {
        pxPerDay: pxPerDay(extent.end - extent.start, strip.right - strip.left),
        minGap: MARKER_GAP,
      });
      for (const m of list) {
        const el = markerEls.current.get(m.id);
        if (!el) continue;
        const x = engine.xOf(m.t);
        const visible = ready && !!layout.get(m.id)?.visible;
        el.style.setProperty('transform', `translateX(${x.toFixed(1)}px)`);
        el.dataset.visible = visible ? 'true' : 'false';
        el.dataset.inside = m.t >= view.start && m.t <= view.end ? 'true' : 'false';
        el.tabIndex = visible ? 0 : -1;
      }
    };
    engine.kick();
  }, [engine, viewport, ready, markers]);

  // Якорь — над значком: полоса значков стоит в самом верху блока.
  const showHint = (m: VizMarker<Id, Kind>) =>
    engine && setHint({ id: m.id, point: { x: engine.xOf(m.t), y: 0 } });
  const hintMarker = hint ? markers?.find((m) => m.id === hint.id) : null;

  return (
    <div className={classNames('viz-nav', className)}>
      {markers?.length && renderMarkerIcon ? (
        <div className="viz-nav__markers">
          {markers.map((m) => (
            <button
              key={m.id}
              ref={(el) => {
                if (el) markerEls.current.set(m.id, el);
                else markerEls.current.delete(m.id);
              }}
              type="button"
              className="viz-nav__marker"
              data-visible="false"
              aria-label={`${m.label}, ${formatDay(m.t, timeZone)}`}
              onPointerEnter={() => showHint(m)}
              onPointerLeave={() => setHint(null)}
              onFocus={() => showHint(m)}
              onBlur={() => setHint(null)}
              onClick={() => {
                const span = Math.min(target.end - target.start, MARKER_SPAN);
                viewport.setRange({ start: m.t - span / 2, end: m.t + span / 2 });
                onMarkerSelect?.(m);
              }}
            >
              {renderMarkerIcon(m)}
            </button>
          ))}
        </div>
      ) : null}
      <canvas
        ref={canvasRef}
        className="viz-nav__canvas"
        tabIndex={0}
        role="slider"
        aria-label={ariaLabel}
        aria-valuemin={viewport.extent.start}
        aria-valuemax={viewport.extent.end}
        aria-valuenow={(target.start + target.end) / 2}
        aria-valuetext={formatRange(target.start, target.end, timeZone)}
      />
      <Hint
        open={!!hintMarker && !!hint}
        content={
          hintMarker ? (
            <span className="viz-nav__hint">{`${hintMarker.label} · ${formatDay(hintMarker.t, timeZone)}`}</span>
          ) : null
        }
        direction={['top', 'top-start', 'top-end']}
        offset={6}
        interactive={false}
        className="viz-hint"
      >
        <VizAnchor point={hint?.point ?? { x: 0, y: 0 }} />
      </Hint>
    </div>
  );
}
