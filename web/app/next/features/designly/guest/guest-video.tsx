// A guest link to a video: the published MP4 with its chapters, a switch between the formats that are
// published, a strip of comment markers with a range select, and comments on a moment or a stretch (the gateway's anchor
// with state {v: 'video', scene, t0, t1, format}). The name is asked once; what the guest looked at is counted per scene.
import { useEffect, useMemo, useRef, useState, type PointerEvent } from 'react';
import { statsOn } from '@/next/features/designly/deck/deck-link-view';
import { createSeenTracker, sendSeen } from '@/next/features/designly/deck/guest-seen';
import { chaptersOf, clock, currentChapter } from '@/next/features/designly/deck/video-chapters';
import { keepWho, postComment, readWho, type IGuestWho } from '@/next/features/designly/guest/guest-comments';
import type { IGuestConfig } from '@/next/features/designly/guest/guest-config';
import GuestNameDialog from '@/next/features/designly/guest/guest-name-dialog';
import {
  marksOf,
  mediaName,
  pickOf,
  scenesOf,
  sceneAt,
  videoAnchor,
  type IGuestPick,
} from '@/next/features/designly/guest/guest-video-model';
import { useComments } from '@/next/features/designly/guest/use-guest-comments';
import { autoplayOn, startFormat, timelineCommentsOn } from '@/next/features/designly/video/share-view';
import { VIDEO_FORMATS, type TVideoFormat } from '@/next/features/designly/video/types';
import { Button } from '@/next/ui/button';
import { toast } from '@/next/features/designly/sdk/toast';
import { t } from '@/next/i18n';

export interface IGuestVideoProps {
  /** A config whose `deck.type` is `video`. */
  config: IGuestConfig;
}

const clockNow = (): number => Date.now();

/** An element placed on the strip by custom properties (a share of the strip, in percent). */
const Placed = ({
  from,
  to,
  tag: Tag = 'span',
  ...rest
}: {
  from: number;
  to?: number;
  tag?: 'span' | 'button';
  className: string;
  title?: string;
  onClick?: () => void;
  'data-on'?: string;
}) => (
  <Tag
    {...rest}
    type={Tag === 'button' ? 'button' : undefined}
    ref={(el: HTMLElement | null) => {
      el?.style.setProperty('--hx-vg-from', `${from}%`);
      el?.style.setProperty('--hx-vg-size', `${to === undefined ? 0 : to - from}%`);
    }}
  />
);

