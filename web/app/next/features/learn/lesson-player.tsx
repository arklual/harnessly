// A lesson's film over the page: the film with its caption under it and its own controls (the chapters are marks on
// the line), the lesson's chapters beside it to jump to, the way to do it by hand and the next lesson. Esc closes it,
// the space bar pauses it; it leaves with its animation.
import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent } from 'react';
import { createPortal } from 'react-dom';
import { doLesson } from '@/next/features/learn/do-lesson';
import {
  chapterAt,
  clock,
  filmOf,
  lessonById,
  nextLesson,
  posterOf,
  type ILesson,
} from '@/next/features/learn/lessons';
import { t } from '@/next/i18n';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';

const EXIT_MS = 220;

interface IFilmProps {
  lesson: ILesson;
  closing: boolean;
  onOpen: (id: string) => void;
  onClose: () => void;
}

const Film = ({ lesson, closing, onOpen, onClose }: IFilmProps) => {
  const video = useRef<HTMLVideoElement>(null);
  const line = useRef<HTMLDivElement>(null);
  const dialog = useRef<HTMLDivElement>(null);
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [ended, setEnded] = useState(false);
  const next = nextLesson(lesson.id);
  const chapter = chapterAt(lesson, time);
  const share = Math.min(1, time / lesson.dur);

  useEffect(() => {
    dialog.current?.focus();
  }, []);

  // The line's fill and knob follow the film through a custom property: no inline style.
  useEffect(() => {
    line.current?.style.setProperty('--at', String(share));
  }, [share]);

  const seekTo = (s: number) => {
    const v = video.current;
    if (!v) return;
    v.currentTime = s;
    setEnded(false);
    void v.play().catch(() => undefined);
  };
  const toggle = () => {
    const v = video.current;
    if (!v) return;
    if (ended) return seekTo(0);
    if (v.paused) void v.play().catch(() => undefined);
    else v.pause();
  };
  const seek = (event: MouseEvent<HTMLDivElement>) => {
    const r = event.currentTarget.getBoundingClientRect();
    seekTo(Math.max(0, Math.min(1, (event.clientX - r.left) / r.width)) * lesson.dur);
  };
  const onKey = (event: KeyboardEvent) => {
    if (event.key === 'Escape') onClose();
    if (event.key === ' ' && !(event.target instanceof HTMLButtonElement)) {
      event.preventDefault();
      toggle();
    }
  };
  const doIt = () => {
    onClose();
    doLesson(lesson.id);
  };
  const title = t(`next.learn.${lesson.id}.title`);

  return (
    <>
      <div
        className="hx-learn-scrim"
        data-state={closing ? 'closing' : 'open'}
        aria-hidden="true"
        onClick={onClose}
      />
      <div
        ref={dialog}
        className="hx-learn-player"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        data-state={closing ? 'closing' : 'open'}
        onKeyDown={onKey}
      >
        <div className="hx-learn-player__left">
          <div className="hx-learn-stage">
            <video
              key={lesson.id}
              ref={video}
              className="hx-learn-stage__video"
              src={filmOf(lesson.id)}
              poster={posterOf(lesson.id)}
              playsInline
              muted
              autoPlay
              preload="auto"
              onClick={toggle}
              onPlay={() => setPlaying(true)}
              onPause={() => setPlaying(false)}
              onTimeUpdate={(event) => setTime(event.currentTarget.currentTime)}
              onEnded={() => {
                setEnded(true);
                setTime(lesson.dur);
              }}
            />
            {ended && (
              <div className="hx-learn-end">
                <span className="hx-learn-end__t">{t('next.learn.ended')}</span>
                <div className="hx-learn-end__acts">
                  <Button variant="ghost" icon="rotate-ccw" onClick={() => seekTo(0)}>
                    {t('next.learn.again')}
                  </Button>
                  <Button variant="primary" icon="square-mouse-pointer" onClick={doIt}>
                    {t('next.learn.do')}
                  </Button>
                </div>
              </div>
            )}
          </div>
          <p className="hx-learn-sub" key={chapter}>
            {t(`next.learn.${lesson.id}.c${chapter}`)}
          </p>
          <div className="hx-learn-ctrl">
            <button
              type="button"
              className="hx-learn-ctrl__btn"
              aria-label={playing ? t('next.learn.pause') : t('next.learn.play')}
              onClick={toggle}
            >
              <Icon name={playing ? 'pause' : 'play'} size="sm" />
            </button>
            <div
              ref={line}
              className="hx-learn-line"
              role="presentation"
              aria-label={t('next.learn.seek')}
              onClick={seek}
            >
              <span className="hx-learn-line__track" />
              <span className="hx-learn-line__fill" />
              {lesson.chapters.slice(1).map((c) => (
                <LineTick key={c} share={c / lesson.dur} />
              ))}
              <span className="hx-learn-line__knob" />
            </div>
            <span className="hx-learn-ctrl__time">{`${clock(time)} / ${clock(lesson.dur)}`}</span>
          </div>
        </div>
        <div className="hx-learn-player__right">
          <button
            type="button"
            className="hx-learn-player__x"
            aria-label={t('next.learn.close')}
            onClick={onClose}
          >
            <Icon name="x" size="sm" />
          </button>
          <span className="hx-learn__eyebrow">{t(`next.learn.section.${lesson.section}`)}</span>
          <h2 className="hx-learn-player__title">{title}</h2>
          <p className="hx-learn-player__desc">{t(`next.learn.${lesson.id}.desc`)}</p>
          <ol className="hx-learn-chaps" aria-label={t('next.learn.chapters')}>
            {lesson.chapters.map((c, i) => (
              <li key={c}>
                <button
                  type="button"
                  className="hx-learn-chap"
                  aria-current={i === chapter || undefined}
                  onClick={() => seekTo(c + 0.01)}
                >
                  <span className="hx-learn-chap__t">{clock(c)}</span>
                  <span>{t(`next.learn.${lesson.id}.c${i}`)}</span>
                </button>
              </li>
            ))}
          </ol>
          <div className="hx-learn-player__acts">
            <Button variant="primary" icon="square-mouse-pointer" onClick={doIt}>
              {t('next.learn.doLong')}
            </Button>
            <button type="button" className="hx-learn-next" onClick={() => onOpen(next.id)}>
              <img src={posterOf(next.id)} alt="" loading="lazy" />
              <span className="hx-learn-next__t">
                <small>{t('next.learn.next')}</small>
                {t(`next.learn.${next.id}.title`)}
              </span>
            </button>
          </div>
        </div>
      </div>
    </>
  );
};

const LineTick = ({ share }: { share: number }) => {
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    ref.current?.style.setProperty('--at', String(share));
  }, [share]);
  return <span ref={ref} className="hx-learn-line__tick" />;
};

interface ILessonPlayerProps {
  /** The lesson whose film is open; null: none. */
  id: string | null;
  onOpen: (id: string) => void;
  onClose: () => void;
}

const LessonPlayer = ({ id, onOpen, onClose }: ILessonPlayerProps) => {
  const container = usePortalContainer();
  const lesson = id ? lessonById(id) : null;
  const shown = usePresence(lesson, EXIT_MS);
  const host = container?.current;
  if (!shown.value || !host) return null;
  return createPortal(
    <Film
      key={shown.value.id}
      lesson={shown.value}
      closing={shown.state === 'closing'}
      onOpen={onOpen}
      onClose={onClose}
    />,
    host,
  );
};

export default LessonPlayer;
