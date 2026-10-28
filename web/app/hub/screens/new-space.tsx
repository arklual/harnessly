// «Пространство»: a name and, if the person wants, colleagues to invite by e-mail or by a link. With `?space=<slug>`
// the same screen edits a space that exists (the step bar and «Изменить» of the last screen lead here).
import { useEffect, useState } from 'react';

import { Button, TextField } from '@kit';

import { useApi, useEnv } from '../lib/env';
import { errorText } from '../lib/errors';
import { isEmailLike } from '../lib/format';
import { t } from '../lib/i18n';
import { navigate, safeNext, useQuery } from '../lib/router';
import { useMe, useSession } from '../lib/session';
import type { Invite, Space } from '../lib/types';
import { AccountMenu, StepFrame } from '../ui/frame';
import { Check, LinkIcon, Plus, Users } from '../ui/icons';
import { CopyBox, Label, ListSkeleton, Msg, Note } from '../ui/parts';

const PLACEHOLDERS = ['roma@company.ru', 'kirill@company.ru'];
const NAME_MAX = 60;

export const NewSpaceScreen = () => {
  const api = useApi();
  const env = useEnv();
  const me = useMe();
  const { reload, clear } = useSession();
  const query = useQuery();
  const editSlug = query.get('space');
  const back = safeNext(query.get('back'));

  const [space, setSpace] = useState<Space | null>(null);
  const [loading, setLoading] = useState(editSlug !== null);
  const [name, setName] = useState('');
  const [emails, setEmails] = useState<string[]>(['', '']);
  const [busy, setBusy] = useState<null | 'next' | 'link'>(null);
  const [error, setError] = useState<string | null>(null);
  const [nameError, setNameError] = useState<string | null>(null);
  const [linkCopied, setLinkCopied] = useState(false);
  const [unsent, setUnsent] = useState<Invite[]>([]);

  useEffect(() => {
    if (editSlug === null) return;
    let alive = true;
    void api.spaces
      .bySlug(editSlug)
      .then((r) => {
        if (!alive) return;
        setSpace(r.space);
        setName(r.space.name);
        setLoading(false);
      })
      .catch((e: unknown) => {
        if (!alive) return;
        setLoading(false);
        setError(errorText(e));
      });
    return () => {
      alive = false;
    };
  }, [api, editSlug]);

  const cleanName = name.trim();

  /** The space the screen works on: made on the first need (the link or the next step), renamed when the name moved. */
  const ensureSpace = async (): Promise<Space | null> => {
    if (cleanName === '' || cleanName.length > NAME_MAX) {
      setNameError(cleanName === '' ? t('space.nameEmpty') : t('space.nameLong', { n: NAME_MAX }));
      return null;
    }
    setNameError(null);
    if (!space) {
      const { space: made } = await api.spaces.create(cleanName);
      setSpace(made);
      await reload();
      return made;
    }
    if (space.name !== cleanName) {
      const { space: renamed } = await api.spaces.update(space.id, { name: cleanName });
      setSpace(renamed);
      await reload();
      return renamed;
    }
    return space;
  };

  const copyLink = async () => {
    setError(null);
    setBusy('link');
    try {
      const target = await ensureSpace();
      if (!target) return;
      let { invite } = await api.spaces.inviteLink(target.id, {});
      // The hub returns an active link as it is; its address is shown only when it is made.
      if (!invite.url) ({ invite } = await api.spaces.inviteLink(target.id, { days: 7 }));
      if (invite.url && (await env.copyText(invite.url))) setLinkCopied(true);
      else setError(t('space.linkNotCopied'));
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const next = async () => {
    setError(null);
    const typed = emails.map((e) => e.trim()).filter(Boolean);
    if (typed.some((e) => !isEmailLike(e))) {
      setError(t('space.badInvite'));
      return;
    }
    setBusy('next');
    try {
      const target = await ensureSpace();
      if (!target) return;
      if (typed.length > 0) {
        const { invites } = await api.spaces.invite(target.id, typed);
        setEmails(['', '']);
        const lost = invites.filter((i) => i.sent === false && i.url);
        if (lost.length > 0) {
          // No SMTP on the hub: the person gets the links to pass on, and goes on with the next press.
          setUnsent(lost);
          return;
        }
      }
      if (editSlug !== null) {
        navigate(back ?? `/s/${target.slug}/setup/machines`);
        return;
      }
      const { space: moved } = await api.spaces.update(target.id, { onboardingStep: 'machines' });
      navigate(`/s/${moved.slug}/setup/machines`);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const continueAfterLinks = async () => {
    if (!space) return;
    setBusy('next');
    try {
      if (editSlug !== null) {
        navigate(back ?? `/s/${space.slug}/setup/machines`);
        return;
      }
      const { space: moved } = await api.spaces.update(space.id, { onboardingStep: 'machines' });
      navigate(`/s/${moved.slug}/setup/machines`);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const signOut = () => {
    void env.api.auth.logout().finally(() => {
      clear();
      navigate('/signin', { replace: true });
    });
  };

  return (
    <StepFrame
      step={1}
      icon={<Users size={22} />}
      title={t('space.title')}
      lead={t('space.lead')}
      goStep={(i) => i === 0 && navigate('/settings')}
      headSide={
        <AccountMenu
          name={me.user.name || me.user.email}
          spaces={me.spaces.map((s) => ({ slug: s.slug, name: s.name, current: s.slug === space?.slug }))}
          onSignOut={signOut}
        />
      }
      foot={
        <>
          <span className="hub-spacer" />
          <Button
            appearance="primary"
            size="l"
            loading={busy === 'next'}
            onClick={() => void (unsent.length > 0 ? continueAfterLinks() : next())}
          >
            {editSlug !== null && unsent.length === 0 ? t('common.save') : t('common.next')}
          </Button>
        </>
      }
    >
      {loading ? (
        <ListSkeleton rows={2} />
      ) : (
        <>
          <TextField
            size="l"
            label={<Label required>{t('space.name')}</Label>}
            required
            aria-required="true"
            autoComplete="off"
            maxLength={NAME_MAX + 20}
            placeholder={t('space.namePlaceholder')}
            value={name}
            error={nameError ?? undefined}
            onChange={(e) => setName(e.target.value)}
          />
          <div className="hub-stack">
            <span className="hub-section__t">{t('space.invite')}</span>
            {emails.map((value, i) => (
              <TextField
                key={i}
                size="l"
                type="email"
                autoComplete="off"
                aria-label={t('space.colleagueMail')}
                placeholder={PLACEHOLDERS[i] ?? 'name@company.ru'}
                value={value}
                onChange={(e) => setEmails((list) => list.map((v, j) => (j === i ? e.target.value : v)))}
              />
            ))}
            <div className="hub-row" data-between>
              <button
                type="button"
                className="hub-add"
                onClick={() => setEmails((list) => (list.length < 20 ? [...list, ''] : list))}
              >
                <Plus size={16} />
                {t('space.another')}
              </button>
              <button
                type="button"
                className="hub-link"
                disabled={busy === 'link'}
                onClick={() => void copyLink()}
              >
                {linkCopied ? <Check size={16} /> : <LinkIcon size={16} />}
                {linkCopied ? t('space.linkCopied') : t('space.copyLink')}
              </button>
            </div>
          </div>
          {unsent.length > 0 && (
            <div className="hub-stack">
              <Msg tone="warn">{t('space.noMail')}</Msg>
              {unsent.map((i) => (
                <div key={i.id} className="hub-stack">
                  <Note>{i.email}</Note>
                  <CopyBox text={i.url ?? ''} wrap />
                </div>
              ))}
            </div>
          )}
          {error && <Msg tone="danger">{error}</Msg>}
        </>
      )}
    </StepFrame>
  );
};
