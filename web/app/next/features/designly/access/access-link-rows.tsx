// The rows of «Ссылки»: a link without sign-in (copy again, a menu to extend, to change what it allows and to revoke) and
// an invite link (revoke only: its address is given when it is made). Each says who it opens for.
import { useState } from 'react';
import type { IGuestLink, IInvite, TGuestRights, TTtlValue } from '@/next/features/designly/access/api';
import { copyText } from '@/next/features/designly/access/copy-link';
import { canLabel, untilLabel } from '@/next/features/designly/access/labels';
import { dayLabel, ttlLabel } from '@/next/features/designly/access/ttl-picker';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export const EXTEND = '7d';

export type TLinkItem = { kind: 'guest'; link: IGuestLink } | { kind: 'invite'; invite: IInvite };
export const itemKey = (item: TLinkItem): string =>
  item.kind === 'guest' ? `g:${item.link.id}` : `i:${item.invite.id}`;

export const copied = (copy: Promise<void>) =>
  copy.then(
    () => toast(t('next.access.copied')),
    (failure: Error) => toast(t('next.site.copyFailed'), { tone: 'error', description: failure.message }),
  );

const guestTerm = (link: IGuestLink, now: number): string => {
  if (link.expiresAt === null) return t('next.access.ttl.forever');
  if (new Date(link.expiresAt).getTime() <= now) return t('next.access.guest.ran');
  return untilLabel(link.expiresAt) ?? '';
};

const inviteUntil = (iso: string): string => {
  const day = dayLabel(iso);
  return day === null ? '' : t('next.access.linkUntil', { date: day });
};

interface IRowProps {
  item: TLinkItem;
  leaving: boolean;
  idle: boolean;
  now: number;
  onGuestChange: (link: IGuestLink, change: { ttl?: TTtlValue; rights?: TGuestRights }) => void;
  onGuestRevoke: (link: IGuestLink) => void;
  onInviteRevoke: (invite: IInvite) => void;
}

const LinkRow = ({ item, leaving, idle, now, onGuestChange, onGuestRevoke, onInviteRevoke }: IRowProps) => {
  const [open, setOpen] = useState(false);
  const label = t('next.access.guest.actions');
  const pick = (run: () => void) => {
    setOpen(false);
    run();
  };
  const guest = item.kind === 'guest' ? item.link : null;
  const invite = item.kind === 'invite' ? item.invite : null;
  const name = guest
    ? guest.title || t('next.access.guest.untitled')
    : (invite?.title ?? '') || canLabel(invite?.role ?? 'view');
  const sub = guest
    ? [t('next.access.linkKind.guest'), canLabel(guest.rights), guestTerm(guest, now)]
    : invite
      ? [
          t('next.access.linkKind.member'),
          [canLabel(invite.role), ttlLabel(invite.roleTtl)].filter(Boolean).join(', '),
          inviteUntil(invite.expiresAt),
          invite.maxUses !== null ? t('next.access.firstN', { n: invite.maxUses }) : '',
        ]
      : [];
  const other: TGuestRights = guest?.rights === 'view' ? 'comment' : 'view';

  return (
    <li className="hx-acc-row" data-leaving={leaving || undefined}>
      <span className="hx-acc-av" aria-hidden="true">
        <Icon name="link" size="sm" />
      </span>
      <span className="hx-acc-row__who">
        <span className="hx-acc-row__nm">{name}</span>
        <span className="hx-acc-row__sub">{sub.filter(Boolean).join(' · ')}</span>
      </span>
      {guest && (
        <>
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            disabled={leaving}
            onClick={() => void copied(copyText(Promise.resolve(guest.url)))}
          >
            {t('next.access.copy')}
          </button>
          <Popover
            open={open}
            onOpenChange={setOpen}
            label={label}
            side="bottom"
            align="end"
            className="hx-acc-pop"
            trigger={
              <button type="button" className="hx-head-btn" aria-label={label} disabled={leaving}>
                <Icon name="ellipsis" />
              </button>
            }
          >
            <div role="menu" aria-label={label} className="hx-acc-menu">
              <button
                type="button"
                role="menuitem"
                className="hx-acc-mi"
                disabled={!idle}
                onClick={() => pick(() => onGuestChange(guest, { ttl: EXTEND }))}
              >
                <Icon name="clock" size="sm" className="hx-acc-mi__icon" />
                <span className="hx-acc-mi__text">{t('next.access.guest.extend')}</span>
              </button>
              <button
                type="button"
                role="menuitem"
                className="hx-acc-mi"
                disabled={!idle}
                onClick={() => pick(() => onGuestChange(guest, { rights: other }))}
              >
                <span className="hx-acc-mi__text">{t(`next.access.guest.make.${other}`)}</span>
              </button>
              <span className="hx-acc-mi__sep" role="separator" />
              <button
                type="button"
                role="menuitem"
                className="hx-acc-mi"
                data-danger="true"
                disabled={!idle}
                onClick={() => pick(() => onGuestRevoke(guest))}
              >
                <span className="hx-acc-mi__text">{t('next.access.revoke')}</span>
              </button>
            </div>
          </Popover>
        </>
      )}
      {invite && (
        <button
          type="button"
          className="hx-btn"
          data-variant="ghost"
          data-size="sm"
          disabled={!idle || leaving}
          onClick={() => onInviteRevoke(invite)}
        >
          {t('next.access.revoke')}
        </button>
      )}
    </li>
  );
};

export default LinkRow;
