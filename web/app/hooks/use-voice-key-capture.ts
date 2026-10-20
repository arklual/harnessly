import { useCallback, useEffect, useState } from 'react';
import { isAssignableVoiceKey } from '@/lib/voice-key';

/**
 * The dictation key is assigned by pressing it. While capturing, the next key press is taken in the capture
 * phase and goes nowhere else: Escape leaves the capture instead of closing the dialog around it, and a key that
 * types text is refused with a word why.
 */
const useVoiceKeyCapture = (onChange: (key: string) => void) => {
  const [capturing, setCapturing] = useState(false);
  const [refused, setRefused] = useState(false);

  useEffect(() => {
    if (!capturing) return;
    const press = (event: KeyboardEvent) => {
      event.preventDefault();
      event.stopPropagation();
      if (event.code === 'Escape') {
        setCapturing(false);
        setRefused(false);
        return;
      }
      if (!isAssignableVoiceKey(event.code)) {
        setRefused(true);
        return;
      }
      onChange(event.code);
      setCapturing(false);
      setRefused(false);
    };
    window.addEventListener('keydown', press, true);
    return () => window.removeEventListener('keydown', press, true);
  }, [capturing, onChange]);

  const start = useCallback(() => {
    setCapturing(true);
    setRefused(false);
  }, []);
  const cancel = useCallback(() => {
    setCapturing(false);
    setRefused(false);
  }, []);
  return { capturing, refused, start, cancel };
};

export default useVoiceKeyCapture;
