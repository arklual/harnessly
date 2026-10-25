// Going on from an earlier message: the person's own message offers «Изменить и продолжить отсюда» on hover
// (a long press on the phone) and becomes an editor in place. Sending it asks one thing, «Только разговор» or
// «Разговор и файлы» (the folder back to its snapshot after the turn before, with the files that would change), and
// the turns from that message on fold into one line, «Откатено N ходов», that opens to show them as they were.
import { createContext, use, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import type { RewoundItem, SayEntry, UserEntry } from '@/next/features/chat/types';
import type { Entry, FileChange, RewindPlan, Turn, TurnsPage } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { PRESENCE, usePresence, useSeen } from '@/next/ui/presence';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { plural, t } from '@/next/i18n';
import Md from '@/next/features/chat/md';
import '@/next/styles/chat.css';

/** What the feed's rewinds ask of the chat; each promise rejects with the words of why it did not happen. */
export interface IRewindActions {
  /** Whether the message that opened this turn can be edited and the chat go on from it. */
  can: (turnId: string) => boolean;
  /** The agent works or waits for an answer: it is stopped first. */
  busy: boolean;
  plan: (turnId: string) => Promise<RewindPlan>;
  rewind: (turnId: string, text: string, files: boolean) => Promise<void>;
  stop: () => Promise<void>;
  undo: () => Promise<void>;
  rewound: (n: number) => Promise<TurnsPage>;
}

export const RewindActions = createContext<IRewindActions | null>(null);

/** The editor's exit, and the bubble's return after it. */
const EDITOR_EXIT_MS = 180;
/** A long press on the phone shows the action. */
const LONG_PRESS_MS = 480;

const CHANGE_KEYS: Record<FileChange, string> = {
  restored: 'next.rewind.change.restored',
  changed: 'next.rewind.change.changed',
  removed: 'next.rewind.change.removed',
} as const;

type TPlanState = { k: 'loading' } | { k: 'ready'; plan: RewindPlan } | { k: 'failed'; reason: string };

const reasonOf = (error: unknown): string => (error instanceof Error ? error.message : String(error));

interface IChoiceProps {
  plan: TPlanState;
  busy: boolean;
  pending: 'chat' | 'files' | null;
  failed: string | null;
  onPick: (files: boolean) => void;
}

/** The one question of a rewind: the conversation alone, or the files with it. */
const Choice = ({ plan, busy, pending, failed, onPick }: IChoiceProps) => {
  if (plan.k === 'loading')
    return (
      <div className="hx-rewind-choice__state" data-busy="true" role="status">
        <span className="hx-spin" aria-hidden="true" />
        {t('next.rewind.planLoading')}
      </div>
    );
  if (plan.k === 'failed')
    return (
      <div className="hx-rewind-choice__state" data-tone="fail" role="alert">
        <Icon name="circle-alert" size="sm" />
        {plan.reason}
      </div>
    );
  const { files, turns } = plan.plan;
  return (
    <>
      <div className="hx-rewind-choice__title">{plural(turns, 'next.rewind.willRewind')}</div>
      <button
        type="button"
        className="hx-rewind-choice__opt"
        disabled={busy}
        data-busy={pending === 'chat' || undefined}
        onClick={() => onPick(false)}
      >
        <Icon name="message-square" size="sm" className="hx-rewind-choice__ic" />
        <span className="hx-rewind-choice__text">
          <span className="hx-rewind-choice__name">{t('next.rewind.chatOnly')}</span>
          <span className="hx-rewind-choice__hint">{t('next.rewind.chatOnlyHint')}</span>
        </span>
        <span className="hx-spin" aria-hidden="true" />
      </button>
      <button
        type="button"
        className="hx-rewind-choice__opt"
        disabled={busy || files === null}
        data-busy={pending === 'files' || undefined}
        onClick={() => onPick(true)}
      >
        <Icon name="history" size="sm" className="hx-rewind-choice__ic" />
        <span className="hx-rewind-choice__text">
          <span className="hx-rewind-choice__name">{t('next.rewind.withFiles')}</span>
          <span className="hx-rewind-choice__hint">
            {files === null && t('next.rewind.noSnapshot')}
            {files !== null && files.changes.length === 0 && t('next.rewind.filesSame')}
            {files !== null &&
              files.changes.length > 0 &&
              plural(files.changes.length + files.more, 'next.rewind.filesWill')}
          </span>
        </span>
        <span className="hx-spin" aria-hidden="true" />
      </button>
      {files !== null && files.changes.length > 0 && (
        <ul className="hx-rewind-choice__files">
          {files.changes.map((c) => (
            <li key={c.path} className="hx-rewind-choice__file" data-change={c.change}>
              <span className="hx-rewind-choice__path">{c.path}</span>
              <span className="hx-rewind-choice__change">{t(CHANGE_KEYS[c.change])}</span>
            </li>
          ))}
          {files.more > 0 && (
            <li className="hx-rewind-choice__more">{plural(files.more, 'next.rewind.filesMore')}</li>
          )}
        </ul>
      )}
      {failed && (
        <div className="hx-rewind-choice__state" data-tone="fail" role="alert">
          <Icon name="circle-alert" size="sm" />
          {failed}
        </div>
      )}
    </>
  );
};

interface IEditorProps {
  turnId: string;
  text: string;
  closing: boolean;
  actions: IRewindActions;
  onClose: () => void;
}

/** The message as an editor in its place; sending it asks the rewind's one question. */
const Editor = ({ turnId, text, closing, actions, onClose }: IEditorProps) => {
  const [draft, setDraft] = useState(text);
  const [plan, setPlan] = useState<TPlanState>({ k: 'loading' });
  const [asking, setAsking] = useState(false);
  const send = usePending<'chat' | 'files'>();
  const stop = usePending<'stop'>();
  const [failed, setFailed] = useState<string | null>(null);
  const field = useRef<HTMLTextAreaElement>(null);

  // The plan is asked when the editor opens, so the question has its answer by the time the words are ready.
  useEffect(() => {
    let gone = false;
    actions.plan(turnId).then(
      (p) => !gone && setPlan({ k: 'ready', plan: p }),
      (error: unknown) => !gone && setPlan({ k: 'failed', reason: reasonOf(error) }),
    );
    return () => {
      gone = true;
    };
  }, [actions, turnId]);

  useEffect(() => {
    const el = field.current;
    if (!el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }, []);

  const words = draft.trim();
  const pick = (files: boolean) => {
    setFailed(null);
    send.run(files ? 'files' : 'chat', () =>
      actions.rewind(turnId, words, files).then(onClose, (error: unknown) => {
        setFailed(reasonOf(error));
        throw error;
      }),
    );
  };
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
    }
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey) && words && !actions.busy) {
      event.preventDefault();
      setAsking(true);
    }
  };
  const sending = isBusy(send.phase);

  return (
    <div className="hx-msg" data-kind="user">
      <div className="hx-rewind-edit" data-state={closing ? PRESENCE.closing : PRESENCE.open}>
        <textarea
          ref={field}
          className="hx-rewind-edit__field"
          value={draft}
          aria-label={t('next.rewind.editLabel')}
          rows={2}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={onKeyDown}
        />
        {actions.busy && (
          <div className="hx-rewind-edit__note" role="status">
            <Icon name="info" size="sm" />
            <span>{t('next.rewind.busy')}</span>
            <Button
              size="sm"
              variant="ghost"
              icon="circle-stop"
              busy={isBusy(stop.phase)}
              onClick={() => stop.run('stop', actions.stop)}
            >
              {stop.phase === PENDING.failed ? t('next.rewind.stopFailed') : t('next.rewind.stop')}
            </Button>
          </div>
        )}
        <div className="hx-rewind-edit__bar">
          <Button size="sm" variant="ghost" disabled={sending} onClick={onClose}>
            {t('next.rewind.cancel')}
          </Button>
          <Popover
            open={asking}
            onOpenChange={(open) => !sending && setAsking(open && !!words && !actions.busy)}
            label={t('next.rewind.choiceLabel')}
            className="hx-rewind-choice"
            side="bottom"
            align="end"
            trigger={
              <Button size="sm" variant="primary" icon="send" disabled={!words || actions.busy}>
                {t('next.rewind.send')}
              </Button>
            }
          >
            <Choice
              plan={plan}
              busy={sending}
              pending={sending ? send.key : null}
              failed={failed}
              onPick={pick}
            />
          </Popover>
        </div>
      </div>
    </div>
  );
};

