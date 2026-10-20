import { useEffect, useEffectEvent } from 'react';
import { createVoiceTap } from '@/lib/voice-key';

/**
 * The dictation key of the settings: a single tap calls `onTap`. Listened to in the capture phase, so the
 * editor in focus does not have to pass the key on.
 */
const useVoiceTapKey = (key: string | null, onTap: () => void, enabled: boolean) => {
  const tapped = useEffectEvent(onTap);
  useEffect(() => {
    if (!key || !enabled) return;
    const tap = createVoiceTap(key);
    const down = (event: KeyboardEvent) => {
      if (!tap.down(event, event.timeStamp)) return;
      event.preventDefault();
      tapped();
    };
    const up = (event: KeyboardEvent) => {
      if (tap.up(event, event.timeStamp)) tapped();
    };
    const reset = () => tap.reset();
    window.addEventListener('keydown', down, true);
    window.addEventListener('keyup', up, true);
    window.addEventListener('pointerdown', reset, true);
    window.addEventListener('blur', reset);
    return () => {
      window.removeEventListener('keydown', down, true);
      window.removeEventListener('keyup', up, true);
      window.removeEventListener('pointerdown', reset, true);
      window.removeEventListener('blur', reset);
    };
  }, [key, enabled]);
};

export default useVoiceTapKey;
