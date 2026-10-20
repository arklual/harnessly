import { HANDOFF_QUESTION } from '@/lib/hl-handoff';
import { streamSideChat } from '@/lib/hl-side-chat-client';
import { t } from '@/lib/i18n';

export type THandoffNote = { ok: true; note: string } | { ok: false; message: string };

/** Asks the agent of a conversation, on the machine it lives on, for its handoff note. */
export const requestHandoffNote = async (
  tabId: string,
  machineId: string | undefined,
  signal: AbortSignal,
): Promise<THandoffNote> => {
  let result: THandoffNote = { ok: false, message: t('lib.hlHandoffClient.noAnswer') };
  await streamSideChat(
    { tabId, question: HANDOFF_QUESTION, excerpts: [], history: [] },
    signal,
    (event) => {
      if (event.type === 'done')
        result = event.text.trim()
          ? { ok: true, note: event.text }
          : { ok: false, message: t('lib.hlHandoffClient.emptyNote') };
      else if (event.type === 'error') result = { ok: false, message: event.message };
    },
    machineId,
  );
  return result;
};