interface IRewindablePromptProps {
  turnId: string;
  text: string;
  /** The message as it is shown when it is not being edited. */
  children: ReactNode;
}

/** The person's message with «Изменить и продолжить отсюда»; without the chat's rewinds it is the message alone. */
export const RewindablePrompt = ({ turnId, text, children }: IRewindablePromptProps) => {
  const actions = use(RewindActions);
  const [editing, setEditing] = useState(false);
  const [revealed, setRevealed] = useState(false);
  const editor = usePresence(editing ? turnId : null, EDITOR_EXIT_MS);
  const press = useRef<ReturnType<typeof setTimeout> | null>(null);
  const holder = useRef<HTMLDivElement>(null);

  // A shown action goes with the next touch anywhere else.
  useEffect(() => {
    if (!revealed) return;
    const away = (event: PointerEvent) => {
      if (!holder.current?.contains(event.target as Node)) setRevealed(false);
    };
    document.addEventListener('pointerdown', away);
    return () => document.removeEventListener('pointerdown', away);
  }, [revealed]);
  useEffect(
    () => () => {
      if (press.current) clearTimeout(press.current);
    },
    [],
  );

  if (!actions || !actions.can(turnId)) return <>{children}</>;
  if (editor.value !== null)
    return (
      <Editor
        turnId={turnId}
        text={text}
        closing={editor.state === PRESENCE.closing}
        actions={actions}
        onClose={() => setEditing(false)}
      />
    );
  const release = () => {
    if (press.current) clearTimeout(press.current);
    press.current = null;
  };
  return (
    <div
      ref={holder}
      className="hx-rewindable"
      data-revealed={revealed || undefined}
      onPointerDown={(event) => {
        if (event.pointerType !== 'touch') return;
        release();
        press.current = setTimeout(() => setRevealed(true), LONG_PRESS_MS);
      }}
      onPointerUp={release}
      onPointerCancel={release}
      onPointerMove={release}
    >
      {children}
      <div className="hx-rewindable__acts">
        <button
          type="button"
          className="hx-msg-user__act hx-rewindable__act"
          onClick={() => {
            setRevealed(false);
            setEditing(true);
          }}
        >
          <Icon name="pencil" size="sm" />
          {t('next.rewind.edit')}
        </button>
      </div>
    </div>
  );
};

