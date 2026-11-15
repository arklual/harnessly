// The agent's chat about a thing, as the feed names it: by the conversation's id, or by its address in this hub. It
// opens here, on the page (the same event a view's pane.open of chat:<id> ends in); one this page cannot open is null.
import { chatTarget } from '@/next/features/app-views/agents';
import { CHIP, type IActResult, type IDayCard } from '@/next/features/day/feed';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { parseRoute, ROUTE } from '@/next/routes/routes';

const byAddress = (address: string | undefined, origin: string, here: string): ConvId | null => {
  if (address === undefined) return null;
  let url: URL;
  try {
    url = new URL(address, origin);
  } catch {
    return null;
  }
  // A chat of another hub is not one this page can open.
  if (url.origin !== origin) return null;
  // The hub's address of a machine's chat (?c=agent:<machine>:<chat>) opens here only on that machine's page.
  const named = /^agent:([^:]+):(.+)$/.exec(url.searchParams.get('c') ?? '');
  if (named) return here.startsWith(`/m/${named[1]}/`) ? chatTarget(`chat:${named[2]}`) : null;
  const route = parseRoute(url.pathname);
  return route.kind === ROUTE.chat ? chatTarget(`chat:${route.id}`) : null;
};

export const chatOf = (
  named: IActResult & { chat?: string },
  origin: string = window.location.origin,
  here?: string,
): ConvId | null => {
  const id = named.chat ?? named.run?.chat;
  if (id !== undefined) return chatTarget(`chat:${id}`);
  return byAddress(named.run?.url ?? named.url, origin, here ?? window.location.pathname);
};

/** An agent's chat that the opened thing shows inside it: a conversation of the page's machine. */
export interface IItemChat {
  conv: ConvId;
  machine: MachineId;
}

// The page is the machine's own (its runtime, or the hub's address of it): it then names the machine «local», while the
// hub's address of a chat names it by its id in the hub, the one the page is served under (/m/<id>/).
const pageOf = (id: string): boolean => {
  if (typeof document === 'undefined') return false;
  const base = document.querySelector('meta[name="api-base"]')?.getAttribute('content') ?? '';
  return (
    `${base.replace(/\/+$/, '')}/`.endsWith(`/m/${id}/`) || window.location.pathname.startsWith(`/m/${id}/`)
  );
};

const chatOnMachine = (
  named: IActResult & { chat?: string },
  machine: MachineId,
  origin: string,
): IItemChat | null => {
  const id = named.chat ?? named.run?.chat;
  if (id !== undefined) {
    const conv = chatTarget(`chat:${id}`);
    return conv === null ? null : { conv, machine };
  }
  const address = named.run?.url ?? named.url;
  if (address === undefined) return null;
  let url: URL;
  try {
    url = new URL(address, origin);
  } catch {
    return null;
  }
  // The hub's address of a machine's chat names the machine: the page shows it only when it is the page's own, whatever
  // host the address was written for (the hub, the machine's own runtime).
  const onMachine = /^agent:([^:]+):(.+)$/.exec(url.searchParams.get('c') ?? '');
  if (onMachine) {
    const conv = onMachine[1] === machine || pageOf(onMachine[1]) ? chatTarget(`chat:${onMachine[2]}`) : null;
    return conv === null ? null : { conv, machine };
  }
  if (url.origin !== origin) return null;
  const route = parseRoute(url.pathname);
  const conv = route.kind === ROUTE.chat ? chatTarget(`chat:${route.id}`) : null;
  return conv === null ? null : { conv, machine };
};

/** The chat an action answered with, when the page can show it inside the opened thing. */
export const itemChatOf = (
  named: IActResult & { chat?: string },
  machine: MachineId,
  origin: string = window.location.origin,
): IItemChat | null => chatOnMachine(named, machine, origin);

/**
 * The agent's chat of an opened thing: the one the card names as its run, else the chat its material or a chip of it
 * leads to. Null when no agent has worked on it here.
 */
export const cardChatOf = (
  card: IDayCard,
  machine: MachineId,
  origin: string = window.location.origin,
): IItemChat | null => {
  const named: (IActResult & { chat?: string })[] = [];
  if (card.run) named.push(card.run);
  if (card.go) named.push(card.go);
  for (const chip of card.chips ?? []) if (chip.kind === CHIP.open && chip.url) named.push({ url: chip.url });
  for (const one of named) {
    const chat = chatOnMachine(one, machine, origin);
    if (chat !== null) return chat;
  }
  return null;
};

/** The chip or the material leads to the chat the opened thing already shows: it has nothing to add there. */
export const leadsTo = (
  named: (IActResult & { chat?: string }) | null | undefined,
  chat: IItemChat,
  origin: string = window.location.origin,
): boolean => {
  if (!named) return false;
  return chatOnMachine(named, chat.machine, origin)?.conv === chat.conv;
};
