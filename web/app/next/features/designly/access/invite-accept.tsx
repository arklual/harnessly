// `/designly/invite/:token`: taking a link-invitation. The link is accepted once the page opens (it is the person's
// own act of following it); the project then opens. A link that ran out, was revoked or is spent says so.
import { useEffect, useState } from 'react';
import { acceptInvite, AccessError } from '@/next/features/designly/access/api';
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import { goTo, ROUTE } from '@/next/routes/routes';
import { t } from '@/next/i18n';
import '@/next/styles/design-access.css';

interface IInviteAcceptProps {
  token: string;
  /** Where the project opens; the screen navigates there by default. */
  onOpen?: (project: string) => void;
}

/** One request per link: the effect runs twice in development, and a second take would spend a second place. */
const TAKEN = new Map<string, Promise<{ project: string }>>();
const takeOnce = (token: string, attempt: number) => {
  const key = `${token}#${attempt}`;
  const known = TAKEN.get(key);
  if (known) return known;
  const fresh = acceptInvite(token);
  TAKEN.set(key, fresh);
  // Both runs of the effect are asked before it answers; after that the link is taken (or refused) for good.
  const forget = () => TAKEN.delete(key);
  fresh.then(forget, forget);
  return fresh;
};

const InviteAccept = ({
  token,
  onOpen = (project) => goTo({ kind: ROUTE.site, id: project }, { replace: true }),
}: IInviteAcceptProps) => {
  const [failure, setFailure] = useState<{ gone: boolean; text: string } | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    takeOnce(token, attempt).then(
      (taken) => live && onOpen(taken.project),
      (error: Error) => {
        if (!live) return;
        const gone = error instanceof AccessError && error.code === 'gone';
        setFailure({ gone, text: gone ? t('next.access.inviteGone') : error.message });
      },
    );
    return () => {
      live = false;
    };
    // onOpen is the host's: a new one each render must not take the link again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, attempt]);

  if (failure === null) {
    return (
      <section className="hx-acc-card" aria-busy="true" aria-label={t('next.access.inviteTaking')}>
        <Delayed>
          <Skeleton className="hx-acc-sk" />
        </Delayed>
      </section>
    );
  }
  return (
    <section className="hx-acc-card" aria-label={t('next.access.inviteTitle')}>
      <h2 className="hx-acc-card__title">
        {failure.gone ? t('next.access.inviteGoneTitle') : t('next.access.inviteFailed')}
      </h2>
      <p className="hx-acc-card__lead">{failure.text}</p>
      <div className="hx-acc-line">
        {!failure.gone && (
          <button
            type="button"
            className="hx-btn"
            data-variant="primary"
            onClick={() => {
              setFailure(null);
              setAttempt((n) => n + 1);
            }}
          >
            {t('next.access.retry')}
          </button>
        )}
        <button
          type="button"
          className="hx-btn"
          data-variant="ghost"
          onClick={() => goTo({ kind: ROUTE.designly })}
        >
          {t('next.access.back')}
        </button>
      </div>
    </section>
  );
};

export default InviteAccept;
