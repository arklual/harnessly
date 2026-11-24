import { useLayoutEffect, useRef, useState, type RefObject } from 'react';

export const PROTOTYPE_FRAME_CAPACITY = 32;

interface Props {
  src: string;
  title: string;
  frameRef: RefObject<HTMLIFrameElement | null>;
  ready: () => void;
  capacity: number;
  nextFrame?: string | null;
}

// DOM order must stay stable. Moving a live iframe between siblings reloads its browsing context.
export default function BoardFrames({ src, title, frameRef, ready, capacity, nextFrame }: Props) {
  const [recent, setRecent] = useState([src]);
  const [order, setOrder] = useState([src]);
  if (recent.at(-1) !== src) {
    const next = [...recent.filter((url) => url !== src), src].slice(-capacity);
    setRecent(next);
    setOrder([...order.filter((url) => next.includes(url)), ...(order.includes(src) ? [] : [src])]);
  }
  const [drawn, setDrawn] = useState<string | null>(null);
  if (drawn === src && nextFrame && !order.includes(nextFrame) && nextFrame !== src) {
    // Keep the active page most recent. Warm one adjacent immutable revision after the current document loads.
    const kept = [...recent.filter((url) => url !== src), nextFrame, src].slice(-capacity);
    setRecent(kept);
    setOrder([...order.filter((url) => kept.includes(url)), nextFrame]);
  }
  const callback = useRef(ready);
  useLayoutEffect(() => {
    callback.current = ready;
  });
  useLayoutEffect(() => {
    if (frameRef.current?.dataset.loaded) {
      setDrawn(src);
      callback.current();
    }
  }, [src, frameRef]);
  return order.map((url) => (
    <iframe
      key={url}
      ref={url === src ? frameRef : undefined}
      className="hx-site__iframe"
      src={url}
      title={title}
      allow="clipboard-write"
      style={url === src ? undefined : { display: 'none' }}
      onLoad={(event) => {
        event.currentTarget.dataset.loaded = '1';
        if (event.currentTarget === frameRef.current) {
          setDrawn(src);
          callback.current();
        } else event.currentTarget.contentWindow?.postMessage({ type: 'tk:deck-live', on: false }, '*');
      }}
    />
  ));
}
