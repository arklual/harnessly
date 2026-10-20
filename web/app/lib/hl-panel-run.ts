import type { IPanelResult, IPanelState, TPaneTabKind, TPanelCommand } from '@/lib/hl-panel-command';
import { t } from '@/lib/i18n';

/** What a command of the agent does to the pane, over the four things the page lets it ask the pane. */

export interface IPanelBus {
  open: (tab: TPaneTabKind, target?: string) => Promise<IPanelResult>;
  setVisible: (visible: boolean) => Promise<IPanelResult>;
  /** null when the pane does not report. */
  state: () => Promise<IPanelState | null>;
  /** Why a dev server must not be opened now (the machine is overloaded), or null. */
  overload: () => string | null;
}

const describeState = ({ visible, open, active }: IPanelState): string => {
  const shown = visible === false ? t('lib.hlPanelRun.hidden') : t('lib.hlPanelRun.shown');
  const tabs = open.length ? t('lib.hlPanelRun.tabs', { tabs: open.join(', ') }) : t('lib.hlPanelRun.noTabs');
  const head = t('lib.hlPanelRun.state', { shown, tabs });
  return active ? t('lib.hlPanelRun.stateActive', { state: head, active }) : head;
};

const runOpen = async (
  tab: TPaneTabKind,
  target: string | undefined,
  bus: IPanelBus,
): Promise<IPanelResult> => {
  const overload = tab === 'browser' && target ? bus.overload() : null;
  if (overload)
    return {
      ok: false,
      message: t('lib.hlPanelRun.overloaded', { overload }),
    };
  return bus.open(tab, target);
};

export const runPanelCommand = async (command: TPanelCommand, bus: IPanelBus): Promise<IPanelResult> => {
  switch (command.kind) {
    case 'open':
      return runOpen(command.tab, command.target, bus);
    case 'show':
      return bus.setVisible(true);
    case 'hide':
      return bus.setVisible(false);
    case 'status': {
      const state = await bus.state();
      return state
        ? { ok: true, message: describeState(state), state }
        : { ok: true, message: t('lib.hlPanelRun.noState') };
    }
  }
};
