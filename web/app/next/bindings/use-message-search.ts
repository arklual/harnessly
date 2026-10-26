// Search over the messages of the open machine's chats, as the search dialog asks it: after the last key, from two
// letters, the answer kept for the words it was asked for. A machine that cannot search (an older runtime) or does not
// answer gives no messages and the dialog goes on with the titles.
import { useEffect, useState } from 'react';
import { searchMessages } from '@/next/commands/pages';
import type { MachineId, MessageHit } from '@/next/protocol/v2';

/** The words are asked of the machine this long after the last key. */
export const MESSAGE_SEARCH_DEBOUNCE_MS = 220;
/** Messages are searched from this many letters. */
export const MESSAGE_SEARCH_FROM = 2;
const HITS_MAX = 40;

export interface IMessageSearch {
  /** The messages found for the words now typed; null while they are asked or when too little is typed. */
  hits: readonly MessageHit[] | null;
  /** The machine is still reading its older chats: more may turn up. */
  indexing: boolean;
  /** Whether messages are being looked for at all (enough typed, a machine to ask). */
  asked: boolean;
}

interface IAnswer {
  machine: MachineId;
  query: string;
  hits: readonly MessageHit[];
  indexing: boolean;
}

export const useMessageSearch = (machine: MachineId | null, query: string, on: boolean): IMessageSearch => {
  const [answer, setAnswer] = useState<IAnswer | null>(null);
  const words = query.trim();
  const asked = on && machine !== null && [...words].length >= MESSAGE_SEARCH_FROM;

  useEffect(() => {
    if (!asked || machine === null) return;
    let stale = false;
    const timer = setTimeout(() => {
      void searchMessages(machine, words, HITS_MAX).then((res) => {
        if (stale) return;
        setAnswer(
          'ok' in res
            ? { machine, query: words, hits: res.value.hits, indexing: res.value.indexing }
            : { machine, query: words, hits: [], indexing: false },
        );
      });
    }, MESSAGE_SEARCH_DEBOUNCE_MS);
    return () => {
      stale = true;
      clearTimeout(timer);
    };
  }, [asked, machine, words]);

  const current = asked && answer?.machine === machine && answer.query === words ? answer : null;
  return { hits: current?.hits ?? null, indexing: current?.indexing ?? false, asked };
};
