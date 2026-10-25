import { Popover } from '@base-ui/react/popover';
import { useRef, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import Sheet from '@/next/ui/sheet';
import { usePhone } from '@/next/ui/use-media';
import { t } from '@/next/i18n';
import { usePortalContainer } from '@/next/ui/portal';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { useDelayed as useDelayedFlag } from '@/next/ui/skeleton';
import usePendingList from '@/next/ui/use-pending-list';
import { formatAgo } from '@/next/i18n/format';

export const NOTIFICATION_KIND = {
  machine: 'machine',
  waiting: 'waiting',
  done: 'done',
  failed: 'failed',
} as const;
export type TNotificationKind = (typeof NOTIFICATION_KIND)[keyof typeof NOTIFICATION_KIND];

const KIND_LOOK: Record<TNotificationKind, { label: string; tone: string }> = {
  machine: { label: t('next.notifications.machines'), tone: 'bad' },
  waiting: { label: t('next.notifications.waiting'), tone: 'warn' },
  done: { label: t('next.notifications.done'), tone: 'ok' },
  failed: { label: t('next.notifications.failed'), tone: 'bad' },
};

const KINDS: readonly TNotificationKind[] = [
  NOTIFICATION_KIND.machine,
  NOTIFICATION_KIND.waiting,
  NOTIFICATION_KIND.done,
  NOTIFICATION_KIND.failed,
];

export interface INotification {
  id: string;
  kind: TNotificationKind;
  icon: string;
  /** The chat's title, or the machine's name. */
  title: string;
  /** What happened: the question, «Готово · 4 мин», the failure. */
  text: string;
  at: number;
  unread: boolean;
  /** The chat a row opens; null for a machine's row, which opens the machine's card. */
  chatId: string | null;
}

export interface INotificationCenterProps {
  open: boolean;
  /** The button it opened from: the sidebar item (the card goes to its right) or the header bell (under it). */
  anchor: HTMLElement | null;
  side: 'right' | 'bottom';
  /** null while it loads. */
  items: readonly INotification[] | null;
  now: number;
  onClose: () => void;
  onOpenItem: (item: INotification) => void;
  onReadAll?: () => Promise<void>;
  /** The list did not come: why, and how to ask again. null while it is on its way or here. */
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
}

const READ_ALL_KEY = 'read-all';

/**
 * The notification centre: a machine about to fail its work, what waits for the person (questions, permissions), what
 * finished, what failed. A row opens its chat (a machine's row its card); «Прочитать все» waits for the server; with nothing in it, it says «Тишина».
 * On the phone the same list is a bottom sheet (ui/sheet.tsx) with finger-sized rows; each section says how many it holds.
 */
const NotificationCenter = ({
  open,
  anchor,
  side,
  items,
  now,
  onClose,
  onOpenItem,
  onReadAll,
  failed = null,
}: INotificationCenterProps) => {
  const container = usePortalContainer();
  // Focus lands on the panel, not on «Прочитать все»: opening the centre must not look like choosing an action.
  const popupRef = useRef<HTMLDivElement>(null);
  const { pending, run } = usePendingList();
  const unavailable = failed !== null && items === null;
  const showSkeleton = useDelayedFlag(open && items === null && !unavailable);
  const reading = pending.has(READ_ALL_KEY);
  const unread = items?.some((item) => item.unread) ?? false;
  const phone = usePhone();

  const pane = (title: ReactNode) => (
    <div className="hx-notif__pane">
      <div className="hx-notif__head">
        {title}
        <span className="hx-spacer" />
        {unread && onReadAll && (
          <button
            type="button"
            className="hx-notif__link"
            data-busy={reading || undefined}
            disabled={reading}
            onClick={() => void run(READ_ALL_KEY, t('next.notifications.readAllFailed'), onReadAll)}
          >
            {t('next.notifications.readAll')}
          </button>
        )}
      </div>
      <div className="hx-notif__body">
        {showSkeleton && (
          <div className="hx-notif__sk" role="status" aria-label={t('next.notifications.loading')}>
            <span className="hx-sk" />
            <span className="hx-sk" />
            <span className="hx-sk" />
          </div>
        )}
        {unavailable && (
          <LoadFailed
            size={LOAD_FAILED_SIZE.pane}
            title={t('next.load.notifications')}
            reason={failed.reason}
            onRetry={failed.onRetry}
          />
        )}
        {items !== null &&
          KINDS.map((kind) => {
            const ofKind = items.filter((item) => item.kind === kind);
            if (ofKind.length === 0) return null;
            return (
              <div key={kind} className="hx-notif__section" data-kind={kind}>
                <div className="hx-notif__label">
                  {KIND_LOOK[kind].label}
                  <span className="hx-notif__count">{ofKind.length}</span>
                </div>
                {ofKind.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    className="hx-notif__row"
                    data-unread={item.unread}
                    data-tone={KIND_LOOK[kind].tone}
                    onClick={() => onOpenItem(item)}
                  >
                    <Icon name={item.icon} size="sm" className="hx-notif__ic" />
                    <span className="hx-notif__main">
                      <span className="hx-notif__name">{item.title}</span>
                      <span className="hx-notif__text">{item.text}</span>
                    </span>
                    <span className="hx-notif__when">{formatAgo(item.at, now)}</span>
                  </button>
                ))}
              </div>
            );
          })}
        {items !== null && items.length === 0 && (
          <div className="hx-notif__empty">
            <Icon name="bell" />
            <b>{t('next.notifications.silence')}</b>
            {t('next.notifications.silenceNote')}
          </div>
        )}
      </div>
    </div>
  );

  if (phone) {
    return (
      <Sheet open={open} onClose={onClose} label={t('next.notifications.title')} className="hx-notif-sheet">
        {pane(<h2 className="hx-notif__title">{t('next.notifications.title')}</h2>)}
      </Sheet>
    );
  }

  return (
    <Popover.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <Popover.Portal container={container}>
        <Popover.Positioner
          className="hx-notif-pos"
          anchor={anchor}
          side={side}
          align={side === 'right' ? 'start' : 'end'}
          sideOffset={side === 'right' ? 10 : 6}
          collisionPadding={8}
        >
          <Popover.Popup
            ref={popupRef}
            initialFocus={popupRef}
            className="hx-notif"
            aria-label={t('next.notifications.title')}
          >
            {pane(<Popover.Title className="hx-notif__title">{t('next.notifications.title')}</Popover.Title>)}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
};

export default NotificationCenter;
