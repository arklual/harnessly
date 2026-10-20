import { useState } from 'react';
import type { ITab } from '@/types/terminal';

type TAgentTabIdentity = Pick<ITab, 'id' | 'sessionName' | 'panelType'>;

export const retainAgentTabs = <T extends TAgentTabIdentity>(
  previous: T[],
  tabs: T[],
  activeTabId: string | null,
): T[] => {
  const agents = tabs.filter((tab) => tab.panelType === 'claude-code' || tab.panelType === 'codex-cli');
  const retained = previous.flatMap((cached) => {
    const current = agents.find(
      (tab) =>
        tab.id === cached.id && tab.sessionName === cached.sessionName && tab.panelType === cached.panelType,
    );
    return current && current.id !== activeTabId ? [current] : [];
  });
  const active = agents.find((tab) => tab.id === activeTabId);
  if (active) retained.push(active);
  return retained.slice(-3);
};

export const useRetainedAgentTabs = (tabs: ITab[], activeTabId: string | null): ITab[] => {
  const [retained, setRetained] = useState<ITab[]>([]);
  const next = retainAgentTabs(retained, tabs, activeTabId);
  if (next.length !== retained.length || next.some((tab, index) => tab !== retained[index]))
    setRetained(next);
  return next;
};
