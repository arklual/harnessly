import type { ITab } from '@/types/terminal';

interface ISessionEndedInput {
  tabs: ITab[];
  activeTabId: string | null;
  sessionName: string;
  paneCount: number;
}

type TSessionEndedPlan =
  | { type: 'ignore' }
  | { type: 'close-pane' }
  | { type: 'delete-tab'; tabId: string; switchToTabId: string | null };

export const planSessionEnded = ({
  tabs,
  activeTabId,
  sessionName,
  paneCount,
}: ISessionEndedInput): TSessionEndedPlan => {
  const endedTab = tabs.find((t) => t.sessionName === sessionName);
  if (!endedTab) return { type: 'ignore' };

  if (tabs.length === 1 && paneCount > 1) return { type: 'close-pane' };

  if (endedTab.id !== activeTabId) {
    return { type: 'delete-tab', tabId: endedTab.id, switchToTabId: null };
  }

  const sorted = [...tabs].sort((a, b) => a.order - b.order);
  const idx = sorted.findIndex((t) => t.id === endedTab.id);
  const adjacent = sorted[idx + 1] || sorted[idx - 1];
  return { type: 'delete-tab', tabId: endedTab.id, switchToTabId: adjacent?.id ?? null };
};
