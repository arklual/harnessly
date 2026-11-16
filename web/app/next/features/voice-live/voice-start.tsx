// The voice button of the start page's field: the same wave as in a chat, where the voice mode is on. A chat
// has to exist for the voice to belong to it, so a press starts a new chat (as a send would) and its field begins the talk.
import { t } from '@/next/i18n';
import { useVoiceState, voiceOffered } from '@/next/features/voice-live/use-voice-state';
import type { MachineId } from '@/next/protocol/v2';
import { RoundButton } from '@/next/ui/button';
import '@/next/styles/voice-live.css';

interface IVoiceStartProps {
  machine: MachineId | null;
  /** Starts the new chat; its field takes the ask and begins the talk. */
  onStart: () => void;
  busy?: boolean;
}

const VoiceStart = ({ machine, onStart, busy = false }: IVoiceStartProps) => {
  const [state] = useVoiceState(machine);
  if (!voiceOffered(state)) return null;
  const label = state && !state.ready ? 'next.chatgpt.voice.noLogin' : 'next.chatgpt.voice.newChat';
  return (
    <span className="hx-voice-holder">
      <RoundButton
        icon="audio-lines"
        className="hx-voice-btn"
        label={t(label)}
        disabled={busy}
        onClick={onStart}
      />
    </span>
  );
};

export default VoiceStart;
