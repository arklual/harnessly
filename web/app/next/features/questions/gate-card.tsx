// A permission or a plan the agent waits on, in the feed where it asked (the design's .hx-gate): the command and where
// it runs, the options read off the agent's screen with digits 1/2/3, and for a plan its text, how to work by it
// (ask before edits or edit without asking), «Revise» and «Start». The chosen button spins until the agent takes it;
// a failure says so and keeps the card. Once decided it shrinks to a line.
import { useEffect, useState } from 'react';
import Md from '@/next/features/chat/md';
import type { GateCloseReason, Permission, PermissionScope } from '@/next/features/questions/types';
import { permTitleKey } from '@/next/features/questions/gate-title';
import useDigitKeys from '@/next/features/questions/use-digit-keys';
import Icon, { type TIconName } from '@/next/ui/icon';
import Segmented from '@/next/ui/segmented';
import Spinner from '@/next/ui/spinner';
import { toast } from '@/next/ui/toast';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import '@/next/styles/ui.css';
import '@/next/styles/questions.css';

/** A shell summary starts with «$ »; the card draws its own prompt. */
const shellLine = (command: string) => command.replace(/^\$\s+/, '');

const clock = (secs: number) => `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}`;

/** Claude's own countdown to denying the request on an unattended session, ticking down off the moment it said. */
const AutoDeny = ({ at }: { at: number }) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const left = Math.ceil((at - now) / 1000);
  if (left <= 0) return null;
  return (
    <div className="hx-gate__autodeny" role="timer">
      <Icon name="clock" size="sm" />
      <span>{t('next.gate.autoDeny', { time: clock(left) })}</span>
    </div>
  );
};

const CLOSE_KEYS: Record<GateCloseReason, string> = {
  'agent-moved-on': 'next.gate.closedMovedOn',
  'chat-about': 'next.gate.closedChat',
  'turn-ended': 'next.gate.closedTurnEnded',
  'agent-gone': 'next.gate.closedGone',
};

const DONE_VIEW: Record<PermissionScope, { icon: TIconName; tone: 'ok' | 'no'; key: string }> = {
  once: { icon: 'circle-check', tone: 'ok', key: 'next.gate.doneOnce' },
  always: { icon: 'circle-check', tone: 'ok', key: 'next.gate.doneAlways' },
  deny: { icon: 'ban', tone: 'no', key: 'next.gate.doneDeny' },
  'plan-auto': { icon: 'circle-check', tone: 'ok', key: 'next.gate.donePlanAuto' },
  'plan-ask': { icon: 'circle-check', tone: 'ok', key: 'next.gate.donePlanAsk' },
  revise: { icon: 'pencil', tone: 'ok', key: 'next.gate.doneRevise' },
};

interface IGateCardProps {
  permission: Permission;
  device: string;
  /** What was chosen, when the page knows it (its own answer). */
  decided: PermissionScope | null;
  onDecide: (permissionId: string, optionId: string) => Promise<unknown>;
  /** Opens the chat's terminal on the agent's pane; null while the chat has none. */
  onOpenTerminal?: (() => void) | null;
}

/** The first option leads (allow once), a refusal is quiet. */
const toolVariant = (index: number, scope: PermissionScope): 'primary' | 'ghost' | undefined => {
  if (index === 0) return 'primary';
  if (scope === 'deny') return 'ghost';
  return undefined;
};

type TPlanMode = 'plan-ask' | 'plan-auto';

const Done = ({ permission, device, decided }: Omit<IGateCardProps, 'onDecide' | 'onOpenTerminal'>) => {
  const { state } = permission;
  if (state.k === 'closed') {
    return (
      <div className="hx-gate__done">
        <Icon name="info" size="sm" />
        <span>{t(CLOSE_KEYS[state.reason])}</span>
      </div>
    );
  }
  if (state.k !== 'answered') return null;
  const elsewhere = state.byDevice !== device;
  if (decided && !elsewhere) {
    const view = DONE_VIEW[decided];
    return (
      <div className="hx-gate__done" data-tone={view.tone}>
        <Icon name={view.icon} size="sm" />
        <span>{t(view.key)}</span>
      </div>
    );
  }
  return (
    <div className="hx-gate__done">
      <Icon name="laptop" size="sm" />
      <span>{t('next.gate.doneElsewhere', { device: state.byDevice })}</span>
    </div>
  );
};

