// «Запросить»: the level asked for and a word to the owner. After it the screen says «Запрос отправлен» for as long as
// the request is open. Used by the card of a person with no access and by the dialogs of one who may only look; who
// receives the request (the older design API or the common /access one, HRN-675) is the caller's `onSend`.
import { useEffect, useRef, useState } from 'react';
import type { TRequestLevel } from '@/next/features/designly/access/api';
import { useAction } from '@/next/features/designly/access/use-action';
import Segmented from '@/next/ui/segmented';
import { t } from '@/next/i18n';

const MESSAGE_MAX = 500;

export type TRequestStatus = 'open' | 'allowed' | 'denied' | null;

interface IRequestFormProps {
  /** One with no place or who may only view may ask to comment too; one who comments asks only to edit. */
  mayComment: boolean;
  /** The person's last request, as the API answers it. */
  status: TRequestStatus;
  onSend: (level: TRequestLevel, message: string) => Promise<void>;
  /** The level chosen first; `edit` where the person came for «Попросить правку». */
  initialLevel?: TRequestLevel;
  /** Put the cursor in the message as the form appears (it was opened on purpose). */
  autoFocus?: boolean;
}

const RequestForm = ({ mayComment, status, onSend, initialLevel, autoFocus = false }: IRequestFormProps) => {
  const action = useAction();
  const [level, setLevel] = useState<TRequestLevel>(mayComment ? (initialLevel ?? 'comment') : 'edit');
  const [message, setMessage] = useState('');
  const box = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (autoFocus) box.current?.focus({ preventScroll: true });
  }, [autoFocus]);

  if (status === 'open') return <p className="hx-acc-note">{t('next.access.requestSent')}</p>;

  const ask = () => void action.run('ask', () => onSend(mayComment ? level : 'edit', message.trim()));
  return (
    <form
      className="hx-acc-ask"
      onSubmit={(event) => {
        event.preventDefault();
        ask();
      }}
    >
      {status === 'denied' && <p className="hx-acc-note">{t('next.access.requestDenied')}</p>}
      {mayComment && (
        <Segmented
          label={t('next.access.level')}
          value={level}
          disabled={action.busy !== null}
          options={[
            { value: 'comment', label: t('next.access.level.comment') },
            { value: 'edit', label: t('next.access.level.edit') },
          ]}
          onChange={setLevel}
        />
      )}
      <textarea
        ref={box}
        className="hx-create__in hx-acc-msg"
        rows={2}
        maxLength={MESSAGE_MAX}
        aria-label={t('next.access.message')}
        placeholder={t('next.access.messagePlaceholder')}
        value={message}
        disabled={action.busy !== null}
        onChange={(event) => setMessage(event.target.value)}
      />
      {action.error && (
        <p className="hx-create__err" role="alert">
          {action.error}
        </p>
      )}
      <button
        type="submit"
        className="hx-btn"
        data-variant="primary"
        data-busy={action.busy === 'ask'}
        disabled={action.busy !== null}
      >
        <span>{mayComment ? t('next.access.ask') : t('next.access.askEdit')}</span>
        <span className="hx-gate__spin" aria-hidden="true" />
      </button>
    </form>
  );
};

export default RequestForm;
