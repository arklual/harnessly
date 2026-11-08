// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';

import { documentTheme, VIZ_THEME_ATTRIBUTES } from '../engine/colors';
import type { VizTheme } from '../model/palette';
import {
  pickGranularity,
  type GranularityThresholds,
  type VizGranularity,
  type VizTimeRange,
} from '../model/time';
import { VizViewport, type VizViewportOptions } from '../model/viewport';

/**
 * Окно времени на весь блок: график, полоса «весь период», значки и кнопки зума двигают одно
 * и то же окно. Создаётся один раз; новая история (`extent`) подхватывается без пересоздания.
 */
export function useVizViewport(options: VizViewportOptions): VizViewport {
  const [viewport] = useState(() => new VizViewport(options));
  const { start, end } = options.extent;
  useEffect(() => {
    viewport.setExtent({ start, end });
  }, [viewport, start, end]);
  useEffect(() => () => viewport.dispose(), [viewport]);
  return viewport;
}

/**
 * Окно, к которому едет вид. Меняется по действию пользователя, а не каждый кадр анимации, —
 * на него вешают запросы к бэкенду и пересчёт сводок.
 */
export function useVizViewportTarget(viewport: VizViewport): VizTimeRange {
  return useSyncExternalStore(
    useCallback((notify) => viewport.subscribe(notify), [viewport]),
    () => viewport.state.target,
    () => viewport.state.target,
  );
}

/**
 * Детализация под окно и ширину графика — с запасом, чтобы на границе час/день график не
 * мигал туда-обратно. `fixed` отключает автоматику.
 */
export function useVizGranularity(
  range: VizTimeRange,
  widthPx: number,
  options: {
    fixed?: VizGranularity;
    thresholds?: Partial<Record<VizGranularity, GranularityThresholds>>;
  } = {},
): VizGranularity {
  const last = useRef<VizGranularity>(options.fixed ?? 'day');
  const next =
    options.fixed ??
    (widthPx > 0
      ? pickGranularity(range.end - range.start, widthPx, last.current, options.thresholds)
      : last.current);
  useEffect(() => {
    last.current = next;
  }, [next]);
  return next;
}

export interface VizSeriesState<Id extends string> {
  /** Скрытые серии. */
  readonly hidden: ReadonlySet<Id>;
  /** Серия под курсором в легенде (подсвечена, остальные приглушены). */
  readonly focus: Id | null;
  readonly setFocus: (id: Id | null) => void;
  /** Скрыть или показать. Последнюю видимую не прячет — вместо этого показывает все. */
  readonly toggle: (id: Id) => void;
  /** Оставить только эту; повтор — вернуть все. */
  readonly solo: (id: Id) => void;
  readonly showAll: () => void;
}

