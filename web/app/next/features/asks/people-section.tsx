// «People» in the sidebar: the agents' recent questions to people, beside the chats — whom an agent wrote to and
// where, and how it stands (waiting, answered, no answer). A row opens the whole ask in a dialog.
import { memo } from 'react';
import type { IAsk } from '@/next/features/asks/api';
import { AskCard, askState, personLabel, type IAskCardActions } from '@/next/features/asks/ask-card';
import Modal from '@/next/features/shell/modal';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/asks.css';

/** The sidebar lists this many; older asks are in the dialog's chat cards and in Settings. */
export const PEOPLE_MAX = 6;
/** An ask closed longer ago than this leaves the sidebar. */
export const PEOPLE_FOR_MS = 3 * 24 * 60 * 60 * 1000;

const STATE_ICON = {
  run: 'clock',
  ok: 'check',
  warn: 'pencil',
  fail: 'circle-alert',
  off: 'circle-x',
} as const;

/** The asks the sidebar shows: every one that waits, then the recent ones, newest first. */
export const peopleAsks = (asks: readonly IAsk[] | null, now: number): IAsk[] => {
  if (!asks) return [];
  const waiting = asks.filter((a) => a.status === 'waiting');
  const recent = asks.filter(
    (a) => a.status !== 'waiting' && now - (a.closedAt || a.createdAt) < PEOPLE_FOR_MS,
  );
  return [...waiting, ...recent].slice(0, PEOPLE_MAX);
};

interface IPeopleSectionProps {
  asks: readonly IAsk[];
  now: number;
  onOpen: (id: string) => void;
}

export const PeopleSection = memo(({ asks, now, onOpen }: IPeopleSectionProps) => {
  if (asks.length === 0) return null;
  return (
    <section className="hx-people" aria-label={t('next.asks.people')}>
      <div className="hx-group__label hx-group__label--static">{t('next.asks.people')}</div>
      {asks.map((ask) => {
        const state = askState(ask, now);
        const line = ask.answer?.text || ask.question;
        return (
          <button
            key={ask.id}
            type="button"
            className="hx-people__row"
            data-tone={state.tone}
            onClick={() => onOpen(ask.id)}
          >
            <Icon name={STATE_ICON[state.tone]} size="sm" className="hx-people__ic" />
            <span className="hx-people__text">
              <span className="hx-people__who">
                {personLabel(ask.to)}
                <span className="hx-people__chat">{ask.chat}</span>
              </span>
              <span className="hx-people__line">{line}</span>
            </span>
          </button>
        );
      })}
    </section>
  );
});

interface IAskDialogProps {
  ask: IAsk | null;
  open: boolean;
  actions: IAskCardActions | null;
  onClose: () => void;
}

/** One ask in full. */
export const AskDialog = ({ ask, open, actions, onClose }: IAskDialogProps) => (
  <Modal
    open={open && ask !== null}
    onClose={onClose}
    title={ask?.kind === 'approval' ? t('next.asks.approvalTitle') : t('next.asks.questionTitle')}
    lead={ask?.agent ? t('next.asks.fromAgent', { agent: ask.agent }) : undefined}
    boxClass="hx-ask-modal"
  >
    <div className="hx-modal__body hx-ask-modal__body">{ask && <AskCard ask={ask} actions={actions} />}</div>
  </Modal>
);
