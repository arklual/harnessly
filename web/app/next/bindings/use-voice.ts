// Dictation into a composer: the microphone records in the browser and every chunk goes to the machine's socket as it
// is recorded, so what was said appears in the field while it is being said (the machine's partial text, replaced by
// each next one) and is fixed by the final one at the stop. Where the machine cannot recognize on the fly, or the
// socket does not open or drops, the whole recording goes to it after the stop (POST /api/v2/voice). Nothing is sent
// as a message. Cancel drops the recording and the recognition and leaves the field as it was; Esc stops.
// Without the voice feature or a recorder in the browser the microphone is not offered.
//
// A dictation goes in where the caret was when it began. The person may edit the field while it listens: what
// was recognized so far becomes their text at that moment and is never written over again, the words heard after it
// are added at the end. Every recording is kept in the browser (voice-takes.ts) while it is made: one that could not be
// recognized stays over the field as a failed take, recognized again by hand or by itself when the link is back.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { transcribe } from '@/next/features/composer/voice-api';
import { openDictation, type IDictation } from '@/next/features/composer/voice-socket';
import {
  dropTake,
  failTake,
  finishTake,
  loadTakes,
  recordTake,
  startTake,
  useFailedTakes,
  type VoiceTake,
} from '@/next/features/composer/voice-takes';
import type { DockNotice, Recording } from '@/next/features/composer/types';
import { connectionStore, useConnection, useLinkRestored } from '@/next/stores/connection';
import { WordedError } from '@/next/core/net/worded';
import { hasFeature, useMachine } from '@/next/stores/machines';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import { dictationLang, voicePrefs } from '@/next/stores/voice';
import { toast } from '@/next/ui/toast';

/** How often the recorder hands over a chunk: the sooner, the sooner the first words are on the screen. */
const SLICE_MS = 250;
/** From the stop to the final text; the machine answers in about a second, the rest is slack for a slow tunnel. */
const FINAL_MS = 15_000;
/** How often the recording so far is written to the browser's storage. */
const KEEP_MS = 1000;

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
  /** The field before the dictation, and what the dictation put there last. */
  original: string;
  written: string;
  /** The field's text around the dictated words: before the caret and after it (or the person's whole text once edited). */
  before: string;
  after: string;
  /** What has been recognized so far (the last partial), and the part of it the person took over by editing the field. */
  spoken: string;
  taken: string;
  /** The recognized text whose words are in the field now. */
  shown: string;
  /** The recording as kept in the browser, and when it was written last. */
  take: string;
  keptAt: number;
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

/** The dictated words between the text before the caret and the text after it. */
const place = (before: string, said: string, after: string) => {
  const head = join(before, said);
  if (!head || !after) return head + after;
  return /^\s/.test(after) ? head + after : `${head} ${after}`;
};

const wordsOf = (text: string) => text.trim().split(/\s+/).filter(Boolean);

/**
 * What was heard beyond the part the person took over. The recognizer's text grows from the start and may change its
 * last words, so where it no longer begins with the taken part the same number of words is skipped.
 */
const unheard = (spoken: string, taken: string) => {
  if (!taken) return spoken;
  if (spoken.startsWith(taken)) return spoken.slice(taken.length);
  return wordsOf(spoken).slice(wordsOf(taken).length).join(' ');
};

