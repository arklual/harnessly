// ⌥ Space away from a chat («Голос — в поле»): the same field rises at the bottom centre, over whatever is on
// screen, and the voice starts in it at once. What is said goes to the last chat's agent, which works in its own chat;
// the field shows who has the floor, what is heard and the voice's reply line by line. Esc, or the voice's button, ends
// it and the field leaves.
import { useEffect, useRef, useState } from 'react';
import { VoiceSlot } from '@/next/features/voice-live/voice-layer';
import { RecordingWave } from '@/next/features/composer/recording';
import { askVoiceStart, FIELD_FLOOR, useVoiceField } from '@/next/features/voice-live/field';
import { t } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import '@/next/styles/voice-live.css';

/** The field leaves in this long (the closing animation). */
const EXIT_MS = 200;
/** A line said aloud, as long as the field shows: its latest words. */
const LINE_SHOWN = 90;
const tail = (text: string) => (text.length > LINE_SHOWN ? `…${text.slice(-LINE_SHOWN)}` : text);

interface IVoiceFloatProps {
  machine: MachineId;
  conv: ConvId;
  onClose: () => void;
}

const VoiceFloat = ({ machine, conv, onClose }: IVoiceFloatProps) => {
  const root = useRef<HTMLDivElement>(null);
  const voice = useVoiceField(root);
  const [closing, setClosing] = useState(false);
  const started = useRef(false);
  const close = () => {
    if (closing) return;
    setClosing(true);
    window.setTimeout(onClose, EXIT_MS);
  };

  // The voice in this field starts as the field comes.
  useEffect(() => {
    askVoiceStart();
  }, []);
  // The voice ended (its button, Esc): the field leaves with it.
  useEffect(() => {
    if (voice) started.current = true;
    else if (started.current) close();
    // close only reads the closing flag of the moment.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voice]);
  // Before the voice is on (it may be refused, with a toast), Esc takes the field away.
  useEffect(() => {
    if (voice) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // close only reads the closing flag of the moment.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voice]);

  const line = voice?.heard || voice?.said || '';
  return (
    <div className="hx-voice-float" data-closing={closing || undefined}>
      <div ref={root} className="hx-composer hx-voice-float__field" data-live={voice?.floor}>
        <div
          className="hx-voice-float__line"
          data-said={voice && !voice.heard && voice.said ? 'true' : undefined}
        >
          {line ? tail(line) : t('next.chatgpt.voice.floatHint')}
        </div>
        {voice && (
          <RecordingWave
            level={voice.level}
            listening={voice.floor !== FIELD_FLOOR.thinking && voice.floor !== FIELD_FLOOR.starting}
          />
        )}
        <div className="hx-voice-float__tools">
          <VoiceSlot machine={machine} conv={conv} />
        </div>
      </div>
    </div>
  );
};

export default VoiceFloat;
