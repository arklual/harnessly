// Notes by voice: the words are recognized by the machine as everywhere else (bindings/use-voice.ts), then the model
// makes notes of them (`tidy`: one slide's text, or the whole deck's split by slide). The raw words are what the runtime
// keeps as «из диктовки». The state is in ./dictation-state.ts.
import { useEffect, useReducer, useRef, useState } from 'react';
import { useVoice } from '@/next/bindings/use-voice';
import { RuntimeError } from '@/next/core/net/runtime-json';
import { tidyDeckNotes } from '@/next/features/designly/deck/api';
import {
  DICT,
  DICT_IDLE,
  dictReduce,
  type IDictState,
  type TDictTarget,
} from '@/next/features/designly/deck/dictation-state';
import type { IDeckTidied } from '@/next/features/designly/deck/types';
import { locale, t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { changeDeck } from '@/next/stores/deck';
import { toast } from '@/next/ui/toast';

/** The tidied notes laid over the deck the page holds. */
export const putTidied = (artifact: string, tidied: readonly IDeckTidied[]): void =>
  changeDeck(artifact, (d) => ({
    ...d,
    slides: d.slides.map((s) => {
      const x = tidied.find((n) => n.id === s.id);
      return x ? { ...s, notes: x.notes, seconds: x.seconds, voice: x.voice } : s;
    }),
  }));

export interface IDeckDictation {
  state: IDictState;
  /** The words so far (the raw dictation), '' before the first. */
  said: string;
  /** The machine cannot recognize speech here (no voice feature, no microphone in the browser): no mic is offered. */
  available: boolean;
  /** Starts dictating `target`; `done` is the notice when its notes are made. */
  start: (target: TDictTarget, done: string) => void;
  /** Ends the recording; the words said are made into notes. */
  stop: () => void;
  /** Starts, or stops when this very target is being dictated. */
  toggle: (target: TDictTarget, done: string) => void;
  /** Whether `target` is the one being dictated (listening or tidying). */
  on: (target: TDictTarget) => boolean;
}

const sameTarget = (a: TDictTarget | null, b: TDictTarget): boolean =>
  a !== null && (a.kind === 'all' ? b.kind === 'all' : b.kind === 'slide' && a.page === b.page);

export const useDeckDictation = (machine: MachineId | null, artifact: string): IDeckDictation => {
  const [state, dispatch] = useReducer(dictReduce, DICT_IDLE);
  const raw = useRef('');
  const pending = useRef<{ target: TDictTarget; done: string } | null>(null);
  const running = useRef<{ target: TDictTarget; done: string } | null>(null);
  const seen = useRef(0);
  // The recognizer writes into this text, as into a field: the partial words, then the final ones.
  const [text, setText] = useState('');
  const voice = useVoice(machine, text, (value) => {
    raw.current = value;
    setText(value);
    dispatch({ k: 'words', raw: value });
  });

  const tidy = async (target: TDictTarget, done: string, words: string) => {
    if (machine === null) return;
    try {
      const out = await tidyDeckNotes(
        machine,
        artifact,
        target.kind === 'slide' ? target.page : null,
        words,
        locale(),
      );
      putTidied(artifact, out);
      toast(done, { tone: 'success' });
      dispatch({ k: 'tidied' });
    } catch (failure) {
      toast(failure instanceof RuntimeError ? failure.message : t('next.failure.http'), { tone: 'error' });
      dispatch({ k: 'failed' });
    }
  };

  const recording = voice.recording !== null;
  const recognized = voice.recognized;
  useEffect(() => {
    if (recording && pending.current) {
      running.current = pending.current;
      pending.current = null;
      raw.current = '';
      dispatch({ k: 'start', target: running.current.target });
      return;
    }
    if (recognized !== seen.current) {
      seen.current = recognized;
      const job = running.current;
      running.current = null;
      if (job) {
        dispatch({ k: 'final', raw: raw.current });
        void tidy(job.target, job.done, raw.current);
      }
      return;
    }
    if (!recording && running.current) {
      running.current = null;
      dispatch({ k: 'end' });
    }
    // tidy reads the machine and artifact of this render; the effect follows the recording and the recognition only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [recording, recognized]);

  const start = (target: TDictTarget, done: string) => {
    if (state.phase !== DICT.idle || !voice.onMic) return;
    pending.current = { target, done };
    raw.current = '';
    setText('');
    voice.onMic();
  };
  const stop = () => {
    if (voice.recording && !voice.recording.busy) voice.onFinishRecording();
  };
  const on = (target: TDictTarget) => state.phase !== DICT.idle && sameTarget(state.target, target);

  return {
    state,
    said: state.raw,
    available: voice.onMic !== undefined,
    start,
    stop,
    toggle: (target, done) => {
      if (state.phase === DICT.listening && on(target)) stop();
      else start(target, done);
    },
    on,
  };
};
