// A notice over the field, one line (the design's .hx-dockbar): the machine of the chat stopped answering (the turn
// waits for it), or the subscription's limit ran out (switch the model, use an API key, or wait). On a phone it is a
// single line and opens its details and actions on a tap.
import { useState } from 'react';
import type { DockNotice } from '@/next/features/composer/types';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import Spinner from '@/next/ui/spinner';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import { clock } from '@/next/i18n/format';

export const DOCK_BAR_EXIT_MS = 240;

interface IAction {
  key: string;
  label: string;
  variant?: 'ghost';
  run: () => Promise<unknown>;
}

const DockBar = ({ notice }: { notice: DockNotice | null }) => {
  const { value, state } = usePresence(notice, DOCK_BAR_EXIT_MS);
  const [open, setOpen] = useState(false);
  const action = usePending<string>();
  if (!value) return null;
  const busy = isBusy(action.phase);
  let icon: 'server' | 'gauge' | 'key-round' = 'server';
  let title = '';
  let sub: string | null = null;
  let quiet = false;
  let actions: IAction[] = [];
  if (value.kind === 'machine') {
    title = t('next.dockBar.machine', { machine: value.machine });
    sub = t('next.dockBar.machineSub');
    actions = [{ key: 'stop', label: t('next.dockBar.stop'), variant: 'ghost', run: value.onStop }];
  } else if (value.kind === 'key') {
    icon = 'key-round';
    title = t('next.dockBar.keyGone', { vendor: value.vendor });
    const { onOpen, onContinue } = value;
    actions = [{ key: 'open', label: t('next.dockBar.keyOpen'), run: async () => onOpen() }];
    if (onContinue)
      actions.push({
        key: 'continue',
        label: t('next.rowMenu.continue'),
        variant: 'ghost',
        run: async () => onContinue(),
      });
  } else {
    icon = 'gauge';
    title = t('next.dockBar.limit', {
      provider: t(`next.agent.${value.provider}`),
      until: clock(value.until),
    });
    quiet = value.waiting;
    if (value.waiting) sub = t('next.dockBar.limitWaiting');
    else {
      if (value.switchTo)
        actions.push({
          key: 'switch',
          label: t('next.dockBar.switch', { model: value.switchTo }),
          run: value.onSwitch,
        });
      if (value.onUseKey) actions.push({ key: 'key', label: t('next.dockBar.useKey'), run: value.onUseKey });
      const onWait = value.onWait;
      actions.push({
        key: 'wait',
        label: t('next.dockBar.wait'),
        variant: 'ghost',
        run: async () => onWait(),
      });
    }
  }
  if (action.phase === PENDING.failed) sub = t('next.dockBar.actionFailed');
  return (
    <div
      className="hx-dockbar"
      data-tone={quiet ? 'quiet' : 'warn'}
      data-open={String(open)}
      data-state={state}
      role="status"
    >
      <button
        type="button"
        className="hx-dockbar__line"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <Icon name={icon} size="sm" className="hx-dockbar__ic" />
        <span className="hx-dockbar__text">
          <span className="hx-dockbar__title">{title}</span>
          {sub && <span className="hx-dockbar__sub">{sub}</span>}
        </span>
        <Icon name="chevron-up" size="sm" className="hx-dockbar__chev" />
      </button>
      {actions.length > 0 && (
        <span className="hx-dockbar__acts">
          {actions.map((item) => (
            <button
              key={item.key}
              type="button"
              className="hx-btn"
              data-size="sm"
              data-variant={item.variant}
              data-busy={(busy && action.key === item.key) || undefined}
              disabled={busy}
              onClick={() => action.run(item.key, item.run)}
            >
              <span>{item.label}</span>
              <Spinner />
            </button>
          ))}
        </span>
      )}
    </div>
  );
};

export default DockBar;
