// The voice button of the start page's field: the same wave as in a chat, where the voice mode is on. A chat
// has to exist for the voice to belong to it, so a press starts a new chat (as a send would) and its field begins the talk.
import { t } from '@/next/i18n';
import { useVoiceState, voiceOffered } from '@/next/features/voice-live/use-voice-state';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
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
  // Not read yet: nothing, so a machine without the voice does not flash a button.
  if (!state) return null;
  // The voice can not go now (no ChatGPT sign-in, or the mode is off): the wave stays, quiet, and says what to do;
  // a press leads to the settings where it is fixed instead of starting a chat that would fail.
  if (!state.ready) {
    const label = voiceOffered(state) ? 'next.chatgpt.voice.noLogin' : 'next.chatgpt.voice.offHint';
    return (
      <span className="hx-voice-holder">
        <RoundButton
          icon="audio-lines"
          className="hx-voice-btn"
          data-off=""
          aria-disabled="true"
          label={t(label)}
          onClick={() => goTo({ kind: ROUTE.settings, section: 'accounts' })}
        />
      </span>
    );
  }
  return (
    <span className="hx-voice-holder">
      <RoundButton
        icon="audio-lines"
        className="hx-voice-btn"
        label={t('next.chatgpt.voice.newChat')}
        disabled={busy}
        onClick={onStart}
      />
    </span>
  );
};

export default VoiceStart;
