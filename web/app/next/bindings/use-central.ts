// The two hooks that read the cluster's copy of the chats: the list (mounted once in the shell) and one open
// chat of a machine that is switched off (mounted in the chat view).
import { useEffect } from 'react';
import { batch } from '@/next/core/store';
import { pageClock } from '@/next/core/clock';
import { applyConvSnapshot } from '@/next/stores/conversation';
import { isMachineOff, loadCentralChat, refreshCentral, useCentral } from '@/next/stores/central';
import { useConnection } from '@/next/stores/connection';
import { refreshProfileSidebar } from '@/next/stores/profile-sidebar';
import type { ConvId, MachineId, Queue } from '@/next/protocol/v2';

const REFRESH_MS = 30_000;

/** The list and the person's sidebar: asked on mount, every half minute while the page is visible, and when a link goes down or comes back. */
export function useCentralHistory(links: string): void {
  useEffect(() => {
    // The person's sidebar (projects, favorites) comes with the list, on the same beat.
    const refresh = () => Promise.all([refreshCentral(), refreshProfileSidebar()]);
    void refresh();
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') void refresh();
    }, REFRESH_MS);
    return () => clearInterval(timer);
  }, [links]);
}

const emptyQueue = (conv: ConvId): Queue => ({ conversationId: conv, items: [], paused: null, rev: 0 });

/** Whether the chat on this machine is read from the copy (the machine is off), and fetches it once it is. */
export function useCentralFeed(machine: MachineId, conv: ConvId): boolean {
  const central = useCentral();
  const connection = useConnection(machine);
  const off = central.enabled && central.off.has(machine) && connection?.k !== 'live';
  useEffect(() => {
    if (!off || !isMachineOff(machine)) return;
    let live = true;
    void loadCentralChat(conv).then((res) => {
      if (!live || !('ok' in res)) return;
      const { turns, entries, beforeTurnId } = res.value;
      batch(() =>
        applyConvSnapshot(
          conv,
          machine,
          'replace',
          {
            turns,
            entries,
            beforeTurnId,
            queue: emptyQueue(conv),
            messages: [],
            questions: [],
            permissions: [],
            background: [],
            now: null,
          },
          pageClock().now(),
        ),
      );
    });
    return () => {
      live = false;
    };
  }, [off, machine, conv]);
  return off;
}
