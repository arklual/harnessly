import { memo, useContext, useRef, type KeyboardEvent, type MouseEvent } from 'react';
import { CloudOff, MoreHorizontal } from 'lucide-react';
import ProviderIcon, { PROVIDER_LABELS, type TProviderStatus } from '@/components/agents/provider-icon';
import { LAYER_ALIGN, TwKbd, TwMenuButton, TwSpinner, cx, useTwContextMenu } from '@/components/agents/ui';
import ProjectTile from '@/components/agents/sidebar/project-tile';
import StatusOrb from '@/components/agents/status-orb';
import { backgroundSummary } from '@/components/agents/background-chip';
import useConversationBackground from '@/components/agents/sidebar/use-conversation-background';
import useMinuteClock from '@/components/agents/sidebar/use-minute-clock';
import { conversationMenuItems } from '@/components/agents/sidebar/menus';
import { useModKey } from '@/components/agents/sidebar/organizer-actions';
import { useSidebarActions, useSidebarRowState } from '@/components/agents/sidebar/sidebar-context';
import usePauseOffscreen from '@/hooks/use-pause-offscreen';
import { RuntimeMachineContext } from '@/hooks/use-fleet-status';
import { cleanConversationTitle, parseConversationRef, type Conversation } from '@/lib/hl-conversations';
import { compactAge, machineTag } from '@/lib/hl-compact-time';
import { conversationHref } from '@/lib/hl-conversation-url';
import { ROW_STATE, type IRowModel } from '@/lib/hl-sidebar-model';
import type { TConversationRef } from '@/lib/hl-organizer';
import { t } from '@/lib/i18n';

export const STATUS_LABEL = {
  idle: '',
  working: t('sidebar.conversationRow.statusWorking'),
  'needs-input': t('sidebar.conversationRow.statusNeedsInput'),
  ready: t('sidebar.conversationRow.statusReady'),
  done: t('sidebar.conversationRow.statusDone'),
  failed: t('sidebar.conversationRow.statusFailed'),
  gone: t('sidebar.conversationRow.statusGone'),
} as const;

const placeholderTitle = (row: IRowModel) => {
  if (row.conversation) return cleanConversationTitle(row.conversation.title);
  if (parseConversationRef(row.ref)?.kind === 'chat') return t('sidebar.conversationRow.chat');
  return row.machineName
    ? t('sidebar.conversationRow.conversationOn', { machine: row.machineName })
    : t('sidebar.conversationRow.conversation');
};

export const STATE_NOTE: Partial<Record<IRowModel['state'], (row: IRowModel) => string>> = {
  [ROW_STATE.machineUnavailable]: (row) =>
    t('sidebar.conversationRow.machineUnavailable', { machine: row.machineName ?? '' }),
  [ROW_STATE.machineRemoved]: (row) =>
    t('sidebar.conversationRow.machineRemoved', { machine: row.machineName ?? '' }),
  [ROW_STATE.chatMissing]: () => t('sidebar.conversationRow.chatMissing'),
  [ROW_STATE.chatError]: () => t('sidebar.conversationRow.chatError'),
};

/** States drawn as Claude Desktop's tile in place of the agent mark; working turns the mark itself (no spinner, no orbit). */
const TILE_STATUSES: ReadonlySet<TProviderStatus> = new Set([
  'needs-input',
  'ready',
  'done',
  'failed',
  'gone',
]);

/** As in Claude Desktop, a shell or monitor that still runs keeps the conversation working, unless it waits for you. */
export const shownStatus = (status: TProviderStatus, background: number): TProviderStatus =>
  background > 0 && status !== 'needs-input' ? 'working' : status;

interface IRowLeadProps {
  provider: string | null;
  status: TProviderStatus;
  label: string | undefined;
}

/** The one icon slot of a row: the agent mark, turning while it works, or its status tile. */
export const RowLead = ({ provider, status, label }: IRowLeadProps) => {
  if (TILE_STATUSES.has(status) || !provider)
    return (
      <span className="tw-srow-lead">
        <StatusOrb status={status} label={label} />
      </span>
    );
  return (
    <span className="tw-srow-lead" title={label}>
      <ProviderIcon provider={provider} status={status} size="sm" />
    </span>
  );
};

/** The row's age on the shared clock; only this label re-renders every tick, not the row and its menus. */
const RowAge = ({ at }: { at: number }) => {
  const now = useMinuteClock();
  return (
    <time className="tw-srow-time" dateTime={new Date(at).toISOString()}>
      {compactAge(at, now)}
    </time>
  );
};

interface IConversationRowProps {
  /** A row of the model (pins, projects) or a plain list entry (Conversations). */
  row?: IRowModel;
  item?: Conversation;
  /** Pinned rows show which project they belong to. */
  showProject?: boolean;
  nested?: boolean;
}

