// A new chat whose first message waits in the hub for its machine: the message as the person wrote it, what
// it waits for, and taking it back. The chat itself opens in its place once the machine has made it.
import { useState } from 'react';
import { t } from '@/next/i18n';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';

export const QUEUED_VIEW = {
  waiting: 'waiting',
  starting: 'starting',
  failed: 'failed',
  gone: 'gone',
} as const;
export type TQueuedView = (typeof QUEUED_VIEW)[keyof typeof QUEUED_VIEW];

export interface IQueuedChatProps {
  view: TQueuedView;
  /** The first message; null for a chat no longer waiting. */
  text: string | null;
  machine: string;
  /** Why the machine did not make the chat, in its words. */
  error: string | null;
  /** Takes the chat back; a promise keeps the button busy until it settles. */
  onCancel: () => Promise<unknown>;
}

const statusOf = (view: TQueuedView, machine: string, error: string | null): string => {
  if (view === QUEUED_VIEW.starting) return t('next.queuedChat.starting');
  if (view === QUEUED_VIEW.failed) return t('next.queuedChat.failed', { reason: error ?? '' });
  if (view === QUEUED_VIEW.gone) return t('next.queuedChat.gone');
  return t('next.queuedChat.waiting', { machine });
};

export const QueuedChat = ({ view, text, machine, error, onCancel }: IQueuedChatProps) => {
  const [busy, setBusy] = useState(false);
  const cancel = () => {
    setBusy(true);
    void onCancel().finally(() => setBusy(false));
  };
  const canCancel = view === QUEUED_VIEW.waiting || view === QUEUED_VIEW.failed;
  return (
    <div className="hx-queued">
      {text !== null && (
        <div
          className="hx-msg-user hx-queued__msg"
          data-send={view === QUEUED_VIEW.failed ? 'failed' : 'waiting'}
        >
          <div className="hx-bubble">
            <p>{text}</p>
          </div>
        </div>
      )}
      <div className="hx-queued__status" role="status" data-view={view}>
        {view === QUEUED_VIEW.starting ? (
          <span className="hx-spin" aria-hidden="true" />
        ) : (
          <Icon name="clock" size="sm" />
        )}
        <span>{statusOf(view, machine, error)}</span>
      </div>
      {canCancel && (
        <Button variant="ghost" size="sm" busy={busy} onClick={cancel} className="hx-queued__cancel">
          {view === QUEUED_VIEW.failed ? t('next.queuedChat.remove') : t('next.queuedChat.cancel')}
        </Button>
      )}
    </div>
  );
};
