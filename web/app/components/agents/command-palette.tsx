import { useState } from 'react';
import {
  ArrowLeft,
  ChartColumnBig,
  FolderInput,
  Palette,
  FolderMinus,
  FolderPlus,
  Moon,
  PanelLeft,
  PanelRight,
  Pin,
  PinOff,
  Search,
  Sun,
  TrendingUp,
} from 'lucide-react';
import { TwSpinner } from '@/components/agents/ui';
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from '@/components/ui/command';
import ProviderIcon, { PROVIDER_LABELS } from '@/components/agents/provider-icon';
import ProjectTile from '@/components/agents/sidebar/project-tile';
import { organizerActions, useModKey } from '@/components/agents/sidebar/organizer-actions';
import { useSidebarContext } from '@/components/agents/sidebar/sidebar-context';
import { newConversationOptions, type TNewConversationKind } from '@/lib/hl-new-conversation';
import { cleanConversationTitle, type Conversation } from '@/lib/hl-conversations';
import type { TConversationRef } from '@/lib/hl-organizer';
import type { ISidebarModel } from '@/lib/hl-sidebar-model';
import useTranscriptSearch, { type ITranscriptSearchHit } from '@/hooks/use-transcript-search';
import { locale, plural, t } from '@/lib/i18n';

/** \u0001/\u0002 wrap the matched words in a ts_headline snippet; never HTML from the stored text itself. */
const renderSnippet = (snippet: string) => {
  const parts = snippet.split(/[\u0001\u0002]/);
  return parts.map((part, i) => (i % 2 === 1 ? <mark key={i}>{part}</mark> : <span key={i}>{part}</span>));
};

const roleLabel: Record<ITranscriptSearchHit['role'], string> = {
  user: t('agents.commandPalette.roleYou'),
  assistant: t('agents.commandPalette.roleAgent'),
};

interface ICommandPaletteProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  conversations: Conversation[];
  model: ISidebarModel;
  /** The conversation on screen: «Actions on the current one» act on it. */
  current: Conversation | null;
  theme: 'light' | 'dark';
  onCreate: (kind: TNewConversationKind) => void;
  onRevealProject: (projectId: string) => void;
  onToggleTheme: () => void;
  onToggleSidebar: () => void;
  onToggleInspector: () => void;
  onOpenStats: () => void;
  onOpenEfficiency?: () => void;
  efficiencyAlert?: boolean;
  onOpenDesign?: () => void;
  /** Opened already switched to «Search conversations» (the sidebar's search field). */
  initialMode?: 'default' | 'search';
  /** A search hit was picked: open the conversation it belongs to (when the tab is still tracked) and jump to that moment. */
  onOpenSearchHit?: (item: Conversation, atIso: string | null) => void;
}