/** Состояние серий для легенды и графика: видимость, фокус, «только эта». */
export function useVizSeriesState<Id extends string>(
  ids: readonly Id[],
  initialHidden: readonly Id[] = [],
): VizSeriesState<Id> {
  const [hidden, setHidden] = useState<ReadonlySet<Id>>(() => new Set(initialHidden));
  const [focus, setFocus] = useState<Id | null>(null);
  const key = ids.join('\u0000');
  // Серии сменились (другая метрика) — скрытые несуществующие забываем.
  useEffect(() => {
    setHidden((prev) => {
      const next = new Set([...prev].filter((id) => ids.includes(id)));
      return next.size === prev.size ? prev : next;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- key отражает ids
  }, [key]);

  const toggle = useCallback(
    (id: Id) => {
      setHidden((prev) => {
        const visible = ids.filter((x) => !prev.has(x));
        if (!prev.has(id) && visible.length === 1) return new Set();
        const next = new Set(prev);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return next;
      });
      setFocus((f) => (f === id ? null : f));
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- key отражает ids
    [key],
  );

  const solo = useCallback(
    (id: Id) => {
      setHidden((prev) => {
        const only = ids.every((x) => (x === id ? !prev.has(x) : prev.has(x)));
        return only ? new Set() : new Set(ids.filter((x) => x !== id));
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- key отражает ids
    [key],
  );

  const showAll = useCallback(() => setHidden(new Set()), []);
  return useMemo(
    () => ({ hidden, focus, setFocus, toggle, solo, showAll }),
    [hidden, focus, toggle, solo, showAll],
  );
}

/** Ширина элемента в CSS-пикселях, живьём. */
export function useElementWidth<T extends HTMLElement>(): [(node: T | null) => void, number] {
  const [width, setWidth] = useState(0);
  const observer = useRef<ResizeObserver | null>(null);
  const ref = useCallback((node: T | null) => {
    observer.current?.disconnect();
    observer.current = null;
    if (!node) return;
    setWidth(node.clientWidth);
    if (typeof ResizeObserver !== 'function') return;
    observer.current = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.current.observe(node);
  }, []);
  return [ref, width];
}

/** Тема документа (`dark`/`light`), живьём: палитра графиков у тем своя. */
export function useVizTheme(): VizTheme {
  return useSyncExternalStore(
    subscribeTheme,
    () => documentTheme(),
    () => 'light' as const,
  );
}

function subscribeTheme(notify: () => void): () => void {
  if (typeof MutationObserver !== 'function') return () => undefined;
  const observer = new MutationObserver(notify);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: [...VIZ_THEME_ATTRIBUTES],
  });
  return () => observer.disconnect();
}

export const prefersReducedMotion = () =>
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/**
 * Числа, которые плавно доезжают до новых значений: появление — рост от нуля, новые данные —
 * перетекание. `duration` — мс, `slow` — множитель (0 — без анимации).
 */
export function useVizTween(
  target: readonly number[],
  {
    duration = 700,
    slow = 1,
    initial,
  }: { duration?: number; slow?: number; initial?: readonly number[] } = {},
): readonly number[] {
  // По умолчанию — рост от нуля; `initial` — с чего начинать (масштаб, который не должен расти).
  const [shown, setShown] = useState<readonly number[]>(() => target.map((_, i) => initial?.[i] ?? 0));
  const from = useRef<readonly number[]>(shown);
  const current = useRef<readonly number[]>(shown);
  const key = target.join('|');
  useEffect(() => {
    const total = duration * slow;
    if (total <= 0 || prefersReducedMotion() || typeof requestAnimationFrame !== 'function') {
      current.current = target;
      setShown(target);
      return;
    }
    from.current = target.map((_, i) => current.current[i] ?? 0);
    const started = performance.now();
    let frame = 0;
    const tick = (now: number) => {
      const u = Math.min(1, (now - started) / total);
      const e = 1 - (1 - u) ** 3;
      const next = target.map((v, i) => {
        const a = from.current[i] ?? 0;
        const b = Number.isFinite(v) ? v : 0;
        return a + (b - a) * e;
      });
      current.current = next;
      setShown(next);
      if (u < 1) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- key отражает target
  }, [key, duration, slow]);
  // Число значений сменилось (стало больше шагов): до первого кадра анимации новые — с нуля.
  return shown.length === target.length ? shown : target.map((_, i) => shown[i] ?? 0);
}

/**
 * Последнее значение, пока подсказка открыта, — и оно же, пока она закрывается. Hint кита уходит
 * обратной анимацией появления; если в этот момент отдать ему пустое содержимое или сбросить якорь,
 * пузырь схлопнется рывком.
 */
export function useVizHeld<T>(value: T | null, open: boolean): T | null {
  const held = useRef<T | null>(value);
  if (open && value !== null) held.current = value;
  return held.current;
}

/**
 * Пузырь уже стоит на месте: можно плавно переезжать между отрезками. Сразу после открытия —
 * нельзя: позиция приходит из 0,0, и пузырь прилетел бы из угла.
 */
export function useVizSettled(open: boolean): boolean {
  const [settled, setSettled] = useState(false);
  useEffect(() => {
    if (!open) {
      setSettled(false);
      return;
    }
    let second = 0;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => setSettled(true));
    });
    return () => {
      cancelAnimationFrame(first);
      cancelAnimationFrame(second);
    };
  }, [open]);
  return open && settled;
}

/**
 * Пауза в показе: `poke()` прячет подсказку на `delay` мс (каждый вызов — отсчёт заново).
 * Сначала — готово: подсказка наведения показывается сразу. `poke(true)` — снять паузу.
 */
export function useVizDwell(delay: number): {
  readonly ready: boolean;
  readonly poke: (immediate?: boolean) => void;
} {
  const [ready, setReady] = useState(true);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const poke = useCallback(
    (immediate = false) => {
      if (timer.current) clearTimeout(timer.current);
      if (immediate || delay <= 0) {
        setReady(true);
        return;
      }
      setReady(false);
      timer.current = setTimeout(() => setReady(true), delay);
    },
    [delay],
  );
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  return useMemo(() => ({ ready, poke }), [ready, poke]);
}
