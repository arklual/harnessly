import { useMemo, useState } from 'react';
import dynamic from 'next/dynamic';
import {
  Bot,
  FileCode2,
  FileText,
  FileType2,
  Film,
  GitCompareArrows,
  Globe2,
  ImageIcon,
  MessageSquareText,
} from 'lucide-react';
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import SiteIcon from '@/components/agents/site-icon';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import useAvailableFiles from '@/hooks/use-available-files';
import { isPeerOnly, parseUserSegments, plainUserText } from '@/lib/message-wrappers';
import { collectTouchedFiles, collectViewableFiles, type IViewableFile } from '@/lib/hl-artifacts';
import useConversationSources from '@/hooks/use-conversation-sources';
import { useLinkMetas } from '@/hooks/use-link-meta';
import { smartFilter } from '@/lib/hl-smart-filter';
import type { TLinkMeta } from '@/lib/link-meta';
import { presentLink } from '@/lib/presented-link';
import { describeToolCall } from '@/lib/hl-tool-label';
import { jumpToEntry } from '@/lib/hl-timeline-jump';
import { openPaneTab } from '@/lib/hl-pane-store';
import type { ITimelineEntry } from '@/types/timeline';

interface IConversationNavigatorProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  snapshotKey: string | null;
}

import AttachmentViewer, { type IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import { isImageHref, linkImageName } from '@/lib/image-link';
import { t } from '@/lib/i18n';

const FileViewer = dynamic(() => import('@/components/agents/inspector/file-viewer'), { ssr: false });

const FILE_ICON = {
  image: ImageIcon,
  video: Film,
  markdown: FileText,
  html: Globe2,
  pdf: FileType2,
  text: FileText,
};

const EMPTY: ITimelineEntry[] = [];

const jumpTo = jumpToEntry;

/** Links listed before anything is typed; a query searches all of them. */
const LINKS_AT_REST = 30;
const LINKS_SEARCHED = 400;

/** What a link is known by beyond its address: the page, board, task or merge request title once fetched. */
const metaWords = (meta: TLinkMeta | null | undefined): string[] => {
  if (!meta) return [];
  if (meta.kind === 'artifact') return [meta.title, meta.description ?? ''];
  if (meta.kind === 'issue') return [meta.key, meta.title, meta.parent?.key ?? '', meta.parent?.title ?? ''];
  return [meta.title];
};

const metaTitle = (meta: TLinkMeta | null | undefined): string | null =>
  meta ? (meta.kind === 'issue' ? `${meta.key} ${meta.title}` : meta.title) : null;

/** ⌘J: jump through this conversation — own prompts, results, subagents, touched files. */
const ConversationNavigator = ({ open, onOpenChange, snapshotKey }: IConversationNavigatorProps) => {
  // Closed, it follows nothing: every message of a long conversation re-walked prompts, files and links for a dialog
  // nobody saw. What it showed last stays for the closing animation.
  const live = useTimelineSnapshotStore((s) =>
    open && snapshotKey ? s.bySession[snapshotKey]?.entries : undefined,
  );
  const [shown, setShown] = useState<ITimelineEntry[]>(EMPTY);
  if (open && live && live !== shown) setShown(live);
  const entries = open ? (live ?? EMPTY) : shown;
  const jsonlPath = useTimelineSnapshotStore((s) =>
    snapshotKey ? (s.bySession[snapshotKey]?.jsonlPath ?? null) : null,
  );
  const [query, setQuery] = useState('');
  const prompts = useMemo(
    () =>
      entries
        .flatMap((e) => {
          if (e.type !== 'user-message') return [];
          const segments = parseUserSegments(e.text);
          return segments.length && !isPeerOnly(segments)
            ? [{ id: e.id, timestamp: e.timestamp, text: plainUserText(e.text) }]
            : [];
        })
        .reverse(),
    [entries],
  );
  const { links } = useConversationSources(open ? jsonlPath : null, null, entries);
  const shownLinks = useMemo(
    () => links.slice(0, query.trim() ? LINKS_SEARCHED : LINKS_AT_REST),
    [links, query],
  );
  const metas = useLinkMetas(
    useMemo(() => (open ? shownLinks.map((link) => link.url) : []), [open, shownLinks]),
  );
  const agents = useMemo(
    () =>
      entries
        .filter((e) => e.type === 'tool-call' && (e.toolName === 'Agent' || e.toolName === 'Task'))
        .reverse(),
    [entries],
  );
  const files = useMemo(() => collectTouchedFiles(entries), [entries]);
  const viewable = useAvailableFiles(useMemo(() => collectViewableFiles(entries), [entries]));
  const [viewing, setViewing] = useState<IViewableFile | null>(null);
  const [viewingImage, setViewingImage] = useState<IAttachmentView | null>(null);
  const run = (action: () => void) => {
    onOpenChange(false);
    setTimeout(action, 60);
  };

  return (
    <>
      <CommandDialog
        open={open}
        onOpenChange={onOpenChange}
        title={t('agents.conversationNavigator.title')}
        description={t('agents.conversationNavigator.description')}
        className="tw-palette tw-navigator"
      >
        <Command loop filter={smartFilter}>
          <CommandInput
            placeholder={t('agents.conversationNavigator.placeholder')}
            value={query}
            onValueChange={setQuery}
          />
          <CommandList className="tw-palette-list">
            <CommandEmpty>{t('agents.conversationNavigator.empty')}</CommandEmpty>
            {shownLinks.length > 0 && (
              <CommandGroup heading={t('agents.conversationNavigator.links')}>
                {shownLinks.map((link) => {
                  const presented = presentLink(link.url);
                  const meta = metas.get(link.url);
                  const kindWords = presented.board
                    ? t('agents.conversationNavigator.boardWords')
                    : presented.kind === 'artifact'
                      ? t('agents.conversationNavigator.artifactWords')
                      : presented.kind === 'mr'
                        ? t('agents.conversationNavigator.mrWords')
                        : presented.kind === 'issue'
                          ? t('agents.conversationNavigator.issueWords')
                          : '';
                  return (
                    <CommandItem
                      key={link.url}
                      value={t('agents.conversationNavigator.linkValue', { url: link.url })}
                      keywords={[
                        link.label ?? '',
                        presented.label,
                        presented.detail,
                        kindWords,
                        ...metaWords(meta),
                      ]}
                      onSelect={() =>
                        run(() =>
                          isImageHref(link.url)
                            ? setViewingImage({
                                kind: 'image',
                                url: link.url,
                                name: linkImageName(link.url, link.label ?? ''),
                              })
                            : window.open(link.url, '_blank', 'noopener'),
                        )
                      }
                    >
                      <SiteIcon href={link.url} className="tw-nav-icon" />
                      <span className="tw-palette-title">
                        {metaTitle(meta) ?? link.label ?? presented.label}
                      </span>
                      <span className="tw-palette-hint">{presented.detail}</span>
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            )}
            {prompts.length > 0 && (
              <CommandGroup heading={t('agents.conversationNavigator.prompts')}>
                {prompts.slice(0, 40).map((entry) => (
                  <CommandItem
                    key={entry.id}
                    value={t('agents.conversationNavigator.promptValue', {
                      text: entry.text.slice(0, 300),
                      id: entry.id,
                    })}
                    onSelect={() => run(() => jumpTo(entry.id))}
                  >
                    <MessageSquareText />
                    <span className="tw-palette-title">{entry.text.slice(0, 140)}</span>
                    <span className="tw-palette-hint">
                      {new Date(entry.timestamp).toLocaleTimeString('ru-RU', {
                        hour: '2-digit',
                        minute: '2-digit',
                      })}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {agents.length > 0 && (
              <CommandGroup heading={t('agents.conversationNavigator.subagents')}>
                {agents.slice(0, 20).map(
                  (entry) =>
                    entry.type === 'tool-call' && (
                      <CommandItem
                        key={entry.id}
                        value={t('agents.conversationNavigator.subagentValue', { summary: entry.summary })}
                        onSelect={() => run(() => openPaneTab('subagents'))}
                      >
                        <Bot />
                        <span className="tw-palette-title">
                          {describeToolCall(entry).target || entry.summary}
                        </span>
                      </CommandItem>
                    ),
                )}
              </CommandGroup>
            )}
            {viewable.length > 0 && (
              <CommandGroup heading={t('agents.conversationNavigator.files')}>
                {viewable.slice(0, 30).map((file) => {
                  const Icon = FILE_ICON[file.kind];
                  return (
                    <CommandItem
                      key={file.path}
                      value={t('agents.conversationNavigator.viewableValue', { path: file.path })}
                      onSelect={() => run(() => setViewing(file))}
                    >
                      <Icon />
                      <span className="tw-palette-title">{file.path.split('/').at(-1)}</span>
                      <span className="tw-palette-hint">{file.path.split('/').slice(-3, -1).join('/')}</span>
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            )}
            <CommandGroup heading={t('agents.conversationNavigator.changes')}>
              <CommandItem
                value={t('agents.conversationNavigator.changesValue')}
                onSelect={() => run(() => openPaneTab('changes'))}
              >
                <GitCompareArrows />
                <span className="tw-palette-title">{t('agents.conversationNavigator.openChanges')}</span>
              </CommandItem>
              {files.slice(0, 20).map((file) => (
                <CommandItem
                  key={file.path}
                  value={t('agents.conversationNavigator.fileValue', { path: file.path })}
                  onSelect={() => run(() => openPaneTab('changes'))}
                >
                  <FileCode2 />
                  <span className="tw-palette-title">{file.path.split('/').at(-1)}</span>
                  <span className="tw-palette-hint">{file.path.split('/').slice(-3, -1).join('/')}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </CommandDialog>
      <FileViewer file={viewing} files={viewable} onNavigate={setViewing} onClose={() => setViewing(null)} />
      <AttachmentViewer item={viewingImage} onClose={() => setViewingImage(null)} />
    </>
  );
};

export default ConversationNavigator;
