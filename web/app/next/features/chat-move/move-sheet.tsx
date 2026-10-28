// «Продолжить на другой ВМ»: the person's other machines that are up, what each can take (the repository, the commit,
// the agent and its model), and the one button that moves the chat. A machine that cannot take it says why in a few
// words; one that lacks only the files offers «без локальных файлов». Nothing here picks a machine for the person.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import Modal from '@/next/features/shell/modal';
import { Skeleton } from '@/next/ui/skeleton';
import type {
  IMoveAgent,
  IMoveModel,
  IMoveRequest,
  IMoveTarget,
  IMoveTargets,
} from '@/next/features/chat-move/api';
import { t } from '@/next/i18n';
import '@/next/styles/handoff.css';
import '@/next/styles/chat-move.css';

export interface IMoveChoice {
  target: IMoveTarget;
  agent: IMoveAgent;
  model: IMoveModel;
  withoutFiles: boolean;
  opId: string;
}

export interface IMoveSheetProps {
  open: boolean;
  onClose: () => void;
  load: () => Promise<IMoveTargets>;
  /** Moves the chat; rejects with the reason and leaves the sheet open. */
  onMove: (choice: IMoveChoice) => Promise<void>;
  /** The machine chosen at first («Продолжить здесь» that needs the person's word on the files). */
  prefer?: string | null;
}

type TLoad = { k: 'loading' } | { k: 'failed'; reason: string } | { k: 'ready'; data: IMoveTargets };

const REASON: Record<string, string> = {
  project_missing: 'next.moveVm.reason.project',
  commit_missing: 'next.moveVm.reason.commit',
  worktree_dirty: 'next.moveVm.reason.dirty',
  agent_unavailable: 'next.moveVm.reason.agent',
  model_unavailable: 'next.moveVm.reason.agent',
  offline: 'next.moveVm.reason.offline',
  unsupported: 'next.moveVm.reason.unsupported',
};

export const reasonOf = (target: IMoveTarget): string | null => {
  const code = target.checks.find((c) => !c.ok)?.code ?? (target.ok ? undefined : target.code);
  if (!code) return target.ok ? null : t('next.moveVm.reason.agent');
  return t(REASON[code] ?? 'next.moveVm.reason.agent');
};

