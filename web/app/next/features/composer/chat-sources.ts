// The chats «@» offers beside the files: the chats of the machine by title, and the ones whose messages hold the words
// typed (the runtime's message search). A pick is a place the message points at (protocol MessageRef, kind `chat`): the
// runtime puts an excerpt of that chat under the person's words when the message goes.
import { searchMessages } from '@/next/commands/pages';
import { conversationsStore } from '@/next/stores/conversations';
import { foldText } from '@/next/features/search/highlight';
import type { ConvId, MachineId, MessageRef } from '@/next/protocol/v2';

export interface IChatPick {
  ref: MessageRef;
  /** Why it is here: the words around the match, or nothing for a title. */
  snippet: string | null;
  /** Unix ms of the chat's last activity. */
  at: number;
}

/** A pick lists at most this many chats; the first ones are the newest. */
const PICKS_MAX = 6;
/** Words are asked of the messages from this many letters. */
const TEXT_FROM = 2;

export const chatRef = (id: ConvId, title: string): MessageRef => ({ kind: 'chat', id, title, target: '' });

/** The chats of the machine other than `except`, whose title has every word of the query; the newest first. */
export const chatsByTitle = (machine: MachineId, except: ConvId | null, query: string): IChatPick[] => {
  const words = foldText(query).split(/\s+/).filter(Boolean);
  return [...conversationsStore.values()]
    .filter((c) => c.machineId === machine && c.id !== except)
    .filter((c) => words.every((word) => foldText(c.title).includes(word)))
    .sort((a, b) => b.lastActivityAt - a.lastActivityAt)
    .map((c) => ({ ref: chatRef(c.id, c.title), snippet: null, at: c.lastActivityAt }));
};

/** The chats for what is typed after «@»: by title, then by what was said in them. */
export const findChats = async (
  machine: MachineId,
  except: ConvId | null,
  query: string,
  signal: AbortSignal,
): Promise<IChatPick[]> => {
  const typed = query.trim();
  const byTitle = chatsByTitle(machine, except, typed);
  if (typed.length < TEXT_FROM) return byTitle.slice(0, PICKS_MAX);
  const found = await searchMessages(machine, typed, 30);
  signal.throwIfAborted();
  if (!('ok' in found)) return byTitle.slice(0, PICKS_MAX);
  const seen = new Set<string>(byTitle.map((p) => p.ref.id));
  const byText: IChatPick[] = [];
  for (const hit of found.value.hits) {
    const chat = conversationsStore.get(hit.convId);
    if (!chat || chat.machineId !== machine || hit.convId === except || seen.has(hit.convId)) continue;
    seen.add(hit.convId);
    byText.push({ ref: chatRef(chat.id, chat.title), snippet: hit.snippet, at: chat.lastActivityAt });
  }
  return [...byTitle, ...byText].slice(0, PICKS_MAX);
};
