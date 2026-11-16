// The voice mode's state of a machine (whether the person turned it on, signed in, has the subscription), kept fresh:
// a read that failed (the machine was briefly off the hub) is asked again, and a setting saved later comes in.
import { useEffect, useState, type Dispatch, type SetStateAction } from 'react';
import {
  loadVoiceState,
  onVoiceState,
  voiceStateRetryDelay,
  type IVoiceState,
} from '@/next/features/voice-live/api';
import type { MachineId } from '@/next/protocol/v2';

export const useVoiceState = (
  machine: MachineId | null,
): [IVoiceState | null, Dispatch<SetStateAction<IVoiceState | null>>] => {
  const [state, setState] = useState<IVoiceState | null>(null);
  useEffect(() => {
    if (!machine) return;
    let live = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = (tries: number) => {
      loadVoiceState(machine).then(
        (st) => {
          if (live) setState(st);
        },
        () => {
          if (live) timer = setTimeout(() => load(tries + 1), voiceStateRetryDelay(tries));
        },
      );
    };
    load(0);
    const off = onVoiceState(machine, (st) => {
      if (live) setState(st);
    });
    return () => {
      live = false;
      clearTimeout(timer);
      off();
    };
  }, [machine]);
  return [state, setState];
};

/** The voice button stands where the person turned the voice mode on (or the subscription offers it). */
export const voiceOffered = (state: IVoiceState | null): boolean => !!(state?.ready || state?.subscription);