export const newOpId = (): string =>
  typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 12)}`;

const agentKey = (a: IMoveAgent): string => `${a.provider}/${a.profile}`;

const SheetBody = ({ load, onMove, onClose, prefer = null }: Omit<IMoveSheetProps, 'open'>) => {
  const [state, setState] = useState<TLoad>({ k: 'loading' });
  const [picked, setPicked] = useState<string | null>(prefer);
  const [agentPick, setAgentPick] = useState<{ machine: string; key: string; model: string } | null>(null);
  const [skipFiles, setSkipFiles] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // One id for the whole life of the sheet: a second press is the same move, never a second session.
  const opId = useRef(newOpId());

  const run = useCallback(
    () =>
      load().then(
        (data) => setState({ k: 'ready', data }),
        (failure: unknown) =>
          setState({
            k: 'failed',
            reason: failure instanceof Error ? failure.message : t('next.moveVm.failed'),
          }),
      ),
    [load],
  );
  useEffect(() => {
    void run();
  }, [run]);
  const retry = () => {
    setState({ k: 'loading' });
    void run();
  };

  const data = state.k === 'ready' ? state.data : null;
  const targets = data?.targets ?? [];
  const target = targets.find((x) => x.machineId === picked && x.ok) ?? targets.find((x) => x.ok) ?? null;

  const agent: IMoveAgent | null = useMemo(() => {
    if (!target) return null;
    const chosen = agentPick?.machine === target.machineId ? agentPick.key : null;
    return target.agents.find((a) => agentKey(a) === chosen) ?? target.agents[0] ?? null;
  }, [target, agentPick]);
  const model: IMoveModel | null = useMemo(() => {
    if (!agent) return null;
    const chosen =
      agentPick && agentPick.machine === target?.machineId && agentPick.key === agentKey(agent)
        ? agentPick.model
        : null;
    return (
      agent.models.find((m) => m.id === chosen) ??
      agent.models.find((m) => m.default) ??
      agent.models[0] ??
      null
    );
  }, [agent, agentPick, target]);

  const source = data?.source;
  const dirty = source?.dirty === true;
  const blocked = source ? !source.ok || source.busy : false;
  const needsSkip = target !== null && (!target.filesOk || dirty);
  const withoutFiles = needsSkip && skipFiles;
  const canSend =
    state.k === 'ready' &&
    !blocked &&
    target?.ok === true &&
    agent !== null &&
    model !== null &&
    (!needsSkip || skipFiles) &&
    !sending;

  const send = async () => {
    if (!canSend || !target || !agent || !model) return;
    setError(null);
    setSending(true);
    try {
      await onMove({ target, agent, model, withoutFiles, opId: opId.current });
      onClose();
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : t('next.moveVm.failed'));
    } finally {
      setSending(false);
    }
  };

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void send();
      }}
    >
      <div className="hx-modal__body hx-mv">
        {state.k === 'loading' && (
          <div className="hx-hsheet__wait" role="status">
            <Skeleton className="hx-hsheet__sk" />
            <span>{t('next.moveVm.loading')}</span>
          </div>
        )}
        {state.k === 'failed' && (
          <div className="hx-hsheet__fail" role="alert">
            <span>{state.reason}</span>
            <button type="button" className="hx-btn" data-variant="ghost" onClick={retry}>
              {t('next.load.retry')}
            </button>
          </div>
        )}
        {data && source?.busy && <p className="hx-mv__note">{t('next.moveVm.sourceBusy')}</p>}
        {data && source && !source.ok && !source.busy && (
          <p className="hx-mv__note">{t('next.moveVm.noHistory')}</p>
        )}
        {data && targets.length === 0 && <p className="hx-mv__note">{t('next.moveVm.none')}</p>}
        {data && targets.length > 0 && (
          <div className="hx-mv__list" role="radiogroup" aria-label={t('next.moveVm.title')}>
            {targets.map((x) => {
              const why = reasonOf(x);
              const active = target?.machineId === x.machineId;
              return (
                <button
                  key={x.machineId}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  className="hx-mv__vm"
                  data-active={active || undefined}
                  disabled={!x.ok || sending}
                  onClick={() => {
                    setError(null);
                    setPicked(x.machineId);
                  }}
                >
                  <span className="hx-mv__name">{x.name}</span>
                  {why && <span className="hx-mv__why">{why}</span>}
                </button>
              );
            })}
          </div>
        )}
        {target && target.agents.length > 0 && (
          <div className="hx-mv__agents" role="radiogroup" aria-label={t('next.moveVm.agent')}>
            {target.agents.map((a) =>
              a.models.map((m) => {
                const active = agent !== null && agentKey(a) === agentKey(agent) && model?.id === m.id;
                return (
                  <button
                    key={`${agentKey(a)}/${m.id}`}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    className="hx-mv__model"
                    data-active={active || undefined}
                    disabled={sending}
                    onClick={() => setAgentPick({ machine: target.machineId, key: agentKey(a), model: m.id })}
                  >
                    {a.profile ? `${a.short} · ${m.label}` : m.label}
                  </button>
                );
              }),
            )}
          </div>
        )}
        {needsSkip && (
          <button
            type="button"
            role="switch"
            aria-checked={skipFiles}
            className="hx-mv__skip"
            data-on={skipFiles || undefined}
            disabled={sending}
            onClick={() => setSkipFiles((now) => !now)}
          >
            <span className="hx-mv__knob" aria-hidden="true" />
            <span className="hx-mv__skiptxt">
              <strong>{t('next.moveVm.withoutFiles')}</strong>
              <span>{dirty ? t('next.moveVm.sourceDirty') : t('next.moveVm.filesHint')}</span>
            </span>
          </button>
        )}
        {error && (
          <p className="hx-create__err" role="alert">
            {error}
          </p>
        )}
      </div>
      <div className="hx-modal__foot">
        <span className="hx-spacer" />
        <button type="button" className="hx-btn" data-variant="ghost" onClick={onClose}>
          {t('next.rowMenu.cancel')}
        </button>
        <button
          type="submit"
          className="hx-btn hx-create__ok"
          data-variant="primary"
          data-busy={sending}
          disabled={!canSend}
        >
          <span>{t('next.moveVm.send')}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      </div>
    </form>
  );
};

/** The sheet: a dialog on a wide window, a sheet from below on the phone (the Modal's own), both animated out. */
const MoveSheet = ({ open, onClose, ...body }: IMoveSheetProps) => (
  <Modal
    open={open}
    onClose={onClose}
    title={t('next.moveVm.title')}
    lead={t('next.moveVm.lead')}
    boxClass="hx-create hx-mv-box"
  >
    <SheetBody {...body} onClose={onClose} />
  </Modal>
);

export type { IMoveRequest };
export default MoveSheet;