const ConversationRow = ({ row, item, showProject = false, nested = false }: IConversationRowProps) => {
  const actions = useSidebarActions();
  const modKey = useModKey();
  const rowRef = useRef<HTMLDivElement>(null);
  const conversation = row ? row.conversation : (item ?? null);
  const ref = (row?.ref ?? item?.key) as TConversationRef;
  const live = useSidebarRowState(ref);
  const { pending, active } = live;
  const state = row?.state ?? ROW_STATE.ok;
  const title = row ? placeholderTitle(row) : cleanConversationTitle(item?.title ?? '');
  const note = STATE_NOTE[state]?.(row!);
  const inactive = state !== ROW_STATE.ok && state !== ROW_STATE.loading;
  const menuItems = conversationMenuItems(ref, actions, live.status, row, conversation);
  const contextMenu = useTwContextMenu({
    items: menuItems,
    'aria-label': t('sidebar.conversationRow.actionsFor', { title }),
  });
  const provider = conversation ? conversation.provider : null;
  const machineName = conversation?.machineName ?? row?.machineName ?? null;
  const hotkey = row?.hotkey ?? null;
  const background = useConversationBackground(conversation);
  const status = shownStatus(live.status, background.length);
  usePauseOffscreen(rowRef, status === 'working');
  const homeMachineId = useContext(RuntimeMachineContext);
  // The home machine goes without saying; a tag appears only on rows of another machine.
  const elsewhere = Boolean(
    homeMachineId && conversation?.machineId && conversation.machineId !== homeMachineId && machineName,
  );
  const leadLabel =
    [
      STATUS_LABEL[status],
      background.length > 0 &&
        t('sidebar.conversationRow.inBackground', { summary: backgroundSummary(background) }),
    ]
      .filter(Boolean)
      .join(' · ') || undefined;

  const open = (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    if (row) actions.onOpenRow(row);
    else if (item) actions.onSelect(item);
  };
  const pressSpace = (event: KeyboardEvent<HTMLAnchorElement>) => {
    if (event.key !== ' ') return;
    event.preventDefault();
    event.currentTarget.click();
  };

  return (
    <div
      ref={rowRef}
      {...contextMenu.bind}
      className={cx(
        'tw-row tw-srow',
        nested && 'tw-srow-nested',
        active && 'tw-row-active',
        status !== 'idle' && `tw-row-${status}`,
        inactive && 'tw-srow-inactive',
        state === ROW_STATE.loading && 'tw-srow-loading',
        (contextMenu.open || pending) && 'tw-srow-held',
      )}
    >
      <a
        href={conversationHref(ref)}
        className="tw-srow-main"
        onClick={open}
        onKeyDown={pressSpace}
        aria-current={active ? 'page' : undefined}
        aria-keyshortcuts={hotkey ? `Meta+${hotkey} Control+${hotkey}` : undefined}
        title={[title, note, provider && (PROVIDER_LABELS[provider] ?? provider), leadLabel, machineName]
          .filter(Boolean)
          .join(' · ')}
      >
        <RowLead provider={provider} status={status} label={leadLabel} />
        <span className="tw-row-title">{title}</span>
        {elsewhere && (
          <span
            className="tw-srow-machine"
            title={t('sidebar.conversationRow.machine', { machine: machineName ?? '' })}
          >
            {machineTag(machineName!)}
          </span>
        )}
        {showProject && row?.project && (
          <ProjectTile project={row.project} size="sm" className="tw-srow-project" />
        )}
        <span className="tw-srow-meta">
          {note && (
            <span className="tw-srow-note" role="img" aria-label={note}>
              <CloudOff aria-hidden="true" />
            </span>
          )}
          {!note && Boolean(conversation?.updatedAt) && <RowAge at={conversation!.updatedAt} />}
          {hotkey !== null && (
            <TwKbd className="tw-srow-kbd">
              {modKey}
              {hotkey}
            </TwKbd>
          )}
        </span>
      </a>
      <span className="tw-srow-actions">
        {pending ? (
          <span className="tw-srow-spinner">
            <TwSpinner label={t('sidebar.conversationRow.saving')} />
          </span>
        ) : (
          <TwMenuButton
            items={menuItems}
            menuLabel={t('sidebar.conversationRow.actionsFor', { title })}
            align={LAYER_ALIGN.end}
            className="tw-srow-more"
            aria-label={t('sidebar.conversationRow.actionsFor', { title })}
            title={t('sidebar.conversationRow.actions')}
          >
            <MoreHorizontal />
          </TwMenuButton>
        )}
      </span>
      {contextMenu.menu}
    </div>
  );
};

const sameRow = (a: IRowModel | undefined, b: IRowModel | undefined) =>
  a === b ||
  (!!a &&
    !!b &&
    a.ref === b.ref &&
    a.conversation === b.conversation &&
    a.state === b.state &&
    a.hotkey === b.hotkey &&
    a.project === b.project &&
    a.machineName === b.machineName);

/** The model is rebuilt on every status change; a row whose own fields did not move skips its render and its menus. */
export default memo(
  ConversationRow,
  (prev, next) =>
    sameRow(prev.row, next.row) &&
    prev.item === next.item &&
    prev.showProject === next.showProject &&
    prev.nested === next.nested,
);
