import { Popover } from '@base-ui/react/popover';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import Sheet from '@/next/ui/sheet';
import { usePhone } from '@/next/ui/use-media';
import { t } from '@/next/i18n';
import { usePortalContainer } from '@/next/ui/portal';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { useDelayed as useDelayedFlag } from '@/next/ui/skeleton';
import usePendingList from '@/next/ui/use-pending-list';
import { formatAgo } from '@/next/i18n/format';
import { groupNotifications, type INotificationGroup } from '@/next/features/notifications/group';
import { LEAVE_MS, useFlip, useSwipe } from '@/next/features/notifications/motion';
import {
  NOTIFICATION_KIND,
  type INotification,
  type TNotificationKind,
} from '@/next/features/notifications/kinds';

export { NOTIFICATION_KIND, type INotification, type TNotificationKind };

const SECTION_KEY = {
  waiting: 'next.nc.waiting',
  machine: 'next.nc.machine',
  failed: 'next.nc.failed',
  done: 'next.nc.done',
} as const satisfies Record<TNotificationKind, string>;

/** What can be read and taken away: history. What waits and a machine in trouble go only when they are dealt with. */
const REMOVABLE = new Set<TNotificationKind>([NOTIFICATION_KIND.done, NOTIFICATION_KIND.failed]);

/** Rows a section shows before «Показать ещё». */
const SECTION_ROWS = 6;

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
  /** «Прочитано»: the row's events stay, dimmed. */
  onRead?: (items: readonly INotification[]) => void;
  /** «Убрать», a swipe, «Очистить»: the events leave the centre. */
  onRemove?: (items: readonly INotification[]) => void;
  onReadAll?: () => Promise<void>;
  /** The list did not come: why, and how to ask again. null while it is on its way or here. */
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  /** What waits, in its row: the question and the answer in place (wait-row.tsx). Without it the row only opens. */
  renderWait?: (item: INotification, open: boolean, toggle: () => void) => ReactNode;
}

const READ_ALL_KEY = 'read-all';

interface IRowProps {
  group: INotificationGroup;
  now: number;
  open: boolean;
  onToggle: () => void;
  onOpen: () => void;
  onRead?: () => void;
  onRemove?: () => void;
  renderWait?: INotificationCenterProps['renderWait'];
}

const Row = ({ group, now, open, onToggle, onOpen, onRead, onRemove, renderWait }: IRowProps) => {
  const rowRef = useRef<HTMLDivElement>(null);
  const removable = REMOVABLE.has(group.kind) && onRemove !== undefined;
  // «Убрать» flies the row off the same way a swipe does, then takes it away.
  const [leaving, setLeaving] = useState(false);
  useSwipe(rowRef, removable && !leaving, () => onRemove?.());
  useEffect(() => {
    if (!leaving) return;
    const timer = window.setTimeout(() => onRemove?.(), LEAVE_MS);
    return () => window.clearTimeout(timer);
  }, [leaving, onRemove]);
  const { top } = group;
  const waits = group.kind === NOTIFICATION_KIND.waiting;
  return (
    <div className="hx-nc__g" data-flip={group.key}>
      <div
        ref={rowRef}
        className="hx-nc__row"
        data-kind={group.kind}
        data-unread={group.unread}
        data-open={open || undefined}
        data-leaving={leaving || undefined}
      >
        <button type="button" className="hx-nc__hit" onClick={onOpen} aria-label={group.title} />
        <span className="hx-nc__ic">
          <Icon name={top.icon} size="sm" />
        </span>
        <span className="hx-nc__tx">
          <span className="hx-nc__t1">
            <b className="hx-nc__name">{group.title}</b>
            <span className="hx-nc__when">{formatAgo(top.at, now)}</span>
          </span>
          {waits && renderWait ? (
            renderWait(top, open, onToggle)
          ) : (
            <span className="hx-nc__gist">
              {top.text}
              {top.note && <span className="hx-nc__note"> · {top.note}</span>}
            </span>
          )}
          {group.older.length > 0 && (
            <span className="hx-nc__older">
              {group.older.map((item) => (
                <span key={item.id}>
                  <em>{formatAgo(item.at, now)}</em>
                  {item.text}
                  {item.note && ` · ${item.note}`}
                </span>
              ))}
            </span>
          )}
        </span>
        {removable && (
          <span className="hx-nc__hover">
            {group.unread && onRead && (
              <button
                type="button"
                className="hx-nc__round"
                aria-label={t('next.nc.markRead')}
                title={t('next.nc.markRead')}
                onClick={onRead}
              >
                <Icon name="check" size="sm" />
              </button>
            )}
            <button
              type="button"
              className="hx-nc__round"
              aria-label={t('next.nc.remove')}
              title={t('next.nc.remove')}
              onClick={() => setLeaving(true)}
            >
              <Icon name="x" size="sm" />
            </button>
          </span>
        )}
      </div>
    </div>
  );
};

