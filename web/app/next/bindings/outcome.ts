// What a command's end means to the person (spec §12): every outcome is a promise that resolves on success and
// rejects with words — the refusal by its code, a lost answer by its kind — so the controls that wait for it
// (use-pending) show the reason and offer «Повторить».
import type { OpHandle } from '@/next/commands';
import { t } from '@/next/i18n';
import type { PageOutcome } from '@/next/core/net/http';
import type { OpFailure } from '@/next/stores/ops';

type PageFailure = Exclude<PageOutcome<unknown>, { ok: true }>;

export const failureText = (f: OpFailure): string =>
  f.r === 'refused' ? t(`next.refuse.${f.code}`) : t(`next.failure.${f.code}`);

/** Resolves when the command is done on the machine; rejects with the reason otherwise. */
export async function settled(handle: OpHandle | null): Promise<void> {
  if (!handle) return;
  const st = await handle.done;
  if (st.k === 'failed') throw new Error(failureText(st.failure));
}

/** The words for a page that did not load. */
export const pageText = (res: PageFailure): string =>
  res.r === 'refused' ? t(`next.refuse.${res.code}`) : t(`next.failure.${res.code}`);
