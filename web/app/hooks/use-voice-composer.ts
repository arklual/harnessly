import { useCallback, useEffect, useRef, useState } from 'react';
import useConfigStore from '@/hooks/use-config-store';
import { shouldSendOnFinal } from '@/lib/dictation-intent';
import useVoiceInput, {
  type IUseVoiceInputOptions,
  type IUseVoiceInputResult,
} from '@/hooks/use-voice-input';
import { VOICE_STATE, type TVoiceState } from '@/lib/voice-transcript';

export const VOICE_PHASE = {
  requesting: 'requesting',
  recording: 'recording',
  transcribing: 'transcribing',
} as const;

export type TVoicePhase = (typeof VOICE_PHASE)[keyof typeof VOICE_PHASE];

const PHASE_OF: Partial<Record<TVoiceState, TVoicePhase>> = {
  [VOICE_STATE.requesting]: VOICE_PHASE.requesting,
  [VOICE_STATE.recording]: VOICE_PHASE.recording,
  [VOICE_STATE.transcribing]: VOICE_PHASE.transcribing,
};

export interface IVoiceComposer {
  voice: IUseVoiceInputResult;
  /** Dictation in progress: the composer shows the recorder instead of its buttons. */
  phase: TVoicePhase | null;
  /** Pointer down on the mic button: push-to-talk in "hold" mode, a click otherwise. */
  pressMic: () => void;
  /** Stop recording and keep what was said. */
  finish: () => void;
  /** Stop recording and send what was said once, whether or not "send after recording" is on. */
  finishAndSend: () => void;
  /** The draft of the dictation in progress; empty outside it. */
  preview: string;
  /** Stop recording and put the field back as it was before it started; nothing is sent. */
  cancel: () => void;
}

interface IUseVoiceComposerOptions extends Omit<IUseVoiceInputOptions, 'onAutoSend'> {
  /** Called once with the dictated text when it should go out: auto-send is on, or Enter was pressed mid-dictation. */
  onAutoSend?: (text: string) => void;
}

/**
 * The composer's side of dictation: what the recorder bar needs on top of `useVoiceInput` — a phase,
 * a start time, "done" and "cancel" that work the same in both mic modes, and a push-to-talk release that
 * still lands when the button under the finger has been replaced by the recorder.
 */
const useVoiceComposer = ({
  value,
  onValueChange,
  onAutoSend,
  disabled,
  hotkey,
}: IUseVoiceComposerOptions): IVoiceComposer => {
  const mode = useConfigStore((s) => s.voiceSettings.mode);
  // Read by the engine's callbacks, which fire after the render that cancelled; state would still hold the old value there.
  const cancelledRef = useRef(false);
  const [base, setBase] = useState(value);

  const guardedChange = useCallback(
    (next: string) => {
      if (!cancelledRef.current) onValueChange(next);
    },
    [onValueChange],
  );
  // Enter pressed mid-dictation: the final transcript that follows sends, and only once per recording.
  const wantSendRef = useRef(false);
  const sentRecordingIdRef = useRef<number | null>(null);
  const autoSend = useConfigStore((s) => s.voiceSettings.autoSend);
  const guardedAutoSend = useCallback(
    (text: string, recordingId: number, heard: boolean) => {
      if (cancelledRef.current) return;
      const send = shouldSendOnFinal({
        recordingId,
        sentRecordingId: sentRecordingIdRef.current,
        wantSend: wantSendRef.current,
        autoSend,
        heard,
        text,
      });
      wantSendRef.current = false;
      if (!send) return;
      sentRecordingIdRef.current = recordingId;
      onAutoSend?.(text);
    },
    [onAutoSend, autoSend],
  );

  const [shownState, setShownState] = useState<TVoiceState>(VOICE_STATE.idle);
  // `disabled` only forbids starting: a composer that turns busy mid-recording must still let it be stopped.
  const recordingNow = PHASE_OF[shownState] !== undefined;
  const voice = useVoiceInput({
    value,
    onValueChange: guardedChange,
    onAutoSend: onAutoSend ? guardedAutoSend : undefined,
    disabled: disabled && !recordingNow,
    hotkey,
  });
  // A cancelled recording leaves the composer at once, even while the engine still finishes it in the background.
  const [dismissed, setDismissed] = useState(false);
  const phase = dismissed ? null : (PHASE_OF[voice.state] ?? null);

  if (voice.state !== shownState) {
    setShownState(voice.state);
    if (voice.state === VOICE_STATE.requesting) {
      setBase(value);
      setDismissed(false);
    }
  }
  useEffect(() => {
    if (voice.state === VOICE_STATE.requesting) cancelledRef.current = false;
    // Not on `requesting`: an engine fallback restarts the same dictation and must keep an Enter already pressed.
    else if (voice.state === VOICE_STATE.idle || voice.state === VOICE_STATE.error)
      wantSendRef.current = false;
  }, [voice.state]);

  const voiceRef = useRef(voice);
  useEffect(() => {
    voiceRef.current = voice;
  });

  const stop = useCallback(() => {
    const current = voiceRef.current;
    if (current.state !== VOICE_STATE.recording) return;
    if (mode === 'hold') current.press();
    else current.toggle();
  }, [mode]);

  // Finished while the browser still asked for the microphone: there is nothing to stop until it starts recording.
  const stopPendingRef = useRef(false);
  useEffect(() => {
    if (!stopPendingRef.current || voice.state === VOICE_STATE.requesting) return;
    stopPendingRef.current = false;
    stop();
  }, [voice.state, stop]);

  const finish = useCallback(() => {
    if (voiceRef.current.state === VOICE_STATE.requesting) stopPendingRef.current = true;
    else stop();
  }, [stop]);

  const finishAndSend = useCallback(() => {
    wantSendRef.current = true;
    finish();
  }, [finish]);

  const cancel = useCallback(() => {
    cancelledRef.current = true;
    wantSendRef.current = false;
    setDismissed(true);
    onValueChange(base);
    finish();
  }, [base, finish, onValueChange]);

  const pressMic = useCallback(() => {
    const current = voiceRef.current;
    if (mode !== 'hold') {
      current.toggle();
      return;
    }
    current.press();
    // The button is replaced by the recorder while held, so its own pointerup never comes.
    const release = () => {
      window.removeEventListener('pointerup', release);
      window.removeEventListener('pointercancel', release);
      voiceRef.current.release();
    };
    window.addEventListener('pointerup', release);
    window.addEventListener('pointercancel', release);
  }, [mode]);

  return { voice, phase, preview: voice.preview, pressMic, finish, finishAndSend, cancel };
};

export default useVoiceComposer;
