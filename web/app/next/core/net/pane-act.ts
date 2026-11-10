// The machine's door for the agent's actions on the pane's browser (HRN-678): the page waits for the next action of a
// chat (a long poll the machine holds up to 10 s) and says how it ended.
import { runtimePath } from '@/next/core/net/machine';
import type { ConvId, MachineId } from '@/next/protocol/v2';

type TPath = Parameters<typeof runtimePath>[0];

export const PANE_ACT_STATE = { done: 'done', failed: 'failed', asking: 'asking' } as const;
export type TPaneActState = (typeof PANE_ACT_STATE)[keyof typeof PANE_ACT_STATE];

/** A refusal of the poll that is not worth asking again soon (an older machine has no such door). */
export class PaneActClosed extends Error {}

/** The next action the agent asked for: its JSON, or null when none came while the machine held the request. */
export const pollPaneAct = async (
  machine: MachineId,
  conv: ConvId,
  signal: AbortSignal,
): Promise<unknown> => {
  const res = await fetch(
    runtimePath(`/api/v2/pane-act?conversation=${encodeURIComponent(conv)}` as TPath, machine),
    { credentials: 'same-origin', signal, cache: 'no-store' },
  );
  // A gateway that gave up before the machine answered is a poll with nothing in it: the next one goes at once.
  if (res.status === 204 || res.status === 504) return null;
  if (res.status === 404 || res.status === 405) throw new PaneActClosed(String(res.status));
  if (!res.ok) throw new Error(String(res.status));
  return res.json();
};

/** Says how an action ended, or that the person is being asked (the machine then waits longer). */
export const answerPaneAct = (
  machine: MachineId,
  conv: ConvId,
  id: string,
  state: TPaneActState,
  text = '',
): Promise<void> =>
  fetch(runtimePath('/api/v2/pane-act' as TPath, machine), {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ conversation: conv, id, state, text }),
  }).then(
    () => undefined,
    () => undefined,
  );