const GateCard = ({ permission, device, decided, onDecide, onOpenTerminal = null }: IGateCardProps) => {
  const action = usePending<string>();
  const waiting = permission.state.k === 'open' || permission.state.k === 'answering';
  const refused = permission.state.k === 'open' && permission.state.refused === 'menu-unmatched';
  const autoDenyAt = permission.state.k === 'open' ? (permission.state.autoDenyAt ?? null) : null;
  // A menu the machine cannot match gets no key: the card does not offer buttons that would do nothing.
  const live = waiting && !refused;
  const busy = isBusy(action.phase) || permission.state.k === 'answering';
  const plan = permission.kind === 'plan';
  const [mode, setMode] = useState<TPlanMode>('plan-ask');
  const decide = (optionId: string) => action.run(optionId, () => onDecide(permission.id, optionId));

  useEffect(() => {
    if (action.phase !== PENDING.failed) return;
    toast(t('next.gate.failed'), {
      tone: 'error',
      action: { label: t('next.gate.retry'), onClick: action.retry },
    });
  }, [action.phase, action.retry]);

  const tools = permission.options.filter(
    (option) => option.scope === 'once' || option.scope === 'always' || option.scope === 'deny',
  );
  useDigitKeys(live && !plan && !busy, tools.length, (n) => decide(tools[n - 1].id));

  const optionBy = (scope: PermissionScope) =>
    permission.options.find((option) => option.scope === scope) ?? null;
  const start = optionBy(mode);
  const revise = optionBy('revise');

  return (
    <div className="hx-gate" data-gate={plan ? 'plan' : 'perm'} data-st={waiting ? 'wait' : 'done'}>
      <div className="hx-gate__head">
        <span className="hx-gate__ic">
          <Icon name={plan ? 'list-checks' : 'shield-question'} size="sm" />
        </span>
        <span className="hx-gate__title">
          {plan ? t('next.gate.planTitle') : t(permTitleKey(permission.tool, permission.command))}
        </span>
        <span className="hx-gate__where">
          {plan ? '' : [permission.tool, permission.cwd].filter(Boolean).join(' · ')}
        </span>
      </div>
      {!plan && permission.command && (
        <div className="hx-gate__cmd">
          <span className="hx-gate__prompt">$</span>
          <code>{shellLine(permission.command)}</code>
        </div>
      )}
      {plan && permission.planMd && waiting && (
        <div className="hx-gate__planmd">
          <Md md={permission.planMd} />
        </div>
      )}
      {live && !plan && (
        <div className="hx-gate__acts">
          {tools.map((option, index) => (
            <button
              key={option.id}
              type="button"
              className="hx-gate__btn"
              data-variant={toolVariant(index, option.scope)}
              data-busy={(busy && action.key === option.id) || undefined}
              disabled={busy}
              onClick={() => decide(option.id)}
            >
              <span className="hx-gate__btxt">{option.label}</span>
              <kbd>{index + 1}</kbd>
              <Spinner />
            </button>
          ))}
        </div>
      )}
      {live && plan && (
        <div className="hx-gate__acts">
          <Segmented
            label={t('next.gate.planModeLabel')}
            value={mode}
            options={[
              { value: 'plan-ask', label: t('next.gate.planAsk') },
              { value: 'plan-auto', label: t('next.gate.planAuto') },
            ]}
            onChange={setMode}
            disabled={busy}
          />
          <span className="hx-spacer" />
          {revise && (
            <button
              type="button"
              className="hx-gate__btn"
              data-variant="ghost"
              data-busy={(busy && action.key === revise.id) || undefined}
              disabled={busy}
              onClick={() => decide(revise.id)}
            >
              <span className="hx-gate__btxt">{t('next.gate.revise')}</span>
              <Spinner />
            </button>
          )}
          {start && (
            <button
              type="button"
              className="hx-gate__btn"
              data-variant="primary"
              data-busy={(busy && action.key === start.id) || undefined}
              disabled={busy}
              onClick={() => decide(start.id)}
            >
              <span className="hx-gate__btxt">{t('next.gate.start')}</span>
              <Spinner />
            </button>
          )}
        </div>
      )}
      {live && autoDenyAt !== null && <AutoDeny at={autoDenyAt} />}
      {waiting && action.phase === PENDING.slow && <div className="hx-gate__slow">{t('next.gate.slow')}</div>}
      {refused && (
        <div className="hx-gate__refused" role="status">
          <Icon name="square-terminal" size="sm" />
          <span className="hx-gate__refused-txt">{t('next.gate.refused')}</span>
          {onOpenTerminal && (
            <button type="button" className="hx-gate__btn" data-variant="ghost" onClick={onOpenTerminal}>
              <span className="hx-gate__btxt">{t('next.gate.openTerminal')}</span>
            </button>
          )}
        </div>
      )}
      {!waiting && <Done permission={permission} device={device} decided={decided} />}
    </div>
  );
};

export default GateCard;
