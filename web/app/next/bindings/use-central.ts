// The two hooks that read the cluster's copy of the chats: the list (mounted once in the shell) and one open
// chat's feed, whatever its machine does (mounted in the chat view).
import { useEffect } from 'react';
import { adoptParkedMessages } from '@/next/commands/messages';
import { batch } from '@/next/core/store';
import { pageClock } from '@/next/core/clock';
import { applyConvSnapshot, feedStore, importCentralFeed } from '@/next/stores/conversation';
import { forgetResume } from '@/next/core/realtime/cursors';
import {
  isMachineOff,
  keepsHistory,
  loadCentralChat,
  refreshCentral,
  useCentral,
} from '@/next/stores/central';
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

/**
 * The feed of a chat from the cluster's copy (spec v10: reading goes through the hub). It is asked as soon as
 * the chat opens whenever the copy keeps the machine's chats, not only once the cluster calls the machine off: a
 * machine that is starting, hung or only just gone (the cluster names it offline 45 s later) used to hold the chat on a
 * skeleton and then «no answer». The machine's own snapshot, when it comes, has the last word; with the machine off the
 * copy is the whole feed and nothing runs. Returns whether the machine is off.
 */
export function useCentralFeed(machine: MachineId, conv: ConvId): boolean {
  const central = useCentral();
  const connection = useConnection(machine);
  const kept = central.enabled && central.on.has(machine);
  const off = kept && central.off.has(machine) && connection?.k !== 'live';
  useEffect(() => {
    if (!kept || !keepsHistory(machine)) return;
    let live = true;
    void loadCentralChat(conv).then((res) => {
      if (!live || !('ok' in res)) return;
      const { turns, entries, beforeTurnId } = res.value;
      batch(() => {
        // A machine that is off: the copy is the chat, stopped, with nothing queued on the machine.
        if (isMachineOff(machine)) {
          // GitHub #35. The copy may not have the chat yet (a new one, a machine the cluster still names offline while
          // the link only stalled): it does not wipe what the machine itself drew.
          if (turns.length === 0 && entries.length === 0 && feedStore.get(conv)?.loaded) {
            void adoptParkedMessages(machine, conv);
            return;
          }
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
          );
          // The feed is the copy's now, not the machine's: the machine's next snapshot is a whole one, not the frames
          // after a cursor of a feed that is no longer here (and that the chat cache would keep across F5).
          forgetResume(machine, `conv:${conv}`);
          // What waits in the cluster for this machine (written on another device, or before a tab was closed).
          void adoptParkedMessages(machine, conv);
          return;
        }
        importCentralFeed(conv, machine, { turns, entries, beforeTurnId });
      });
    });
    return () => {
      live = false;
    };
  }, [kept, off, machine, conv]);
  return off;
}
