// The scene «Запись»: the slide full screen, the prompter at the top that follows the voice, the camera circle, and the
// capsule with the clock, «Пауза», «Переписать слайд» and «Готово». The prompter, the hint and the capsule are only for the
// presenter: the video is the camera and the voice, and the runtime renders the slides from the events.
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import {
  finishDeckRec,
  sendDeckChunk,
  sendDeckEvents,
  startDeckRec,
} from '@/next/features/designly/deck/api';
import { canCapture, makeRecorder, openCapture, stopStream } from '@/next/features/designly/deck/capture';
import { formatClock } from '@/next/features/designly/deck/clock';
import { useDeckPlay, type IDeckPlayHost } from '@/next/features/designly/deck/deck-play';
import {
  advance,
  createPrompter,
  finished,
  view,
  type IPrompter,
} from '@/next/features/designly/deck/prompter';
import {
  CursorSampler,
  RecSession,
  VoiceFeed,
  pickMime,
  pickVoiceMime,
  type IRecApi,
} from '@/next/features/designly/deck/recorder';
import type { IDeckRecording } from '@/next/features/designly/deck/types';
import { openDictation } from '@/next/features/composer/voice-socket';
import { WordedError } from '@/next/core/net/worded';
import { locale, t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { hasFeature } from '@/next/stores/machines';
import Icon from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';

/** The hint stays this long (its animation is 4.2 s). */
const HINT_MS = 4300;
/** A finished script moves to the next slide after this pause, so the last word is not cut off. */
const AUTO_NEXT_MS = 1200;
const CLOCK_TICK_MS = 250;

export const REC_PHASE = { starting: 'starting', live: 'live', finishing: 'finishing' } as const;
type TRecPhase = (typeof REC_PHASE)[keyof typeof REC_PHASE];

export interface IDeckRecProps {
  host: IDeckPlayHost;
  machine: MachineId;
  /** The project's id. */
  artifact: string;
  /** The recording is saved: the shell shows «Готово» with it. */
  onFinished: (rec: IDeckRecording) => void;
  /** Back to the editor: Esc (the recording is saved first), or a recording that could not start. */
  onExit: () => void;
  /** Whether the recording stands still, for the shell's `data-paused`. */
  onPaused?: (paused: boolean) => void;
}

const api = (machine: MachineId, artifact: string): IRecApi => ({
  start: (camera, mime) => startDeckRec(machine, artifact, camera, mime),
  chunk: (rec, seq, chunk) => sendDeckChunk(machine, artifact, rec, seq, chunk),
  events: (rec, lines) => sendDeckEvents(machine, artifact, rec, lines),
  finish: (rec, ms) => finishDeckRec(machine, artifact, rec, ms),
});

const failureText = (error: unknown) =>
  error instanceof WordedError ? error.message : t('next.deck.rec.failed');

const DeckRec = ({ host, machine, artifact, onFinished, onExit, onPaused }: IDeckRecProps) => {
  const { slides, index, step } = host;
  const slide = slides[index];
  const [phase, setPhase] = useState<TRecPhase>(REC_PHASE.starting);
  const [paused, setPaused] = useState(false);
  const [camera, setCamera] = useState(false);
  const [hint, setHint] = useState(false);
  const [byVoice, setByVoice] = useState(false);
  const [ms, setMs] = useState(0);
  const [take, setTake] = useState(0);
  const [prompter, setPrompter] = useState<IPrompter>(() => createPrompter(slide?.notes ?? ''));
  const [scriptKey, setScriptKey] = useState(`${index}:0`);

  const session = useRef<RecSession | null>(null);
  const feed = useRef<VoiceFeed | null>(null);
  const stream = useRef<MediaStream | null>(null);
  const video = useRef<HTMLVideoElement>(null);
  const live = useRef(false);
  const prevIndex = useRef<number | null>(null);
  // the events of the shell and the page, read at the moment they are used, not when the effects were set up
  const toFirst = useEffectEvent(() => {
    host.onGo(0);
    host.onStep(0);
  });
  const leave = useEffectEvent(() => onExit());

  // a new slide, or a retake, starts its script from the top
  const key = `${index}:${take}`;
  if (key !== scriptKey) {
    setScriptKey(key);
    setPrompter(createPrompter(slide?.notes ?? ''));
  }

  const finish = async (back: boolean) => {
    const s = session.current;
    if (!s || phase !== REC_PHASE.live) return;
    setPhase(REC_PHASE.finishing);
    feed.current?.stop();
    try {
      const rec = await s.finish();
      live.current = false;
      stopStream(stream.current);
      stream.current = null;
      if (back) onExit();
      else onFinished(rec);
    } catch (error) {
      live.current = false;
      stopStream(stream.current);
      stream.current = null;
      toast(failureText(error), { tone: 'error' });
      onExit();
    }
  };

  const { next } = useDeckPlay(host, {
    onExit: () => void finish(true),
  });

  // ---- start: the devices, the runtime's recording, the recorder, the recognition
  useEffect(() => {
    let cancelled = false;
    // a recording starts at the first slide: it is on screen while the devices are asked for
    toFirst();
    const begin = async () => {
      if (!canCapture()) {
        toast(t('next.deck.rec.noDevices'), { tone: 'error' });
        leave();
        return;
      }
      let capture;
      try {
        capture = await openCapture();
      } catch {
        if (!cancelled) {
          toast(t('next.deck.rec.noDevices'), { tone: 'error' });
          leave();
        }
        return;
      }
      if (cancelled) {
        stopStream(capture.stream);
        return;
      }
      stream.current = capture.stream;
      const mime = pickMime(capture.camera, (m) => MediaRecorder.isTypeSupported(m));
      if (mime === '') {
        // no container the runtime can take: nothing is started
        stopStream(capture.stream);
        toast(t('next.deck.rec.noDevices'), { tone: 'error' });
        leave();
        return;
      }
      const created = new RecSession({
        api: api(machine, artifact),
        stream: capture.stream,
        camera: capture.camera,
        mime,
        makeRecorder,
        clock: () => performance.now(),
        onError: () => toast(t('next.deck.rec.failed'), { tone: 'error' }),
      });
      try {
        await created.begin();
      } catch (error) {
        stopStream(capture.stream);
        stream.current = null;
        if (!cancelled) {
          toast(failureText(error), { tone: 'error' });
          leave();
        }
        return;
      }
      if (cancelled) {
        stopStream(capture.stream);
        return;
      }
      session.current = created;
      live.current = true;
      setCamera(capture.camera);
      const audioMime = pickVoiceMime((m) => MediaRecorder.isTypeSupported(m));
      if (hasFeature(machine, 'voice') && capture.stream.getAudioTracks().length > 0) {
        const voice = new VoiceFeed({
          audio: new MediaStream(capture.stream.getAudioTracks()),
          mime: audioMime,
          makeRecorder,
          open: (events) => openDictation(machine, locale(), events),
          now: () => created.ms(),
          onPartial: (text) => setPrompter((p) => advance(p, text)),
          onFinal: (text, t0) => created.event({ k: 'asr', t0, text }),
          onFailure: () => setByVoice(false),
        });
        feed.current = voice;
        voice.start();
        setByVoice(true);
      }
      setHint(true);
      setPhase(REC_PHASE.live);
    };
    void begin();
    return () => {
      cancelled = true;
      feed.current?.stop();
      feed.current = null;
      // leaving without «Готово» (the scene was closed from outside): nothing keeps the devices on
      if (live.current) stopStream(stream.current);
    };
    // the recording starts once per mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // the camera circle shows the same stream, mirrored by CSS
  useEffect(() => {
    const el = video.current;
    if (el && camera && stream.current) el.srcObject = stream.current;
  }, [camera, phase]);

  useEffect(() => {
    if (!hint) return;
    const id = setTimeout(() => setHint(false), HINT_MS);
    return () => clearTimeout(id);
  }, [hint]);

  useEffect(() => {
    if (phase !== REC_PHASE.live) return;
    const id = setInterval(() => setMs(session.current?.ms() ?? 0), CLOCK_TICK_MS);
    return () => clearInterval(id);
  }, [phase]);

  // ---- events of the show: the slide, the step
  useEffect(() => {
    if (phase !== REC_PHASE.live || !slide) return;
    session.current?.event({ k: 'slide', page: slide.id, i: index });
    // a new slide is a new stretch of speech for the recognition, but the first one is already open
    if (prevIndex.current !== null && prevIndex.current !== index) feed.current?.rotate();
    prevIndex.current = index;
    // only a move to another slide is an event
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase, index]);
  useEffect(() => {
    if (phase !== REC_PHASE.live || step === 0) return;
    session.current?.event({ k: 'step', n: step });
  }, [phase, step]);

  // ---- the pointer: cursor ≤30 Hz and clicks, in board pixels
  const { setPointer, onPointer } = host;
  useEffect(() => {
    if (phase !== REC_PHASE.live) return;
    const sampler = new CursorSampler();
    let down = false;
    setPointer(true);
    const off = onPointer((p) => {
      const s = session.current;
      if (!s || s.paused) return;
      if (sampler.take(s.ms())) s.event({ k: 'cursor', x: Math.round(p.x), y: Math.round(p.y) });
      if (p.down && !down) s.event({ k: 'click', x: Math.round(p.x), y: Math.round(p.y) });
      down = p.down;
    });
    return () => {
      off();
      setPointer(false);
    };
  }, [phase, setPointer, onPointer]);

  // ---- the script ended: on to the next slide
  const ended = finished(prompter);
  useEffect(() => {
    if (!ended || paused || phase !== REC_PHASE.live) return;
    const id = setTimeout(() => {
      next();
    }, AUTO_NEXT_MS);
    return () => clearTimeout(id);
    // `next` closes over the host of this render; the timer belongs to this slide's end
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ended, paused, phase, index]);

  const togglePause = () => {
    const s = session.current;
    if (!s || phase !== REC_PHASE.live) return;
    if (paused) {
      s.resume();
      feed.current?.resume();
    } else {
      s.pause();
      feed.current?.pause();
    }
    setPaused(!paused);
    onPaused?.(!paused);
  };

  const retake = () => {
    if (!slide || phase !== REC_PHASE.live) return;
    session.current?.event({ k: 'retake', page: slide.id });
    feed.current?.rotate();
    setTake((n) => n + 1);
    toast(t('next.deck.rec.retakeToast', { n: index + 1 }));
  };

  const line = view(prompter);
  const busy = phase !== REC_PHASE.live;

  return (
    <div className="hx-deckm-play" data-scene="rec" data-paused={paused ? '1' : '0'} data-phase={phase}>
      {slide?.notes.trim() ? (
        <div className="hx-deckm-prompt" role="region" aria-label={t('next.deck.rec.prompter')}>
          <span className="hx-deckm-camhint">
            <i />
            {t('next.deck.rec.camHint')}
          </span>
          <p className="was">{line.was}</p>
          <p className="now">
            <em>{line.said}</em>
            {line.rest}
          </p>
          <p className="next">{line.next}</p>
        </div>
      ) : null}
      {camera ? (
        <video
          className="hx-deckm-cam"
          ref={video}
          autoPlay
          muted
          playsInline
          aria-label={t('next.deck.rec.camera')}
        />
      ) : null}
      <div className="hx-deckm-cap" aria-busy={busy}>
        <span className="hx-deckm-time">
          <span className="hx-deckm-cap__dot hx-deckm-cap__dot--rec" />
          {formatClock(ms / 1000)}
          <small>{t('next.deck.rec.position', { n: index + 1, total: slides.length })}</small>
        </span>
        <i className="hx-deckm-cap__sep" />
        <button type="button" onClick={togglePause} disabled={busy}>
          <Icon name={paused ? 'play' : 'pause'} size="sm" />
          <span className="hx-deckm-lbl">{t(paused ? 'next.deck.rec.resume' : 'next.deck.rec.pause')}</span>
        </button>
        <button type="button" onClick={retake} disabled={busy}>
          <Icon name="rotate-ccw" size="sm" />
          <span className="hx-deckm-lbl">{t('next.deck.rec.retake')}</span>
        </button>
        <i className="hx-deckm-cap__sep" />
        <button type="button" data-v="done" onClick={() => void finish(false)} disabled={busy}>
          <Icon name="square" size="sm" />
          {t(phase === REC_PHASE.finishing ? 'next.deck.rec.finishing' : 'next.deck.rec.done')}
        </button>
      </div>
      {hint ? (
        <div className="hx-deckm-hint" role="status">
          <span>
            <Icon name="eye" size="sm" />
            {t('next.deck.rec.hintPrivate')}
          </span>
          {byVoice ? (
            <span>
              {t('next.deck.rec.hintVoice')}
              <span className="hx-deckm-kbd">→</span>
              {t('next.deck.rec.hintManual')}
            </span>
          ) : null}
        </div>
      ) : null}
    </div>
  );
};

export default DeckRec;
