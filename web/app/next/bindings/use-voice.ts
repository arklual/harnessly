// Dictation into a composer: the microphone records in the browser and every chunk goes to the machine's socket as it
// is recorded, so what was said appears in the field while it is being said (the machine's partial text, replaced by
// each next one) and is fixed by the final one at the stop. Where the machine cannot recognize on the fly, or the
// socket does not open or drops, the whole recording goes to it after the stop (POST /api/v2/voice). Nothing is sent
// as a message. Cancel drops the recording and the recognition and leaves the field as it was; Esc stops.
// Without the voice feature or a recorder in the browser the microphone is not offered.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { transcribe } from '@/next/features/composer/voice-api';
import { openDictation, type IDictation } from '@/next/features/composer/voice-socket';
import type { Recording } from '@/next/features/composer/types';
import { WordedError } from '@/next/core/net/worded';
import { hasFeature, useMachine } from '@/next/stores/machines';
import type { MachineId } from '@/next/protocol/v2';
import { locale, t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';

/** How often the recorder hands over a chunk: the sooner, the sooner the first words are on the screen. */
const SLICE_MS = 250;
/** From the stop to the final text; the machine answers in about a second, the rest is slack for a slow tunnel. */
const FINAL_MS = 15_000;

const MIME_TYPES = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', 'audio/aac'];

interface IMeter {
  level: () => number;
  close: () => void;
}

interface ISession {
  stream: MediaStream;
  rec: MediaRecorder;
  meter: IMeter | null;
  /** Everything recorded, in order: the socket takes it as it comes, the whole-recording route takes all of it. */
  chunks: Blob[];
  /** How many of the chunks the socket has. */
  sent: number;
  /** null: no socket (the machine cannot, or it dropped), the whole recording goes after the stop. */
  dictation: IDictation | null;
  /** The person asked to stop. */
  stopped: boolean;
  /** The recorder handed over its last chunk. */
  recorded: boolean;
  /** Over: cancelled, finished or failed; nothing else is taken from it. */
  closed: boolean;
  abort: AbortController;
  finalTimer: ReturnType<typeof setTimeout> | null;
  /** The field before the dictation (as it was, and without its trailing blanks), and what the dictation put there last. */
  original: string;
  base: string;
  written: string;
  /** What has been recognized so far (the last partial). */
  spoken: string;
}

const failureText = (failure: unknown) => {
  if (failure instanceof WordedError && failure.code === 'unsupported') return t('next.voice.unsupported');
  if (failure instanceof WordedError) return failure.message;
  return t('next.failure.http');
};

const canRecord = () => typeof MediaRecorder !== 'undefined' && !!navigator.mediaDevices?.getUserMedia;

const join = (text: string, said: string) => {
  const left = text.trimEnd();
  const right = said.trim();
  if (!left) return right;
  return right ? `${left} ${right}` : left;
};

/** The microphone's loudness as a number read on demand; null where the browser has no audio graph. */
const openMeter = (stream: MediaStream): IMeter | null => {
  try {
    const ctx = new AudioContext();
    void ctx.resume().catch(() => {});
    const analyser = ctx.createAnalyser();
    analyser.fftSize = 512;
    ctx.createMediaStreamSource(stream).connect(analyser);
    const data = new Uint8Array(analyser.fftSize);
    return {
      level: () => {
        analyser.getByteTimeDomainData(data);
        let sum = 0;
        for (const value of data) sum += ((value - 128) / 128) ** 2;
        return Math.min(1, Math.sqrt(sum / data.length) * 4);
      },
      close: () => void ctx.close().catch(() => {}),
    };
  } catch {
    return null;
  }
};

/**
 * `current` reads the field's text where it is kept (a draft in its store): a dictation that outlives its field (the
 * person went to another chat while speaking) still lands in that field's draft, after what was typed there meanwhile.
 */
export const useVoice = (
  machine: MachineId | null,
  text: string,
  setText: (value: string) => void,
  current?: () => string,
) => {
  useMachine(machine);
  const [recording, setRecording] = useState<Recording | null>(null);
  const [recognized, setRecognized] = useState(0);
  const live = useRef<ISession | null>(null);
  const opening = useRef(false);
  const gone = useRef(false);
  const latest = useRef({ text, setText, current });
  useLayoutEffect(() => {
    latest.current = { text, setText, current };
  });

  const fieldNow = () => (latest.current.current ? latest.current.current() : latest.current.text);

  /** The person typed in the field while it was dictating: what is recognized is added to their text, never over it. */
  const edited = (session: ISession) => fieldNow() !== session.written;

  const put = (session: ISession, value: string) => {
    session.written = value;
    latest.current.setText(value);
  };

  const close = (session: ISession) => {
    if (session.closed) return;
    session.closed = true;
    if (session.finalTimer) clearTimeout(session.finalTimer);
    session.finalTimer = null;
    session.abort.abort();
    session.rec.onstop = null;
    if (session.rec.state !== 'inactive') session.rec.stop();
    session.stream.getTracks().forEach((track) => track.stop());
    session.meter?.close();
    if (live.current === session) {
      live.current = null;
      setRecording(null);
    }
  };

  const show = (session: ISession, spoken: string) => {
    session.spoken = spoken;
    if (!edited(session)) put(session, join(session.base, spoken));
  };

  const commit = (session: ISession, said: string) => {
    if (session.closed) return;
    const whole = said.trim() || session.spoken.trim();
    if (!whole) toast(t('next.voice.silence'));
    else {
      put(session, edited(session) ? join(fieldNow(), whole) : join(session.base, whole));
      setRecognized((n) => n + 1);
    }
    close(session);
  };

  const fail = (session: ISession, message: string) => {
    if (session.closed) return;
    close(session);
    // What was heard stays in the field; the words of the machine say what went wrong.
    toast(t('next.voice.failed'), { tone: 'error', description: message || undefined });
  };

  const recognizeWhole = async (m: MachineId, session: ISession) => {
    try {
      const blob = new Blob(session.chunks, { type: session.rec.mimeType });
      const said = (await transcribe(m, blob, locale(), session.abort.signal)).text;
      commit(session, said);
    } catch (failure) {
      if (!session.closed) fail(session, failureText(failure));
    }
  };

  const flush = (session: ISession) => {
    if (!session.dictation?.ready) return;
    while (session.sent < session.chunks.length) session.dictation.send(session.chunks[session.sent++]);
  };

  /** The recording is complete and the socket is ready: the last chunks, then the stop, and the final is awaited. */
  const askFinal = (m: MachineId, session: ISession) => {
    flush(session);
    session.dictation?.stop();
    session.finalTimer = setTimeout(() => {
      if (session.closed) return;
      session.dictation?.abort();
      session.dictation = null;
      if (session.spoken) commit(session, session.spoken);
      else void recognizeWhole(m, session);
    }, FINAL_MS);
  };

  const start = async (m: MachineId) => {
    if (live.current || opening.current) return;
    opening.current = true;
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true },
      });
    } catch {
      toast(t('next.voice.denied'), { tone: 'error' });
      return;
    } finally {
      opening.current = false;
    }
    // The field went while the browser was asking for the microphone: nothing is recorded for it.
    if (gone.current) {
      stream.getTracks().forEach((track) => track.stop());
      return;
    }
    const mimeType = MIME_TYPES.find((type) => MediaRecorder.isTypeSupported(type));
    const meter = openMeter(stream);
    const session: ISession = {
      stream,
      rec: new MediaRecorder(stream, mimeType ? { mimeType } : undefined),
      meter,
      chunks: [],
      sent: 0,
      dictation: null,
      stopped: false,
      recorded: false,
      closed: false,
      abort: new AbortController(),
      finalTimer: null,
      original: latest.current.text,
      base: latest.current.text.trimEnd(),
      written: latest.current.text,
      spoken: '',
    };
    session.dictation = openDictation(m, locale(), {
      onPreparing: () => {
        if (session.closed || live.current !== session) return;
        setRecording((now) => (now && !now.preparing ? { ...now, preparing: true } : now));
      },
      onReady: () => {
        if (session.closed) return;
        setRecording((now) => (now?.preparing ? { ...now, preparing: false } : now));
        flush(session);
        if (session.recorded) askFinal(m, session);
      },
      onPartial: (said) => {
        if (!session.closed) show(session, said);
      },
      onFinal: (said) => commit(session, said),
      onFailure: (failure) => {
        if (session.closed) return;
        if (!failure.unavailable) {
          fail(session, failure.message);
          return;
        }
        session.dictation = null;
        if (session.recorded) void recognizeWhole(m, session);
      },
      onLost: () => {
        if (session.closed) return;
        session.dictation = null;
        if (session.finalTimer) clearTimeout(session.finalTimer);
        session.finalTimer = null;
        if (!session.recorded) return;
        if (session.spoken) commit(session, session.spoken);
        else void recognizeWhole(m, session);
      },
    });
    session.rec.ondataavailable = (event) => {
      if (event.data.size === 0 || session.closed) return;
      session.chunks.push(event.data);
      flush(session);
    };
    session.rec.onstop = () => {
      session.stream.getTracks().forEach((track) => track.stop());
      session.meter?.close();
      session.recorded = true;
      if (session.closed || !session.stopped) return;
      if (!session.dictation) void recognizeWhole(m, session);
      else if (session.dictation.ready) askFinal(m, session);
    };
    live.current = session;
    session.rec.start(SLICE_MS);
    setRecording({ startedAt: Date.now(), busy: false, level: meter?.level ?? null });
  };

  const finish = () => {
    const session = live.current;
    if (!session || session.stopped || session.rec.state === 'inactive') return;
    session.stopped = true;
    setRecording((now) => now && { ...now, busy: true });
    session.rec.stop();
  };

  const cancel = () => {
    const session = live.current;
    if (!session) return;
    session.dictation?.abort();
    session.dictation = null;
    // The field is as it was before the dictation, unless the person has been typing in it meanwhile.
    if (!edited(session) && session.written !== session.original) latest.current.setText(session.original);
    close(session);
  };

  const listening = recording !== null && !recording.busy;
  useEffect(() => {
    if (!listening) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      event.stopPropagation();
      finish();
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [listening]);

  // Leaving the field (another chat, another screen) ends the recording as «stop» does, not as «cancel»: the microphone
  // goes off at once and what was said is recognized into the field's draft, which outlives the field.
  const leave = useRef(finish);
  useLayoutEffect(() => {
    leave.current = finish;
  });
  useEffect(() => {
    gone.current = false;
    return () => {
      gone.current = true;
      leave.current();
    };
  }, []);

  return {
    recording,
    recognized,
    onMic: machine && hasFeature(machine, 'voice') && canRecord() ? () => void start(machine) : undefined,
    onCancelRecording: cancel,
    onFinishRecording: finish,
  };
};
