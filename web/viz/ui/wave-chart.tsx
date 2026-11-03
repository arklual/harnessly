// SPDX-License-Identifier: Apache-2.0
import {
  type ReactNode,
  type Ref,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react';

import { classNames } from '../internal/cx';
import { useIsMobile } from '../internal/use-mobile';
import { Hint } from '../internal/hint';
import { ColumnEngine } from '../engine/bar-engine';
import {
  DEFAULT_WAVE_OPTIONS,
  VizTimeEngine,
  WAVE_STATUS,
  type WaveEngineOptions,
  type WaveHover,
  type WavePress,
  type WaveStatus,
} from '../engine/time-engine';
import { WaveEngine } from '../engine/wave-engine';
import { computeEffect, type VizEffect } from '../model/effect';
import { formatBucket, formatDay, formatSpan, formatTime, formatValue, plural } from '../model/format';
import { layoutMarkers, pxPerDay } from '../model/markers';
import { DAY, type VizGranularity, type VizTimeRange } from '../model/time';
import type { VizBucketFrame, VizMarker, VizZone } from '../model/types';
import type { VizViewport } from '../model/viewport';
import { prefersReducedMotion, useVizDwell, useVizHeld, useVizSettled } from './hooks';
import { VizAnchor, type VizAnchorPoint } from './primitives';
import { useVizScrub } from './scrub';
import { VizMobileSheet } from '../internal/sheet';
import { VizEffectCard, type VizEffectCardRow, WaveTooltip, type WaveTooltipLabels } from './tooltip-content';

import '../viz.css';

/** Серия на графике: id, подпись и цвет (обычно `vizSeriesColor(slot, theme)`). */
export interface VizChartSeries<Id extends string = string> {
  readonly id: Id;
  readonly label: string;
  readonly color: string;
}

export interface WaveBucketValue<Id extends string = string> extends VizChartSeries<Id> {
  /** `NaN` — нет данных. */
  readonly value: number;
}

/** Что под курсором: отрезок, значения видимых серий, зона и изменения внутри него. */
export interface WaveTooltipContext<Id extends string = string, Kind extends string = string> {
  readonly k: number;
  readonly start: number;
  readonly step: number;
  readonly granularity: VizGranularity;
  readonly frame: VizBucketFrame<Id>;
  readonly values: readonly WaveBucketValue<Id>[];
  /** Сумма видимых серий в прошлом отрезке; `NaN` — нет. */
  readonly previousTotal: number;
  /** Отрезок ещё идёт. */
  readonly current: boolean;
  readonly zone: VizZone | null;
  readonly markers: readonly VizMarker<Id, Kind>[];
  /** Серия под курсором. */
  readonly seriesId: Id | null;
  readonly percent: boolean;
  /** Подсказка закреплена кликом мимо данных; `close` её снимает. */
  readonly pinned: boolean;
  readonly close: () => void;
}

/** Закреплённая серия: продукт строит по ней сводку. */
export interface WaveSummaryContext<Id extends string = string> {
  readonly series: VizChartSeries<Id>;
  readonly k: number;
  readonly start: number;
  readonly step: number;
  readonly granularity: VizGranularity;
  readonly frame: VizBucketFrame<Id>;
  /** Окно на экране — «за период». */
  readonly range: VizTimeRange;
  readonly close: () => void;
  /** Сводка в шите на телефоне: там есть место под ленту типов, превью и плитки (`VizSeriesSheet`). */
  readonly mobile: boolean;
  /** Видимые серии — между ними можно листать прямо в сводке. */
  readonly allSeries: readonly VizChartSeries<Id>[];
  /** Сменить серию сводки, не закрывая её. */
  readonly select: (id: Id) => void;
  /** Сменить отрезок сводки (тап по превью). */
  readonly selectBucket: (k: number) => void;
  /** Приблизить график к отрезку сводки (дни раскроются часами) и закрыть сводку. */
  readonly zoomIn: () => void;
}

export interface WaveEffectContext<Id extends string = string, Kind extends string = string> {
  readonly marker: VizMarker<Id, Kind>;
  readonly effect: VizEffect<Id>;
  readonly series: readonly VizChartSeries<Id>[];
}

export interface WaveChartLabels extends WaveTooltipLabels {
  /** Суффикс изменения к прошлому отрезку по детализации. */
  readonly previous: Readonly<Record<VizGranularity, string>>;
  /** Строка эффекта, когда изменение касалось всех серий. */
  readonly allSeries: string;
  /** «в день» после значения эффекта. */
  readonly perDay: string;
  readonly effectWaiting: string;
  readonly effectNoBase: string;
  readonly effectJustNow: string;
  readonly effectNote: (window: string, after: string) => string;
  readonly effectBefore: (window: string) => string;
  /** Длина окна сравнения словами: «сутки», «3 дня». */
  readonly effectWindow: (ms: number) => string;
  readonly effectAfter: (window: string, partial: boolean) => string;
  /** Подсказка клавиатуры для экранного диктора. */
  readonly keyboard: string;
}

export const DEFAULT_WAVE_CHART_LABELS: WaveChartLabels = {
  current: 'идёт сейчас',
  total: 'Всего',
  empty: 'Нет данных за этот отрезок',
  pick: 'Клик — подробнее',
  more: (count) => `и ещё ${count}`,
  previous: { hour: 'к пред. часу', day: 'к пред. дню', week: 'к пред. неделе' },
  allSeries: 'Все серии',
  perDay: 'в день',
  effectWaiting: 'ждём',
  effectNoBase: 'нет базы',
  effectJustNow: 'Изменение только что — эффект появится позже',
  effectNote: (window, after) => `Среднее в день: ${window} до против ${after} после`,
  effectBefore: (window) => `${window} до`,
  effectWindow: (ms) => {
    const days = Math.round(ms / DAY);
    if (ms < DAY) return formatSpan(ms);
    if (days === 1) return 'сутки';
    return `${days} ${plural(days, ['день', 'дня', 'дней'])}`;
  },
  effectAfter: (window, partial) => (partial ? `после · пока ${window}` : `${window} после`),
  keyboard: 'Стрелки — по отрезкам, плюс и минус — масштаб, ноль — весь период, Enter — подробнее.',
};

export const WAVE_CHART_SIZE = { s: 's', m: 'm', l: 'l' } as const;
export type WaveChartSize = (typeof WAVE_CHART_SIZE)[keyof typeof WAVE_CHART_SIZE];

export interface WaveChartProps<Id extends string = string, Kind extends string = string> {
  readonly series: readonly VizChartSeries<Id>[];
  /** Последний кадр. Пока ждём новый, держите прошлый — график перетечёт. */
  readonly frame: VizBucketFrame<Id> | null;
  readonly status: WaveStatus;
  /** Кадр на экране, догружается новый: график приглушается. */
  readonly refreshing?: boolean;
  readonly viewport: VizViewport;
  readonly options?: Partial<WaveEngineOptions>;
  readonly hidden?: ReadonlySet<Id>;
  /** Серия под курсором в легенде. */
  readonly focus?: Id | null;
  /** Серия под курсором на графике сменилась (`null` — курсор ушёл). */
  readonly onSeriesHover?: (id: Id | null) => void;
  /** Другая метрика: пока кадр не пришёл, показанные кривые растягиваются под новую шкалу. */
  readonly scaleKey?: string;
  readonly zones?: readonly VizZone[];
  readonly markers?: readonly VizMarker<Id, Kind>[];
  /** Иконка значка по его виду. Без неё значки не рисуются. */
  readonly renderMarkerIcon?: (marker: VizMarker<Id, Kind>) => ReactNode;
  /** Подпись значка для экранного диктора: «Реклама: пост в канале». */
  readonly markerLabel?: (marker: VizMarker<Id, Kind>) => string;
  /** Текущий момент: справа от него — будущее. */
  readonly now?: number;
  /** Подсказка отрезка. По умолчанию — `WaveTooltip`; `null` — без подсказки. */
  readonly renderTooltip?: ((context: WaveTooltipContext<Id, Kind>) => ReactNode) | null;
  /** Сводка по серии после клика. Без неё клик ничего не закрепляет. */
  readonly renderSummary?: (context: WaveSummaryContext<Id>) => ReactNode;
  /** Карточка «что сдвинулось» у значка. По умолчанию — `VizEffectCard`; `null` — без неё. */
  readonly renderEffect?: ((context: WaveEffectContext<Id, Kind>) => ReactNode) | null;
  readonly onPress?: (press: WavePress) => void;
  /** Черновик зоны в режиме разметки (`options.zoneDraw`); `done` — отпустили. */
  readonly onZoneDraft?: (range: VizTimeRange | null, done: boolean) => void;
  readonly onZoneRemove?: (id: string) => void;
  /** Закреплённый значок изменения. Без пропса график держит его сам. */
  readonly selectedMarker?: string | null;
  readonly onSelectedMarkerChange?: (id: string | null) => void;
  readonly labels?: Partial<WaveChartLabels>;
  /**
   * Пауза, мс, перед тем как снова показать подсказку наведения, когда закреплённую подсказку
   * закрыли кликом по графику (курсор остался на месте). Обычное наведение — без задержки.
   */
  readonly tooltipDelay?: number;
  readonly size?: WaveChartSize;
  /** Поверх графика: состояния ошибки и пустоты. */
  readonly overlay?: ReactNode;
  readonly engineRef?: Ref<VizTimeEngine | null>;
  readonly className?: string;
  readonly 'aria-label': string;
}

interface Pin {
  readonly seriesId: string;
  readonly k: number;
}

const samePoint = (a: VizAnchorPoint | null, b: VizAnchorPoint | null) =>
  a === b ||
  (!!a &&
    !!b &&
    Math.abs(a.x - b.x) < 0.3 &&
    Math.abs(a.y - b.y) < 0.3 &&
    Math.abs((a.width ?? 0) - (b.width ?? 0)) < 0.3);

const TOOLTIP_DIRECTIONS = ['end', 'start', 'top', 'bottom'] as const;
const EFFECT_DIRECTIONS = ['bottom-start', 'bottom', 'bottom-end'] as const;
const MARKER_GAP = 26;

/** Как создать движок: волны, столбики. */
export type VizEngineFactory = (canvas: HTMLCanvasElement, viewport: VizViewport) => VizTimeEngine;

export const createWaveEngine: VizEngineFactory = (canvas, viewport) => new WaveEngine(canvas, viewport);
export const createColumnEngine: VizEngineFactory = (canvas, viewport) => new ColumnEngine(canvas, viewport);

/** Пропсы любого графика времени: у волн и столбиков они одни и те же. */
export type VizTimeChartProps<Id extends string = string, Kind extends string = string> = WaveChartProps<
  Id,
  Kind
>;
export type ColumnChartProps<Id extends string = string, Kind extends string = string> = WaveChartProps<
  Id,
  Kind
>;

/**
 * Волновой график: свой движок на canvas — плавные кривые с градиентом, наложение, стопка и
 * доли, зоны, значки изменений, зум колесом и щипком, инерция, перетекание между часами, днями
 * и неделями. Подсказки — Hint кита, сводка серии на телефоне — шит снизу.
 */
export function WaveChart<Id extends string = string, Kind extends string = string>(
  props: WaveChartProps<Id, Kind>,
) {
  return <VizTimeChart<Id, Kind> {...props} createEngine={createWaveEngine} />;
}

/**
 * Столбчатый график — брат волнового: те же окно, легенда, подсказки, зоны, значки, цвета и
 * анимации. Группой, стопкой и долями; при смене детализации столбик дня распадается на часы и
 * обратно; появление — рост от основания слева направо.
 */
export function ColumnChart<Id extends string = string, Kind extends string = string>(
  props: ColumnChartProps<Id, Kind>,
) {
  return <VizTimeChart<Id, Kind> {...props} createEngine={createColumnEngine} />;
}

/** Общий каркас графика времени: движок даёт `createEngine`, всё остальное — общее. */
export function VizTimeChart<Id extends string = string, Kind extends string = string>(
  props: VizTimeChartProps<Id, Kind> & { readonly createEngine: VizEngineFactory },
) {
  const {
    series,
    frame,
    status,
    refreshing = false,
    viewport,
    options,
    hidden,
    focus = null,
    zones,
    markers,
    renderMarkerIcon,
    markerLabel,
    now = 0,
    renderTooltip,
    renderSummary,
    renderEffect,
    size = 'm',
    overlay,
    engineRef,
    createEngine,
    tooltipDelay = 1000,
    className,
    'aria-label': ariaLabel,
  } = props;
  const labels = useMemo(() => ({ ...DEFAULT_WAVE_CHART_LABELS, ...props.labels }), [props.labels]);
  const mobile = useIsMobile();
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  // Телефон: зажал и ведёшь — подсказка идёт за пальцем, график и страница стоят.
  const scrubEngine = useRef<VizTimeEngine | null>(null);
  const scrub = useVizScrub<HTMLCanvasElement>({
    enabled: mobile,
    onMove: (x, y) => scrubEngine.current?.scrubAt(x, y),
    onEnd: () => scrubEngine.current?.endScrub(),
  });
  const [engine, setEngine] = useState<VizTimeEngine | null>(null);
  scrubEngine.current = engine;
  const [hover, setHover] = useState<WaveHover | null>(null);
  const [tipPoint, setTipPoint] = useState<VizAnchorPoint | null>(null);
  const [pin, setPin] = useState<Pin | null>(null);
  const [markerHover, setMarkerHover] = useState<string | null>(null);
  const [ownMarkerPin, setOwnMarkerPin] = useState<string | null>(null);
  const markerPin = props.selectedMarker !== undefined ? props.selectedMarker : ownMarkerPin;
  const [markerPoint, setMarkerPoint] = useState<VizAnchorPoint | null>(null);
  const markerEls = useRef(new Map<string, HTMLButtonElement>());
  const keyboardId = useId();
  const latest = useRef(props);
  latest.current = props;
  const markerPinRef = useRef(markerPin);
  markerPinRef.current = markerPin;
  const setMarkerPin = useCallback((next: string | null | ((current: string | null) => string | null)) => {
    const value = typeof next === 'function' ? next(markerPinRef.current) : next;
    if (value === markerPinRef.current) return;
    markerPinRef.current = value;
    setOwnMarkerPin(value);
    latest.current.onSelectedMarkerChange?.(value);
  }, []);

  useLayoutEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const created = createEngine(canvas, viewport);
    setEngine(created);
    return () => {
      created.destroy();
      setEngine(null);
    };
  }, [viewport, createEngine]);

  useEffect(() => {
    if (!engineRef) return;
    if (typeof engineRef === 'function') engineRef(engine);
    else engineRef.current = engine;
  }, [engine, engineRef]);

  /** Отрезок, на котором закреплена подсказка (клик мимо линий и столбиков). */
  const [bucketPin, setBucketPin] = useState<number | null>(null);
  const unpin = useCallback(() => setPin(null), []);
  /**
   * Закрыть всё закреплённое и наведение тоже: иначе пузырь на миг переключится на подсказку
   * отрезка под курсором и уйдёт уже с ней, а не со своей сводкой.
   */
  const dismissRef = useRef<() => void>(() => undefined);
  const dismiss = useCallback(() => dismissRef.current(), []);
  dismissRef.current = () => {
    setPin(null);
    setBucketPin(null);
    setMarkerPin(null);
    engine?.lockHover(null);
    engine?.setHoverState(null);
  };
  // Окно едет (зум, сдвиг, инерция) — подсказка наведения прячется и возвращается, как только встало.
  const viewQuiet = useVizDwell(160);
  // Закреплённую подсказку закрыли кликом по графику — подсказка наведения вернётся через паузу.
  const afterDismiss = useVizDwell(tooltipDelay);
  const quietRef = useRef(viewQuiet.poke);
  quietRef.current = viewQuiet.poke;
  const pokeRef = useRef(afterDismiss.poke);
  pokeRef.current = afterDismiss.poke;
  useEffect(() => viewport.subscribe(() => quietRef.current()), [viewport]);
  const mobileRef = useRef(mobile);
  mobileRef.current = mobile;
  /** Серия с наибольшим значением в отрезке `k` среди видимых. */
  const topSeriesAt = (k: number): string | null => {
    const { frame: f, series: list, hidden: off } = latest.current;
    if (!f || k < 0 || k >= f.length) return null;
    let best: string | null = null;
    let bestValue = Number.NEGATIVE_INFINITY;
    for (const s of list) {
      if (off?.has(s.id)) continue;
      const v = Number(f.values[s.id]?.[k] ?? Number.NaN);
      if (v === v && v > bestValue) {
        bestValue = v;
        best = s.id;
      }
    }
    return best;
  };
  const pinnedRef = useRef(false);
  pinnedRef.current = pin !== null || bucketPin !== null;
  const markerFocus = markerPin ?? markerHover;

  // ───── входные данные → движок ─────
  useEffect(() => {
    // With reduced motion asked for, everything settles at once: no reveal, no easing between frames.
    const still = prefersReducedMotion();
    if (still) viewport.setTau(0);
    engine?.setOptions({ ...DEFAULT_WAVE_OPTIONS, ...options, ...(still ? { slow: 0 } : null) });
  }, [engine, options, viewport]);
  useEffect(() => {
    engine?.setSeries(series);
  }, [engine, series]);
  useEffect(() => {
    engine?.setFrame(frame);
  }, [engine, frame]);
  useEffect(() => {
    engine?.setStatus(status, refreshing);
  }, [engine, status, refreshing]);
  useEffect(() => {
    engine?.setHidden(hidden ?? new Set());
  }, [engine, hidden]);
  useEffect(() => {
    engine?.setFocus(focus, pin?.seriesId ?? null);
  }, [engine, focus, pin]);
  useEffect(() => {
    engine?.lockHover(bucketPin);
  }, [engine, bucketPin]);
  // Сменилась детализация или данные — закреплённый отрезок уже другой.
  useEffect(() => {
    setBucketPin(null);
  }, [frame?.granularity, frame?.start]);
  useEffect(() => {
    engine?.setZones(zones ?? []);
  }, [engine, zones]);
  useEffect(() => {
    engine?.setMarkers(markers ?? [], markerFocus);
  }, [engine, markers, markerFocus]);
  useEffect(() => {
    engine?.setTimeline(now);
  }, [engine, now]);

  // Серия пропала или стала скрытой — сводку закрываем.
  useEffect(() => {
    if (pin && (hidden?.has(pin.seriesId as Id) || !series.some((s) => s.id === pin.seriesId))) setPin(null);
  }, [pin, hidden, series]);

  // ───── эффект изменения ─────
  const effect = useMemo(() => {
    if (!markerFocus || !frame || status !== WAVE_STATUS.ready) return null;
    const marker = markers?.find((m) => m.id === markerFocus);
    if (!marker || (now && marker.t > now)) return null;
    return { marker, effect: computeEffect(frame, marker, now || Number.POSITIVE_INFINITY) };
  }, [markerFocus, frame, status, markers, now]);

  useEffect(() => {
    if (!engine) return;
    if (!effect) {
      engine.setEffectWindow(null);
      return;
    }
    const { marker, effect: fx } = effect;
    const own = fx.rows[0]?.id ? series.find((s) => s.id === fx.rows[0].id) : null;
    const window = labels.effectWindow(fx.window);
    engine.setEffectWindow({
      t: marker.t,
      window: fx.window,
      afterEnd: fx.afterEnd,
      color: own?.color,
      beforeLabel: labels.effectBefore(window),
      afterLabel: labels.effectAfter(
        fx.partial ? labels.effectWindow(fx.afterEnd - marker.t) : window,
        fx.partial,
      ),
    });
  }, [engine, effect, series, labels]);

  // Метрика сменилась — следующий кадр растягивает показанные кривые под новую шкалу.
  const scaleKeyRef = useRef(props.scaleKey);
  useEffect(() => {
    if (scaleKeyRef.current === props.scaleKey) return;
    scaleKeyRef.current = props.scaleKey;
    engine?.expectRescale();
  }, [engine, props.scaleKey]);

  // Серия под курсором сменилась — сообщаем наружу (легенда подсвечивает её).
  const hoverSeriesId = hover?.seriesId ?? null;
  const onSeriesHoverRef = useRef(props.onSeriesHover);
  onSeriesHoverRef.current = props.onSeriesHover;
  useEffect(() => {
    onSeriesHoverRef.current?.(hoverSeriesId as Id | null);
  }, [hoverSeriesId]);

  // ───── события движка ─────
  useEffect(() => {
    if (!engine) return;
    engine.callbacks = {
      onHover: (h) => setHover(h),
      onPress: (press) => {
        latest.current.onPress?.(press);
        // Что-то уже закреплено — клик вне пузыря только закрывает его; следующий клик закрепит новое.
        if (pinnedRef.current) {
          dismissRef.current();
          // Курсор остался на графике: наведение — на отрезке под ним, подсказка вернётся после секунды покоя.
          if (press.k >= 0 && engine) {
            const plot = engine.plot;
            engine.setHoverState({ k: press.k, y: plot.top + plot.height / 2 }, press.seriesId);
          }
          pokeRef.current();
          return;
        }
        setMarkerPin(null);
        // Телефон: любой тап открывает шит — по пустому месту берём серию, которой в отрезке больше всего.
        const seriesId =
          press.seriesId ?? (mobileRef.current && latest.current.renderSummary ? topSeriesAt(press.k) : null);
        // По линии или столбику — сводка серии; мимо данных — подсказка отрезка остаётся на месте.
        if (seriesId && latest.current.renderSummary) {
          setBucketPin(null);
          setPin((cur) => {
            if (cur && cur.seriesId === seriesId && cur.k === press.k) return null;
            return { seriesId, k: press.k };
          });
          return;
        }
        setPin(null);
        setBucketPin((cur) => (cur === press.k || press.k < 0 ? null : press.k));
      },
      onZoneDraft: (range, done) => latest.current.onZoneDraft?.(range, done),
      onZoneRemove: (id) => latest.current.onZoneRemove?.(id),
      onInteract: () => {
        setPin(null);
        setBucketPin(null);
        setMarkerPin(null);
      },
      onEscape: () => dismissRef.current(),
      onPaint: () => paintOverlays(),
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- paintOverlays читает свежее через ref
  }, [engine]);

  const overlaysRef = useRef<() => void>(() => undefined);
  const paintOverlays = () => overlaysRef.current();

  overlaysRef.current = () => {
    if (!engine) return;
    const plot = engine.plot;
    // Якорь подсказки: серия в фокусе или верх отрезка — где именно, решает способ рисования.
    const target =
      pin ?? (engine.hover && !engine.isDragging ? { seriesId: engine.pick, k: engine.hover.k } : null);
    const point: VizAnchorPoint | null =
      target && status === WAVE_STATUS.ready ? engine.tooltipAnchor(target.k, target.seriesId, !!pin) : null;
    setTipPoint((prev) => (samePoint(prev, point) ? prev : point));

    // Значки изменений: приоритеты по масштабу, соседи — в «+N».
    const list = latest.current.markers;
    if (!list?.length || !latest.current.renderMarkerIcon) return;
    const view = viewport.state.current;
    const show = status === WAVE_STATUS.ready || status === WAVE_STATUS.empty;
    const layout = layoutMarkers(list, (t) => engine.xOf(t), {
      pxPerDay: pxPerDay(view.end - view.start, plot.width),
      minGap: MARKER_GAP,
      focusId: markerPin ?? markerHover,
    });
    let focusPoint: VizAnchorPoint | null = null;
    for (const m of list) {
      const el = markerEls.current.get(m.id);
      if (!el) continue;
      const x = engine.xOf(m.t);
      const slot = layout.get(m.id);
      const visible = show && !!slot?.visible && x >= plot.left - 2 && x <= plot.left + plot.width + 2;
      el.style.setProperty('transform', `translateX(${x.toFixed(1)}px)`);
      el.dataset.visible = visible ? 'true' : 'false';
      el.tabIndex = visible ? 0 : -1;
      const extra = slot?.hidden ? `+${slot.hidden}` : '';
      const badge = el.lastElementChild as HTMLElement | null;
      if (badge && badge.textContent !== extra) badge.textContent = extra;
      if (m.id === (markerPin ?? markerHover) && visible)
        focusPoint = {
          x,
          y: ((el.offsetParent as HTMLElement | null)?.offsetTop ?? 0) + el.offsetTop + el.offsetHeight,
        };
    }
    setMarkerPoint((prev) => (samePoint(prev, focusPoint) ? prev : focusPoint));
  };

  useLayoutEffect(() => {
    engine?.kick();
  });

  // Закреплённая сводка закрывается кликом мимо и прокруткой страницы.
  useEffect(() => {
    if (!pin && !markerPin && bucketPin === null) return;
    // composedPath, а не target: в теневом дереве target у документа — хост, а не холст.
    const inside = (event: Event, selector: string) =>
      event
        .composedPath()
        .some((node) => node === canvasRef.current || (node instanceof Element && node.matches(selector)));
    const onDown = (event: PointerEvent) => {
      if (!inside(event, '.viz-hint--pinned, .viz-wave__marker, [data-bs-content]')) dismiss();
    };
    const onScroll = (event: Event) => {
      if (!inside(event, '.viz-hint--pinned, [data-bs-content]')) dismiss();
    };
    document.addEventListener('pointerdown', onDown, true);
    if (!mobile) window.addEventListener('scroll', onScroll, true);
    return () => {
      document.removeEventListener('pointerdown', onDown, true);
      window.removeEventListener('scroll', onScroll, true);
    };
  }, [pin, markerPin, bucketPin, mobile, setMarkerPin, dismiss]);

  // ───── содержимое подсказок ─────
  const percent = (options?.mode ?? DEFAULT_WAVE_OPTIONS.mode) === 'percent';
  const unit = options?.unit ?? DEFAULT_WAVE_OPTIONS.unit;
  const timeZone = options?.timeZone ?? DEFAULT_WAVE_OPTIONS.timeZone;
  const showZones = options?.zones ?? DEFAULT_WAVE_OPTIONS.zones;

  const tooltipContext = useMemo((): WaveTooltipContext<Id, Kind> | null => {
    if (!hover || !frame || pin) return null;
    const k = hover.k;
    if (k < 0 || k >= frame.length) return null;
    const start = frame.start + k * frame.step;
    const visible = series.filter((s) => !hidden?.has(s.id));
    const at = (id: Id, i: number) => {
      const v = frame.values[id]?.[i];
      return v === undefined ? Number.NaN : Number(v);
    };
    let previousTotal = Number.NaN;
    if (k > 0) {
      previousTotal = 0;
      for (const s of visible) {
        const v = at(s.id, k - 1);
        if (v === v) previousTotal += v;
      }
    }
    const mid = start + frame.step / 2;
    return {
      k,
      start,
      step: frame.step,
      granularity: frame.granularity,
      frame,
      values: visible.map((s) => ({ ...s, value: at(s.id, k) })),
      previousTotal,
      current: !!now && now >= start && now < start + frame.step,
      zone: showZones ? (zones?.find((z) => mid >= z.from && mid < z.to) ?? null) : null,
      markers: markers?.filter((m) => m.t >= start && m.t < start + frame.step) ?? [],
      seriesId: (hover.seriesId as Id | null) ?? null,
      percent,
      pinned: bucketPin !== null,
      close: dismiss,
    };
  }, [hover, frame, pin, series, hidden, now, showZones, zones, markers, percent, bucketPin, dismiss]);

  const tooltip = useMemo(() => {
    if (!tooltipContext || renderTooltip === null) return null;
    if (renderTooltip) return renderTooltip(tooltipContext);
    const c = tooltipContext;
    return (
      <WaveTooltip
        title={formatBucket(c.start, c.granularity, c.step, timeZone)}
        current={c.current}
        zone={c.zone}
        rows={c.values}
        unit={unit}
        percent={c.percent}
        focusId={renderSummary ? c.seriesId : null}
        previousTotal={c.previousTotal}
        deltaSuffix={labels.previous[c.granularity]}
        notes={c.markers.slice(0, 2).map((m) => ({ id: m.id, label: m.label, icon: renderMarkerIcon?.(m) }))}
        labels={labels}
        onClose={c.pinned ? c.close : undefined}
      />
    );
  }, [tooltipContext, renderTooltip, renderSummary, timeZone, unit, labels, renderMarkerIcon]);

  const summary = useMemo(() => {
    if (!pin || !frame || !renderSummary) return null;
    const s = series.find((x) => x.id === pin.seriesId);
    if (!s) return null;
    return renderSummary({
      series: s,
      k: pin.k,
      start: frame.start + pin.k * frame.step,
      step: frame.step,
      granularity: frame.granularity,
      frame,
      range: viewport.state.target,
      close: unpin,
      mobile,
      allSeries: series.filter((x) => !hidden?.has(x.id)),
      select: (id) => setPin((cur) => (cur ? { ...cur, seriesId: id } : cur)),
      selectBucket: (k) => setPin((cur) => (cur && k >= 0 && k < frame.length ? { ...cur, k } : cur)),
      zoomIn: () => {
        const t = frame.start + pin.k * frame.step;
        unpin();
        viewport.setRange({ start: t - frame.step * 0.5, end: t + frame.step * 1.5 });
      },
    });
  }, [pin, frame, renderSummary, series, hidden, viewport, unpin, mobile]);

  const effectCard = useMemo(() => {
    if (!effect || renderEffect === null) return null;
    if (renderEffect) return renderEffect({ ...effect, series });
    const { marker, effect: fx } = effect;
    const window = labels.effectWindow(fx.window);
    const after = fx.partial ? labels.effectWindow(fx.afterEnd - marker.t) : window;
    const perDay = (v: number) => (v === v ? formatValue(v, unit) : '—');
    const rows: VizEffectCardRow[] = fx.rows.map((r) => {
      const s = r.id ? series.find((x) => x.id === r.id) : null;
      return {
        id: r.id ?? 'all',
        label: s?.label ?? labels.allSeries,
        color: s?.color,
        change: `${perDay(r.before)} → ${perDay(r.after)} ${labels.perDay}`,
        deltaPct: r.deltaPct,
        status: r.before > 0 ? labels.effectWaiting : labels.effectNoBase,
      };
    });
    return (
      <VizEffectCard
        icon={renderMarkerIcon?.(marker)}
        kicker={`${formatDay(marker.t, timeZone)}, ${formatTime(marker.t, timeZone)}`}
        title={marker.label}
        detail={marker.detail}
        rows={rows}
        note={fx.afterEnd - marker.t < 3_600_000 ? labels.effectJustNow : labels.effectNote(window, after)}
      />
    );
  }, [effect, renderEffect, series, unit, labels, renderMarkerIcon, timeZone]);

  // На телефоне пузырей нет: тап открывает шит со сводкой.
  const hoverOpen =
    (!mobile || scrub.scrubbing) &&
    !!tooltip &&
    !!tipPoint &&
    !pin &&
    status === WAVE_STATUS.ready &&
    ((viewQuiet.ready && afterDismiss.ready) || bucketPin !== null);
  const pinOpen = !!summary && !!tipPoint && !mobile;
  const effectOpen = !!effectCard && !!markerPoint;
  // Один пузырь на наведение и закреплённую сводку: клик меняет содержимое, а не гасит один
  // пузырь и зажигает другой. Пока пузырь уходит, держим последнее содержимое и место.
  const mainOpen = hoverOpen || pinOpen;
  // Новый отрезок — содержимое проявляется, пока пузырь переезжает, а не подменяется рывком.
  const mainKey = pinOpen ? `p:${pin?.seriesId}:${pin?.k}` : `h:${hover?.k}`;
  const mainLive = pinOpen ? summary : tooltip;
  const mainContent = useVizHeld<ReactNode>(
    mainLive ? (
      <div key={mainKey} className="viz-swap">
        {mainLive}
      </div>
    ) : null,
    mainOpen,
  );
  const mainPoint = useVizHeld(tipPoint, mainOpen);
  const mainGlide = useVizSettled(mainOpen);
  const effectContent = useVizHeld<ReactNode>(effectCard, effectOpen);
  const effectPoint = useVizHeld(markerPoint, effectOpen);

  return (
    <div className={classNames('viz-wave', `viz-wave--${size}`, className)}>
      <div className="viz-wave__stage" data-overlay={overlay ? 'true' : undefined}>
        <canvas
          ref={(node) => {
            canvasRef.current = node;
            scrub.ref(node);
          }}
          className="viz-wave__canvas viz-scrub-area"
          tabIndex={0}
          role="img"
          aria-label={ariaLabel}
          aria-describedby={keyboardId}
          aria-busy={status === WAVE_STATUS.loading || refreshing || undefined}
        />
        <span id={keyboardId} className="viz-visually-hidden">
          {labels.keyboard}
        </span>
        {markers?.length && renderMarkerIcon ? (
          <div className="viz-wave__markers">
            {markers.map((m) => (
              <button
                key={m.id}
                ref={(el) => {
                  if (el) markerEls.current.set(m.id, el);
                  else markerEls.current.delete(m.id);
                }}
                type="button"
                className="viz-wave__marker"
                data-visible="false"
                data-focus={markerFocus === m.id || undefined}
                aria-pressed={markerPin === m.id}
                aria-label={markerLabel?.(m) ?? m.label}
                onPointerEnter={() => setMarkerHover(m.id)}
                onPointerLeave={() => setMarkerHover((cur) => (cur === m.id ? null : cur))}
                onFocus={() => setMarkerHover(m.id)}
                onBlur={() => setMarkerHover((cur) => (cur === m.id ? null : cur))}
                onClick={() => {
                  setPin(null);
                  setMarkerPin((cur) => (cur === m.id ? null : m.id));
                }}
              >
                {renderMarkerIcon(m)}
                <span className="viz-wave__marker-more" />
              </button>
            ))}
          </div>
        ) : null}
        <Hint
          open={mainOpen}
          onOpenChange={(open) => !open && pinOpen && unpin()}
          content={mainContent}
          direction={TOOLTIP_DIRECTIONS}
          offset={18}
          interactive={pinOpen || bucketPin !== null}
          role={pinOpen ? 'dialog' : 'tooltip'}
          aria-label={pinOpen ? series.find((s) => s.id === pin?.seriesId)?.label : undefined}
          className={classNames(
            'viz-hint',
            (pinOpen || bucketPin !== null) && 'viz-hint--pinned',
            mainGlide && 'viz-hint--glide',
          )}
        >
          <VizAnchor point={mainPoint ?? { x: 0, y: 0 }} />
        </Hint>
        <Hint
          open={effectOpen}
          onOpenChange={(open) => !open && setMarkerPin(null)}
          content={effectContent}
          direction={EFFECT_DIRECTIONS}
          offset={12}
          interactive={!!markerPin}
          className={classNames('viz-hint', 'viz-hint--effect', markerPin && 'viz-hint--pinned')}
        >
          <VizAnchor point={effectPoint ?? { x: 0, y: 0 }} />
        </Hint>
        {overlay ? <div className="viz-wave__overlay">{overlay}</div> : null}
      </div>
      {mobile && renderSummary ? (
        <VizMobileSheet open={!!summary} onClose={unpin}>
          {summary}
        </VizMobileSheet>
      ) : null}
    </div>
  );
}
