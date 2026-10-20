import { useEffect, type RefObject } from 'react';
import { wheelHandledInside } from '@/lib/wheel-owner';

const LINE_PX = 16;

const visibleScroller = (column: HTMLElement): HTMLElement | null =>
  [...column.querySelectorAll<HTMLElement>('.agent-timeline-scroller')].find(
    (el) => el.offsetParent !== null,
  ) ?? null;

/**
 * The chat column scrolls as one zone, as in Claude and Codex: the wheel beside the composer or over the strip above
 * it moves the timeline instead of reaching an outer box or the page.
 */
const useWheelToTimeline = (column: RefObject<HTMLElement | null>) => {
  useEffect(() => {
    const element = column.current;
    if (!element) return;
    const onWheel = (event: WheelEvent) => {
      if (event.ctrlKey || event.deltaY === 0 || !(event.target instanceof Element)) return;
      const scroller = visibleScroller(element);
      if (!scroller || scroller.contains(event.target)) return;
      if (wheelHandledInside(event.target, element, event.deltaY)) return;
      event.preventDefault();
      const unit =
        event.deltaMode === WheelEvent.DOM_DELTA_LINE
          ? LINE_PX
          : event.deltaMode === WheelEvent.DOM_DELTA_PAGE
            ? scroller.clientHeight
            : 1;
      scroller.scrollBy({ top: event.deltaY * unit });
    };
    element.addEventListener('wheel', onWheel, { passive: false });
    return () => element.removeEventListener('wheel', onWheel);
  }, [column]);
};

export default useWheelToTimeline;
