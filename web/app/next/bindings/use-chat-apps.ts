// The apps on in one chat, and the person's switch for them. Pessimistic: the list on screen is the chat's record from
// the machine, so a switch changes nothing until the machine has taken it; a refusal is told in words and the row stays
// as it was.
import { useCallback, useState } from 'react';
import { settled } from '@/next/bindings/outcome';
import { setConversationApps } from '@/next/commands';
import { withApp } from '@/next/features/app-main/chat-apps';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { useConversation } from '@/next/stores/conversations';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

const NONE: readonly string[] = [];

export interface IChatApps {
  /** The ids on in the chat now (the machine's word). */
  on: readonly string[];
  /** The app whose change is on its way to the machine; one at a time. */
  busy: string | null;
  turn: (id: string, on: boolean) => Promise<void>;
}

export const useChatApps = (machine: MachineId, conv: ConvId | null): IChatApps => {
  const on = useConversation(conv)?.apps ?? NONE;
  const [busy, setBusy] = useState<string | null>(null);
  const turn = useCallback(
    async (id: string, next: boolean) => {
      if (busy !== null || conv === null) return;
      setBusy(id);
      try {
        await settled(setConversationApps(machine, conv, withApp(on, id, next)));
      } catch (e) {
        toast(t('next.chatapps.failed'), {
          tone: 'error',
          description: e instanceof Error ? e.message : undefined,
        });
      } finally {
        setBusy(null);
      }
    },
    [machine, conv, on, busy],
  );
  return { on, busy, turn };
};
