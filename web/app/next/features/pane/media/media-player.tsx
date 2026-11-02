// The player of a video or a sound the pane opens: a browser's element under our own controls. The time runs in the CSS
// variable --p, written from a frame loop and not through React, so the bar is smooth and a playing file renders nothing.
// A video fits its frame to the pane by its own proportions; its controls leave after a pause in the pointer's moves. Keys
// work while the focus is in the player (a click puts it there), so typing in the chat's field never drives it.
import { useEffect, useEffectEvent, useRef, useState, type KeyboardEvent } from 'react';
import { mediaSelection, type IFileSelection } from '@/next/features/pane/file-selection';
import { parseTimePlace } from '@/next/features/pane/file-place';
import Icon from '@/next/ui/icon';
import Tooltip from '@/next/ui/tooltip';
import { t } from '@/next/i18n';
import MediaTrack from '@/next/features/pane/media/media-track';
import { useAudioWave, WAVE_MAX_SECONDS } from '@/next/features/pane/media/use-audio-wave';
import {
  clamp01,
  FRAME_SECONDS,
  formatTime,
  JUMP_SECONDS,
  MEDIA_ACTION,
  MEDIA_KIND,
  mediaActionOf,
  nextRate,
  tenthOf,
  VOLUME_STEP,
  type TMediaKind,
  type TRate,
  nextMark,
  type IMediaMark,
} from '@/next/features/pane/media/media-model';

const REPORT_PAUSE_MS = 200;

export interface IMediaPlayerProps {
  path: string;
  onSelection?: (selection: IFileSelection | null) => void;
  /** A time or stretch the agent asked to show; onPlaced when it is there. */
  place?: string | null;
  onPlaced?: () => void;
  src: string;
  kind: TMediaKind;
  name: string;
  /** Where to save the file: a file the browser cannot play is still the person's. */
  saveHref: string;
}

const IDLE_MS = 2600;
const VOLUME_KEY = 'hx.media.volume';

const savedVolume = (): number => {
  try {
    const kept = Number(localStorage.getItem(VOLUME_KEY) ?? 1);
    return Number.isFinite(kept) ? clamp01(kept) : 1;
  } catch {
    return 1;
  }
};

const keepVolume = (volume: number) => {
  try {
    localStorage.setItem(VOLUME_KEY, String(volume));
  } catch {
    // A browser that keeps nothing starts at full volume next time.
  }
};

interface IControlProps {
  on?: boolean;
  icon: string;
  label: string;
  keys?: string[];
  onClick: () => void;
}

const Control = ({ icon, label, keys, on, onClick }: IControlProps) => (
  <Tooltip label={label} keys={keys}>
    <button
      type="button"
      className="hx-media__btn"
      aria-label={label}
      data-on={on || undefined}
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
    >
      <Icon name={icon} size="sm" />
    </button>
  </Tooltip>
);

const Wave = ({ peaks, played }: { peaks: number[] | null; played?: boolean }) => (
  <svg
    className="hx-media__wave"
    data-played={played ? 'true' : undefined}
    viewBox={`0 0 ${peaks?.length ?? 1} 40`}
    preserveAspectRatio="none"
    aria-hidden="true"
  >
    {peaks?.map((peak, bar) => {
      const height = Math.max(1.5, peak * 38);
      return <rect key={bar} x={bar + 0.15} y={20 - height / 2} width={0.7} height={height} rx={0.3} />;
    })}
  </svg>
);

