// The live player and its stage (WP3a): two frames (the scene on screen and the next), swapped at scene boundaries and
// scaled to the format, driven through the bridge (tk:video-seek/-play/-pause) by the host's clock; or the published MP4
// in a `<video>`. The format and the source are switched in the bar (video-bar.tsx). The playhead is the view's (`time`); the player reports where it is while it plays. Keys: play/pause,
// frame step, ±5 s / ±10 s, scenes, 0–9, I/O, C, F (use-video-keys.ts). A click on the picture pauses it while it plays
// and, while it is paused, picks the block under the pointer for a comment.
import { useEffect, useEffectEvent, useRef, useState, type MouseEvent } from 'react';
import { formatMs } from '@/next/features/designly/deck/clock';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { videoFileHref } from '@/next/features/designly/video/api';
import { postVideo, readVideoMessage, type TVideoBoardMessage } from '@/next/features/designly/video/bridge';
import {
  clampTime,
  clockAt,
  clockPause,
  clockPlay,
  clockSeek,
  clockStart,
  clockTick,
  frameMs,
  markIn,
  markOut,
  needsResync,
  nextSceneTarget,
  prevSceneTarget,
  sceneAt,
  STEP_BIG_MS,
  STEP_SMALL_MS,
  subtitleAt,
  tenthOf,
  type IClock,
} from '@/next/features/designly/video/video-clock';
import VideoOverlayLayer from '@/next/features/designly/video/video-overlays';
import VideoOverlayInspector from '@/next/features/designly/video/video-overlay-inspector';
import { newCaptionPatch, overlayWire } from '@/next/features/designly/video/overlays-model';
import type { IVideoOverlayEditor } from '@/next/features/designly/video/use-video-overlays';
import VideoStage, { type TVideoSlotIndex } from '@/next/features/designly/video/video-stage';
import { VIDEO_KEY, useVideoKeys, type TVideoKeyAction } from '@/next/features/designly/video/use-video-keys';
import {
  VIDEO_SOURCE,
  type IVideoPick,
  type IVideoRange,
  type IVideoState,
  type TVideoFormat,
  type TVideoSource,
} from '@/next/features/designly/video/types';
import type { MachineId } from '@/next/protocol/v2';
import type { IPlayerCommand } from '@/next/features/designly/video/use-video-view';

export interface IVideoPlayerProps {
  machine: MachineId;
  id: string;
  video: IVideoState;
  /** The format on screen: one of the video's enabled formats. */
  format: TVideoFormat;
  /** The slot's ticket address of the board on screen (`path`); null while the slot opens. It opens one frame, once. */
  src: string | null;
  /** The board `src` opens (the site's page on screen). */
  path: string | null;
  /** The plain address of any board once the first frame loaded. */
  frameSrc: (path: string) => string | null;
  source: TVideoSource;
  onSource: (source: TVideoSource) => void;
  /** The playhead, ms of the whole video. The player reports where it is while it plays. */
  time: number;
  onTime: (time: number) => void;
  /** The selected stretch (I/O); null when none. */
  range: IVideoRange | null;
  onRange: (range: IVideoRange | null) => void;
  /** Loop the range while playing. */
  loop: boolean;
  /** C: comment the moment (or the range) under the playhead. */
  onComment: () => void;
  /** A block picked in the scene for a comment (`tk:video-hit`). */
  onPick: (pick: IVideoPick) => void;
  /** What the agent asked the player to do; a new `seq` acts once. */
  command?: IPlayerCommand | null;
  /** Says whether it is playing, for what the agent is told. */
  onPlaying?: (playing: boolean) => void;
  /** The overlay layer's editing. */
  overlays: IVideoOverlayEditor;
}

/** A playhead this far from where the player is (while it plays) is a seek of the view's, ms. */
const EXTERNAL_SEEK_MS = 250;

const SLOTS: readonly TVideoSlotIndex[] = [0, 1];

const nowMs = () => performance.now();

