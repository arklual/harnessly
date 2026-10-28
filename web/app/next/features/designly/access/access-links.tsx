// «Ссылки», a view of the share dialog: one list of the live links (without sign-in and for the signed-in, each says
// which) and one form for a new one. «Кто откроет» chooses what the form asks: without sign-in a name, what the link
// allows and its term; for the signed-in a name, the role and its term, the link's own term and «первые N». A made link
// is copied at once (the address of an invite is only given when it is made).
import { useEffect, useMemo, useState } from 'react';
import LinkRow, { copied, itemKey, type TLinkItem } from '@/next/features/designly/access/access-link-rows';
import {
  changeGuestLink,
  createGuestLink,
  createInvite,
  fetchAccess,
  GUEST_VERSION,
  isDocRef,
  revokeGuestLink,
  revokeInvite,
  type IGuestLink,
  type IGuestLinkChange,
  type IInvite,
  type TGuestRights,
  type TGuestVersion,
  type TInviteRole,
  type TTtlValue,
} from '@/next/features/designly/access/api';
import { copyText, isAlive } from '@/next/features/designly/access/copy-link';
import { guestViewOf } from '@/next/features/designly/access/guest-view';
import { INVITE_ROLES } from '@/next/features/designly/access/labels';
import { PagesPicker, VersionPicker, type IScopePage } from '@/next/features/designly/access/link-scope';
import RoleMenu from '@/next/features/designly/access/role-menu';
import { change, type ISectionProps } from '@/next/features/designly/access/section';
import TtlPicker, { STEPS } from '@/next/features/designly/access/ttl-picker';
import { useGuestLinks } from '@/next/features/designly/access/use-guest-links';
import { useLeavingList } from '@/next/ui/presence';
import Segmented from '@/next/ui/segmented';
import { t } from '@/next/i18n';

const OUT_MS = 200;
const INVITE_STEPS = ['1h', '1d', '7d'] as const;
const GUEST_RIGHTS: readonly TGuestRights[] = ['view', 'comment'];
const NOW_MS = 30_000;
const WHO = { guest: 'guest', member: 'member' } as const;
type TWho = (typeof WHO)[keyof typeof WHO];

/** The clock, read again every so often: a link that ran out while the dialog stayed open leaves the list. */
const useNow = (every: number) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), every);
    return () => clearInterval(timer);
  }, [every]);
  return now;
};

interface IAccessLinksProps extends ISectionProps {
  /** The design's pages, to limit a new link to some of them; fewer than two and there is nothing to choose. */
  pages?: readonly IScopePage[];
  /** What a link without sign-in may allow here. */
  guestRights?: readonly TGuestRights[];
}

const NO_PAGES: readonly IScopePage[] = [];

