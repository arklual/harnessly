// The place a pane tool works in: a chat (its machine, its id, its folder), or, on the start page where no chat is
// open, the machine's own place — its projects' folder, named to the machine by HOME_CONV (wire.HomeConv): «Файлы» and
// «Терминал» go there with no chat. A chat is a place as it is: the tools take only what they read of it.
import type { Conversation, ConvId } from '@/next/protocol/v2';

export type IPlace = Pick<Conversation, 'id' | 'machineId' | 'cwd'>;

/** The machine's own place: no conversation has this id. */
export const HOME_CONV: ConvId = 'tab-_home';

/** The place is a chat: what is picked or quoted in it can go to the chat's field. */
export const isChatPlace = (place: Pick<IPlace, 'id'>): boolean => place.id !== HOME_CONV;
