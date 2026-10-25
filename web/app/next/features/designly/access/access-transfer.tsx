// «Передать владение», a view of the share dialog that only the owner reaches from «…»: who gets the project, then a
// second step that names them before anything is sent.
import { useState } from 'react';
import { EMAIL, transferOwner, type IProjectAccess } from '@/next/features/designly/access/api';
import PersonField from '@/next/features/designly/access/person-field';
import type { ISectionProps } from '@/next/features/designly/access/section';
import { t } from '@/next/i18n';

interface IAccessTransferProps extends ISectionProps {
  /** The hand-over is done: the dialog goes back to its main view. */
  onDone: () => void;
}

const AccessTransfer = ({ project, action, onNext, onDone }: IAccessTransferProps) => {
  const [heir, setHeir] = useState('');
  const [sure, setSure] = useState(false);
  const email = heir.trim().toLowerCase();
  const idle = action.busy === null;

  const hand = () =>
    void action.run('transfer', async () => {
      const next: IProjectAccess = await transferOwner(project, email);
      onNext(next);
      onDone();
    });

  return (
    <div className="hx-acc-stack" data-section="transfer">
      <p className="hx-acc-note">{t('next.access.transferLead')}</p>
      <PersonField
        label={t('next.access.heir')}
        placeholder={t('next.access.addPlaceholder')}
        value={heir}
        disabled={!idle || sure}
        onChange={setHeir}
      />
      {sure ? (
        <div className="hx-acc-ask-box" role="group" aria-label={t('next.access.transferTitle')}>
          <p className="hx-acc-ask-box__q">{t('next.access.transferAsk', { who: email })}</p>
          <div className="hx-acc-ask-box__do">
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              onClick={() => setSure(false)}
            >
              {t('next.rowMenu.cancel')}
            </button>
            <button
              type="button"
              className="hx-btn"
              data-variant="danger"
              data-size="sm"
              data-busy={action.busy === 'transfer'}
              disabled={!idle}
              onClick={hand}
            >
              <span>{t('next.access.transferDo')}</span>
              <span className="hx-gate__spin" aria-hidden="true" />
            </button>
          </div>
        </div>
      ) : (
        <div className="hx-acc-ask-box__do">
          <button
            type="button"
            className="hx-btn"
            data-size="sm"
            disabled={!idle || !EMAIL.test(email)}
            onClick={() => setSure(true)}
          >
            {t('next.access.transferNext')}
          </button>
        </div>
      )}
    </div>
  );
};

export default AccessTransfer;
