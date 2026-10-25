// The card of a chat our agent delegated, under the turn that started it: «Поручено: <title> · <model>», the chat's
// state as it goes, and the chat opens on a click.
import { memo, useContext } from 'react';
import { DelegatedChats, childState, startedIn, type TChildState } from '@/next/features/chat/delegation';
import type { Conversation } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/delegation.css';

const AGENT_NAME = { claude: 'Claude Code', codex: 'Codex' } as const;

/** How the state reads: the tone of its dot and words. */
const STATE_TONE: Record<TChildState, 'run' | 'ok' | 'warn' | 'fail' | 'off'> = {
  starting: 'run',
  working: 'run',
  waiting: 'warn',
  done: 'ok',
  stopped: 'off',
  failed: 'fail',
};

/** The model as the card names it: the model's id with its effort, else the agent's name. */
const modelOf = (c: Conversation): string => {
  if (!c.model) return AGENT_NAME[c.provider];
  return c.model.effort ? `${c.model.id} · ${c.model.effort}` : c.model.id;
};

export const DelegationCard = memo(({ chat }: { chat: Conversation }) => {
  const state = childState(chat);
  return (
    <button
      type="button"
      className="hx-deleg"
      data-tone={STATE_TONE[state]}
      onClick={() => goTo({ kind: ROUTE.chat, id: chat.id })}
    >
      <Icon name="workflow" size="sm" className="hx-deleg__ic" />
      <span className="hx-deleg__text">
        <span className="hx-deleg__label">{t('next.delegation.card')}</span>
        <span className="hx-deleg__title">{chat.title || t('next.delegation.untitled')}</span>
        <span className="hx-deleg__model">{modelOf(chat)}</span>
      </span>
      <span className="hx-deleg__state" role="status">
        <span className="hx-deleg__dot" aria-hidden="true" />
        {t(`next.delegation.state.${state}`)}
      </span>
      <Icon name="chevron-right" size="sm" className="hx-deleg__go" />
    </button>
  );
});

/** The cards of the chats a turn delegated, from its start to its end. */
const DelegationCards = ({ startedAt, endedAt }: { startedAt: number; endedAt: number | null }) => {
  const chats = startedIn(useContext(DelegatedChats), startedAt, endedAt);
  if (chats.length === 0) return null;
  return (
    <div className="hx-delegs">
      {chats.map((chat) => (
        <DelegationCard key={chat.id} chat={chat} />
      ))}
    </div>
  );
};

export default DelegationCards;
