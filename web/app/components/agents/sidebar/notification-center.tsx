import { useContext, useId, useMemo, useRef, useState, type KeyboardEvent, type Ref } from 'react';
import { Bell, BellRing, CheckCheck, Check, Settings2 } from 'lucide-react';
import { toast } from 'sonner';
import { useShallow } from 'zustand/react/shallow';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  LAYER_ALIGN,
  NAV_ORIENTATION,
  TwButton,
  TwPopover,
  cx,
  nextIndex,
  twPopoverTriggerProps,
} from '@/components/agents/ui';
import PushSettings, { PUSH_STATE_LABEL } from '@/components/agents/notification-control';
import { RowLead, STATUS_LABEL } from '@/components/agents/sidebar/conversation-row';
import { useSidebarContext } from '@/components/agents/sidebar/sidebar-context';
import useMinuteClock from '@/components/agents/sidebar/use-minute-clock';
import useTabStore from '@/hooks/use-tab-store';
import useFleetStatus, { RuntimeMachineContext } from '@/hooks/use-fleet-status';
import { dismissTab } from '@/hooks/use-agent-status';
import { dismissRemoteTab } from '@/hooks/use-remote-machine-statuses';
import { usePushStore } from '@/hooks/use-web-push';
import { cleanConversationTitle, type Conversation } from '@/lib/hl-conversations';
import { compactAge, machineTag } from '@/lib/hl-compact-time';
import {
  NOTICE_GROUP,
  NOTICE_GROUP_LABELS,
  composeNotices,
  noticeDetailOf,
  type INotice,
  type INoticeDetail,
  type TNoticeGroup,
} from '@/lib/hl-notices';
import { t } from '@/lib/i18n';

const READ_KEY = 'hl:notices-read';
const GROUP_ORDER: readonly TNoticeGroup[] = [
  NOTICE_GROUP.waiting,
  NOTICE_GROUP.finished,
  NOTICE_GROUP.failed,
];
const VIEW = { list: 'list', push: 'push' } as const;

const EMPTY_TEXT: Record<TNoticeGroup, string> = {
  waiting: t('sidebar.notificationCenter.emptyWaiting'),
  finished: t('sidebar.notificationCenter.emptyFinished'),
  failed: t('sidebar.notificationCenter.emptyFailed'),
};

/** Read marks of this device for turns the runtime cannot dismiss (a failure already idle): ref → time of the turn read. */
const loadReadMarks = (): Record<string, number> => {
  try {
    return JSON.parse(localStorage.getItem(READ_KEY) || '{}') as Record<string, number>;
  } catch {
    return {};
  }
};

const saveReadMarks = (marks: Record<string, number>) => {
  try {
    localStorage.setItem(READ_KEY, JSON.stringify(marks));
  } catch {}
};

/** What the bell quotes for every agent conversation: the local tab store here, the status sockets for other machines. */
const useNoticeDetails = (
  conversations: readonly Conversation[],
  native: string | undefined,
): Record<string, INoticeDetail> => {
  const agents = useMemo(() => conversations.filter((item) => item.kind === 'agent'), [conversations]);
  const localTabs = useTabStore(
    useShallow((s) =>
      Object.fromEntries(
        agents
          .filter((item) => !item.machineId || item.machineId === native)
          .map((item) => [item.key, s.tabs[item.id]]),
      ),
    ),
  );
  const remote = useFleetStatus((s) => s.details);
  return useMemo(() => {
    const details: Record<string, INoticeDetail> = {};
    for (const item of agents) {
      const local = localTabs[item.key];
      const detail = local ? noticeDetailOf(local) : remote[item.machineId ?? '']?.[item.id];
      if (detail) details[item.key] = detail;
    }
    return details;
  }, [agents, localTabs, remote]);
};

interface INoticeItemProps {
  notice: INotice;
  now: number;
  showMachine: boolean;
  onOpen: (notice: INotice) => void;
  onRead: (notice: INotice) => void;
  mainRef?: Ref<HTMLButtonElement>;
}