const originOf = (src: string | null): string | null =>
  src === null ? null : new URL(src, window.location.href).origin;

const VideoPlayer = ({
  machine,
  id,
  video,
  format,
  src,
  path,
  frameSrc,
  source,
  onSource,
  time,
  onTime,
  range,
  onRange,
  loop,
  onComment,
  onPick,
  command = null,
  onPlaying,
  overlays,
}: IVideoPlayerProps) => {
  const total = video.durMs;
  const scenes = video.scenes;
  const live = source === VIDEO_SOURCE.live;
  const root = useRef<HTMLDivElement>(null);
  const slide = useRef<HTMLDivElement>(null);
  const frameA = useRef<HTMLIFrameElement>(null);
  const frameB = useRef<HTMLIFrameElement>(null);
  const frameRefs = [frameA, frameB] as const;
  const mp4 = useRef<HTMLVideoElement>(null);
  const clock = useRef<IClock>(clockStart(0));
  const reported = useRef(0);
  const hits = useRef(new Map<number, { page: string; x: number; y: number }>());
  const hitId = useRef(0);
  const [playing, setPlaying] = useState(false);
  const [full, setFull] = useState(false);
  // The address each frame has said it is ready for.
  const [ready, setReady] = useState<readonly [string | null, string | null]>([null, null]);
  const [front, setFront] = useState<TVideoSlotIndex>(0);
  const [failure, setFailure] = useState<string | null>(null);

  const at = sceneAt(scenes, time);
  const index = at?.index ?? 0;
  const current = scenes[index];
  const currentSlot: TVideoSlotIndex = index % 2 === 0 ? 0 : 1;
  // The scene each frame holds: the one on screen and the next.
  const sceneOf = (slot: TVideoSlotIndex): number | null => {
    if (scenes.length === 0) return null;
    if (slot === currentSlot) return index;
    return index + 1 < scenes.length ? index + 1 : null;
  };
  // The ticket address sets the frames' cookie and is spent once its frame has loaded: only its own scene takes it, and
  // only while that scene stays in its slot; any later load of it is the plain address.
  const [spent, setSpent] = useState(false);
  const ticketScene = scenes.findIndex((s) => s.path === path);
  // The plain address is refused until the ticket's frame has set the cookie, and a refused frame is not loaded again:
  // the other frame waits for it, whenever that frame is on the way.
  const ticketPending =
    !spent &&
    ticketScene !== -1 &&
    (src === null || !ready.includes(src)) &&
    (index === ticketScene || index + 1 === ticketScene);
  const addressOf = (i: number | null): string | null => {
    if (i === null) return null;
    if (!spent && i === ticketScene) return src;
    if (ticketPending) return null;
    return frameSrc(scenes[i].path);
  };
  const slotSrc = (slot: TVideoSlotIndex) => addressOf(sceneOf(slot));
  const slots = [
    { src: slotSrc(0), title: slotTitle(sceneOf(0)) },
    { src: slotSrc(1), title: slotTitle(sceneOf(1)) },
  ] as const;
  function slotTitle(i: number | null): string {
    return i === null ? '' : t('next.video.player.scene', { n: i + 1 });
  }
  const isReady = (slot: TVideoSlotIndex) => slots[slot].src !== null && ready[slot] === slots[slot].src;
  if (
    !spent &&
    src !== null &&
    ready.includes(src) &&
    sceneOf(0) !== ticketScene &&
    sceneOf(1) !== ticketScene
  )
    setSpent(true);

  // The frame that shows is the current scene's once it is ready; until then the one that was.
  if (live && isReady(currentSlot) && front !== currentSlot) setFront(currentSlot);

  const publishedInfo = video.renders.find((r) => r.format === format);
  const hasMp4 = publishedInfo !== undefined && publishedInfo.published;

  // ---- the scenes

  const push = (slot: TVideoSlotIndex, scene: number | null) => {
    const frame = frameRefs[slot].current;
    if (frame === null || scene === null) return;
    const address = slots[slot].src;
    const t0 = clock.current;
    const here = clockAt(t0, nowMs(), total);
    const place = sceneAt(scenes, here);
    if (place === null) return;
    if (scene === place.index) {
      if (t0.playing) postVideo(frame, address, { type: 'tk:video-play', t: place.local, rate: t0.rate });
      else postVideo(frame, address, { type: 'tk:video-seek', t: place.local });
      return;
    }
    postVideo(frame, address, { type: 'tk:video-pause' });
    postVideo(frame, address, { type: 'tk:video-seek', t: 0 });
  };
  /** Tells both frames where the clock is: the current scene plays or shows its time, the other waits at its start. */
  const sync = () => {
    if (!live) return;
    for (const slot of SLOTS) if (isReady(slot)) push(slot, sceneOf(slot));
  };
  // The same from effects, which must see the latest props.
  const syncNow = useEffectEvent(sync);

  // The scene's words for its frame: the transition and the overlay layer (the one being typed is left out: the field is it).
  const sceneMessage = (scene: number) => ({
    type: 'tk:video-scene' as const,
    transition: scenes[scene].transition,
    overlays: overlays
      .overlaysOf(scenes[scene])
      .filter((o) => o.id !== overlays.editing)
      .map(overlayWire),
  });
  const sendScene = useEffectEvent((slot: TVideoSlotIndex) => {
    const scene = sceneOf(slot);
    if (scene === null || !isReady(slot)) return;
    postVideo(frameRefs[slot].current, slots[slot].src, sceneMessage(scene));
  });
  const overlayKey = (slot: TVideoSlotIndex): string => {
    const scene = sceneOf(slot);
    return scene === null ? '' : JSON.stringify(sceneMessage(scene).overlays);
  };
  const key0 = overlayKey(0);
  const key1 = overlayKey(1);
  // A change of the layer (a drag, a typed text, the agent's edit) goes to the frames that hold the scene.
  useEffect(() => {
    sendScene(0);
    sendScene(1);
  }, [key0, key1]);

  const take = useEffectEvent((slot: TVideoSlotIndex, message: TVideoBoardMessage) => {
    const address = slots[slot].src;
    switch (message.k) {
      case 'ready': {
        setReady((was) => (slot === 0 ? [address, was[1]] : [was[0], address]));
        setFailure(null);
        const frame = frameRefs[slot].current;
        const scene = sceneOf(slot);
        if (frame === null || scene === null) return;
        postVideo(frame, address, sceneMessage(scene));
        push(slot, scene);
        return;
      }
      case 'time': {
        if (slot !== currentSlot || !clock.current.playing) return;
        const place = sceneAt(scenes, clockAt(clock.current, nowMs(), total));
        if (place !== null && needsResync(place.local, message.t)) push(slot, place.index);
        return;
      }
      case 'hit': {
        const pending = hits.current.get(message.id);
        if (pending === undefined) return;
        hits.current.delete(message.id);
        onPick({ page: pending.page, block: message.block, x: pending.x, y: pending.y });
        return;
      }
      case 'error':
        if (slot === currentSlot) setFailure(message.msg);
    }
  });

  // An effect event: the ticket may come after the player mounted, so the addresses are the ones of the moment.
  const listen = useEffectEvent((event: MessageEvent) => {
    for (const slot of SLOTS) {
      const win = frameRefs[slot].current?.contentWindow;
      if (!win || event.source !== win) continue;
      if (event.origin !== originOf(slots[slot].src)) return;
      const read = readVideoMessage(event.data);
      if (read !== null) take(slot, read);
      return;
    }
  });

  useEffect(() => {
    const hear = (event: MessageEvent) => listen(event);
    window.addEventListener('message', hear);
    return () => window.removeEventListener('message', hear);
  }, []);

  // A scene on screen, a format or a source changed: both frames are told again.
  useEffect(() => {
    syncNow();
  }, [index, format, live, ready]);

  // ---- the transport

  const report = (t: number) => {
    const rounded = Math.round(t);
    reported.current = rounded;
    onTime(rounded);
  };
  const seekTo = (target: number) => {
    const to = clampTime(target, total);
    if (live) {
      clock.current = clockSeek(clock.current, to, nowMs(), total);
      sync();
    } else if (mp4.current) {
      mp4.current.currentTime = to / 1000;
    }
    report(to);
  };
  const playNow = () => {
    if (live) {
      clock.current = clockPlay(clock.current, nowMs(), total, range);
      setPlaying(true);
      report(clock.current.t);
      sync();
    } else if (mp4.current) {
      void mp4.current.play();
    }
  };
  const pauseNow = () => {
    if (live) {
      clock.current = clockPause(clock.current, nowMs(), total);
      setPlaying(false);
      report(clock.current.t);
      sync();
    } else {
      mp4.current?.pause();
    }
  };
  const toggle = () => (playing ? pauseNow() : playNow());
  const where = () =>
    live ? clockAt(clock.current, nowMs(), total) : (mp4.current?.currentTime ?? 0) * 1000;
  const jump = (delta: number) => seekTo(where() + delta);

  // While the live clock runs: report the time, swap scenes (the effect above), stop at the end, loop the range.
  useEffect(() => {
    if (!live || !playing) return;
    let frame = 0;
    const tick = () => {
      const was = clock.current;
      const next = clockTick(was, nowMs(), total, range, loop);
      clock.current = next;
      report(clockAt(next, nowMs(), total));
      if (next !== was) {
        setPlaying(next.playing);
        syncNow();
      }
      if (next.playing) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
    // report is stable in effect; the clock is a ref
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live, playing, total, range, loop]);

  // The MP4 reports its own time, and loops the range.
  useEffect(() => {
    if (live || !playing) return;
    let frame = 0;
    const tick = () => {
      const el = mp4.current;
      if (el) {
        const now = el.currentTime * 1000;
        if (loop && range !== null && reported.current < range.t1 && now >= range.t1) {
          el.currentTime = range.t0 / 1000;
          report(range.t0);
        } else {
          report(now);
        }
      }
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live, playing, range, loop]);

  // What the agent asked: acts once per command, after the seek of the same command has landed.
  const commandSeq = command?.seq ?? 0;
  useEffect(() => {
    if (command === null) return;
    const timer = setTimeout(() => {
      if (command.kind === 'play') playNow();
      else pauseNow();
    }, 0);
    return () => clearTimeout(timer);
    // one command, one action
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [commandSeq]);

  useEffect(() => {
    onPlaying?.(playing);
  }, [playing, onPlaying]);

  // The playhead moved by the view (the timeline, a comment): seek there. While playing, the player's own reports come
  // back through `time` a render late, so only a jump counts.
  useEffect(() => {
    if (time === reported.current) return;
    const here = where();
    if (playing && Math.abs(time - here) <= EXTERNAL_SEEK_MS) return;
    seekTo(time);
    // seeking is the effect of a new playhead only
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [time]);

  // A source switch stops playing; the MP4 starts where the live player was.
  const [wasLive, setWasLive] = useState(live);
  if (wasLive !== live) {
    setWasLive(live);
    setPlaying(false);
  }
  useEffect(() => {
    if (live) {
      clock.current = clockSeek(clockPause(clock.current, nowMs(), total), reported.current, nowMs(), total);
      syncNow();
    } else {
      mp4.current?.pause();
    }
    // a switch of source is the only trigger
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live]);

  // An MP4 that is not there for this format cannot be the source.
  useEffect(() => {
    if (!live && !hasMp4) onSource(VIDEO_SOURCE.live);
  }, [live, hasMp4, onSource]);

  useEffect(() => {
    const change = () => setFull(document.fullscreenElement === root.current);
    document.addEventListener('fullscreenchange', change);
    return () => document.removeEventListener('fullscreenchange', change);
  }, []);

  const toggleFull = () => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void root.current?.requestFullscreen();
  };

  const onKey = (action: TVideoKeyAction) => {
    switch (action.key) {
      case VIDEO_KEY.toggle:
        return toggle();
      case VIDEO_KEY.back5:
        return jump(-STEP_SMALL_MS);
      case VIDEO_KEY.forward5:
        return jump(STEP_SMALL_MS);
      case VIDEO_KEY.back10:
        return jump(-STEP_BIG_MS);
      case VIDEO_KEY.forward10:
        return jump(STEP_BIG_MS);
      case VIDEO_KEY.frameBack:
        pauseNow();
        return jump(-frameMs(video.settings.fps));
      case VIDEO_KEY.frameForward:
        pauseNow();
        return jump(frameMs(video.settings.fps));
      case VIDEO_KEY.prevScene:
        return seekTo(prevSceneTarget(scenes, where()));
      case VIDEO_KEY.nextScene: {
        const target = nextSceneTarget(scenes, where());
        if (target !== null) seekTo(target);
        return;
      }
      case VIDEO_KEY.start:
        return seekTo(0);
      case VIDEO_KEY.end:
        return seekTo(total);
      case VIDEO_KEY.tenth:
        return seekTo(tenthOf(action.n, total));
      case VIDEO_KEY.markIn:
        return onRange(markIn(range, where(), total));
      case VIDEO_KEY.markOut:
        return onRange(markOut(range, where()));
      case VIDEO_KEY.comment:
        pauseNow();
        return onComment();
      case VIDEO_KEY.full:
        return toggleFull();
    }
  };
  useVideoKeys(true, onKey);

  // A click on the picture: pause while it plays; while paused, ask the scene what is under the pointer.
  const onSlideClick = (event: MouseEvent<HTMLDivElement>) => {
    if (playing) {
      pauseNow();
      return;
    }
    if (!live || current === undefined || event.detail === 0) return;
    const box = (slide.current ?? event.currentTarget).getBoundingClientRect();
    if (box.width <= 0 || box.height <= 0) return;
    const x = ((event.clientX - box.left) / box.width) * 100;
    const y = ((event.clientY - box.top) / box.height) * 100;
    const size = frameRefs[currentSlot].current;
    if (size === null) return;
    hitId.current += 1;
    hits.current.set(hitId.current, { page: current.id, x, y });
    const scale = size.offsetWidth / box.width;
    postVideo(size, slots[currentSlot].src, {
      type: 'tk:video-hit',
      id: hitId.current,
      x: (event.clientX - box.left) * scale,
      y: (event.clientY - box.top) * scale,
    });
  };

  const addOverlay = async () => {
    if (current === undefined || at === null) return;
    pauseNow();
    const id = await overlays.add(
      current.id,
      newCaptionPatch(at.local, current.dur, t('next.video.overlay.newText')),
    );
    if (id === null) return;
    overlays.setOn(true);
    overlays.select(id);
    overlays.setEditing(id);
  };
  const editing = live && overlays.on && !playing && current !== undefined;
  const selectedOverlay =
    editing && overlays.selected !== null
      ? (overlays.overlaysOf(current).find((o) => o.id === overlays.selected) ?? null)
      : null;

  const sub =
    live && video.settings.subs && current !== undefined && at !== null
      ? subtitleAt(current.notes, at.local, current.dur)
      : '';

  return (
    <div ref={root} className="hx-video-player" data-full={full ? '1' : undefined} data-source={source}>
      <div className="hx-video__picture">
        {live ? (
          <VideoStage
            format={format}
            slots={slots}
            front={front}
            frameRefs={frameRefs}
            slideRef={slide}
            loading={!isReady(front) && !isReady(currentSlot)}
          >
            {sub !== '' && <div className="hx-video__sub">{sub}</div>}
            {failure !== null && (
              <div className="hx-video__failed" role="alert">
                <span>{t('next.video.player.sceneFailed')}</span>
                <small>{failure}</small>
              </div>
            )}
            {editing && at !== null ? (
              <VideoOverlayLayer editor={overlays} scene={current} local={at.local} format={format} />
            ) : null}
            <div className="hx-video__hit" data-playing={playing ? '1' : '0'} onClick={onSlideClick} />
          </VideoStage>
        ) : (
          <div className="hx-video__slot" data-format={format}>
            <div className="hx-video__slide">
              <video
                key={format}
                ref={mp4}
                className="hx-video__mp4"
                src={videoFileHref(machine, id, format)}
                preload="metadata"
                playsInline
                onLoadedMetadata={(event) => {
                  event.currentTarget.currentTime = reported.current / 1000;
                }}
                onPlay={() => setPlaying(true)}
                onPause={() => setPlaying(false)}
                onEnded={() => setPlaying(false)}
              />
              <div className="hx-video__hit" data-playing={playing ? '1' : '0'} onClick={onSlideClick} />
            </div>
          </div>
        )}
      </div>
      <div className="hx-video__controls">
        <div className="hx-video__transport">
          <button
            type="button"
            className="hx-video__btn"
            aria-label={t('next.video.player.prevScene')}
            onClick={() => seekTo(prevSceneTarget(scenes, where()))}
          >
            <Icon name="chevron-left" />
          </button>
          <button
            type="button"
            className="hx-video__btn"
            data-text="1"
            aria-label={t('next.video.player.back5')}
            onClick={() => jump(-STEP_SMALL_MS)}
          >
            −5
          </button>
          <button
            type="button"
            className="hx-video__btn"
            data-main="1"
            aria-label={t(playing ? 'next.video.player.pause' : 'next.video.player.play')}
            onClick={toggle}
          >
            <Icon name={playing ? 'pause' : 'play'} />
          </button>
          <button
            type="button"
            className="hx-video__btn"
            data-text="1"
            aria-label={t('next.video.player.forward5')}
            onClick={() => jump(STEP_SMALL_MS)}
          >
            +5
          </button>
          <button
            type="button"
            className="hx-video__btn"
            aria-label={t('next.video.player.nextScene')}
            onClick={() => {
              const target = nextSceneTarget(scenes, where());
              if (target !== null) seekTo(target);
            }}
          >
            <Icon name="chevron-right" />
          </button>
          <span className="hx-video__time">
            {formatMs(time)} / {formatMs(total)}
          </span>
        </div>
        {current !== undefined && (
          <VideoOverlayInspector
            editor={overlays}
            page={current.id}
            overlay={selectedOverlay}
            format={format}
          />
        )}
        <div className="hx-video__opts">
          {overlays.error !== null && (
            <span className="hx-video__fail" role="status">
              {overlays.error}
            </span>
          )}
          {live && overlays.on && (
            <button
              type="button"
              className="hx-video__btn"
              data-text="1"
              disabled={playing}
              onClick={() => void addOverlay()}
            >
              {t('next.video.overlay.add')}
            </button>
          )}
          {live && (
            <button
              type="button"
              className="hx-video__btn"
              data-on={overlays.on ? '1' : '0'}
              aria-pressed={overlays.on}
              aria-label={t('next.video.overlay.mode')}
              title={t('next.video.overlay.mode')}
              onClick={() => {
                if (!overlays.on) pauseNow();
                overlays.setOn(!overlays.on);
              }}
            >
              <Icon name="text-quote" />
              <span className="hx-video__btn-text">{t('next.video.overlay.mode')}</span>
            </button>
          )}
          <button
            type="button"
            className="hx-video__btn"
            aria-label={t('next.video.player.comment')}
            onClick={() => {
              pauseNow();
              onComment();
            }}
          >
            <Icon name="message-square-plus" />
          </button>
          <button
            type="button"
            className="hx-video__btn"
            aria-label={t(full ? 'next.video.player.exitFull' : 'next.video.player.enterFull')}
            onClick={toggleFull}
          >
            <Icon name={full ? 'minimize-2' : 'maximize-2'} />
          </button>
        </div>
      </div>
    </div>
  );
};

export default VideoPlayer;
