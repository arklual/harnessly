// The machine a kept screen draws its design or space on. A chat is one entity whichever machine it runs on: the screen
// of a chat's design follows the chat (the address's `?m=`, else where the chat's record says it is), not the machine
// that happens to be active in the shell. Drawn on the active one while the chat is on another, its list does not have
// the chat, and the column took an existing chat for a gone one and offered a new chat in its place.

import { ROUTE, type TRoute } from '@/next/routes/routes';

/** The chat a screen's address names, when the machine of the chat can decide where the screen is drawn. */
export const screenChatOf = (route: TRoute): string | null =>
  (route.kind === ROUTE.site || route.kind === ROUTE.app || route.kind === ROUTE.review) && route.chat
    ? route.chat
    : null;

/** How long a screen waits for the record of its chat before it draws on the active machine (a chat that is not there). */
export const CHAT_RECORD_WAIT_MS = 8_000;

/**
 * Whether the screen must wait: it names a chat whose machine nothing has told yet (no address `?m=`, no record, no
 * feed). Drawn now it would use the active machine, whose list has not got the chat, and the column would take an
 * existing chat for a gone one and offer a new chat.
 */
export const screenWaits = (route: TRoute, chatMachine: string | null): boolean => {
  if (screenChatOf(route) === null || chatMachine) return false;
  return !(route.kind === ROUTE.site && route.machine);
};

/**
 * Where the screen of `route` is drawn: the machine its address names, else the one its chat is on (`chatMachine`, from
 * the chat's record, null while unknown), else the shell's active machine.
 */
export const screenMachine = <M extends string>(
  route: TRoute,
  chatMachine: string | null,
  active: M,
): M | string => {
  if (route.kind === ROUTE.site && route.machine) return route.machine;
  return screenChatOf(route) !== null && chatMachine ? chatMachine : active;
};
