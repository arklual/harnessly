// «Запросить»: the level asked for and a word to the owner. After it the screen says «Запрос отправлен» for as long as
// the request is open. Used by the card of a person with no access and by the dialog of one who may only look.
import { useState } from 'react';
import {
  askAccess,
  type IProjectAccess,
  type TRequestLevel,
  type TRole,
} from '@/next/features/designly/access/api';
import { useAction } from '@/next/features/designly/access/use-action';
import Segmented from '@/next/ui/segmented';
import { t } from '@/next/i18n';

const MESSAGE_MAX = 500;

interface IRequestFormProps {
  project: string;
  role: TRole | null;
  request: IProjectAccess['request'];
  onAsked: (next: IProjectAccess) => void;
}

const RequestForm = ({ project, role, request, onAsked }: IRequestFormProps) => {
  const action = useAction();
  const mayComment = role === null || role === 'view';
  const [level, setLevel] = useState<TRequestLevel>(mayComment ? 'comment' : 'edit');
  const [message, setMessage] = useState('');

  if (request?.status === 'open') return <p className="hx-acc-note">{t('next.access.requestSent')}</p>;

  const ask = () =>
    void action.run('ask', async () =>
      onAsked(await askAccess(project, mayComment ? level : 'edit', message.trim())),
    );
  return (
    <form
      className="hx-acc-ask"
      onSubmit={(event) => {
        event.preventDefault();
        ask();
      }}
    >
      {request?.status === 'denied' && <p className="hx-acc-note">{t('next.access.requestDenied')}</p>}
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
