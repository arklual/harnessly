import { useSyncExternalStore, type ReactElement } from 'react';
import {
  ChartColumnBig,
  Monitor,
  Palette,
  Moon,
  PanelLeftOpen,
  Plus,
  Search,
  Server,
  Sun,
  TrendingUp,
} from 'lucide-react';
import ProviderIcon from '@/components/agents/provider-icon';
import { THEME_LABELS, initials, type TThemeMode } from '@/components/agents/conversation-sidebar';
import { LAYER_SIDE, TW_MENU_KIND, TwMenuButton, cx, type TTwMenuEntry } from '@/components/agents/ui';
import ProjectTile from '@/components/agents/sidebar/project-tile';
import { newConversationMenuItems } from '@/components/agents/sidebar/menus';
import { useModKey } from '@/components/agents/sidebar/organizer-actions';
import { useSidebarContext, type ISidebarContext } from '@/components/agents/sidebar/sidebar-context';
import { cleanConversationTitle, type Conversation } from '@/lib/hl-conversations';
import { RAIL_TOOLTIP_KIND, railTooltipOf, type TRailTooltipInput } from '@/lib/hl-rail-tooltip';
import type { IProjectModel, IRowModel, ISidebarModel } from '@/lib/hl-sidebar-model';
import StatusOrb from '@/components/agents/status-orb';
import RailTooltip from '@/components/agents/rail-tooltip';
import { STATE_NOTE, shownStatus } from '@/components/agents/sidebar/conversation-row';
import useMinuteClock from '@/components/agents/sidebar/use-minute-clock';
import useConversationBackground from '@/components/agents/sidebar/use-conversation-background';
import { useMachineLoad } from '@/lib/hl-machine-load-store';
import { LEVEL_LABEL, indicatorText } from '@/lib/hl-machine-load-view';
import HlMark from '@/components/agents/hl-mark';
import { t } from '@/lib/i18n';

const RECENT_LIMIT = 5;
const THEME_ICONS = { system: Monitor, light: Sun, dark: Moon } as const;

const subscribeNever = () => () => {};

const RailAction = ({
  tip,
  children,
}: {
  tip: Extract<TRailTooltipInput, { kind: typeof RAIL_TOOLTIP_KIND.action }>;
  children: ReactElement<{ 'aria-describedby'?: string }>;
}) => <RailTooltip tip={railTooltipOf(tip)}>{children}</RailTooltip>;

const RailRow = ({ row, item }: { row?: IRowModel; item?: Conversation }) => {
  const context = useSidebarContext();
  const modKey = useModKey();
  const conversation = row ? row.conversation : (item ?? null);
  const key = row?.ref ?? item?.key ?? '';
  const title = conversation
    ? cleanConversationTitle(conversation.title)
    : t('agents.conversationRail.conversationFallback');
  const provider = conversation ? conversation.provider : 'terminal';
  const hotkey = row?.hotkey ?? null;
  const now = useMinuteClock();
  const background = useConversationBackground(conversation);
  const status = shownStatus(context.statuses[key] ?? 'idle', background.length);
  const tip = railTooltipOf(
    {
      kind: RAIL_TOOLTIP_KIND.conversation,
      title,
      provider,
      status,
      machineName: conversation?.machineName ?? row?.machineName,
      updatedAt: conversation?.updatedAt,
      shortcut: hotkey ? `${modKey}${hotkey}` : undefined,
      unavailable: row ? STATE_NOTE[row.state]?.(row) : undefined,
    },
    now,
  );
  return (
    <RailTooltip tip={tip}>
      <button
        type="button"
        className={cx(
          'tw-rail-btn tw-rail-conv',
          context.selectedKey === key && 'tw-rail-active',
          row && row.state !== 'ok' && 'tw-rail-inactive',
        )}
        aria-label={title}
        aria-current={context.selectedKey === key ? 'page' : undefined}
        onClick={() => (row ? context.onOpenRow(row) : item && context.onSelect(item))}
      >
        <ProviderIcon
          provider={provider}
          status={status === 'working' ? 'working' : 'idle'}
          size="sm"
          native={false}
        />
        {status !== 'idle' && status !== 'working' && <StatusOrb status={status} className="tw-rail-dot" />}
        {hotkey !== null && (
          <span className="tw-rail-hotkey" aria-hidden="true">
            {hotkey}
          </span>
        )}
      </button>
    </RailTooltip>
  );
};

