// Access cards approve existing values; value cards send a new value directly to the machine safe.
import { useRef, useState, type FormEvent } from 'react';
import Modal from '@/next/features/shell/modal';
import type { ISecretAsk } from '@/next/features/safe/api';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import SecretField from '@/next/ui/secret-field';
import '@/next/styles/safe.css';

/** Every action resolves null when the machine took it, else the words of why not. */
type TDone = Promise<string | null>;

export interface ISafeAskDialogProps {
  /** The ask on screen; null closes the dialog (with its exit). */
  ask: ISecretAsk | null;
  /** How many more asks wait after this one. */
  more: number;
  projectName?: string;
  onPut: (ask: ISecretAsk, value: string) => TDone;
  onGrant: (ask: ISecretAsk) => TDone;
  onDecline: (ask: ISecretAsk) => TDone;
  /** Put aside on this page, unanswered. */
  onLater: (ask: ISecretAsk) => void;
}

const SafeAskDialog = ({
  ask,
  more,
  projectName,
  onPut,
  onGrant,
  onDecline,
  onLater,
}: ISafeAskDialogProps) => {
  const field = useRef<HTMLInputElement>(null);
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState<'put' | 'decline' | null>(null);
  const [error, setError] = useState<string | null>(null);
  // The dialog keeps showing the last ask while it closes.
  const [shown, setShown] = useState<ISecretAsk | null>(ask);
  if (ask !== null && ask !== shown) {
    setShown(ask);
    setValue('');
    setError(null);
  }

  const run = async (kind: 'put' | 'decline', action: () => TDone) => {
    setBusy(kind);
    setError(null);
    const refused = await action();
    setBusy(null);
    setError(refused);
  };

  const needsGrant = (request: ISecretAsk | null) => request?.grant === true || request?.originGrant === true;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!ask || (!needsGrant(ask) && value.trim() === '') || busy !== null) return;
    if (needsGrant(ask)) {
      void run('put', () => onGrant(ask));
      return;
    }
    const sent = value;
    // The value leaves the page's state the moment it is sent.
    setValue('');
    void run('put', () => onPut(ask, sent));
  };
  const decline = () => {
    if (!ask || busy !== null) return;
    void run('decline', () => onDecline(ask));
  };

  let lead = t('next.safe.ask.lead');
  if (shown?.title) lead = t('next.safe.ask.leadChat', { chat: shown.title });
  let title = t('next.safe.ask.title', { name: shown?.name ?? '' });
  if (shown?.replace) title = t('next.safe.ask.titleReplace', { name: shown.name });

  if (shown?.grant) {
    title = t('next.safe.ask.titleGrant', { project: projectName ?? shown.project ?? '', name: shown.name });
    lead = t('next.safe.ask.leadGrant');
  } else if (shown?.originGrant) {
    title = t('next.safe.ask.titleOriginGrant', { name: shown.name, site: shown.origin ?? '' });
    lead = t('next.safe.ask.leadOriginGrant');
  }

  return (
    <Modal
      open={ask !== null}
      onClose={() => {
        if (ask && busy === null) onLater(ask);
      }}
      title={title}
      lead={lead}
      boxClass="hx-safe-ask"
      initialFocus={needsGrant(shown) ? undefined : field}
    >
      <form onSubmit={submit}>
        <div className="hx-modal__body hx-safe-ask__body">
          {shown?.why && (
            <p className="hx-safe-ask__why">
              <Icon name="message-square-text" size="sm" />
              <span>{shown.why}</span>
            </p>
          )}
          {shown?.origin && (
            <p className="hx-safe-ask__origin">
              <span>{t('next.safe.ask.site')}</span>
              <strong>{shown.origin}</strong>
            </p>
          )}
          {!needsGrant(shown) && (
            <SecretField
              ref={field}
              value={value}
              onChange={(next) => {
                setValue(next);
                setError(null);
              }}
              label={t('next.safe.value', { name: shown?.name ?? '' })}
              placeholder={t(shown?.origin ? 'next.safe.ask.sitePlaceholder' : 'next.safe.value.placeholder')}
              disabled={busy !== null}
              invalid={error !== null}
            />
          )}
          <p className="hx-safe-ask__note">
            <Icon name="lock" size="sm" />
            <span>
              {t(
                shown?.grant
                  ? 'next.safe.ask.noteGrant'
                  : shown?.originGrant
                    ? 'next.safe.ask.noteOriginGrant'
                    : shown?.origin
                      ? 'next.safe.ask.siteNote'
                      : 'next.safe.ask.note',
              )}
            </span>
          </p>
          {error && (
            <p className="hx-create__err" role="alert">
              {error}
            </p>
          )}
          {more > 0 && <p className="hx-safe-ask__more">{t('next.safe.ask.more', { n: more })}</p>}
        </div>
        <div className="hx-modal__foot">
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-busy={busy === 'decline'}
            disabled={busy !== null}
            onClick={decline}
          >
            {t('next.safe.ask.decline')}
          </button>
          <button
            type="submit"
            className="hx-btn hx-create__ok"
            data-variant="primary"
            data-busy={busy === 'put'}
            disabled={busy !== null || (!needsGrant(shown) && value.trim() === '')}
          >
            <span>{t(needsGrant(shown) ? 'next.safe.ask.allow' : 'next.safe.ask.put')}</span>
            <span className="hx-gate__spin" aria-hidden="true" />
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default SafeAskDialog;
