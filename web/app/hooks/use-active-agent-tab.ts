import { useMemo } from 'react';
import { useLayoutStore, collectPanes } from '@/hooks/use-layout';
import type { ITab } from '@/types/terminal';

/** The tab currently shown in the focused pane of the loaded workspace layout. */
const useActiveAgentTab = (): ITab | null => {
  const layout = useLayoutStore((s) => s.layout);
  return useMemo(() => {
    if (!layout) return null;
    const panes = collectPanes(layout.root);
    const pane = panes.find((p) => p.id === layout.activePaneId) ?? panes[0];
    return pane?.tabs.find((t) => t.id === pane.activeTabId) ?? null;
  }, [layout]);
};

export default useActiveAgentTab;