const AccessLinks = (props: IAccessLinksProps) => {
  const { project, access, action, pages = NO_PAGES, guestRights = GUEST_RIGHTS } = props;
  const now = useNow(NOW_MS);
  const guests = useGuestLinks(project);
  const [who, setWho] = useState<TWho>(WHO.guest);
  const [title, setTitle] = useState('');
  const [rights, setRights] = useState<TGuestRights>('view');
  const [guestTtl, setGuestTtl] = useState<TTtlValue>('7d');
  // A new link shows the design as it is when the link is made: the owner keeps editing, the guest does not see it.
  const [version, setVersion] = useState<TGuestVersion>(GUEST_VERSION.pinned);
  const [scope, setScope] = useState<string[] | null>(null);
  const [role, setRole] = useState<TInviteRole>('view');
  const [roleTtl, setRoleTtl] = useState<TTtlValue>(null);
  const [inviteTtl, setInviteTtl] = useState<TTtlValue>('7d');
  const [places, setPlaces] = useState('');
  const idle = action.busy === null;
  // Without the gateway's links there is only the invite.
  // A document has the links without sign-in only: no invitations yet.
  const doc = isDocRef(project);
  const mode: TWho = doc || guests.state.k === 'ready' ? who : WHO.member;

  // The list keeps its identity between renders: the leaving list compares it by reference.
  const items = useMemo<TLinkItem[]>(
    () => [
      ...(guests.state.k === 'ready' ? guests.state.links : [])
        .filter((link) => link.revokedAt === null)
        .map((link): TLinkItem => ({ kind: 'guest', link })),
      ...(access.invites ?? [])
        .filter((invite) => isAlive(invite, now))
        .map((invite): TLinkItem => ({ kind: 'invite', invite })),
    ],
    [guests.state, access.invites, now],
  );
  const rows = useLeavingList(items, itemKey, OUT_MS);
  const links = guests.state.k === 'ready' ? guests.state.links : [];

  const makeGuest = () =>
    action.run('link', async () => {
      // The copy starts in the press (a browser keeps the permission of the click for it) and takes the address once made.
      const view = guestViewOf(project);
      const made = createGuestLink(project, {
        title: title.trim(),
        rights,
        ttl: guestTtl,
        version,
        pages: scope,
        ...(view ? { view } : {}),
      });
      const copy = copyText(made.then((link) => link.url));
      let link: IGuestLink;
      try {
        link = await made;
      } catch (failure) {
        copy.catch(() => undefined);
        throw failure;
      }
      guests.put([link, ...links]);
      setTitle('');
      await copied(copy);
    });

  const makeInvite = () => {
    const count = Number.parseInt(places, 10);
    return action.run('link', async () => {
      const made = createInvite(project, {
        title: title.trim(),
        role,
        roleTtl,
        ttl: inviteTtl,
        maxUses: Number.isInteger(count) && count > 0 ? count : null,
      });
      const copy = copyText(made.then((invite) => invite.url));
      try {
        await made;
      } catch (failure) {
        copy.catch(() => undefined);
        throw failure;
      }
      setTitle('');
      setPlaces('');
      // The list of links is the gateway's: it is read again behind the new one.
      const next = await fetchAccess(project);
      if (next) props.onNext(next);
      await copied(copy);
    });
  };

  const changeGuest = (link: IGuestLink, patch: IGuestLinkChange) =>
    void action.run(`g:${link.id}`, async () => {
      const next = await changeGuestLink(project, link.id, patch);
      guests.put(links.map((l) => (l.id === next.id ? next : l)));
    });

  const revokeGuest = (link: IGuestLink) =>
    void action.run(`g:${link.id}`, async () => {
      await revokeGuestLink(project, link.id);
      guests.put(links.map((l) => (l.id === link.id ? { ...l, revokedAt: new Date().toISOString() } : l)));
    });

  const revokeMember = (invite: IInvite) =>
    change(props, `rv:${invite.id}`, () => revokeInvite(project, invite.id));

  return (
    <div className="hx-acc-stack" data-section="invites">
      {rows.length === 0 && <p className="hx-acc-note hx-acc-note--quiet">{t('next.access.linksNone')}</p>}
      {rows.length > 0 && (
        <ul className="hx-acc-rows">
          {rows.map(({ item, leaving }) => (
            <LinkRow
              key={itemKey(item)}
              item={item}
              leaving={leaving}
              idle={idle}
              now={now}
              onGuestChange={changeGuest}
              onGuestRevoke={revokeGuest}
              onInviteRevoke={revokeMember}
              editable={!isDocRef(project)}
            />
          ))}
        </ul>
      )}
      <h3 className="hx-acc-sec__title">{t('next.access.linkNew')}</h3>
      <form
        className="hx-acc-new"
        onSubmit={(event) => {
          event.preventDefault();
          void (mode === WHO.guest ? makeGuest() : makeInvite());
        }}
      >
        {guests.state.k === 'ready' && !doc && (
          <Segmented
            label={t('next.access.linkWho')}
            value={mode}
            options={[
              { value: WHO.guest, label: t('next.access.linkWho.guest') },
              { value: WHO.member, label: t('next.access.linkWho.member') },
            ]}
            onChange={setWho}
            disabled={!idle}
          />
        )}
        <p className="hx-acc-note">
          {mode === WHO.guest ? t('next.access.guest.lead') : t('next.access.linksLead')}
        </p>
        <input
          className="hx-create__in hx-acc-in"
          type="text"
          aria-label={t('next.access.linkTitle')}
          placeholder={t('next.access.linkTitle')}
          maxLength={80}
          value={title}
          disabled={!idle}
          onChange={(event) => setTitle(event.target.value)}
        />
        <div className="hx-acc-new__opts">
          {mode === WHO.guest ? (
            <>
              <RoleMenu
                label={t('next.access.linkRole')}
                role={rights}
                roles={guestRights}
                onRole={setRights}
                disabled={!idle}
              />
              <TtlPicker
                label={t('next.access.linkTerm')}
                value={guestTtl}
                steps={STEPS}
                withDate={false}
                disabled={!idle}
                onChange={setGuestTtl}
              />
              <VersionPicker value={version} onChange={setVersion} disabled={!idle} />
              {pages.length > 1 && (
                <PagesPicker pages={pages} value={scope} onChange={setScope} disabled={!idle} />
              )}
            </>
          ) : (
            <>
              <RoleMenu
                label={t('next.access.linkRole')}
                role={role}
                roles={INVITE_ROLES}
                onRole={setRole}
                term={{ value: roleTtl, onChange: setRoleTtl }}
                showTerm
                disabled={!idle}
              />
              <TtlPicker
                label={t('next.access.linkTerm')}
                prefix={t('next.access.linkLives')}
                value={inviteTtl}
                steps={INVITE_STEPS}
                withDate={false}
                withForever={false}
                disabled={!idle}
                onChange={setInviteTtl}
              />
              <input
                className="hx-create__in hx-acc-in hx-acc-in--num"
                type="text"
                inputMode="numeric"
                aria-label={t('next.access.places')}
                placeholder={t('next.access.places')}
                value={places}
                disabled={!idle}
                onChange={(event) => setPlaces(event.target.value.replace(/\D/g, '').slice(0, 4))}
              />
            </>
          )}
        </div>
        <button
          type="submit"
          className="hx-btn hx-acc-new__go"
          data-variant="primary"
          data-size="sm"
          data-busy={action.busy === 'link'}
          disabled={!idle}
        >
          <span>{t('next.access.linkMake')}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      </form>
    </div>
  );
};

export default AccessLinks;
