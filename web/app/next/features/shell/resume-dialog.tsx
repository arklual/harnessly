import { useRef, useState, type KeyboardEvent } from 'react';
import { t } from '@/next/i18n';
import Modal from '@/next/features/shell/modal';
import { formatAgo } from '@/next/i18n/format';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import usePendingList from '@/next/ui/use-pending-list';

export interface IResumeSession {
  id: string;
  provider: 'claude' | 'codex';
  /** The first message: what the session was about. */
  first: string;
  last: string;
  dir: string;
  branch: string | null;
  machine: string;
  at: number;
  /** Its machine does not answer: shown, not picked. */
  off: boolean;
}

export interface IResumeDialogProps {
  open: boolean;
  onClose: () => void;
  /** null while the machines are asked. */
  sessions: readonly IResumeSession[] | null;
  /** The list did not come: why, and how to ask again. */
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  now: number;
  /** Resolves once the chat is there; the dialog closes then. */
  onResume: (id: string) => Promise<void>;
}

const TABS = [
  { id: 'all', label: t('next.resume.all') },
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
] as const;
type TTab = (typeof TABS)[number]['id'];

const matches = (session: IResumeSession, query: string, tab: TTab) => {
  if (tab !== 'all' && session.provider !== tab) return false;
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [session.first, session.last, session.dir, session.branch ?? ''].some((text) =>
    text.toLowerCase().includes(q),
  );
};

/** «Продолжить сессию»: agents started in a terminal on the person's machines; a pick brings the session in whole. */
const ResumeDialog = ({ open, onClose, sessions, failed = null, now, onResume }: IResumeDialogProps) => {
  const queryRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const [tab, setTab] = useState<TTab>('all');
  const { pending, run } = usePendingList();
  const shown = sessions?.filter((session) => matches(session, query, tab)) ?? null;

  const pick = (session: IResumeSession) => {
    if (session.off || pending.size > 0) return;
    void run(session.id, t('next.resume.failed'), () => onResume(session.id)).then((ok) => {
      if (ok) onClose();
    });
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    const first = shown?.find((session) => !session.off);
    if (event.key === 'Enter' && first) {
      event.preventDefault();
      pick(first);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('next.resume.title')}
      lead={t('next.resume.lead')}
      boxClass="hx-resume"
      initialFocus={queryRef}
    >
      <div className="hx-resume__bar">
        <label className="hx-resume__q">
          <i className="hx-icon" data-i="search" data-size="sm" aria-hidden="true" />
          <input
            ref={queryRef}
            type="text"
            autoComplete="off"
            placeholder={t('next.resume.placeholder')}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={onKeyDown}
          />
        </label>
        <div className="hx-set__seg" role="tablist">
          {TABS.map((item) => (
            <button
              key={item.id}
              type="button"
              role="tab"
              className="hx-set__seg-item"
              aria-selected={item.id === tab}
              onClick={() => setTab(item.id)}
            >
              {item.label}
            </button>
          ))}
        </div>
      </div>
      <div className="hx-resume__list">
        {shown === null && failed !== null && (
          <LoadFailed
            size={LOAD_FAILED_SIZE.pane}
            title={t('next.load.sessions')}
            reason={failed.reason}
            onRetry={failed.onRetry}
          />
        )}
        {shown === null && failed === null && (
          <div className="hx-resume__wait" role="status">
            <span className="hx-sk" />
            <span className="hx-sk" />
            <span className="hx-sk" />
          </div>
        )}
        {shown?.map((session) => (
          <button
            key={session.id}
            type="button"
            className="hx-resume__row"
            data-off={session.off}
            data-busy={pending.has(session.id)}
            aria-disabled={session.off}
            onClick={() => pick(session)}
          >
            <i className="hx-agent" data-agent={session.provider} aria-hidden="true" />
            <span className="hx-resume__txt">
              <span className="hx-resume__first">{session.first}</span>
              <span className="hx-resume__last">{session.last}</span>
              <span className="hx-resume__meta">
                <span>{session.dir}</span>
                {session.branch && <span>{session.branch}</span>}
                <span>{session.machine}</span>
              </span>
            </span>
            <span className="hx-resume__when">{formatAgo(session.at, now)}</span>
            <span className="hx-gate__spin" aria-hidden="true" />
          </button>
        ))}
        {shown?.length === 0 && <div className="hx-resume__empty">{t('next.resume.empty')}</div>}
      </div>
      <div className="hx-resume__foot">
        <span>
          <kbd>↵</kbd>
          {t('next.resume.openFirst')}
        </span>
        <span>
          {t('next.resume.terminal')} <code>claude --resume</code>
        </span>
      </div>
    </Modal>
  );
};

export default ResumeDialog;
