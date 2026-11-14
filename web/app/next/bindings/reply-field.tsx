// A card's reply field with its machine's voice: an agent's question, another agent's message, the picture
// editor and a review's remark answer in the chat's own field with the microphone of the machine the host names.
import { useContext } from 'react';
import { useVoice } from '@/next/bindings/use-voice';
import DockBar from '@/next/features/composer/dock-bar';
import { ReplyComposer, ReplyMachine, type IReplyFieldProps } from '@/next/features/composer/reply-field';
import { DEFAULT_LIMITS, limitsOf } from '@/next/stores/machines';
import { useEnterSends } from '@/next/stores/me';

export const CardReply = (props: IReplyFieldProps) => {
  const machine = useContext(ReplyMachine);
  const enterSends = useEnterSends();
  const { notice, recording, recognized, caret, onMic, onCancelRecording, onFinishRecording } = useVoice(
    machine,
    props.value,
    props.onChange,
  );
  return (
    <ReplyComposer
      {...props}
      voice={{ recording, recognized, caret, onMic, onCancelRecording, onFinishRecording }}
      maxText={(machine ? limitsOf(machine) : DEFAULT_LIMITS).maxText}
      enterSends={enterSends}
      bar={<DockBar notice={notice ?? null} />}
    />
  );
};
