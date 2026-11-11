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

/** 0:42, 12:05: the length of a recording. */
const duration = (ms: number) => {
  const seconds = Math.max(1, Math.round(ms / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
};

interface IAction {
  key: string;
  label: string;
  variant?: 'ghost';
  run: () => Promise<unknown>;
  /** Busy by the notice's own state (a recognition that outlasts the press), not by the press's promise. */
  busy?: boolean;
}

const DockBar = ({ notice }: { notice: DockNotice | null }) => {
  const { value, state } = usePresence(notice, DOCK_BAR_EXIT_MS);
  const [open, setOpen] = useState(false);
  const action = usePending<string>();
  if (!value) return null;
  const busy = isBusy(action.phase);
  let icon: 'server' | 'gauge' | 'key-round' | 'mic' = 'server';
  let title = '';
  let sub: string | null = null;
  let quiet = false;
  /** The notice runs something of its own: every action waits for it. */
  let held = false;
  let actions: IAction[] = [];
  if (value.kind === 'move') {
    title = value.line;
    sub = value.codes || null;
    const onAgent = value.onAgent;
    actions = [
      { key: 'retry', label: t('next.move.retry'), run: value.onRetry },
      { key: 'agent', label: t('next.move.agent'), variant: 'ghost', run: async () => onAgent() },
    ];
  } else if (value.kind === 'off') {
    title = t('next.dockBar.off', { machine: value.machine });
    sub = t('next.dockBar.offSub');
    quiet = true;
  } else if (value.kind === 'noMachines') {
    title = t('next.dockBar.noMachines');
    sub = t('next.dockBar.noMachinesSub');
  } else if (value.kind === 'signIn') {
    icon = 'key-round';
    title = t('next.dockBar.signIn');
    sub = t('next.dockBar.signInSub');
    const onOpen = value.onOpen;
    actions = [{ key: 'open', label: t('next.dockBar.signInOpen'), run: async () => onOpen() }];
  } else if (value.kind === 'machine') {
    title = t('next.dockBar.machine', { machine: value.machine });
    sub = t('next.dockBar.machineSub');
    actions = [{ key: 'stop', label: t('next.dockBar.stop'), variant: 'ghost', run: value.onStop }];
  } else if (value.kind === 'voice') {
    icon = 'mic';
    held = value.busy;
    const drop: IAction = {
      key: 'drop',
      label: t('next.dockBar.voiceDrop'),
      variant: 'ghost',
      run: value.onDrop,
    };
    if (value.waiting && !value.busy) {
      // Nothing to press but «Удалить»: the take is recognized by itself when the machine is back.
      title = t('next.dockBar.voiceKept', { length: duration(value.ms) });
      sub = t('next.dockBar.voiceWaiting');
      quiet = true;
      actions = [drop];
    } else {
      title = t('next.dockBar.voice', { length: duration(value.ms) });
      if (value.busy) sub = t('next.voice.recognizing');
      else if (value.failure) sub = value.failure;
      else if (value.more > 0) sub = t('next.dockBar.voiceMore', { n: value.more });
      const onRetry = value.onRetry;
      actions = [
        { key: 'retry', label: t('next.dockBar.voiceRetry'), busy: value.busy, run: async () => onRetry() },
        drop,
      ];
    }
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
              data-busy={(busy && action.key === item.key) || item.busy || undefined}
              disabled={busy || held}
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
