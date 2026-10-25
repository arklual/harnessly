// The side chat of the open conversation over the machine: one question at a time, its answer filled in as the frames
// come, the finished pairs sent back as history with the next question. A stop aborts the stream (the machine ends
// the fork); a question asked again replaces the one that was cut.
import { useEffect, useRef, useState } from 'react';
import { askSideChat, type ISideTurn } from '@/next/features/pane/side-chat-api';
import type { ISideMessage } from '@/next/features/pane/side-chat';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

let seq = 0;
const nextId = () => `s${++seq}`;

const historyOf = (messages: readonly ISideMessage[]): ISideTurn[] => {
  const out: ISideTurn[] = [];
  for (let i = 0; i + 1 < messages.length; i += 1) {
    const q = messages[i];
    const a = messages[i + 1];
    if (q.who === 'me' && a.who === 'agent' && !a.typing && a.cut === null)
      out.push({ question: q.text, answer: a.text });
  }
  return out;
};

/** The side chat belongs to the open conversation: another one starts it empty (and stops a running answer). */
export const useSideChat = (machine: MachineId | null, conv: ConvId | null) => {
  const [messages, setMessages] = useState<ISideMessage[]>([]);
  const [busy, setBusy] = useState(false);
  const [owner, setOwner] = useState(conv);
  const abortRef = useRef<AbortController | null>(null);
  if (owner !== conv) {
    setOwner(conv);
    setMessages([]);
    setBusy(false);
  }
  useEffect(() => () => abortRef.current?.abort(), [conv]);

  const patch = (id: string, change: (m: ISideMessage) => ISideMessage) =>
    setMessages((list) => list.map((m) => (m.id === id ? change(m) : m)));

  const ask = (question: string, before: readonly ISideMessage[] = messages) => {
    if (busy || !machine || !conv) return;
    const answerId = nextId();
    const history = historyOf(before);
    setMessages([
      ...before,
      { id: nextId(), who: 'me', text: question, typing: false, cut: null },
      { id: answerId, who: 'agent', text: '', typing: true, cut: null },
    ]);
    setBusy(true);
    const abort = new AbortController();
    abortRef.current = abort;
    void askSideChat(machine, conv, { question, history }, abort.signal, (event) => {
      if (event.type === 'delta') patch(answerId, (m) => ({ ...m, text: m.text + event.text }));
      else if (event.type === 'done') patch(answerId, (m) => ({ ...m, text: event.text, typing: false }));
      else
        patch(answerId, (m) => ({
          ...m,
          typing: false,
          cut: event.message || t(`next.side.error.${event.code}`),
        }));
    }).finally(() => {
      if (abortRef.current === abort) abortRef.current = null;
      setBusy(false);
    });
  };

  const stop = () => {
    const abort = abortRef.current;
    if (!abort) return;
    abort.abort();
    setMessages((list) =>
      list.map((m) => (m.typing ? { ...m, typing: false, cut: t('next.side.stopped') } : m)),
    );
  };

  /** Asks the question of a cut answer again, in its place. */
  const retry = (answerId: string) => {
    const at = messages.findIndex((m) => m.id === answerId);
    const question = messages[at - 1];
    if (at < 1 || question?.who !== 'me') return;
    ask(question.text, messages.slice(0, at - 1));
  };

  const clear = () => {
    abortRef.current?.abort();
    setMessages([]);
  };

  return { messages, busy, ask, stop, retry, clear };
};
