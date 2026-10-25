// The limits of a machine from the platform route the current interface reads (`…/machines`): the runtime answers it
// to the page's session, for itself, and a gateway for the person's machines.
import { platformPath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
import { readMachineLimits, type MachineLimits } from '@/next/features/limits/model';
import type { MachineId } from '@/next/protocol/v2';

/** Null when the answer does not list this machine's limits at all. Throws when the machine did not answer. */
export const loadMachineLimits = async (m: MachineId): Promise<MachineLimits | null> =>
  readMachineLimits(await runtimeJson<unknown>(m, platformPath('/machines')), m);