const MediaPlayer = ({
  src,
  kind,
  name,
  path,
  saveHref,
  onSelection,
  place,
  onPlaced,
}: IMediaPlayerProps) => {
  const video = kind === MEDIA_KIND.video;
  const media = useRef<HTMLMediaElement | null>(null);
  const root = useRef<HTMLDivElement | null>(null);
  const preview = useRef<HTMLVideoElement | null>(null);
  const pendingPreview = useRef<number | null>(null);
  const idleTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [playing, setPlaying] = useState(false);
  const [waiting, setWaiting] = useState(false);
  const [duration, setDuration] = useState<number | null>(null);
  const [time, setTime] = useState(0);
  const [volume, setVolume] = useState(savedVolume);
  const [muted, setMuted] = useState(false);
  const [rate, setRate] = useState<TRate>(1);
  const [full, setFull] = useState(false);
  const [idle, setIdle] = useState(false);
  const [failed, setFailed] = useState(false);
  const [hover, setHover] = useState<number | null>(null);
  const [previewOn, setPreviewOn] = useState(false);
  const [mark, setMark] = useState<IMediaMark | null>(null);
  const fromAgent = useRef(false);
  const peaks = useAudioWave(src, !video && duration !== null && duration <= WAVE_MAX_SECONDS);

  const paint = (seconds: number, length: number | null) => {
    root.current?.style.setProperty('--p', length ? String(clamp01(seconds / length)) : '0');
  };

  // The bar follows the picture frame by frame while it plays.
  useEffect(() => {
    if (!playing) return;
    let frame = requestAnimationFrame(function tick() {
      const element = media.current;
      if (element) paint(element.currentTime, element.duration);
      frame = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(frame);
  }, [playing]);

  useEffect(() => {
    const onFull = () => setFull(document.fullscreenElement === root.current);
    document.addEventListener('fullscreenchange', onFull);
    return () => document.removeEventListener('fullscreenchange', onFull);
  }, []);

  // The element stops with the player: a closed tab does not go on sounding.
  useEffect(() => {
    const element = media.current;
    return () => {
      element?.pause();
      if (idleTimer.current !== null) clearTimeout(idleTimer.current);
    };
  }, []);

  // What is marked, or the moment it stands paused at once the person moved it, goes to the chat. Asked from a place by the
  // agent, it is no selection of the person's.
  const reportable = time > 0 && !playing;
  const selected = mark ? `${mark.from}-${mark.to}` : reportable ? `${time}` : '';
  const reportNow = useEffectEvent(() => {
    if (fromAgent.current) {
      fromAgent.current = false;
      return;
    }
    if (mark) onSelection?.(mediaSelection(video, path, mark.from, mark.to));
    else if (reportable) onSelection?.(mediaSelection(video, path, time, null));
    else onSelection?.(null);
  });
  useEffect(() => {
    const timer = setTimeout(reportNow, REPORT_PAUSE_MS);
    return () => clearTimeout(timer);
  }, [selected]);

  const goToPlace = useEffectEvent((asked: string) => {
    const found = parseTimePlace(asked);
    if (found) {
      fromAgent.current = true;
      seek(found.from);
      setMark(found.to === null ? null : { from: found.from, to: found.to });
    }
    onPlaced?.();
  });
  useEffect(() => {
    if (place && duration !== null) goToPlace(place);
  }, [place, duration]);

  const toggle = () => {
    const element = media.current;
    if (!element) return;
    if (element.paused) void element.play().catch(() => setPlaying(false));
    else element.pause();
  };

  const seek = (seconds: number) => {
    const element = media.current;
    if (!element || duration === null) return;
    const next = Math.min(duration, Math.max(0, seconds));
    element.currentTime = next;
    setTime(next);
    paint(next, duration);
  };

  const changeVolume = (next: number) => {
    const element = media.current;
    const level = clamp01(next);
    if (element) {
      element.volume = level;
      element.muted = level === 0;
    }
    setVolume(level);
    setMuted(level === 0);
    keepVolume(level);
  };

  const toggleMute = () => {
    const element = media.current;
    if (!element) return;
    element.muted = !element.muted;
    setMuted(element.muted);
  };

  const toggleFull = () => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void root.current?.requestFullscreen();
  };

  const changeRate = () => {
    const next = nextRate(rate);
    if (media.current) media.current.playbackRate = next;
    setRate(next);
  };

  const wake = () => {
    if (idleTimer.current !== null) clearTimeout(idleTimer.current);
    setIdle(false);
    idleTimer.current = setTimeout(() => setIdle(true), IDLE_MS);
  };

  const hoverAt = (share: number | null) => {
    setHover(share);
    if (share === null || duration === null || !video) return;
    root.current?.style.setProperty('--x', String(share));
    setPreviewOn(true);
    const element = preview.current;
    // One seek at a time: the frame the pointer is at now comes next, those it passed are skipped.
    if (!element || element.readyState === 0 || element.seeking) pendingPreview.current = share * duration;
    else element.currentTime = share * duration;
  };

  const seekPreview = () => {
    const next = pendingPreview.current;
    pendingPreview.current = null;
    if (next !== null && preview.current) preview.current.currentTime = next;
  };

  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.altKey || event.ctrlKey || event.metaKey) return;
    const tenth = tenthOf(event.code);
    if (tenth !== null && duration !== null) {
      event.preventDefault();
      seek(tenth * duration);
      return;
    }
    const action = mediaActionOf(event.code);
    if (action === null) return;
    // The focus on a button: Space presses that button, as it should.
    if (event.code === 'Space' && event.target instanceof HTMLButtonElement) return;
    event.preventDefault();
    if (action === MEDIA_ACTION.toggle) toggle();
    if (action === MEDIA_ACTION.mark) setMark((before) => nextMark(before, time));
    if (action === MEDIA_ACTION.back) seek(time - JUMP_SECONDS);
    if (action === MEDIA_ACTION.forward) seek(time + JUMP_SECONDS);
    if (action === MEDIA_ACTION.frameBack) seek(time - FRAME_SECONDS);
    if (action === MEDIA_ACTION.frameForward) seek(time + FRAME_SECONDS);
    if (action === MEDIA_ACTION.louder) changeVolume(volume + VOLUME_STEP);
    if (action === MEDIA_ACTION.quieter) changeVolume(volume - VOLUME_STEP);
    if (action === MEDIA_ACTION.mute) toggleMute();
    if (action === MEDIA_ACTION.full && video) toggleFull();
    if (action === MEDIA_ACTION.start) seek(0);
    if (action === MEDIA_ACTION.end && duration !== null) seek(duration);
  };

  const events = {
    onLoadedMetadata: () => {
      const element = media.current;
      if (!element) return;
      // The file changed under the player (a render finished): the same moment of the new one.
      if (time > 0) element.currentTime = Math.min(time, element.duration);
      // A video whose picture the browser cannot decode loads as sound alone: a black frame is no player.
      if (element instanceof HTMLVideoElement) {
        if (element.videoWidth === 0) {
          setFailed(true);
          return;
        }
        root.current?.style.setProperty('--ar', String(element.videoWidth / element.videoHeight));
      }
      element.volume = volume;
      setDuration(Number.isFinite(element.duration) ? element.duration : null);
    },
    onDurationChange: () => {
      const length = media.current?.duration;
      if (length !== undefined && Number.isFinite(length)) setDuration(length);
    },
    onTimeUpdate: () => {
      const element = media.current;
      if (!element) return;
      setTime(element.currentTime);
      paint(element.currentTime, element.duration);
    },
    onProgress: () => {
      const element = media.current;
      if (!element || !element.duration || element.buffered.length === 0) return;
      const end = element.buffered.end(element.buffered.length - 1);
      root.current?.style.setProperty('--b', String(clamp01(end / element.duration)));
    },
    onPlay: () => {
      setPlaying(true);
      wake();
    },
    onPause: () => setPlaying(false),
    onEnded: () => setPlaying(false),
    onWaiting: () => setWaiting(true),
    onPlaying: () => setWaiting(false),
    onCanPlay: () => setWaiting(false),
    onError: () => setFailed(true),
  };

  if (failed)
    return (
      <div className="hx-media__failed" role="status">
        <Icon name={video ? 'film' : 'music'} />
        <span className="hx-media__failed-title">{t('next.media.cantPlay')}</span>
        <a className="hx-media__failed-save" href={saveHref} download>
          {t('next.delivered.save')}
        </a>
      </div>
    );

  const clock = `${formatTime(time)} / ${formatTime(duration)}`;
  const sound = muted ? 0 : volume;

  const controls = (
    <div className="hx-media__controls">
      <MediaTrack
        className="hx-media__track"
        label={t('next.media.seek')}
        now={duration ? (time / duration) * 100 : 0}
        text={clock}
        onPick={(share) => duration !== null && seek(share * duration)}
        onStretch={(from, to) =>
          duration !== null &&
          setMark({ from: Math.min(from, to) * duration, to: Math.max(from, to) * duration })
        }
        onHover={hoverAt}
      >
        {mark && duration !== null && duration > 0 && (
          <span
            className="hx-media__stretch"
            ref={(element) => {
              element?.style.setProperty('--a', String(clamp01(mark.from / duration)));
              element?.style.setProperty('--b', String(clamp01((mark.to ?? mark.from) / duration)));
            }}
          />
        )}
        {video ? (
          <span className="hx-media__rail">
            <span className="hx-media__buffered" />
            <span className="hx-media__played" />
          </span>
        ) : (
          <span className="hx-media__wave-box">
            <Wave peaks={peaks} />
            <Wave peaks={peaks} played />
          </span>
        )}
      </MediaTrack>
      <div className="hx-media__row">
        <Control
          icon={playing ? 'pause' : 'play'}
          label={t(playing ? 'next.media.pause' : 'next.media.play')}
          keys={['Space']}
          onClick={toggle}
        />
        <span className="hx-media__clock">{clock}</span>
        <Control
          icon="locate"
          label={t('next.media.mark')}
          keys={['S']}
          on={mark !== null}
          onClick={() => setMark((before) => nextMark(before, time))}
        />
        <span className="hx-media__grow" />
        <div className="hx-media__volume">
          <Control
            icon={sound === 0 ? 'volume-x' : 'volume-2'}
            label={t(muted ? 'next.media.unmute' : 'next.media.mute')}
            keys={['M']}
            onClick={toggleMute}
          />
          <MediaTrack
            className="hx-media__vol-track"
            label={t('next.media.volume')}
            now={sound * 100}
            text={`${Math.round(sound * 100)}%`}
            onPick={changeVolume}
          >
            <span
              className="hx-media__vol-rail"
              ref={(element) => element?.style.setProperty('--v', String(sound))}
            />
          </MediaTrack>
        </div>
        <Tooltip label={t('next.media.speed')}>
          <button
            type="button"
            className="hx-media__rate"
            data-changed={rate === 1 ? undefined : 'true'}
            aria-label={t('next.media.speed')}
            onClick={(event) => {
              event.stopPropagation();
              changeRate();
            }}
          >
            {rate}×
          </button>
        </Tooltip>
        {video && document.pictureInPictureEnabled && (
          <Control
            icon="picture-in-picture-2"
            label={t('next.media.pip')}
            onClick={() => {
              if (document.pictureInPictureElement) void document.exitPictureInPicture();
              else if (media.current instanceof HTMLVideoElement)
                void media.current.requestPictureInPicture();
            }}
          />
        )}
        {video && document.fullscreenEnabled && (
          <Control
            icon={full ? 'minimize-2' : 'maximize-2'}
            label={t(full ? 'next.media.exitFullscreen' : 'next.media.fullscreen')}
            keys={['F']}
            onClick={toggleFull}
          />
        )}
      </div>
    </div>
  );

  return (
    <div
      ref={root}
      className="hx-media"
      data-kind={kind}
      data-playing={playing ? 'true' : undefined}
      data-idle={idle && playing && hover === null ? 'true' : undefined}
      data-waiting={waiting ? 'true' : undefined}
      data-full={full ? 'true' : undefined}
      tabIndex={0}
      aria-label={name}
      onKeyDown={onKey}
      onPointerMove={video ? wake : undefined}
      onPointerLeave={video && playing ? () => setIdle(true) : undefined}
    >
      {video ? (
        <div className="hx-media__stage" onClick={toggle} onDoubleClick={toggleFull}>
          <video
            ref={(element) => {
              media.current = element;
            }}
            className="hx-media__video"
            src={`${src}#t=0.001`}
            preload="metadata"
            playsInline
            {...events}
          />
          <span className="hx-media__big" aria-hidden="true">
            <Icon name="play" />
          </span>
          <span className="hx-media__busy hx-spin" aria-hidden="true" data-busy="true" />
          {previewOn && (
            <span className="hx-media__preview" data-on={hover === null ? undefined : 'true'}>
              <video
                ref={preview}
                className="hx-media__preview-frame"
                src={src}
                preload="metadata"
                muted
                playsInline
                onLoadedMetadata={seekPreview}
                onSeeked={seekPreview}
              />
              <span className="hx-media__preview-time">
                {formatTime(hover === null || duration === null ? null : hover * duration)}
              </span>
            </span>
          )}
          {controls}
        </div>
      ) : (
        <>
          <audio
            ref={(element) => {
              media.current = element;
            }}
            src={src}
            preload="metadata"
            {...events}
          />
          {controls}
        </>
      )}
    </div>
  );
};

export default MediaPlayer;
