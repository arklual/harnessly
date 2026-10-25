// «Цель сессии»: the /goal the agent works toward. The dialog shows its whole text and where it stands, and edits it:
// «Сохранить» types `/goal <text>` into the agent, «Убрать цель» types `/goal clear`. Like every change that goes to the
// machine it waits for the answer before it closes, and says why when there is none.
import { useRef, useState, type FormEvent } from 'react';
import Modal from '@/next/features/shell/modal';
import { formatAgo } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import type { GoalStatus } from '@/next/protocol/v2';
import { GIVE_UP_MS, SLOW_MS } from '@/next/ui/use-pending-list';
import '@/next/styles/goal.css';

/** What the header knows of the open chat's goal. */
export interface IHeaderGoal {
  /** The goal on the record; null when the session has none. */
  current: { text: string; status: GoalStatus; since: number } | null;
  /** Types the goal into the agent (an empty text clears it); resolves when the runtime has taken it. */
  onSave: (text: string) => Promise<void>;
}

const GOAL_MAX = 2000;

const STATUS_LABEL: Record<GoalStatus, string> = {
  active: t('next.goal.active'),
  paused: t('next.goal.paused'),
  met: t('next.goal.met'),
};

/** The words as one line: the agent's box takes a command in one. */
export const oneLine = (text: string): string => text.replace(/\s+/g, ' ').trim();

const STATE = { idle: 'idle', busy: 'busy', slow: 'slow' } as const;
type TState = (typeof STATE)[keyof typeof STATE];

interface IFormProps {
  goal: IHeaderGoal;
  onClose: () => void;
}

const GoalForm = ({ goal, onClose }: IFormProps) => {
  const { current } = goal;
  const [text, setText] = useState(current?.text ?? '');
  const [state, setState] = useState<TState>(STATE.idle);
  const [error, setError] = useState<string | null>(null);
  const [clearing, setClearing] = useState(false);
  const [now] = useState(() => Date.now());

  const run = async (next: string) => {
    setError(null);
    setState(STATE.busy);
    const slow = setTimeout(() => setState(STATE.slow), SLOW_MS);
    let giveUp: ReturnType<typeof setTimeout> | undefined;
    try {
      await Promise.race([
        goal.onSave(next),
        new Promise((_, reject) => {
          giveUp = setTimeout(() => reject(new Error(t('next.pending.noAnswer'))), GIVE_UP_MS);
        }),
      ]);
      onClose();
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : t('next.goal.failed'));
      setState(STATE.idle);
    } finally {
      clearTimeout(slow);
      clearTimeout(giveUp);
    }
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const next = oneLine(text);
    if (next === '' || next === current?.text) return;
    setClearing(false);
    void run(next);
  };

  const busy = state !== STATE.idle;
  const next = oneLine(text);
  const changed = next !== '' && next !== current?.text;
  return (
    <form onSubmit={submit}>
      <div className="hx-modal__body">
        {current && (
          <p className="hx-goal__state" data-status={current.status}>
            <span className="hx-goal__dot" aria-hidden="true" />
            <span>{STATUS_LABEL[current.status]}</span>
            <span className="hx-goal__since">
              {t('next.goal.since', { when: formatAgo(current.since, now) })}
            </span>
          </p>
        )}
        <label className="hx-create__f">
          <span className="hx-create__l">{t('next.goal.label')}</span>
          <textarea
            className="hx-goal__in"
            rows={5}
            maxLength={GOAL_MAX}
            autoFocus
            placeholder={t('next.goal.placeholder')}
            value={text}
            disabled={busy}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
                event.preventDefault();
                event.currentTarget.form?.requestSubmit();
              }
            }}
          />
        </label>
        <p className="hx-goal__note">{t('next.goal.note')}</p>
        {error && (
          <p className="hx-create__err" role="alert">
            {error}
          </p>
        )}
        {state === STATE.slow && <p className="hx-create__slow">{t('next.chatRow.slow')}</p>}
      </div>
      <div className="hx-modal__foot">
        {current && (
          <button
            type="button"
            className="hx-btn hx-goal__clear"
            data-variant="ghost"
            data-busy={busy && clearing}
            disabled={busy}
            onClick={() => {
              setClearing(true);
              void run('');
            }}
          >
            {t('next.goal.clear')}
          </button>
        )}
        <span className="hx-spacer" />
        <button type="button" className="hx-btn" data-variant="ghost" onClick={onClose}>
          {t('next.rowMenu.cancel')}
        </button>
        <button
          type="submit"
          className="hx-btn hx-create__ok"
          data-variant="primary"
          data-busy={busy && !clearing}
          disabled={!changed || busy}
        >
          <span>{t('next.goal.save')}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      </div>
    </form>
  );
};

export interface IGoalDialogProps {
  open: boolean;
  goal: IHeaderGoal;
  onClose: () => void;
}

const GoalDialog = ({ open, goal, onClose }: IGoalDialogProps) => {
  const focus = useRef<HTMLElement | null>(null);
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('next.goal.title')}
      lead={t('next.goal.lead')}
      boxClass="hx-create hx-goal"
      initialFocus={focus}
    >
      <GoalForm goal={goal} onClose={onClose} />
    </Modal>
  );
};

export default GoalDialog;
