import { ArrowUp, Square } from 'lucide-react';
import { t } from '@/lib/i18n';

export const SEND_KIND = {
  send: 'send',
  stop: 'stop',
} as const;

type TSendKind = (typeof SEND_KIND)[keyof typeof SEND_KIND];

interface IComposerSendButtonProps {
  kind?: TSendKind;
  disabled?: boolean;
  /** The prompt is on its way (attachments confirming, a command running): the arrow breathes, no spinner. */
  busy?: boolean;
  /** Codex's send is drawn in its own neutral colour, as its mark is. */
  codex?: boolean;
  label?: string;
  title?: string;
  /** A stable handle for the browser tests. */
  testId?: string;
  onClick: () => void;
}

/** The round send button of every composer: an arrow up, or a square while an answer can be stopped. */
const ComposerSendButton = ({
  kind = SEND_KIND.send,
  disabled,
  busy,
  codex,
  label,
  title,
  testId,
  onClick,
}: IComposerSendButtonProps) => {
  const stop = kind === SEND_KIND.stop;
  const name =
    label ?? (stop ? t('composer.composerSendButton.stop') : t('composer.composerSendButton.send'));
  return (
    <button
      type="button"
      className="tw-csend"
      data-testid={testId}
      data-kind={kind}
      data-busy={busy || undefined}
      data-codex={codex || undefined}
      disabled={disabled}
      aria-label={name}
      aria-busy={busy || undefined}
      title={title ?? name}
      onClick={onClick}
    >
      {stop ? (
        <Square fill="currentColor" strokeWidth={0} aria-hidden="true" />
      ) : (
        <ArrowUp aria-hidden="true" />
      )}
    </button>
  );
};

export default ComposerSendButton;
