// The voice of a ChatGPT chat, mounted by the app's voice layer: what the person says goes to the chat as a
// message, and once the answer is written it goes back to the voice, which tells it. It does not depend on the chat's
// screen, so the talk goes on while the person is elsewhere; the screen follows through voice-bridge.ts.
import { useEffect, useRef } from 'react';
import { appendSpoken, followAnswer, loadChat, sendMessage } from '@/next/features/chatgpt/api';
import { GPT_VOICE_EVENT, gptPickOf, tellGptScreen } from '@/next/features/chatgpt/voice-bridge';
import { gptChatsChanged, useGptChats } from '@/next/features/chatgpt/store';
import { VOICE_MODE } from '@/next/features/voice-live/api';
import { spokenResult, spokenTask } from '@/next/features/voice-live/spoken';
import VoiceLive from '@/next/features/voice-live/voice-live';
import type { ConvId, MachineId } from '@/next/protocol/v2';

interface IVoiceLiveGptProps {
  machine: MachineId;
  /** The talk's mark in the layer (gptConv). */
  conv: ConvId;
  /** The ChatGPT chat. */
  id: string;
}

const VoiceLiveGpt = ({ machine, conv, id }: IVoiceLiveGptProps) => {
  // The answer being written, followed until it is over; null while the chat writes nothing for the voice.
  const unfollow = useRef<(() => void) | null>(null);
  useEffect(() => () => unfollow.current?.(), []);
  const title = useGptChats(machine)?.find((c) => c.id === id)?.title ?? '';

  return (
    <VoiceLive
      machine={machine}
      conv={conv}
      mode={VOICE_MODE.chat}
      onTask={(task, { say }) => {
        const words = spokenTask(task);
        if (!words) return;
        if (unfollow.current) {
          void say('The chat is still writing its previous answer; ask again in a moment.', task.id);
          return;
        }
        const pick = gptPickOf(id);
        sendMessage(machine, id, { text: words, model: pick.model, effort: pick.effort, files: [] }).then(
          () => {
            gptChatsChanged();
            tellGptScreen(id, GPT_VOICE_EVENT.sent);
            unfollow.current = followAnswer(machine, id, {
              onDelta: () => undefined,
              onDone: () => {
                unfollow.current = null;
                loadChat(machine, id).then(
                  (answer) => {
                    const last = answer.chat.messages?.findLast((m) => m.role === 'assistant');
                    const said = last?.text ? spokenResult(last.text) : '';
                    void say(
                      said
                        ? `The chat wrote this on screen: ${said}`
                        : 'The chat finished without an answer.',
                      task.id,
                    );
                  },
                  () => void say('The chat finished, but its answer could not be read.', task.id),
                );
              },
            });
          },
          () => void say('The chat refused to take it; nothing was written.', task.id),
        );
      }}
      context={() => (title ? `ChatGPT chat «${title}»` : '')}
      onLine={(line) => {
        // Each line said goes to the feed as it is over, the person's and ChatGPT's.
        if (!line.text.trim()) return;
        void appendSpoken(machine, id, [line]).then(() => {
          gptChatsChanged();
          tellGptScreen(id, GPT_VOICE_EVENT.kept);
        });
      }}
    />
  );
};

export default VoiceLiveGpt;