const NoticeItem = ({ notice, now, showMachine, onOpen, onRead, mainRef }: INoticeItemProps) => {
  const { conversation, status, text, unread, group } = notice;
  const title = cleanConversationTitle(conversation.title);
  const machine = showMachine && conversation.machineName ? conversation.machineName : null;
  return (
    <li className={cx('tw-notice', unread && 'tw-notice-unread')} data-group={group}>
      <button
        ref={mainRef}
        type="button"
        className="tw-notice-main"
        data-notice-item
        onClick={() => onOpen(notice)}
        aria-label={[title, STATUS_LABEL[status], unread && t('sidebar.notificationCenter.unread')]
          .filter(Boolean)
          .join(', ')}
      >
        <RowLead provider={conversation.provider} status={status} label={undefined} />
        <span className="tw-notice-body">
          <span className="tw-notice-top">
            <span className="tw-notice-title">{title}</span>
            {machine && (
              <span className="tw-srow-machine" title={t('sidebar.notificationCenter.machine', { machine })}>
                {machineTag(machine)}
              </span>
            )}
            <time className="tw-notice-time" dateTime={new Date(notice.at).toISOString()}>
              {compactAge(notice.at, now)}
            </time>
          </span>
          <span className="tw-notice-text">{text ?? EMPTY_TEXT[group]}</span>
        </span>
        {unread && <span className="tw-notice-dot" aria-hidden="true" />}
      </button>
      <span className="tw-notice-actions">
        {group === NOTICE_GROUP.waiting && (
          <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.primary} onClick={() => onOpen(notice)}>
            {t('sidebar.notificationCenter.reply')}
          </TwButton>
        )}
        {group !== NOTICE_GROUP.waiting && unread && (
          <button
            type="button"
            className="tw-icon-button tw-notice-read"
            aria-label={t('sidebar.notificationCenter.readLabel', { title })}
            title={t('sidebar.notificationCenter.markRead')}
            onClick={() => onRead(notice)}
          >
            <Check />
          </button>
        )}
      </span>
    </li>
  );
};

/** Arrow keys walk the items from anywhere in the popover, Home and End jump to the ends; Tab still reaches the buttons of an item. */
const walkItems = (event: KeyboardEvent<HTMLElement>) => {
  const items = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[data-notice-item]')];
  const current = items.indexOf(document.activeElement as HTMLButtonElement);
  const next = nextIndex(current, event.key, {
    count: items.length,
    orientation: NAV_ORIENTATION.vertical,
    loop: true,
  });
  if (next === null) return;
  event.preventDefault();
  items[next]?.focus();
};

interface INotificationCenterProps {
  conversations: Conversation[];
}

/**
 * The bell: one place for what agents want from the person. The count is only «Waiting for you»; the list also keeps
 * finished and failed turns, unread first. Opening an item opens the conversation and marks it read.
 */