const faces = (page: TurnsPage): { turn: Turn; prompt: UserEntry | null; answer: SayEntry | null }[] => {
  const byId = new Map<string, Entry>(page.entries.map((e) => [e.id, e]));
  return page.turns.map((turn) => {
    const prompt = byId.get(turn.id);
    const answer = turn.answerEntryId ? byId.get(turn.answerEntryId) : undefined;
    return {
      turn,
      prompt: prompt?.t === 'user' ? prompt : null,
      answer: answer?.t === 'say' ? answer : null,
    };
  });
};

type TBranch = { k: 'loading' } | { k: 'ready'; page: TurnsPage } | { k: 'failed'; reason: string };

/** The turns a rewind left behind, as they were: the person's words and the agent's answers, nothing to press. */
const Branch = ({ n }: { n: number }) => {
  const actions = use(RewindActions);
  const [branch, setBranch] = useState<TBranch>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!actions) return;
    let gone = false;
    actions.rewound(n).then(
      (page) => !gone && setBranch({ k: 'ready', page }),
      (error: unknown) => !gone && setBranch({ k: 'failed', reason: reasonOf(error) }),
    );
    return () => {
      gone = true;
    };
  }, [actions, n, attempt]);
  if (branch.k === 'loading')
    return (
      <div className="hx-rewound__state" data-busy="true" role="status">
        <span className="hx-spin" aria-hidden="true" />
        {t('next.rewind.branchLoading')}
      </div>
    );
  if (branch.k === 'failed')
    return (
      <div className="hx-rewound__state" data-tone="fail" role="alert">
        <Icon name="circle-alert" size="sm" />
        {branch.reason}
        <button
          type="button"
          className="hx-msg-user__act"
          onClick={() => {
            setBranch({ k: 'loading' });
            setAttempt((a) => a + 1);
          }}
        >
          {t('next.pending.retry')}
        </button>
      </div>
    );
  return (
    <ol className="hx-rewound__turns">
      {faces(branch.page).map(({ turn, prompt, answer }) => (
        <li key={turn.id} className="hx-rewound__turn">
          {prompt && (
            <div className="hx-rewound__prompt">
              <div className="hx-bubble">{prompt.text}</div>
            </div>
          )}
          {answer && (
            <div className="hx-rewound__answer">
              <Md md={answer.md} />
            </div>
          )}
        </li>
      ))}
    </ol>
  );
};

/** Whether the page's clock is before `until`; turns false by itself when it passes. */
const useBefore = (until: number | null): boolean => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (until === null) return;
    const left = until - Date.now();
    if (left <= 0) return;
    const timer = setTimeout(() => setNow(Date.now()), left + 20);
    return () => clearTimeout(timer);
  }, [until]);
  return until !== null && now < until;
};

/** «Откатено N ходов»: a line in the feed where the turns were; it opens to show them, the last can be taken back. */
export const RewoundRow = ({ item }: { item: RewoundItem }) => {
  const actions = use(RewindActions);
  const [open, setOpen] = useState(false);
  const seen = useSeen(open);
  const undo = usePending<'undo'>();
  const undoable = useBefore(item.undoUntil) && !!actions;
  const shown = usePresence(undoable || isBusy(undo.phase) ? true : null, EDITOR_EXIT_MS);
  return (
    <div className="hx-rewound" aria-expanded={open}>
      <div className="hx-rewound__line">
        <button
          type="button"
          className="hx-rewound__head"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          <Icon name="history" size="sm" />
          <span>
            {plural(item.turns, 'next.rewind.rewound')}
            {item.files && ` · ${t('next.rewind.rewoundFiles')}`}
          </span>
          <Icon name="chevron-right" size="sm" className="hx-rewound__chev" />
        </button>
        {shown.value && actions && (
          <span className="hx-rewound__undo" data-state={shown.state}>
            <button
              type="button"
              className="hx-msg-user__act"
              data-busy={isBusy(undo.phase) || undefined}
              disabled={isBusy(undo.phase)}
              onClick={() => undo.run('undo', actions.undo)}
            >
              <Icon name="undo-2" size="sm" />
              {undo.phase === PENDING.failed ? t('next.rewind.undoFailed') : t('next.rewind.undo')}
              <span className="hx-spin" aria-hidden="true" />
            </button>
          </span>
        )}
      </div>
      <div className="hx-rewound__body">
        <div>{seen && <Branch n={item.n} />}</div>
      </div>
    </div>
  );
};
