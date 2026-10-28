// A small, live copy of a slide: its board in a frame scaled to the width of the box it stands in. Used for the next
// slide of the presenter's panel and for the picture of a recording. Not interactive.
import { useLayoutEffect, useRef } from 'react';
import { postDeck } from '@/next/features/designly/deck/bridge';

interface IDeckMiniProps {
  /** The board's address; null until the slot is open (an empty box keeps the place). */
  src: string | null;
  /** The slide's own size, board pixels. */
  w: number;
  h: number;
  title: string;
}

const DeckMini = ({ src, w, h, title }: IDeckMiniProps) => {
  const box = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.setProperty('--w', String(w));
    el.style.setProperty('--h', String(h));
    const fit = () => el.style.setProperty('--k', String(el.clientWidth / w));
    fit();
    const watch = new ResizeObserver(fit);
    watch.observe(el);
    return () => watch.disconnect();
  }, [w, h]);
  return (
    <div className="hx-deckm-mini" ref={box}>
      {src ? (
        <iframe
          className="hx-deckm-mini__frame"
          src={src}
          title={title}
          tabIndex={-1}
          loading="lazy"
          // The board draws the library's boards bar unless it is told the host draws its own.
          onLoad={(event) =>
            postDeck(event.currentTarget, src, { type: 'tk:tweaks-host', on: true, open: false })
          }
        />
      ) : null}
    </div>
  );
};

export default DeckMini;
