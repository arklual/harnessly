// The ring of a step that is about an element inside the design's page (HRN-792): the page's bridge follows it as it
// follows a note's block (`tk:anchors`) and tells where it shows; this element stands over that place in the frame's
// own pixels, so the tour rings it like any element of the product. Hidden while the page does not show it.
import { useContext, useLayoutEffect, useRef } from 'react';
import { AnchorContext } from '@/next/features/designly/note-anchor';

/** The key the tour's element goes under in the page's set of followed blocks. */
export const TOUR_ANCHOR = '~tour';

const px = (v: number) => `${Math.round(v * 100) / 100}px`;

const TourAnchor = () => {
  const store = useContext(AnchorContext);
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !store) return;
    const draw = () => {
      const geo = store.get()?.items.get(TOUR_ANCHOR);
      const box = geo && !geo.covered ? geo.shown : null;
      el.dataset.place = box ? 'shown' : 'hidden';
      if (!box) return;
      el.style.setProperty('--x', px(box.x));
      el.style.setProperty('--y', px(box.y));
      el.style.setProperty('--w', px(box.w));
      el.style.setProperty('--h', px(box.h));
    };
    draw();
    return store.subscribe(draw);
  }, [store]);
  return <div ref={ref} className="hx-tour-anchor" data-place="hidden" aria-hidden="true" />;
};

export default TourAnchor;