const projectItems = (model: IProjectModel, context: ISidebarContext, modKey: string): TTwMenuEntry[] => [
  { kind: TW_MENU_KIND.label, id: 'title', label: model.project.name },
  ...model.rows.map<TTwMenuEntry>((row) => ({
    kind: TW_MENU_KIND.action,
    id: row.ref,
    label: row.conversation
      ? cleanConversationTitle(row.conversation.title)
      : t('agents.conversationRail.conversationFallback'),
    hint: row.state === 'ok' ? (row.machineName ?? undefined) : t('agents.conversationRail.unavailable'),
    icon: <ProviderIcon provider={row.conversation ? row.conversation.provider : 'terminal'} size="sm" />,
    shortcut: row.hotkey ? `${modKey}${row.hotkey}` : undefined,
    onSelect: () => context.onOpenRow(row),
  })),
  ...(model.rows.length
    ? []
    : [{ kind: TW_MENU_KIND.label, id: 'empty', label: t('agents.conversationRail.projectEmpty') } as const]),
  { kind: TW_MENU_KIND.separator, id: 'sep' },
  {
    kind: TW_MENU_KIND.submenu,
    id: 'new',
    label: t('agents.conversationRail.newInProject'),
    icon: <Plus />,
    items: newConversationMenuItems(context, model.project.id),
  },
];

const RailProject = ({ model }: { model: IProjectModel }) => {
  const context = useSidebarContext();
  const modKey = useModKey();
  const tip = railTooltipOf({
    kind: RAIL_TOOLTIP_KIND.project,
    name: model.project.name,
    count: model.rows.length,
    summary: model.summary,
  });
  return (
    <RailTooltip tip={tip}>
      <TwMenuButton
        items={projectItems(model, context, modKey)}
        menuLabel={t('agents.conversationRail.project', { name: model.project.name })}
        side={LAYER_SIDE.right}
        className="tw-rail-btn tw-rail-project"
        aria-label={t('agents.conversationRail.project', { name: model.project.name })}
      >
        <ProjectTile project={model.project} />
        {model.summary && (
          <span className={cx('tw-row-dot tw-rail-dot', `tw-row-dot-${model.summary.status}`)} />
        )}
      </TwMenuButton>
    </RailTooltip>
  );
};

interface IConversationRailProps {
  owner: string;
  model: ISidebarModel;
  themeMode: TThemeMode;
  onToggleTheme: () => void;
  onOpenPalette: () => void;
  statsOpen?: boolean;
  onOpenStats?: () => void;
  efficiencyOpen?: boolean;
  efficiencyAlert?: boolean;
  onOpenEfficiency?: () => void;
  designOpen?: boolean;
  onOpenDesign?: () => void;
  onExpand: () => void;
}

