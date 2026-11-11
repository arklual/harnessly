// The machine a new chat starts on does not hang on the machine picked on top: a machine of the person's that
// answers now and has the agent takes it. The pill and the limits come from what the hub remembers of the machines
// (the fleet's list), so they are drawn whatever the machines do; a pick in the pill's popover stays the person's word.
import type { MachineId, Provider } from '@/next/protocol/v2';
import type { MachineView } from '@/next/stores/machines';

/** A machine as a new chat's start sees it; the machine on top is the first. */
export interface IStartCandidate {
  id: MachineId;
  /** Cannot take a chat at all: unreachable, or an older runtime. */
  off: boolean;
  /** Its runtime answers now. */
  answers: boolean;
  /** The agents ready on it, as it last said. */
  providers: readonly Provider[];
  /** The person's own, not one they only administer (#57). */
  mine: boolean;
}

/** What the machine's socket says: live, down (stale or offline), or nothing yet (connecting). */
export const LINK = { live: 'live', down: 'down', unknown: 'unknown' } as const;
export type Link = (typeof LINK)[keyof typeof LINK];

/** Whether the machine's runtime answers now: its socket is live, or, before the socket says, the hub lists it on and
 *  not hanging. */
export const answersNow = (view: MachineView, link: Link): boolean => {
  if (view.machine.reach === 'unreachable' || view.runtime === 'outdated') return false;
  if (link === LINK.live) return true;
  if (link === LINK.down) return false;
  return view.machine.reach === 'online' && view.hangs !== true;
};

export const startCandidateOf = (view: MachineView, link: Link, off: boolean): IStartCandidate => ({
  id: view.machine.id,
  off,
  answers: !off && answersNow(view, link),
  providers: view.machine.agents.filter((a) => a.status === 'ready').map((a) => a.provider),
  mine: view.machine.mine !== false,
});

/**
 * The machine a new chat starts on: the one picked in the pill, unless it is off; else the first that answers now and
 * has the agent of the last choice, else the first that answers and has an agent; with none answering, the first whose
 * agents are known, so the pill is drawn from them. A machine the person only administers is taken only on top.
 */
export const startMachineOf = (
  candidates: readonly IStartCandidate[],
  picked: MachineId | null,
  provider: Provider | null,
): MachineId | null => {
  const chosen = candidates.find((c) => c.id === picked && !c.off);
  if (chosen) return chosen.id;
  const top = candidates[0]?.id;
  const allowed = candidates.filter((c) => c.id === top || c.mine);
  const answering = allowed.filter((c) => c.answers && c.providers.length > 0);
  const tiers = [
    answering.find((c) => provider !== null && c.providers.includes(provider)),
    answering[0],
    allowed.find((c) => c.answers),
    allowed.find((c) => !c.off && c.providers.length > 0),
    allowed.find((c) => c.providers.length > 0),
    allowed.find((c) => !c.off),
    candidates[0],
  ];
  return tiers.find((c) => c !== undefined)?.id ?? null;
};