/**
 * The notification centre, black as the smart notice: what waits for the person (answered right in its row), a machine
 * in trouble, what broke, what is ready — one row per chat, its name whole, the gist instead of «Новый ответ». A row
 * opens its chat; «Прочитано» dims it, «Убрать» or a swipe aside takes it away, «Очистить» empties a section. Rows come,
 * leave and grow with a spring (motion.ts); with nothing in it, it says «Тишина». On the phone the same list is a
 * bottom sheet.
 */
const NotificationCenter = ({
  open,
  anchor,
  side,
  items,
  now,
  onClose,
  onOpenItem,
  onRead,
  onRemove,
  onReadAll,
  failed = null,
  renderWait,
}: INotificationCenterProps) => {
  const container = usePortalContainer();
  // Focus lands on the panel, not on «Прочитать все»: opening the centre must not look like choosing an action.
  const popupRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const { pending, run } = usePendingList();
  const [expanded, setExpanded] = useState<string | null>(null);
  const [more, setMore] = useState<ReadonlySet<string>>(new Set());
  const unavailable = failed !== null && items === null;
  const showSkeleton = useDelayedFlag(open && items === null && !unavailable);
  const reading = pending.has(READ_ALL_KEY);
  const unread = items?.some((item) => item.unread && REMOVABLE.has(item.kind)) ?? false;
  const phone = usePhone();
  const sections = items === null ? [] : groupNotifications(items);
  useFlip(bodyRef, open);

  const pane = (title: ReactNode) => (
    <div className="hx-nc__pane hx-notif__pane">
      <div className="hx-nc__head">
        {title}
        {unread && onReadAll && (
          <button
            type="button"
            className="hx-nc__link"
            data-busy={reading || undefined}
            disabled={reading}
            onClick={() => void run(READ_ALL_KEY, t('next.notifications.readAllFailed'), onReadAll)}
          >
            {t('next.notifications.readAll')}
          </button>
        )}
      </div>
      <div className="hx-nc__body" ref={bodyRef}>
        {showSkeleton && (
          <div className="hx-nc__skeleton" role="status" aria-label={t('next.notifications.loading')}>
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
        {sections.map(({ kind, groups }) => {
          const all = more.has(kind);
          const shown = all ? groups : groups.slice(0, SECTION_ROWS);
          return (
            <section key={kind} className="hx-nc__sec" data-kind={kind} aria-label={t(SECTION_KEY[kind])}>
              <div className="hx-nc__label" data-flip={`sec:${kind}`}>
                <span>{t(SECTION_KEY[kind])}</span>
                {REMOVABLE.has(kind) && onRemove && (
                  <button
                    type="button"
                    className="hx-nc__clear"
                    onClick={() => onRemove(groups.flatMap((g) => g.all))}
                  >
                    {t('next.nc.clear')}
                  </button>
                )}
              </div>
              {shown.map((group) => (
                <Row
                  key={group.key}
                  group={group}
                  now={now}
                  open={expanded === group.key}
                  onToggle={() => setExpanded((was) => (was === group.key ? null : group.key))}
                  onOpen={() => onOpenItem(group.top)}
                  onRead={onRead && (() => onRead(group.all))}
                  onRemove={onRemove && (() => onRemove(group.all))}
                  renderWait={renderWait}
                />
              ))}
              {groups.length > shown.length && (
                <button
                  type="button"
                  className="hx-nc__more"
                  data-flip={`more:${kind}`}
                  onClick={() => setMore((was) => new Set(was).add(kind))}
                >
                  {t('next.nc.showMore')}
                </button>
              )}
            </section>
          );
        })}
        {items !== null && items.length === 0 && (
          <div className="hx-nc__empty">
            <span className="hx-nc__empty-ic">
              <Icon name="bell" />
            </span>
            <b>{t('next.notifications.silence')}</b>
            <span>{t('next.notifications.silenceNote')}</span>
          </div>
        )}
      </div>
    </div>
  );

  if (phone) {
    return (
      <Sheet open={open} onClose={onClose} label={t('next.notifications.title')} className="hx-nc-sheet">
        {pane(<h2 className="hx-nc__title">{t('next.notifications.title')}</h2>)}
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
          align="end"
          sideOffset={side === 'right' ? 10 : 6}
          collisionPadding={8}
        >
          <Popover.Popup
            ref={popupRef}
            initialFocus={popupRef}
            className="hx-nc"
            aria-label={t('next.notifications.title')}
          >
            {pane(<Popover.Title className="hx-nc__title">{t('next.notifications.title')}</Popover.Title>)}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
};

export default NotificationCenter;