/** Collapsed sidebar: new, search, pins with their numbers, project tiles, recent conversations. */
const ConversationRail = ({
  owner,
  model,
  themeMode,
  onToggleTheme,
  onOpenPalette,
  statsOpen = false,
  onOpenStats,
  efficiencyOpen = false,
  efficiencyAlert = false,
  onOpenEfficiency,
  designOpen = false,
  onOpenDesign,
  onExpand,
}: IConversationRailProps) => {
  const context = useSidebarContext();
  const modKey = useModKey();
  const hydrated = useSyncExternalStore(
    subscribeNever,
    () => true,
    () => false,
  );
  const mode = hydrated ? themeMode : 'system';
  const ThemeIcon = THEME_ICONS[mode];
  const recent = model.conversations.flatMap((group) => group.items).slice(0, RECENT_LIMIT);
  const load = useMachineLoad('');
  const freshLoad = load.load && !load.stale ? load.load : null;
  const machineFacts = freshLoad
    ? [
        {
          label: t('agents.conversationRail.state'),
          value: `${LEVEL_LABEL[freshLoad.level].toLowerCase()}, ${indicatorText(freshLoad)}`,
        },
      ]
    : [];
  return (
    <nav className="tw-rail" aria-label={t('agents.conversationRail.navLabel')}>
      <section className="tw-island tw-rail-island">
        <RailAction
          tip={{
            kind: RAIL_TOOLTIP_KIND.action,
            title: t('agents.conversationRail.expand'),
            description: t('agents.conversationRail.expandDescription'),
            shortcut: `${modKey}B`,
          }}
        >
          <button
            type="button"
            className="tw-rail-mark"
            aria-label={t('agents.conversationRail.expand')}
            onClick={onExpand}
          >
            <HlMark className="tw-mark" />
            <PanelLeftOpen className="tw-rail-mark-open" />
          </button>
        </RailAction>
        <span className="tw-rail-sep" />
        <RailAction
          tip={{
            kind: RAIL_TOOLTIP_KIND.action,
            title: t('agents.conversationRail.newConversation'),
            description: t('agents.conversationRail.newConversationDescription'),
          }}
        >
          <TwMenuButton
            items={newConversationMenuItems(context)}
            menuLabel={t('agents.conversationRail.newConversation')}
            side={LAYER_SIDE.right}
            className="tw-rail-btn"
            aria-label={t('agents.conversationRail.newConversation')}
            disabled={context.busy}
          >
            <Plus />
          </TwMenuButton>
        </RailAction>
        <RailAction
          tip={{
            kind: RAIL_TOOLTIP_KIND.action,
            title: t('agents.conversationRail.palette'),
            description: t('agents.conversationRail.paletteDescription'),
            shortcut: `${modKey}K`,
          }}
        >
          <button
            type="button"
            className="tw-rail-btn"
            aria-label={t('agents.conversationRail.palette')}
            onClick={onOpenPalette}
          >
            <Search />
          </button>
        </RailAction>
      </section>
      <section className="tw-island tw-rail-island tw-rail-list">
        {onOpenStats && (
          <RailAction
            tip={{
              kind: RAIL_TOOLTIP_KIND.action,
              title: t('agents.conversationRail.stats'),
              description: t('agents.conversationRail.statsDescription'),
            }}
          >
            <button
              type="button"
              className={cx('tw-rail-btn', statsOpen && 'tw-rail-btn-on')}
              aria-label={t('agents.conversationRail.stats')}
              aria-current={statsOpen ? 'page' : undefined}
              onClick={onOpenStats}
            >
              <ChartColumnBig />
            </button>
          </RailAction>
        )}
        {onOpenEfficiency && (
          <RailAction
            tip={{
              kind: RAIL_TOOLTIP_KIND.action,
              title: t('agents.conversationRail.efficiency'),
              description: efficiencyAlert
                ? t('agents.conversationRail.efficiencyAlert')
                : t('agents.conversationRail.efficiencyDescription'),
            }}
          >
            <button
              type="button"
              className={cx('tw-rail-btn relative', efficiencyOpen && 'tw-rail-btn-on')}
              aria-label={
                efficiencyAlert
                  ? t('agents.conversationRail.efficiencyAlertAria')
                  : t('agents.conversationRail.efficiency')
              }
              aria-current={efficiencyOpen ? 'page' : undefined}
              onClick={onOpenEfficiency}
            >
              <TrendingUp />
              {efficiencyAlert && (
                <span
                  className="absolute right-1.5 top-1.5 size-2 rounded-full bg-(--t-status-danger)"
                  aria-hidden="true"
                />
              )}
            </button>
          </RailAction>
        )}
        {onOpenDesign && (
          <RailAction
            tip={{
              kind: RAIL_TOOLTIP_KIND.action,
              title: 'Claude Design',
              description: t('agents.conversationRail.designDescription'),
            }}
          >
            <button
              type="button"
              className={cx('tw-rail-btn', designOpen && 'tw-rail-btn-on')}
              aria-label="Claude Design"
              aria-current={designOpen ? 'page' : undefined}
              onClick={onOpenDesign}
            >
              <Palette />
            </button>
          </RailAction>
        )}
        {onOpenStats && <span className="tw-rail-sep" />}
        {model.pinned.map((row) => (
          <RailRow key={row.ref} row={row} />
        ))}
        {model.pinned.length > 0 && <span className="tw-rail-sep" />}
        {model.projects.map((project) => (
          <RailProject key={project.project.id} model={project} />
        ))}
        {model.projects.length > 0 && <span className="tw-rail-sep" />}
        {recent.map((item) => (
          <RailRow key={item.key} item={item} />
        ))}
      </section>
      <section className="tw-island tw-rail-island">
        <RailAction
          tip={{
            kind: RAIL_TOOLTIP_KIND.action,
            title: t('agents.conversationRail.machines'),
            description: t('agents.conversationRail.machinesDescription'),
            facts: machineFacts,
          }}
        >
          <button
            type="button"
            className="tw-rail-btn"
            aria-label={t('agents.conversationRail.machines')}
            onClick={onExpand}
          >
            <Server />
          </button>
        </RailAction>
        <RailAction
          tip={{
            kind: RAIL_TOOLTIP_KIND.action,
            title: THEME_LABELS[mode],
            description: t('agents.conversationRail.themeDescription'),
          }}
        >
          <button
            type="button"
            className="tw-rail-btn"
            aria-label={THEME_LABELS[mode]}
            onClick={onToggleTheme}
          >
            <ThemeIcon />
          </button>
        </RailAction>
        <RailAction
          tip={{
            kind: RAIL_TOOLTIP_KIND.action,
            title: owner,
            description: t('agents.conversationRail.profile'),
          }}
        >
          <span className="tw-avatar tw-rail-avatar" role="img" aria-label={owner} tabIndex={0}>
            {initials(owner)}
          </span>
        </RailAction>
      </section>
    </nav>
  );
};

export default ConversationRail;
