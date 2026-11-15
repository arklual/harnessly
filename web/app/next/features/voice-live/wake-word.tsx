// The shell's ear for «Привет, harnessly» (wake.ts): while the setting is on and no voice is on, the page listens for
// the phrase; heard, a quiet chime plays and the voice starts the way ⌥ Space starts it. The microphone is let go while
// a voice is on and when the setting is turned off; the chime plays again when the conversation the phrase began ends.
import { useEffect, useRef, useState } from 'react';
import { loadVoiceState, onVoiceState, type IVoiceState } from '@/next/features/voice-live/api';
import { setWakeListening, useVoiceOn } from '@/next/features/voice-live/field';
import { chime, dropModel, listenForWake } from '@/next/features/voice-live/wake';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

interface IWakeWordProps {
  machine: MachineId;
  /** Starts the voice: in the chat on screen, else over the screen for the last chat. */
  onWake: () => void;
}

const WakeWord = ({ machine, onWake }: IWakeWordProps) => {
  const [on, setOn] = useState(false);
  const voiceOn = useVoiceOn();
  const woke = useRef(false);
  const wakeRef = useRef(onWake);
  useEffect(() => {
    wakeRef.current = onWake;
  }, [onWake]);

  useEffect(() => {
    let live = true;
    const take = (st: IVoiceState) => live && setOn(Boolean(st.ready && st.wake?.on));
    void loadVoiceState(machine)
      .then(take)
      .catch(() => undefined);
    const stop = onVoiceState(machine, take);
    return () => {
      live = false;
      stop();
    };
  }, [machine]);

  useEffect(() => {
    if (!on) dropModel();
  }, [on]);

  // The conversation the phrase began is over: the falling chime.
  useEffect(() => {
    if (voiceOn || !woke.current) return;
    woke.current = false;
    chime('end');
  }, [voiceOn]);

  useEffect(() => {
    if (!on || voiceOn) return;
    let stop: (() => void) | null = null;
    let live = true;
    listenForWake(machine, () => {
      woke.current = true;
      chime('start');
      wakeRef.current();
    }).then(
      (s) => {
        if (live) {
          stop = s;
          setWakeListening(true);
        } else s();
      },
      (e: unknown) => {
        if (!live) return;
        const mic = e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'NotFoundError');
        toast(t(mic ? 'next.chatgpt.voice.wakeMic' : 'next.chatgpt.voice.wakeModel'), { tone: 'warning' });
      },
    );
    return () => {
      live = false;
      stop?.();
      setWakeListening(false);
    };
  }, [on, voiceOn, machine]);

  return null;
};

export default WakeWord;
