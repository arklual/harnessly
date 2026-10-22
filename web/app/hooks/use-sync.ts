// The runtime's sync socket (/api/sync): refetch the workspaces or a layout when another page or the CLI changed them.
import { useEffect } from 'react';
import { ReconnectingSocket } from '@/runtime/socket';
import { homeSocketUrl } from '@/runtime/base';
import useWorkspaceStore from '@/hooks/use-workspace-store';
import { useLayoutStore } from '@/hooks/use-layout';
import useAgentStatus from '@/hooks/use-agent-status';

export default function useSync(): void {
  useAgentStatus();
  useEffect(() => {
    const refreshLayout = () => {
      const id = useLayoutStore.getState().workspaceId;
      if (id) void useLayoutStore.getState().fetchLayout(id, true);
    };
    const sync = new ReconnectingSocket({
      url: () => homeSocketUrl('/api/sync'),
      onOpen: () => {
        void useWorkspaceStore
          .getState()
          .fetchWorkspaces()
          .catch(() => {});
        refreshLayout();
      },
      onMessage: (raw) => {
        const data = String(raw);
        let msg: { type?: string; workspaceId?: string };
        try {
          msg = JSON.parse(data) as typeof msg;
        } catch {
          return;
        }
        if (msg.type === 'workspace')
          void useWorkspaceStore
            .getState()
            .fetchWorkspaces()
            .catch(() => {});
        else if (msg.type === 'layout' && msg.workspaceId === useLayoutStore.getState().workspaceId)
          refreshLayout();
      },
    });
    return () => sync.close();
  }, []);
}