const GuestVideo = ({ config }: IGuestVideoProps) => {
  const [who, setWho] = useState(readWho);
  const [naming, setNaming] = useState(false);
  const asked = useRef<((named: IGuestWho | null) => void) | null>(null);
  const [list, setList] = useComments(config);
  const published = config.deck?.video ?? null;
  const allowed = published !== null && config.view?.video === true;
  const formats = useMemo(() => VIDEO_FORMATS.filter((f) => published?.formats.includes(f)), [published]);
  const [format, setFormat] = useState<TVideoFormat | null>(() => startFormat(config.view, formats));
  const video = useRef<HTMLVideoElement>(null);
  // Where to go and whether to play once the other format's file has its length.
  const resume = useRef<{ at: number; play: boolean } | null>(null);
  const [time, setTime] = useState(0);
  const [length, setLength] = useState<number | null>(null);
  const [playing, setPlaying] = useState(false);
  const [pick, setPick] = useState<IGuestPick | null>(null);
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const drag = useRef<number | null>(null);
  const strip = useRef<HTMLDivElement>(null);

  const total = length !== null ? length * 1000 : (published?.durMs ?? null);
  const chapters = useMemo(
    () => chaptersOf(published?.chapters ?? [], config.boards),
    [published, config.boards],
  );
  const scenes = useMemo(
    () => scenesOf(chapters, config.boards, total ?? 0),
    [chapters, config.boards, total],
  );
  // The link may keep comments off the timeline (HRN-675): the list beside the player still has them.
  const onTimeline = timelineCommentsOn(config.view);
  const marks = useMemo(() => marksOf(list, scenes), [list, scenes]);
  const autoplay = autoplayOn(config.view);
  const nowMs = time * 1000;
  const here = sceneAt(scenes, nowMs);
  const at = currentChapter(
    chapters.map((c) => c.t),
    nowMs,
  );

  const commenting = config.rights === 'comment' && config.commentsUrl !== '';

  const seek = (ms: number, play: boolean) => {
    const el = video.current;
    if (!el) return;
    el.currentTime = ms / 1000;
    if (play) el.play().catch(() => undefined);
  };

  const switchTo = (next: TVideoFormat) => {
    if (next === format) return;
    const el = video.current;
    resume.current = { at: el?.currentTime ?? 0, play: el !== null && !el.paused };
    setLength(null);
    setFormat(next);
  };

  const loaded = (el: HTMLVideoElement) => {
    setLength(el.duration);
    const back = resume.current;
    resume.current = null;
    if (back === null) return;
    el.currentTime = back.at;
    if (back.play) el.play().catch(() => undefined);
  };

  // What the guest looked at: the scene on screen while the video plays and the tab is visible.
  const counting = statsOn(config.view);
  const tracker = useMemo(
    () =>
      createSeenTracker(clockNow, (report) => {
        if (counting) sendSeen(config.seenUrl, who.guestId, report);
      }),
    [counting, config.seenUrl, who.guestId],
  );
  const watched = playing ? (here?.path ?? null) : null;
  useEffect(() => {
    const sync = () => tracker.show(document.visibilityState === 'visible' ? watched : null);
    sync();
    document.addEventListener('visibilitychange', sync);
    return () => document.removeEventListener('visibilitychange', sync);
  }, [tracker, watched]);
  useEffect(() => {
    const leave = () => tracker.show(null);
    window.addEventListener('pagehide', leave);
    return () => {
      window.removeEventListener('pagehide', leave);
      tracker.show(null);
    };
  }, [tracker]);

  const msAt = (event: PointerEvent): number | null => {
    const box = strip.current?.getBoundingClientRect();
    if (!box || box.width === 0 || total === null) return null;
    const share = Math.min(1, Math.max(0, (event.clientX - box.left) / box.width));
    return share * total;
  };
  const down = (event: PointerEvent<HTMLDivElement>) => {
    if (!commenting || !onTimeline) return;
    const ms = msAt(event);
    if (ms === null) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = ms;
    setPick(pickOf(scenes, ms, ms));
  };
  const move = (event: PointerEvent<HTMLDivElement>) => {
    const from = drag.current;
    const ms = msAt(event);
    if (from === null || ms === null) return;
    setPick(pickOf(scenes, from, ms));
  };
  const up = (event: PointerEvent<HTMLDivElement>) => {
    const from = drag.current;
    drag.current = null;
    const ms = msAt(event);
    if (from === null || ms === null) return;
    const done = pickOf(scenes, from, ms);
    setPick(done);
    if (done !== null) seek(done.scene.start + done.t0, false);
  };

  const commentHere = () => {
    video.current?.pause();
    setPick(pickOf(scenes, nowMs, nowMs));
  };

  const ensureName = () =>
    who.name !== ''
      ? Promise.resolve<IGuestWho | null>(who)
      : new Promise<IGuestWho | null>((resolve) => {
          asked.current = resolve;
          setNaming(true);
        });
  const settle = (named: IGuestWho | null) => {
    asked.current?.(named);
    asked.current = null;
  };

  const send = async () => {
    const body = text.trim();
    if (pick === null || format === null || body === '' || sending) return;
    const named = await ensureName();
    if (named === null) return;
    setSending(true);
    try {
      const taken = await postComment(config.commentsUrl, {
        who: named,
        text: body,
        board: pick.scene.path,
        anchor: videoAnchor(pick, format),
      });
      setList((all) => [...all, taken]);
      setText('');
      setPick(null);
      toast(t('next.guest.sent'), { tone: 'success' });
    } catch {
      toast(t('next.guest.failed'), { tone: 'error' });
    } finally {
      setSending(false);
    }
  };

  if (!allowed || format === null)
    return (
      <section className="hx-vg">
        <p className="hx-vg__empty">{t('next.video.guest.noVideo')}</p>
      </section>
    );

  const share = (ms: number): number => (total === null || total === 0 ? 0 : (ms / total) * 100);
  const pickFrom = pick === null ? 0 : pick.scene.start + pick.t0;
  const pickTo = pick === null || pick.t1 === null ? null : pick.scene.start + pick.t1;

  return (
    <section className="hx-vg" data-format={format === '9:16' ? 'portrait' : 'landscape'}>
      <header className="hx-vg__bar">
        <h1 className="hx-vg__title">{config.title}</h1>
        {formats.length > 1 && (
          <div className="hx-vg__formats" role="group" aria-label={t('next.video.guest.format')}>
            {formats.map((f) => (
              <button
                key={f}
                type="button"
                className="hx-vg__format"
                data-on={f === format ? '1' : '0'}
                aria-pressed={f === format}
                onClick={() => switchTo(f)}
              >
                {f}
              </button>
            ))}
          </div>
        )}
        {commenting && (
          <Button size="sm" onClick={commentHere}>
            {t('next.video.guest.comment')}
          </Button>
        )}
      </header>
      <div className="hx-vg__body">
        <div className="hx-vg__main">
          <div className="hx-vg__stage">
            <video
              key={format}
              ref={video}
              src={`${config.mediaBase}${mediaName(format)}`}
              controls
              playsInline
              autoPlay={autoplay}
              muted={autoplay}
              preload={autoplay ? 'auto' : 'metadata'}
              onTimeUpdate={(event) => setTime(event.currentTarget.currentTime)}
              onLoadedMetadata={(event) => loaded(event.currentTarget)}
              onPlay={() => setPlaying(true)}
              onPause={() => setPlaying(false)}
              onEnded={() => setPlaying(false)}
            />
          </div>
          {total !== null && (
            <div
              ref={strip}
              className="hx-vg__strip"
              data-edit={commenting && onTimeline ? '1' : '0'}
              aria-label={t('next.video.guest.strip')}
              onPointerDown={down}
              onPointerMove={move}
              onPointerUp={up}
            >
              {scenes.map((s) => (
                <Placed key={s.id} className="hx-vg__cut" from={share(s.start)} />
              ))}
              {(onTimeline ? marks : []).map((m) => (
                <Placed
                  key={m.id}
                  tag="button"
                  className="hx-vg__mark"
                  data-on={m.to === null ? '0' : '1'}
                  from={share(m.at)}
                  to={m.to === null ? undefined : share(m.to)}
                  title={`${m.name}: ${m.text}`}
                  onClick={() => seek(m.at, false)}
                />
              ))}
              {pick !== null && (
                <Placed
                  className="hx-vg__pick"
                  from={share(pickFrom)}
                  to={pickTo === null ? undefined : share(pickTo)}
                />
              )}
              <Placed className="hx-vg__head" from={share(nowMs)} />
            </div>
          )}
          {pick !== null && commenting && (
            <form
              className="hx-vg__compose"
              onSubmit={(event) => {
                event.preventDefault();
                void send();
              }}
            >
              <div className="hx-vg__at">
                {pick.scene.title}
                <small>
                  {clock(pickFrom)}
                  {pickTo !== null && ` – ${clock(pickTo)}`}
                </small>
              </div>
              <textarea
                className="hx-vg__text"
                value={text}
                rows={3}
                maxLength={2000}
                placeholder={t('next.video.guest.placeholder')}
                aria-label={t('next.video.guest.comment')}
                onChange={(event) => setText(event.target.value)}
              />
              <div className="hx-vg__actions">
                <Button size="sm" variant="ghost" onClick={() => setPick(null)}>
                  {t('next.video.guest.cancel')}
                </Button>
                <Button
                  size="sm"
                  variant="primary"
                  busy={sending}
                  disabled={text.trim() === ''}
                  onClick={() => void send()}
                >
                  {t('next.video.guest.send')}
                </Button>
              </div>
            </form>
          )}
        </div>
        <aside className="hx-vg__side">
          <div className="hx-vg__h">
            {t('next.video.guest.chapters', { len: total === null ? '' : clock(total) })}
          </div>
          {chapters.map((c, i) => (
            <button
              key={c.slide}
              type="button"
              className="hx-vg__chap"
              data-on={at === i ? '1' : '0'}
              aria-current={at === i ? 'true' : undefined}
              onClick={() => seek(c.t, true)}
            >
              <span>{c.title}</span>
              <small>{clock(c.t)}</small>
            </button>
          ))}
          {marks.length > 0 && <div className="hx-vg__h">{t('next.video.guest.comments')}</div>}
          {marks.map((m) => (
            <button key={m.id} type="button" className="hx-vg__note" onClick={() => seek(m.at, false)}>
              <small>
                {m.name} · {clock(m.at)}
              </small>
              <span>{m.text}</span>
            </button>
          ))}
        </aside>
      </div>
      {commenting && (
        <GuestNameDialog
          key={naming ? 'open' : 'shut'}
          open={naming}
          name={who.name}
          onClose={() => {
            setNaming(false);
            settle(null);
          }}
          onSave={(name) => {
            const next = { ...who, name };
            keepWho(next);
            setWho(next);
            setNaming(false);
            settle(next);
          }}
        />
      )}
    </section>
  );
};

export default GuestVideo;