const NotificationCenter = ({ conversations }: INotificationCenterProps) => {
  const context = useSidebarContext();
  const native = useContext(RuntimeMachineContext);
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<(typeof VIEW)[keyof typeof VIEW]>(VIEW.list);
  const [readMarks, setReadMarks] = useState<Record<string, number>>(loadReadMarks);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const firstItemRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const pushState = usePushStore((s) => s.state);
  const now = useMinuteClock();
  const details = useNoticeDetails(conversations, native);

  const groups = useMemo(() => {
    const composed = composeNotices(conversations, context.statuses, details, now);
    const unmark = (notice: INotice): INotice =>
      notice.unread && notice.group !== NOTICE_GROUP.waiting && (readMarks[notice.key] ?? 0) >= notice.at
        ? { ...notice, unread: false }
        : notice;
    return {
      waiting: composed.waiting,
      finished: composed.finished.map(unmark),
      failed: composed.failed.map(unmark),
    };
  }, [conversations, context.statuses, details, now, readMarks]);

  const waiting = groups.waiting.length;
  const unreadRest = [...groups.finished, ...groups.failed].filter((notice) => notice.unread);
  const total = waiting + groups.finished.length + groups.failed.length;
  const first = GROUP_ORDER.map((group) => groups[group][0]).find(Boolean) ?? null;
  const showMachine =
    new Set(conversations.filter((item) => item.kind === 'agent').map((item) => item.machineId ?? native))
      .size > 1;
  const pushInvite = pushState === 'off' || pushState === 'needs-install';

  const markRead = (notices: INotice[]) => {
    const marks = { ...readMarks };
    const away: string[] = [];
    for (const notice of notices) {
      marks[notice.key] = notice.at;
      const { conversation } = notice;
      if (notice.status !== 'ready') continue;
      if (!conversation.machineId || conversation.machineId === native) dismissTab(conversation.id);
      else if (!dismissRemoteTab(conversation.machineId, conversation.id))
        away.push(conversation.machineName ?? conversation.machineId);
    }
    setReadMarks(marks);
    saveReadMarks(marks);
    if (away.length)
      toast.error(t('sidebar.notificationCenter.markFailed', { machines: [...new Set(away)].join(', ') }));
  };

  const openNotice = (notice: INotice) => {
    if (notice.group !== NOTICE_GROUP.waiting) markRead([notice]);
    setOpen(false);
    context.onSelect(notice.conversation);
  };

  const close = () => {
    setOpen(false);
    setView(VIEW.list);
  };

  const Icon = waiting > 0 ? BellRing : Bell;

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className={cx(
          'tw-icon-button tw-shead-bell',
          waiting === 0 && pushInvite && 'tw-notify-invite',
          waiting > 0 && 'tw-shead-bell-waiting',
        )}
        aria-label={
          waiting
            ? t('sidebar.notificationCenter.triggerWaiting', { count: waiting })
            : t('sidebar.notificationCenter.triggerIdle', { push: PUSH_STATE_LABEL[pushState] })
        }
        title={
          waiting
            ? t('sidebar.notificationCenter.waitingTitle', { count: waiting })
            : t('sidebar.notificationCenter.title')
        }
        {...twPopoverTriggerProps(open, popoverId)}
        onClick={() => (open ? close() : setOpen(true))}
      >
        <Icon />
        {waiting > 0 && <span className="tw-shead-badge">{waiting}</span>}
      </button>
      <TwPopover
        id={popoverId}
        open={open}
        onClose={close}
        anchorRef={triggerRef}
        align={LAYER_ALIGN.end}
        aria-label={t('sidebar.notificationCenter.title')}
        className="tw-notices"
        initialFocusRef={view === VIEW.list && first ? firstItemRef : undefined}
      >
        {view === VIEW.push && <PushSettings onBack={() => setView(VIEW.list)} />}
        {view === VIEW.list && (
          <div className="tw-notices-view" onKeyDown={walkItems}>
            <header className="tw-notices-head">
              <strong>{t('sidebar.notificationCenter.title')}</strong>
              <span className="tw-notices-spacer" />
              <button
                type="button"
                className="tw-notices-readall"
                disabled={!unreadRest.length}
                onClick={() => markRead(unreadRest)}
              >
                <CheckCheck aria-hidden="true" /> {t('sidebar.notificationCenter.readAll')}
              </button>
              <button
                type="button"
                className="tw-icon-button tw-notices-settings"
                aria-label={t('sidebar.notificationCenter.pushLabel', { state: PUSH_STATE_LABEL[pushState] })}
                title={t('sidebar.notificationCenter.pushTitle')}
                onClick={() => setView(VIEW.push)}
              >
                <Settings2 />
                {pushInvite && <span className="tw-notices-settings-dot" aria-hidden="true" />}
              </button>
            </header>
            {total === 0 && (
              <div className="tw-notices-empty">
                <span className="tw-notices-empty-icon">
                  <Bell aria-hidden="true" />
                </span>
                <strong>{t('sidebar.notificationCenter.quiet')}</strong>
                <p>{t('sidebar.notificationCenter.emptyList')}</p>
              </div>
            )}
            {total > 0 && (
              <div className="tw-notices-list">
                {GROUP_ORDER.filter((group) => groups[group].length).map((group) => (
                  <section key={group} className="tw-notices-group" aria-label={NOTICE_GROUP_LABELS[group]}>
                    <h3 className="tw-notices-group-title" data-group={group}>
                      {NOTICE_GROUP_LABELS[group]}
                      <span>{groups[group].length}</span>
                    </h3>
                    <ul>
                      {groups[group].map((notice) => (
                        <NoticeItem
                          key={notice.key}
                          notice={notice}
                          now={now}
                          showMachine={showMachine}
                          onOpen={openNotice}
                          onRead={(one) => markRead([one])}
                          mainRef={notice === first ? firstItemRef : undefined}
                        />
                      ))}
                    </ul>
                  </section>
                ))}
              </div>
            )}
          </div>
        )}
      </TwPopover>
    </>
  );
};

export default NotificationCenter;
