// «Попросить больше»: a small panel under the button. Pessimistic: the button spins until the hub has the request, and
// only then the panel closes and the card says it was sent. It leaves with an animation.
import { useState } from 'react';
import { Button } from '@/next/ui/button';
import { usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const AskMore = ({
  who,
  send,
  onSent,
}: {
  who: string;
  send: (message: string) => Promise<void>;
  onSent: () => void;
}) => {
  const [open, setOpen] = useState(false);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const { mounted, state } = usePresentFlag(open, 160);

  const submit = async () => {
    setBusy(true);
    setFailed(false);
    try {
      await send(message.trim());
      setOpen(false);
      setMessage('');
      onSent();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="hx-mg-ask">
      <Button icon="send" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {t('next.manage.ask.button', { who })}
      </Button>
      {mounted && (
        <div
          className="hx-mg-ask__panel"
          data-state={state}
          role="group"
          aria-label={t('next.manage.ask.title')}
        >
          <label className="hx-mg-ask__label" htmlFor="hx-mg-ask-text">
            {t('next.manage.ask.label', { who })}
          </label>
          <textarea
            id="hx-mg-ask-text"
            className="hx-mg-ask__text"
            rows={3}
            value={message}
            disabled={busy}
            placeholder={t('next.manage.ask.placeholder')}
            onChange={(e) => setMessage(e.target.value)}
          />
          {failed && (
            <p className="hx-mg-ask__err" role="alert">
              {t('next.manage.ask.failed')}
            </p>
          )}
          <div className="hx-mg-ask__row">
            <Button variant="ghost" size="sm" disabled={busy} onClick={() => setOpen(false)}>
              {t('next.manage.ask.cancel')}
            </Button>
            <Button variant="primary" size="sm" busy={busy} onClick={() => void submit()}>
              {t('next.manage.ask.send')}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
};

export default AskMore;
