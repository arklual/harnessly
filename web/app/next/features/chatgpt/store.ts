// The list of ChatGPT conversations of a machine, shared by the sidebar and the screens: loaded once, read again when
// a screen says it changed (a new chat, a rename, a delete, an answer kept). And the words a new chat carries from the
// start page's field, taken once by its screen.
import { useEffect, useState } from 'react';
import { loadChats, loadState } from '@/next/features/chatgpt/api';
import type { IGptChatSummary } from '@/next/features/chatgpt/model';
import type { MachineId } from '@/next/protocol/v2';

type TListener = () => void;
const listeners = new Set<TListener>();

/** Tells the lists to read the machine's chats again. */
export const gptChatsChanged = () => {
  for (const listener of listeners) listener();
};

/** The machine's ChatGPT chats, newest first; null until read (and on a machine that has no such integration). */
export const useGptChats = (machine: MachineId | null): readonly IGptChatSummary[] | null => {
  const [chats, setChats] = useState<{ machine: MachineId; list: readonly IGptChatSummary[] } | null>(null);
  useEffect(() => {
    if (!machine) return undefined;
    let live = true;
    const read = () => {
      loadChats(machine).then(
        (list) => {
          if (live) setChats({ machine, list });
        },
        () => {
          // A machine without the integration (an older runtime) shows no list.
        },
      );
    };
    read();
    listeners.add(read);
    return () => {
      live = false;
      listeners.delete(read);
    };
  }, [machine]);
  return chats && chats.machine === machine ? chats.list : null;
};

/** A ChatGPT account of the machine is signed in and may use its plan: the sidebar offers «ChatGPT» then. */
export const useGptSignedIn = (machine: MachineId | null): boolean => {
  const [on, setOn] = useState<{ machine: MachineId; on: boolean } | null>(null);
  useEffect(() => {
    if (!machine) return undefined;
    let live = true;
    const read = () => {
      loadState(machine).then(
        (state) => {
          if (live) setOn({ machine, on: (state.accounts ?? []).some((a) => a.signedIn && a.planUse) });
        },
        () => {
          // A machine without the integration offers nothing.
        },
      );
    };
    read();
    listeners.add(read);
    return () => {
      live = false;
      listeners.delete(read);
    };
  }, [machine]);
  return on !== null && on.machine === machine && on.on;
};

let carried: { chat: string; text: string } | null = null;

/** The words the start page's field held when ChatGPT was picked: the new chat's field starts with them. */
export const carryDraft = (chat: string, text: string) => {
  carried = text.trim() ? { chat, text } : null;
};

export const takeDraft = (chat: string): string => {
  if (carried?.chat !== chat) return '';
  const text = carried.text;
  carried = null;
  return text;
};
