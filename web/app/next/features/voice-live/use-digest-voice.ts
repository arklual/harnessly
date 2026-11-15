// The day's digest read out by voice: one voice call in the digest mode (realtime promptDigest) that says
// the page's phrases one by one, word for word, and when the person speaks over it answers about the item being read
// and asks «Продолжить?». The page gives the phrases and draws digest-listen.tsx and digest-line.tsx from the answer.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { VOICE_MODE } from '@/next/features/voice-live/api';
import type { TDigestVoice } from '@/next/features/voice-live/digest-listen';
import {
  DIGEST_SAID,
  DIGEST_STEP,
  digestSaid,
  digestScreen,
  digestState,
  readNote,
  saidOut,
  splitSaid,
  type IDigestPhrase,
  type TDigestStep,
} from '@/next/features/voice-live/digest-voice';
import { dropEcho } from '@/next/features/voice-live/echo';
import { CALL, useVoiceCall, type TVoiceFail } from '@/next/features/voice-live/use-voice-call';
import type { MachineId } from '@/next/protocol/v2';

export interface IDigestVoice {
  state: TDigestVoice;
  /** The phrase being read or talked about; -1 before the first. */
  index: number;
  /** The anchor of the item talked about (a question about it), else null. */
  item: string | null;
  /** What the person said last, as heard. */
  heard: string;
  /** The voice's words now: said so far, and the rest of the phrase (empty while it answers a question). */
  said: string;
  rest: string;
  level: () => number;
  fail: TVoiceFail | null;
  play: () => void;
  pause: () => void;
  resume: () => void;
  end: () => void;
}

/** How many of the voice's last lines its echo is looked for in. */
const ECHO_LINES = 4;

export const useDigestVoice = (phrases: readonly IDigestPhrase[], machine: MachineId): IDigestVoice => {
  const [step, setStepState] = useState<TDigestStep>(DIGEST_STEP.idle);
  const [index, setIndex] = useState(-1);
  const stepRef = useRef<TDigestStep>(DIGEST_STEP.idle);
  const at = useRef(-1);
  // The last phrase said to its end: a phrase cut by a question or a pause is read again.
  const doneAt = useRef(-1);
  // How many of the voice's lines were over when the phrase being read was sent: the next one says it.
  const [voiceAt, setVoiceAt] = useState(0);
  const voiceCount = useRef(0);
  const phrasesRef = useRef(phrases);
  useEffect(() => {
    phrasesRef.current = phrases;
  }, [phrases]);
  const voiceLines = useRef<string[]>([]);

  const setStep = (s: TDigestStep) => {
    stepRef.current = s;
    setStepState(s);
  };

  const call = useVoiceCall({
    machine,
    mode: VOICE_MODE.digest,
    voice: '',
    model: '',
    context: () => 'The home page of harnessly: the day digest.',
    primer: () => ({ screen: digestScreen(phrasesRef.current) }),
    onReady: () => readAt(0),
    onTurn: (role, text) => (role === 'assistant' ? voiceDone(text) : personDone(text)),
  });
  const { say, stop, hold, lines } = call;

  const finish = () => {
    setStep(DIGEST_STEP.idle);
    hold(false);
    stop();
  };

  function readAt(i: number) {
    const p = phrasesRef.current[i];
    if (!p) {
      finish();
      return;
    }
    at.current = i;
    setIndex(i);
    setVoiceAt(voiceCount.current);
    setStep(DIGEST_STEP.reading);
    void say(readNote(p), undefined, true);
  }

  function voiceDone(text: string) {
    voiceCount.current += 1;
    voiceLines.current = [...voiceLines.current, text].slice(-ECHO_LINES);
    const s = stepRef.current;
    if (s === DIGEST_STEP.reading) {
      // Cut short (the person spoke over it): wait for their words; «да» reads the phrase again.
      if (!saidOut(phrasesRef.current[at.current]?.text ?? '', text)) {
        setStep(DIGEST_STEP.asking);
        return;
      }
      doneAt.current = at.current;
      readAt(at.current + 1);
    } else if (s === DIGEST_STEP.answering) {
      setStep(DIGEST_STEP.asking);
    }
  }

  function personDone(raw: string) {
    const s = stepRef.current;
    if (s === DIGEST_STEP.idle || s === DIGEST_STEP.paused) return;
    const said = digestSaid(dropEcho(raw, voiceLines.current));
    if (said === null) return;
    if (said === DIGEST_SAID.stop) {
      finish();
      return;
    }
    if (said === DIGEST_SAID.on && s !== DIGEST_STEP.answering) {
      readAt(doneAt.current + 1);
      return;
    }
    // A question about the item being read: the voice answers it itself, then asks «Продолжить?».
    setStep(DIGEST_STEP.answering);
  }

  const play = useCallback(() => {
    if (call.call === CALL.connecting || call.call === CALL.live) return;
    at.current = -1;
    doneAt.current = -1;
    voiceCount.current = 0;
    setVoiceAt(0);
    voiceLines.current = [];
    setIndex(-1);
    setStep(DIGEST_STEP.reading);
    void call.start();
  }, [call]);

  const pause = useCallback(() => {
    if (stepRef.current === DIGEST_STEP.idle || stepRef.current === DIGEST_STEP.paused) return;
    hold(true);
    setStep(DIGEST_STEP.paused);
  }, [hold]);

  // readAt and finish read refs, say, hold and stop only.
  const resume = useCallback(() => {
    if (stepRef.current !== DIGEST_STEP.paused) return;
    hold(false);
    readAt(doneAt.current + 1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hold, say]);

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const end = useCallback(() => finish(), [hold, stop]);

  const live = call.call === CALL.connecting || call.call === CALL.live;
  const words = useMemo(() => {
    const voice = lines.filter((l) => l.role === 'assistant')[voiceAt]?.text ?? '';
    const p = phrases[index];
    if (step === DIGEST_STEP.reading && p) return splitSaid(p.text, voice);
    const last = lines[lines.length - 1];
    return { said: last?.role === 'assistant' ? last.text : '', rest: '' };
  }, [lines, phrases, index, step, voiceAt]);
  const heard = [...lines].reverse().find((l) => l.role === 'user')?.text ?? '';
  const talked = step === DIGEST_STEP.answering || step === DIGEST_STEP.asking;

  return {
    state: digestState(live, step, call.floor, call.interrupted),
    index,
    item: talked ? (phrases[index]?.anchor ?? null) : null,
    heard: talked ? heard : '',
    said: words.said,
    rest: words.rest,
    level: call.level,
    fail: call.fail,
    play,
    pause,
    resume,
    end,
  };
};
