// /invite/<token>: who invites to which space, then join. Signed out, the way goes through sign-in and back here.
import { useEffect, useState } from 'react';

import { Button, Spinner } from '@kit';

import { isApiError } from '../lib/api';
import { errorText } from '../lib/errors';
import { useApi } from '../lib/env';
import { t } from '../lib/i18n';
import { Link, navigate } from '../lib/router';
import { useSession } from '../lib/session';
import { StepFrame } from '../ui/frame';
import { Users } from '../ui/icons';
import { Msg } from '../ui/parts';

type Peek =
  | { kind: 'loading' }
  | { kind: 'gone' }
  | { kind: 'failed'; text: string }
  | { kind: 'ok'; spaceName: string; inviterName: string; email: string | null };

export const InviteScreen = ({ token }: { token: string }) => {
  const api = useApi();
  const { me, reload } = useSession();
  const [peek, setPeek] = useState<Peek>({ kind: 'loading' });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    void api.invites
      .peek(token)
      .then((r) => {
        if (!alive) return;
        setPeek(
          r.valid
            ? { kind: 'ok', spaceName: r.spaceName, inviterName: r.inviterName, email: r.email }
            : { kind: 'gone' },
        );
      })
      .catch((e: unknown) => {
        if (!alive) return;
        setPeek(
          isApiError(e, 'not_found') || isApiError(e, 'expired')
            ? { kind: 'gone' }
            : { kind: 'failed', text: errorText(e) },
        );
      });
    return () => {
      alive = false;
    };
  }, [api, token]);

  const accept = async () => {
    setBusy(true);
    setError(null);
    try {
      const { space } = await api.invites.accept(token);
      await reload();
      navigate(`/s/${space.slug}`, { replace: true });
    } catch (e) {
      setBusy(false);
      if (isApiError(e, 'forbidden')) setError(t('invite.wrongMail', { mail: me?.user.email ?? '' }));
      else if (isApiError(e, 'expired')) setPeek({ kind: 'gone' });
      else setError(errorText(e));
    }
  };

  const here = `/invite/${encodeURIComponent(token)}`;
  const next = encodeURIComponent(here);

  return (
    <StepFrame
      step={1}
      bare
      icon={<Users size={22} />}
      title={peek.kind === 'ok' ? t('invite.title', { space: peek.spaceName }) : t('invite.titleGeneric')}
      lead={
        peek.kind === 'ok' ? t('invite.lead', { name: peek.inviterName || t('invite.someone') }) : undefined
      }
    >
      {peek.kind === 'loading' && (
        <div className="hub-enter" role="status">
          <Spinner size="s" />
          {t('common.loading')}
        </div>
      )}
      {peek.kind === 'gone' && <Msg tone="warn">{t('invite.gone')}</Msg>}
      {peek.kind === 'failed' && <Msg tone="danger">{peek.text}</Msg>}
      {peek.kind === 'ok' && (
        <div className="hub-stack">
          {peek.email && <Msg tone="info">{t('invite.forMail', { mail: peek.email })}</Msg>}
          {error && <Msg tone="danger">{error}</Msg>}
          {me ? (
            <>
              <p className="hub-note">{t('invite.asUser', { mail: me.user.email })}</p>
              <Button appearance="primary" size="l" block loading={busy} onClick={() => void accept()}>
                {t('invite.join')}
              </Button>
            </>
          ) : (
            <>
              <Link
                to={`/signin?next=${next}`}
                className="k-btn k-btn--block"
                data-appearance="primary"
                data-size="l"
              >
                <span className="k-btn__content">{t('invite.signinJoin')}</span>
              </Link>
              <p className="hub-note">
                {t('auth.noAccount')}{' '}
                <Link to={`/signup?next=${next}`} className="hub-link">
                  {t('auth.create')}
                </Link>
              </p>
            </>
          )}
        </div>
      )}
    </StepFrame>
  );
};
