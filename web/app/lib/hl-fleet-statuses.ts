import type { TProviderStatus } from '@/components/agents/provider-icon';
import { noticeDetailOf, type INoticeDetail, type INoticeSource } from '@/lib/hl-notices';
import type { ILastEvent, TStatusServerMessage } from '@/types/status';
import type { TCliState } from '@/types/timeline';

export interface IRemoteTab extends INoticeSource {
  cliState: TCliState | null;
  lastEvent?: ILastEvent | null;
  /** Set when a finished turn was opened; cleared by the next prompt. */
  dismissedAt?: number | null;
  agentSessionId?: string | null;
}

export type TRemoteTabs = Readonly<Record<string, IRemoteTab>>;

/**
 * Sidebar status of an agent tab, the same rule for the shell's own machine and the others.
 * A finished turn stays done after it was seen (a stop, or a dismissal), until the next prompt; a failed one stays failed.
 * An agent that exited or was killed shows as gone once it has run at all: it has a hook event or a session.
 */
export const providerStatusOf = (tab: IRemoteTab | undefined): TProviderStatus => {
  const last = tab?.lastEvent?.name;
  switch (tab?.cliState) {
    // `unknown` is `busy` restored after a runtime restart; resolveUnknown turns it idle or ready unless the agent still works.
    case 'busy':
    case 'unknown':
      return 'working';
    case 'needs-input':
      return 'needs-input';
    case 'ready-for-review':
      return last === 'stop-failure' ? 'failed' : 'ready';
    case 'idle':
      if (last === 'stop-failure') return 'failed';
      return last === 'stop' || (tab.dismissedAt && last !== 'interrupt' && last !== 'prompt-submit')
        ? 'done'
        : 'idle';
    default:
      return last || tab?.agentSessionId ? 'gone' : 'idle';
  }
};

/** Tabs of one machine after a message of its status socket; unrelated messages return the same object. */
export const applyStatusMessage = (tabs: TRemoteTabs, msg: TStatusServerMessage): TRemoteTabs => {
  switch (msg.type) {
    case 'status:sync':
      return Object.fromEntries(
        Object.entries(msg.tabs).map(([id, tab]) => [
          id,
          {
            cliState: tab.cliState,
            lastEvent: tab.lastEvent ?? null,
            dismissedAt: tab.dismissedAt ?? null,
            agentSessionId: tab.agentSessionId ?? null,
            lastAssistantMessage: tab.lastAssistantMessage ?? null,
            permissionRequest: tab.permissionRequest ?? null,
            currentAction: tab.currentAction ?? null,
            readyForReviewAt: tab.readyForReviewAt ?? null,
          },
        ]),
      );
    case 'status:update':
      return {
        ...tabs,
        [msg.tabId]: {
          cliState: msg.cliState,
          lastEvent: msg.lastEvent ?? tabs[msg.tabId]?.lastEvent ?? null,
          dismissedAt: msg.dismissedAt ?? null,
          agentSessionId: msg.agentSessionId ?? tabs[msg.tabId]?.agentSessionId ?? null,
          lastAssistantMessage: msg.lastAssistantMessage ?? tabs[msg.tabId]?.lastAssistantMessage ?? null,
          permissionRequest: msg.permissionRequest ?? null,
          currentAction: msg.currentAction ?? null,
          readyForReviewAt: msg.readyForReviewAt ?? null,
        },
      };
    case 'status:hook-event':
      return tabs[msg.tabId] ? { ...tabs, [msg.tabId]: { ...tabs[msg.tabId], lastEvent: msg.event } } : tabs;
    default:
      return tabs;
  }
};

export const providerStatuses = (tabs: TRemoteTabs): Record<string, TProviderStatus> =>
  Object.fromEntries(Object.entries(tabs).map(([id, tab]) => [id, providerStatusOf(tab)]));

/** What the bell shows for each tab of one machine: the question asked, the last answer, whether it was opened. */
export const noticeDetails = (tabs: TRemoteTabs): Record<string, INoticeDetail> =>
  Object.fromEntries(Object.entries(tabs).map(([id, tab]) => [id, noticeDetailOf(tab)]));
