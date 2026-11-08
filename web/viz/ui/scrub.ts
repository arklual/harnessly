// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useRef, useState } from 'react';

/** Сколько держать палец на месте, чтобы начать смотреть значения, мс. */
const HOLD_MS = 280;
/** Сдвиг пальца до срабатывания — значит, листают страницу. */
const SLOP_PX = 8;
/** После просмотра отпущенный палец не должен засчитаться тапом. */
const CLICK_GUARD_MS = 450;

export interface VizScrubOptions {
  readonly enabled: boolean;
  /** Палец в точке `clientX/clientY` — показать значение под ним. */
  readonly onMove: (clientX: number, clientY: number) => void;
  /** Палец отпустили — убрать. */
  readonly onEnd: () => void;
}

export interface VizScrub<T extends Element> {
  readonly ref: (node: T | null) => void;
  /** Элемент, на котором ловим жест. */
  readonly node: T | null;
  /** Сейчас ведут пальцем по значениям. */
  readonly scrubbing: boolean;
  /** Тап сразу после просмотра — не тап: вернёт `true`, и клик надо пропустить. */
  readonly swallowClick: () => boolean;
}

/**
 * Долгое нажатие и ведение пальцем на телефоне: зажал — график отдаёт жест себе (страница не
 * листается), значения идут за пальцем; отпустил — подсказка уходит. Сдвинул палец сразу — это
 * прокрутка страницы, график не мешает. Обычный тап остаётся тапом.
 */
export function useVizScrub<T extends Element>(options: VizScrubOptions): VizScrub<T> {
  const latest = useRef(options);
  latest.current = options;
  const [node, setNode] = useState<T | null>(null);
  const [scrubbing, setScrubbing] = useState(false);
  const guardUntil = useRef(0);

  useEffect(() => {
    if (!node || !options.enabled) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let on = false;
    let sx = 0;
    let sy = 0;
    let lx = 0;
    let ly = 0;
    const stop = () => {
      clearTimeout(timer);
      timer = undefined;
      if (!on) return;
      on = false;
      setScrubbing(false);
      guardUntil.current = performance.now() + CLICK_GUARD_MS;
      latest.current.onEnd();
    };
    const onStart = (event: Event) => {
      const e = event as TouchEvent;
      if (e.touches.length !== 1) {
        stop();
        return;
      }
      sx = lx = e.touches[0].clientX;
      sy = ly = e.touches[0].clientY;
      clearTimeout(timer);
      timer = setTimeout(() => {
        timer = undefined;
        on = true;
        setScrubbing(true);
        navigator.vibrate?.(8);
        latest.current.onMove(lx, ly);
      }, HOLD_MS);
    };
    const onMove = (event: Event) => {
      const e = event as TouchEvent;
      const t = e.touches[0];
      if (!t) return;
      lx = t.clientX;
      ly = t.clientY;
      if (on) {
        if (e.cancelable) e.preventDefault();
        latest.current.onMove(lx, ly);
        return;
      }
      if (timer !== undefined && Math.hypot(lx - sx, ly - sy) > SLOP_PX) {
        clearTimeout(timer);
        timer = undefined;
      }
    };
    // Долгое нажатие на телефоне открывает меню «скопировать/сохранить» — не во время просмотра.
    const onMenu = (event: Event) => {
      if (on || timer !== undefined) event.preventDefault();
    };
    node.addEventListener('touchstart', onStart, { passive: true });
    node.addEventListener('touchmove', onMove, { passive: false });
    node.addEventListener('touchend', stop);
    node.addEventListener('touchcancel', stop);
    node.addEventListener('contextmenu', onMenu);
    return () => {
      clearTimeout(timer);
      node.removeEventListener('touchstart', onStart);
      node.removeEventListener('touchmove', onMove);
      node.removeEventListener('touchend', stop);
      node.removeEventListener('touchcancel', stop);
      node.removeEventListener('contextmenu', onMenu);
    };
  }, [node, options.enabled]);

  const swallowClick = useCallback(() => performance.now() < guardUntil.current, []);
  return { ref: setNode, node, scrubbing, swallowClick };
}