/** Takes being recognized again by some field of this page: two fields of one machine do not both send it. */
const retrying = new Set<string>();

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
  /** Where the caret goes after the dictated words; the field puts it there while it listens. */
  const [caret, setCaret] = useState<number | null>(null);
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

  /** The person edited the field: everything in it is theirs now, and the next words go after it. */
  const adopt = (session: ISession) => {
    if (!edited(session)) return;
    session.before = fieldNow();
    session.after = '';
    session.taken = session.shown;
  };

  const put = (session: ISession, spoken: string) => {
    const said = unheard(spoken, session.taken);
    const value = place(session.before, said, session.after);
    session.shown = spoken;
    session.written = value;
    latest.current.setText(value);
    setCaret(join(session.before, said).length);
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
    adopt(session);
    put(session, spoken);
  };

  const commit = (session: ISession, said: string) => {
    if (session.closed) return;
    const whole = said.trim() || session.spoken.trim();
    if (!whole) {
      toast(t('next.voice.silence'));
      dropTake(session.take);
    } else {
      adopt(session);
      put(session, whole);
      finishTake(session.take, whole);
      setRecognized((n) => n + 1);
    }
    close(session);
  };

  const fail = (session: ISession, message: string) => {
    if (session.closed) return;
    recordTake(session.take, session.chunks);
    failTake(session.take);
    close(session);
    // What was heard stays in the field, the recording stays over it to be recognized again; the words of the machine
    // say what went wrong.
    toast(t('next.voice.kept'), { tone: 'error', description: message || undefined });
  };

  const recognizeWhole = async (m: MachineId, session: ISession) => {
    // No link to the machine: the recording is kept at once instead of waiting for a request that cannot go.
    if (connectionStore.get(m)?.k !== 'live') {
      fail(session, t('next.voice.offline'));
      return;
    }
    // The link drops while it waits: the same, without waiting for the request's two minutes.
    const watch = connectionStore.subscribeKey(m, () => {
      if (connectionStore.get(m)?.k === 'offline') fail(session, t('next.voice.offline'));
    });
    try {
      const blob = new Blob(session.chunks, { type: session.rec.mimeType });
      const said = (await transcribe(m, blob, dictationLang(), session.abort.signal)).text;
      commit(session, said);
    } catch (failure) {
      if (!session.closed) fail(session, failureText(failure));
    } finally {
      watch();
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

  /** `at`: where the caret stands in the field; the dictated words go there (at the end without it). */
  const start = async (m: MachineId, at?: number) => {
    if (live.current || opening.current) return;
    opening.current = true;
    let stream: MediaStream;
    const audio = { channelCount: 1, echoCancellation: true, noiseSuppression: true };
    const { deviceId } = voicePrefs();
    try {
      // The microphone chosen in Settings; one that is gone (unplugged) gives way to the system's.
      stream = await navigator.mediaDevices
        .getUserMedia({ audio: deviceId ? { ...audio, deviceId: { exact: deviceId } } : audio })
        .catch((failure: unknown) => {
          if (!deviceId) throw failure;
          return navigator.mediaDevices.getUserMedia({ audio });
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
    const rec = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
    const text = latest.current.text;
    // A host may hand the click's event over as `at`: anything but a number means the end.
    const caretAt =
      typeof at === 'number' && Number.isFinite(at) ? Math.min(Math.max(at, 0), text.length) : text.length;
    const session: ISession = {
      stream,
      rec,
      meter,
      chunks: [],
      sent: 0,
      dictation: null,
      stopped: false,
      recorded: false,
      closed: false,
      abort: new AbortController(),
      finalTimer: null,
      original: text,
      written: text,
      before: text.slice(0, caretAt),
      after: text.slice(caretAt),
      spoken: '',
      taken: '',
      shown: '',
      take: startTake(m, rec.mimeType || mimeType || 'audio/webm').id,
      keptAt: Date.now(),
    };
    session.dictation = openDictation(m, dictationLang(), {
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
      if (Date.now() - session.keptAt < KEEP_MS) return;
      session.keptAt = Date.now();
      recordTake(session.take, session.chunks);
    };
    session.rec.onstop = () => {
      session.stream.getTracks().forEach((track) => track.stop());
      session.meter?.close();
      session.recorded = true;
      if (!session.closed) recordTake(session.take, session.chunks);
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
    dropTake(session.take);
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

  // The takes the machine could not recognize: one at a time over the field, recognized again by hand or by themselves
  // once the link is live again (each once per return of the link, and not while a dictation is going on).
  useEffect(() => {
    void loadTakes();
  }, []);
  const failed = useFailedTakes(machine);
  const linkLive = useConnection(machine)?.k === 'live';
  const restored = useLinkRestored(machine);
  const tried = useRef(new Set<string>());

  const retry = async (take: VoiceTake, quiet: boolean) => {
    if (retrying.has(take.id)) return;
    retrying.add(take.id);
    try {
      const said = (
        await transcribe(take.machine, take.audio, dictationLang(), new AbortController().signal)
      ).text.trim();
      if (!said) {
        dropTake(take.id);
        if (!quiet) toast(t('next.voice.silence'));
        return;
      }
      latest.current.setText(join(fieldNow(), said));
      finishTake(take.id, said);
    } catch (failure) {
      if (!quiet) toast(t('next.voice.failed'), { tone: 'error', description: failureText(failure) });
    } finally {
      retrying.delete(take.id);
    }
  };

  useEffect(() => {
    tried.current.clear();
  }, [restored]);
  useEffect(() => {
    if (!machine || !linkLive || !hasFeature(machine, 'voice') || live.current) return;
    for (const take of failed) {
      if (tried.current.has(take.id)) continue;
      tried.current.add(take.id);
      void retry(take, true);
    }
  });

  const first = failed[0];
  const notice: DockNotice | null = first
    ? {
        kind: 'voice',
        ms: first.ms,
        more: failed.length - 1,
        onRetry: () => retry(first, false),
        onDrop: async () => dropTake(first.id),
      }
    : null;

  return {
    recording,
    recognized,
    caret,
    notice,
    onMic:
      machine && hasFeature(machine, 'voice') && canRecord()
        ? (at?: number) => void start(machine, at)
        : undefined,
    onCancelRecording: cancel,
    onFinishRecording: finish,
    /** Puts a text after what is in the field (a take's text from «Записи»). */
    insert: (said: string) => latest.current.setText(join(fieldNow(), said)),
  };
};
