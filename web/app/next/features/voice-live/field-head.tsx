// The voice's head at the start of a field's line (harnessly design project, «Агент над доской — только поле»): only
// while the voice of that field is on. It reads the voice by itself, so the field around it does not re-render with
// every word heard.
import type { RefObject } from 'react';
import { useVoiceNow } from '@/next/features/voice-live/field';
import VoiceHead, { HEAD, headOf } from '@/next/features/voice-live/voice-head';
import '@/next/styles/voice-layer.css';

const FieldHead = ({ root }: { root: RefObject<HTMLElement | null> }) => {
  const voice = useVoiceNow();
  if (!voice?.host || !root.current?.contains(voice.host)) return null;
  return (
    <span className="hx-field-head">
      <VoiceHead
        state={voice.interrupted ? HEAD.barged : headOf(voice.floor)}
        kind={voice.head}
        level={voice.level}
      />
    </span>
  );
};

export default FieldHead;