const CommandPalette = ({
  open,
  onOpenChange,
  conversations,
  model,
  current,
  theme,
  onCreate,
  onRevealProject,
  onToggleTheme,
  onToggleSidebar,
  onToggleInspector,
  onOpenStats,
  onOpenEfficiency,
  efficiencyAlert = false,
  onOpenDesign,
  initialMode = 'default',
  onOpenSearchHit,
}: ICommandPaletteProps) => {
  const context = useSidebarContext();
  const modKey = useModKey();
  const { organizer } = context;
  const [mode, setMode] = useState<'default' | 'search'>(initialMode);
  const [searchQuery, setSearchQuery] = useState('');
  const search = useTranscriptSearch(mode === 'search' ? searchQuery : '');
  // Re-enter at `initialMode` every time the dialog opens: adjusted during render (React's
  // documented pattern for resetting state on a prop change) rather than in an effect, so
  // the reset lands in the same render as the open, never one frame late.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setMode(initialMode);
      setSearchQuery('');
    }
  }
  const run = (action: () => void) => {
    onOpenChange(false);
    action();
  };
  const openHit = (hit: ITranscriptSearchHit) => {
    const match =
      conversations.find((c) => c.machineId === hit.machineId && c.jsonlPath === hit.path) ?? null;
    if (!match || !onOpenSearchHit) return;
    run(() => onOpenSearchHit(match, hit.recordedAt));
  };
  const currentRef =
    current && current.key !== 'shared' && current.id !== 'new' ? (current.key as TConversationRef) : null;
  const currentPinned = currentRef ? (organizer?.pins.includes(currentRef) ?? false) : false;
  const currentProjectId = currentRef ? (organizer?.assignments[currentRef] ?? null) : null;

  if (mode === 'search') {
    return (
      <CommandDialog
        open={open}
        onOpenChange={onOpenChange}
        title={t('agents.commandPalette.searchTitle')}
        description={t('agents.commandPalette.searchDescription')}
        className="tw-palette"
      >
        <Command shouldFilter={false} loop>
          <div className="tw-palette-search-head">
            <button
              type="button"
              className="tw-icon-button"
              aria-label={t('agents.commandPalette.backToCommands')}
              onClick={() => setMode('default')}
            >
              <ArrowLeft />
            </button>
            <span className="tw-palette-search-title">{t('agents.commandPalette.searchTitle')}</span>
          </div>
          <CommandInput
            autoFocus
            value={searchQuery}
            onValueChange={setSearchQuery}
            placeholder={t('agents.commandPalette.searchPlaceholder')}
          />
          <CommandList className="tw-palette-list">
            {searchQuery.trim().length < 2 && (
              <CommandEmpty>{t('agents.commandPalette.typeTwoChars')}</CommandEmpty>
            )}
            {searchQuery.trim().length >= 2 && search.loading && !search.hits.length && (
              <div className="tw-palette-search-status">
                <TwSpinner /> {t('agents.commandPalette.searching')}
              </div>
            )}
            {search.error && <div className="tw-palette-search-status">{search.error}</div>}
            {!search.loading && !search.error && searchQuery.trim().length >= 2 && !search.hits.length && (
              <CommandEmpty>{t('agents.commandPalette.nothingFound')}</CommandEmpty>
            )}
            {search.hits.length > 0 && (
              <CommandGroup heading={t('agents.commandPalette.found')}>
                {search.hits.map((hit) => (
                  <CommandItem
                    key={`${hit.fileId}:${hit.byteOffset}`}
                    value={`${hit.fileId}:${hit.byteOffset}`}
                    onSelect={() => openHit(hit)}
                    className="tw-palette-search-item"
                  >
                    <ProviderIcon provider={hit.provider} size="sm" />
                    <span className="tw-palette-search-body">
                      <span className="tw-palette-title">{hit.aiTitle || hit.path.split('/').pop()}</span>
                      <span className="tw-palette-search-snippet">
                        <span className="tw-palette-search-role">{roleLabel[hit.role]}:</span>{' '}
                        {renderSnippet(hit.snippet)}
                      </span>
                    </span>
                    <span className="tw-palette-hint">
                      {hit.machineName}
                      {hit.recordedAt
                        ? ` · ${new Date(hit.recordedAt).toLocaleString(locale() === 'ru' ? 'ru-RU' : 'en-US', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })}`
                        : ''}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </CommandDialog>
    );
  }

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('agents.commandPalette.title')}
      description={t('agents.commandPalette.description')}
      className="tw-palette"
    >
      <Command loop>
        <CommandInput placeholder={t('agents.commandPalette.placeholder')} />
        <CommandList className="tw-palette-list">
          <CommandEmpty>{t('agents.commandPalette.nothingFound')}</CommandEmpty>
          <CommandGroup heading={t('agents.commandPalette.groupSearch')}>
            <CommandItem value={t('agents.commandPalette.valueSearch')} onSelect={() => setMode('search')}>
              <Search />
              <span>{t('agents.commandPalette.searchTitle')}</span>
            </CommandItem>
          </CommandGroup>
          <CommandGroup heading={t('agents.commandPalette.groupStart')}>
            {newConversationOptions(context.features).map((option) => (
              <CommandItem
                key={option.kind}
                value={t('agents.commandPalette.valueNew', { title: option.title, hint: option.hint })}
                onSelect={() => run(() => onCreate(option.kind))}
              >
                <ProviderIcon provider={option.kind} size="sm" />
                <span>{t('agents.commandPalette.newKind', { title: option.title })}</span>
                <span className="tw-palette-hint">{option.hint}</span>
              </CommandItem>
            ))}
            {organizer && (
              <CommandItem
                value={t('agents.commandPalette.valueNewProject')}
                onSelect={() => run(() => context.onNewProject())}
              >
                <FolderPlus />
                <span>{t('agents.commandPalette.newProject')}</span>
              </CommandItem>
            )}
          </CommandGroup>
          {model.pinned.length > 0 && (
            <CommandGroup heading={t('agents.commandPalette.groupPinned')}>
              {model.pinned.map((row) => {
                const title = row.conversation
                  ? cleanConversationTitle(row.conversation.title)
                  : t('agents.commandPalette.conversationFallback');
                return (
                  <CommandItem
                    key={row.ref}
                    value={t('agents.commandPalette.valuePinned', { title, ref: row.ref })}
                    onSelect={() => run(() => context.onOpenRow(row))}
                  >
                    <ProviderIcon
                      provider={row.conversation ? row.conversation.provider : 'terminal'}
                      size="sm"
                    />
                    <span className="tw-palette-title">{title}</span>
                    {row.hotkey && (
                      <CommandShortcut>
                        {modKey}
                        {row.hotkey}
                      </CommandShortcut>
                    )}
                  </CommandItem>
                );
              })}
            </CommandGroup>
          )}
          {model.projects.length > 0 && (
            <CommandGroup heading={t('agents.commandPalette.groupProjects')}>
              {model.projects.map(({ project, rows }) => (
                <CommandItem
                  key={project.id}
                  value={t('agents.commandPalette.valueProject', { name: project.name, id: project.id })}
                  onSelect={() => run(() => onRevealProject(project.id))}
                >
                  <ProjectTile project={project} size="sm" />
                  <span className="tw-palette-title">{project.name}</span>
                  <span className="tw-palette-hint">
                    {rows.length
                      ? plural(rows.length, 'agents.commandPalette.inProject')
                      : t('agents.commandPalette.empty')}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          )}
          {conversations.length > 0 && (
            <CommandGroup heading={t('agents.commandPalette.groupConversations')}>
              {conversations.slice(0, 60).map((item) => {
                const title = cleanConversationTitle(item.title);
                const project = model.projectOf(item.key);
                const hint = [
                  project?.name,
                  PROVIDER_LABELS[item.provider] ?? item.provider,
                  item.machineName,
                ]
                  .filter(Boolean)
                  .join(' · ');
                return (
                  <CommandItem
                    key={item.key}
                    value={`${title} ${project?.name ?? ''} ${item.key}`}
                    onSelect={() => run(() => context.onSelect(item))}
                  >
                    <ProviderIcon provider={item.provider} size="sm" />
                    <span className="tw-palette-title">{title}</span>
                    <span className="tw-palette-hint">{hint}</span>
                  </CommandItem>
                );
              })}
            </CommandGroup>
          )}
          {currentRef && organizer && (
            <CommandGroup heading={t('agents.commandPalette.groupCurrent')}>
              <CommandItem
                value={
                  currentPinned
                    ? t('agents.commandPalette.valueUnpinCurrent')
                    : t('agents.commandPalette.valuePinCurrent')
                }
                onSelect={() =>
                  run(
                    () =>
                      void (currentPinned
                        ? organizerActions.unpin(currentRef)
                        : organizerActions.pin(currentRef)),
                  )
                }
              >
                {currentPinned ? <PinOff /> : <Pin />}
                <span>
                  {currentPinned ? t('agents.commandPalette.unpin') : t('agents.commandPalette.pin')}
                </span>
              </CommandItem>
              {organizer.projects
                .filter((project) => project.id !== currentProjectId)
                .map((project) => (
                  <CommandItem
                    key={project.id}
                    value={t('agents.commandPalette.valueMoveTo', { name: project.name })}
                    onSelect={() => run(() => void organizerActions.assign([currentRef], project.id))}
                  >
                    <FolderInput />
                    <span>{t('agents.commandPalette.moveTo', { name: project.name })}</span>
                  </CommandItem>
                ))}
              <CommandItem
                value={t('agents.commandPalette.valueMoveToNew')}
                onSelect={() => run(() => context.onNewProject([currentRef]))}
              >
                <FolderPlus />
                <span>{t('agents.commandPalette.moveToNew')}</span>
              </CommandItem>
              {currentProjectId && (
                <CommandItem
                  value={t('agents.commandPalette.valueRemoveFromProject')}
                  onSelect={() => run(() => void organizerActions.assign([currentRef], null))}
                >
                  <FolderMinus />
                  <span>{t('agents.commandPalette.removeFromProject')}</span>
                </CommandItem>
              )}
            </CommandGroup>
          )}
          <CommandGroup heading={t('agents.commandPalette.groupView')}>
            <CommandItem value={t('agents.commandPalette.valueStats')} onSelect={() => run(onOpenStats)}>
              <ChartColumnBig />
              <span>{t('agents.commandPalette.stats')}</span>
            </CommandItem>
            {onOpenEfficiency && (
              <CommandItem
                value={t('agents.commandPalette.valueEfficiency')}
                onSelect={() => run(onOpenEfficiency)}
              >
                <TrendingUp />
                <span>{t('agents.commandPalette.efficiency')}</span>
                {efficiencyAlert && (
                  <span
                    className="ml-auto size-2 rounded-full bg-(--t-status-danger)"
                    role="img"
                    aria-label={t('agents.commandPalette.alertForYou')}
                  />
                )}
              </CommandItem>
            )}
            {onOpenDesign && (
              <CommandItem value={t('agents.commandPalette.valueDesign')} onSelect={() => run(onOpenDesign)}>
                <Palette />
                <span>Claude Design</span>
              </CommandItem>
            )}
            <CommandItem
              value={t('agents.commandPalette.valueSidebar')}
              onSelect={() => run(onToggleSidebar)}
            >
              <PanelLeft />
              <span>{t('agents.commandPalette.toggleSidebar')}</span>
              <CommandShortcut>⌘B</CommandShortcut>
            </CommandItem>
            <CommandItem
              value={t('agents.commandPalette.valueInspector')}
              onSelect={() => run(onToggleInspector)}
            >
              <PanelRight />
              <span>{t('agents.commandPalette.toggleInspector')}</span>
              <CommandShortcut>⌘.</CommandShortcut>
            </CommandItem>
            <CommandItem value={t('agents.commandPalette.valueTheme')} onSelect={() => run(onToggleTheme)}>
              {theme === 'dark' ? <Sun /> : <Moon />}
              <span>
                {theme === 'dark'
                  ? t('agents.commandPalette.lightTheme')
                  : t('agents.commandPalette.darkTheme')}
              </span>
            </CommandItem>
          </CommandGroup>
        </CommandList>
      </Command>
    </CommandDialog>
  );
};

export default CommandPalette;
