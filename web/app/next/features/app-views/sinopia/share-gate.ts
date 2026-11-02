// The share gate of a document: before it is shared or published, the checks that run on `share` are asked; the ones
// whose `blockShare` level a finding reaches keep it from going out. The runtime says `blocked` (it holds the levels).
import type { ISinopiaChecked } from '@/next/features/app-views/api';
import type { ISinopiaCheckResult } from '@/next/features/app-views/sinopia/types';

export type TGate =
  { ok: true } | { ok: false; results: ISinopiaCheckResult[] } | { ok: false; error: string };

/**
 * Asks the checks and lets `proceed` run only when none blocks. A check that cannot run is not a block (the person is
 * told by the result's `failed`), but the runtime not answering is: nothing is shared on a guess.
 */
export const gateShare = async (
  check: () => Promise<ISinopiaChecked>,
  proceed: () => void,
): Promise<TGate> => {
  let checked: ISinopiaChecked;
  try {
    checked = await check();
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'check failed' };
  }
  if (checked.blocked) return { ok: false, results: checked.results.filter((r) => r.blocks) };
  proceed();
  return { ok: true };
};
