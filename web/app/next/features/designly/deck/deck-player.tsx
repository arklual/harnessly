// The picture of a recording: the slide that was on screen at the moment (from the takes), the camera's circle in a
// corner, the subtitle under it, and a play button. The video is the camera and the voice; the slide is drawn by the
// page as the runtime will draw it into the file. With the montage's cut on, playback jumps over what the cut takes.
import { useEffect, useRef, useState } from 'react';
import { deckRecMediaHref } from '@/next/features/designly/deck/api';
import { cutRanges, skipTarget, slideAt, subtitleAt } from '@/next/features/designly/deck/cut-model';
import DeckMini from '@/next/features/designly/deck/deck-mini';
import type { IDeckEdit, IDeckRecording, IDeckSlide } from '@/next/features/designly/deck/types';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';

export interface IDeckSeek {
  ms: number;
  /** A new object each time: seeking to the same moment twice is two requests. */
  at: object;
}

interface IDeckPlayerProps {
  machine: MachineId;
  artifact: string;
  rec: IDeckRecording;
  edit: IDeckEdit;
  slides: readonly IDeckSlide[];
  frameSrc: (path: string) => string | null;
  /** Draw the subtitle line (the montage's «Субтитры»). */
  subs: boolean;
  seek?: IDeckSeek | null;
  /** The time of the video, ms, as it plays. */
  onTime?: (ms: number) => void;
}

const DeckPlayer = ({
  machine,
  artifact,
  rec,
  edit,
  slides,
  frameSrc,
  subs,
  seek,
  onTime,
}: IDeckPlayerProps) => {
  const video = useRef<HTMLVideoElement>(null);
  const [playing, setPlaying] = useState(false);
  const [ms, setMs] = useState(0);
  const ranges = cutRanges(rec.segments, edit);

  const here = (time: number) => {
    setMs(time);
    onTime?.(time);
  };

  useEffect(() => {
    const el = video.current;
    if (el && seek) {
      el.currentTime = seek.ms / 1000;
      here(seek.ms);
    }
    // a seek is an event, taken when its object is new
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [seek]);

  // while playing, look every frame so a cut is jumped over before it is heard
  useEffect(() => {
    if (!playing) return;
    let frame = 0;
    const tick = () => {
      const el = video.current;
      if (el) {
        let now = el.currentTime * 1000;
        const target = edit.cut ? skipTarget(ranges, now) : null;
        if (target !== null) {
          el.currentTime = target / 1000;
          now = target;
        }
        here(now);
      }
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
    // ranges is derived from the same two inputs as below
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [playing, rec.segments, edit.cut, edit.kept]);

  const toggle = () => {
    const el = video.current;
    if (!el) return;
    if (el.paused) void el.play().catch(() => setPlaying(false));
    else el.pause();
  };

  const page = slideAt(rec.summary.takes, ms) ?? rec.summary.slides[0] ?? null;
  const slide = slides.find((s) => s.id === page) ?? slides[0];
  const camera = rec.summary.camera && edit.cam !== 'off';
  const sub = subs ? subtitleAt(rec.segments, edit, ms) : '';

  return (
    <div className="hx-deckm-video" data-playing={playing ? '1' : '0'}>
      {slide ? <DeckMini src={frameSrc(slide.path)} w={slide.w} h={slide.h} title={slide.title} /> : null}
      <video
        className="hx-deckm-video__cam"
        ref={video}
        src={deckRecMediaHref(machine, artifact, rec.summary.id)}
        data-cam={camera ? edit.cam : 'off'}
        preload="metadata"
        playsInline
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => setPlaying(false)}
      />
      {sub ? <div className="hx-deckm-video__sub">{sub}</div> : null}
      <button
        className="hx-deckm-video__play"
        type="button"
        onClick={toggle}
        aria-label={t(playing ? 'next.deck.done.pause' : 'next.deck.done.play')}
      >
        <Icon name={playing ? 'pause' : 'play'} />
      </button>
    </div>
  );
};

export default DeckPlayer;
