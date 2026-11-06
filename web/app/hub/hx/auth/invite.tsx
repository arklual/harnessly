// /invite/<token> on the new hub's kit: who invites to which space, then join. Signed out, the way goes through
// sign-in and back here. A link that no longer works says so and where to go next.
import { useEffect, useState } from 'react';

import { isApiError } from '../../lib/api';
import { useApi } from '../../lib/env';
import { errorText } from '../../lib/errors';
import { t } from '../../lib/i18n';
import { Link, navigate } from '../../lib/router';
import { useSession } from '../../lib/session';
import { Btn, Icon } from '../ui';
import { AuthFrame, Note, Problem } from './frame';

type Peek =
  | { kind: 'loading' }
  | { kind: 'gone' }
  | { kind: 'failed'; text: string }
  | { kind: 'ok'; spaceName: string; inviterName: string; email: string | null };

export const HxInvite = ({ token }: { token: string }) => {
  const api = useApi();
  const { me, reload } = useSession();
  const [peek, setPeek] = useState<Peek>({ kind: 'loading' });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

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
  }, [api, token, attempt]);

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

  const next = encodeURIComponent(`/invite/${encodeURIComponent(token)}`);

  if (peek.kind === 'gone') {
    return (
      <AuthFrame title={t('invite.titleGeneric')}>
        <Problem
          title={t('authx.expired.title')}
          text={t('authx.expired.text')}
          actions={
            <Link to={me ? '/' : '/signin'} className="hx-btn" data-variant="primary">
              {me ? t('common.back') : t('auth.signin')}
            </Link>
          }
        />
      </AuthFrame>
    );
  }
  if (peek.kind === 'failed') {
    return (
      <AuthFrame title={t('authx.failed.title')}>
        <Note tone="danger">{peek.text}</Note>
        <Btn onClick={() => setAttempt((n) => n + 1)}>{t('authx.retry')}</Btn>
      </AuthFrame>
    );
  }
  if (peek.kind === 'loading') {
    return (
      <AuthFrame title={t('invite.titleGeneric')}>
        <div className="hxa__center" role="status">
          <Icon i="loader-circle" />
          {t('common.loading')}
        </div>
      </AuthFrame>
    );
  }

  return (
    <AuthFrame
      title={t('invite.title', { space: peek.spaceName })}
      lead={t('invite.lead', { name: peek.inviterName || t('invite.someone') })}
      footer={
        !me && (
          <p className="hxa__note">
            {t('auth.noAccount')} <Link to={`/signup?next=${next}`}>{t('auth.create')}</Link>
          </p>
        )
      }
    >
      {peek.email && <Note>{t('invite.forMail', { mail: peek.email })}</Note>}
      {error && <Note tone="danger">{error}</Note>}
      {me ? (
        <>
          <p className="hxa__note">{t('invite.asUser', { mail: me.user.email })}</p>
          <Btn variant="primary" size="lg" data-block="" busy={busy} onClick={() => void accept()}>
            {t('invite.join')}
          </Btn>
        </>
      ) : (
        <Link
          to={`/signin?next=${next}`}
          className="hx-btn"
          data-variant="primary"
          data-size="lg"
          data-block=""
        >
          {t('invite.signinJoin')}
        </Link>
      )}
    </AuthFrame>
  );
};
