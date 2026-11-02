// A horizontal track the person drags or clicks: the media's time, the volume. It says where the pointer is as a share
// of its width; what is drawn inside is the caller's. Arrows work from the player's own keys, so the track takes no
// keyboard of its own and stays out of the Tab order.
import { useRef, type PointerEvent, type ReactNode } from 'react';
import { clamp01 } from '@/next/features/pane/media/media-model';

interface IMediaTrackProps {
  className: string;
  label: string;
  /** The value now, for a screen reader: 0–100. */
  now: number;
  /** The same in words: «0:07 из 0:18». */
  text: string;
  onPick: (share: number) => void;
  /** Dragged with Shift: a stretch from where it began to where the pointer is, instead of a pick. */
  onStretch?: (from: number, to: number) => void;
  /** Where the pointer hovers; null when it left. */
  onHover?: (share: number | null) => void;
  children: ReactNode;
}

const MediaTrack = ({
  className,
  label,
  now,
  text,
  onPick,
  onStretch,
  onHover,
  children,
}: IMediaTrackProps) => {
  const dragging = useRef(false);
  const stretchFrom = useRef<number | null>(null);
  const shareOf = (event: PointerEvent<HTMLDivElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    return box.width > 0 ? clamp01((event.clientX - box.left) / box.width) : 0;
  };
  return (
    <div
      className={className}
      role="slider"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(now)}
      aria-valuetext={text}
      onClick={(event) => event.stopPropagation()}
      onPointerDown={(event) => {
        dragging.current = true;
        event.currentTarget.setPointerCapture(event.pointerId);
        if (event.shiftKey && onStretch) {
          stretchFrom.current = shareOf(event);
          return;
        }
        onPick(shareOf(event));
      }}
      onPointerMove={(event) => {
        const share = shareOf(event);
        if (dragging.current && stretchFrom.current !== null) onStretch?.(stretchFrom.current, share);
        else if (dragging.current) onPick(share);
        onHover?.(share);
      }}
      onPointerUp={() => {
        dragging.current = false;
        stretchFrom.current = null;
      }}
      onPointerCancel={() => {
        dragging.current = false;
        stretchFrom.current = null;
      }}
      onPointerLeave={() => onHover?.(null)}
    >
      {children}
    </div>
  );
};

export default MediaTrack;
