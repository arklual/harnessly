import { useEffect, useEffectEvent } from 'react';
import { appFetch, currentMachineId } from '@/lib/app-path';
import { readMachineLoad } from '@/lib/hl-machine-load-store';
import { autoOpenAllowed, effectiveLevel, metricText } from '@/lib/hl-machine-load-view';
import { createPaneBus } from '@/lib/hl-pane-bus';
import {
  PANEL_COMMAND_EVENT,
  parsePanelCommand,
  type IPanelCommandEvent,
  type IPanelResult,
} from '@/lib/hl-panel-command';
import { runPanelCommand } from '@/lib/hl-panel-run';
import { t } from '@/lib/i18n';

const ID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

const isCommandEvent = (value: unknown): value is IPanelCommandEvent =>
  typeof value === 'object' &&
  value !== null &&
  typeof (value as IPanelCommandEvent).id === 'string' &&
  typeof (value as IPanelCommandEvent).tabId === 'string';

const overloadReason = (): string | null => {
  const snapshot = readMachineLoad(currentMachineId() ?? '');
  if (autoOpenAllowed(effectiveLevel(snapshot))) return null;
  return snapshot.load?.reason
    ? metricText(snapshot.load.reason, snapshot.load)
    : t('hooks.useAgentPanelCommands.highLoad');
};

const report = async (id: string, result: IPanelResult): Promise<void> => {
  try {
    await appFetch('/api/platform/panel-result', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ id, ...result }),
    });
  } catch {
    // The agent hears about a lost answer as a timeout.
  }
};

/**
 * Runs the commands an agent sends to the right pane of its tab. Only the page that shows the tab answers: another
 * tab's commands are not its business, and the agent's wait ends in a timeout if no page shows it. The pane itself is
 * driven through window events (`hl-pane-bus`), so this hook knows nothing of how the pane is built.
 */
const useAgentPanelCommands = (tabId: string | null): void => {
  const handle = useEffectEvent(async (event: IPanelCommandEvent) => {
    if (event.tabId !== tabId || !ID_RE.test(event.id)) return;
    const parsed = parsePanelCommand(event.command);
    if (!parsed.ok) {
      await report(event.id, { ok: false, message: parsed.error });
      return;
    }
    let result: IPanelResult;
    try {
      result = await runPanelCommand(parsed.command, createPaneBus(window, { overload: overloadReason }));
    } catch {
      result = { ok: false, message: t('hooks.useAgentPanelCommands.uiError') };
    }
    await report(event.id, result);
  });

  useEffect(() => {
    if (!tabId) return;
    const onCommand = (event: Event) => {
      const detail = (event as CustomEvent<unknown>).detail;
      if (isCommandEvent(detail)) void handle(detail);
    };
    window.addEventListener(PANEL_COMMAND_EVENT, onCommand);
    return () => window.removeEventListener(PANEL_COMMAND_EVENT, onCommand);
  }, [tabId]);
};

export default useAgentPanelCommands;
