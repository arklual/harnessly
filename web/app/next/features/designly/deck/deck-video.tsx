// The recording of a deck for a guest: the video and the list of its chapters («Главы · 6:12»): a slide's miniature, its
// title and where it starts. The chapter the video is in is marked; a click seeks there and plays. Used by the guest page
// and by the owner's «Как видит гость».
import { useEffect, useRef, useState } from 'react';
import { clock, currentChapter } from '@/next/features/designly/deck/video-chapters';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

export interface IDeckVideoChapter {
  /** The slide's key. */
  id: string;
  title: string;
  /** Where the slide starts in the video, ms. */
  t: number;
  /** The slide's address for its miniature; null draws a plain tile. */
  frame: string | null;
  w: number;
  h: number;
}

export interface IDeckVideoProps {
  src: string;
  chapters: readonly IDeckVideoChapter[];
  /** The chapter on screen while the video plays; null when it is paused or stopped. */
  onPlaying?: (chapter: number | null) => void;
}

const DEFAULT_W = 1920;
const DEFAULT_H = 1080;

/** A slide's miniature: its board in a frame scaled to the tile, drawn only when it is near the screen. */
const Mini = ({ chapter }: { chapter: IDeckVideoChapter }) => {
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const w = chapter.w || DEFAULT_W;
    el.style.setProperty('--hx-mini-w', `${w}px`);
    el.style.setProperty('--hx-mini-h', `${chapter.h || DEFAULT_H}px`);
    const fit = () => el.style.setProperty('--hx-mini-k', String(el.clientWidth / w));
    fit();
    const watch = new ResizeObserver(fit);
    watch.observe(el);
    return () => watch.disconnect();
  }, [chapter.w, chapter.h]);
  return (
    <div ref={box} className="hx-deck-chap__mini" aria-hidden="true">
      {chapter.frame !== null && <iframe src={chapter.frame} title="" tabIndex={-1} loading="lazy" />}
    </div>
  );
};

const DeckVideo = ({ src, chapters, onPlaying }: IDeckVideoProps) => {
  const video = useRef<HTMLVideoElement>(null);
  const [time, setTime] = useState(0);
  const [length, setLength] = useState<number | null>(null);
  const at = currentChapter(
    chapters.map((c) => c.t),
    time * 1000,
  );

  const playing = (on: boolean) => onPlaying?.(on ? at : null);
  const seek = (ms: number) => {
    const el = video.current;
    if (!el) return;
    el.currentTime = ms / 1000;
    el.play().catch(() => undefined);
  };

  return (
    <section className="hx-deck-lvideo">
      <div className="hx-deck-video">
        <video
          ref={video}
          src={src}
          controls
          playsInline
          preload="metadata"
          onTimeUpdate={(event) => setTime(event.currentTarget.currentTime)}
          onLoadedMetadata={(event) => setLength(event.currentTarget.duration)}
          onPlay={() => playing(true)}
          onPause={() => playing(false)}
          onEnded={() => playing(false)}
        />
      </div>
      <div className="hx-deck-chaps">
        <div className="hx-deck-chaps__h">
          {t('next.deck.guest.chapters', {
            len: length !== null && Number.isFinite(length) ? clock(length * 1000) : '',
          })}
        </div>
        {chapters.map((chapter, i) => (
          <button
            key={chapter.id}
            type="button"
            className="hx-deck-chap"
            data-on={at === i ? '1' : '0'}
            aria-current={at === i ? 'true' : undefined}
            onClick={() => seek(chapter.t)}
          >
            <Mini chapter={chapter} />
            <span>
              {chapter.title}
              <small>{clock(chapter.t)}</small>
            </span>
            {at === i && <Icon name="play" size="sm" />}
          </button>
        ))}
      </div>
    </section>
  );
};

export default DeckVideo;
