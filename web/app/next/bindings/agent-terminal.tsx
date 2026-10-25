// The agent's terminal of the open chat: the header's button and the panel under the stage. It is there while the
// chat's agent runs on a machine whose runtime serves terminals; ⌃` opens and folds it.
import { useEffect, useState } from 'react';
import AgentTerminal from '@/next/features/terminal/agent-terminal';
import { uuidv7 } from '@/next/core/ids';
import { machineApi } from '@/next/core/net/machine';
import { hasFeature, useMachines } from '@/next/stores/machines';
import type { Conversation } from '@/next/protocol/v2';

/** One id per page: the runtime keeps one socket per client and terminal, a reopened panel replaces its own. */
export const CLIENT_ID = uuidv7();

export const useAgentTerminal = (conv: Conversation | null) => {
  const [open, setOpen] = useState(false);
  useMachines();
  const available = !!conv && conv.state.k !== 'stopped' && hasFeature(conv.machineId, 'terminal');
  const shown = available && open;

  useEffect(() => {
    if (!available) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.ctrlKey && !event.metaKey && !event.altKey && event.key === '`') {
        event.preventDefault();
        setOpen((current) => !current);
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [available]);

  const header = available ? { open: shown, onToggle: () => setOpen((current) => !current) } : null;
  const panel = conv && available && (
    <AgentTerminal
      key={`term:${conv.id}`}
      open={shown}
      url={machineApi(conv.machineId).ws(
        `/terminal?conversation=${encodeURIComponent(conv.id)}&clientId=${CLIENT_ID}`,
      )}
      cwd={conv.cwd}
      title={conv.title}
      provider={conv.provider}
      onClose={() => setOpen(false)}
    />
  );
  /** Opens the panel; null while the chat has no terminal. */
  const reveal = available ? () => setOpen(true) : null;
  return { header, panel, reveal };
};
