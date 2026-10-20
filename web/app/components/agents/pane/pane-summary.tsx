import { useState, type ComponentType, type MouseEvent, type ReactNode, type RefObject } from 'react';
import { FileText, Link2, MessageCircle, Plus } from 'lucide-react';
import { LAYER_ALIGN, TwPopover, type TDismissReason } from '@/components/agents/ui';
import FileViewer from '@/components/agents/inspector/file-viewer';
import SiteIcon from '@/components/agents/site-icon';
import usePaneData from '@/hooks/use-pane-data';
import { presentLink } from '@/lib/presented-link';
import { openPaneTab, paneKeyOf, usePane } from '@/lib/hl-pane-store';
import { taskKeyOf } from '@/lib/hl-task';
import { paneDocTargetOf } from '@/lib/hl-doc-path';
import type { IViewableFile } from '@/lib/hl-artifacts';
import type { ITab } from '@/types/terminal';
import { t } from '@/lib/i18n';

const OUTPUTS_SHOWN = 6;
const SOURCES_SHOWN = 4;
const MIDDLE_BUTTON = 1;

interface IRowProps {
  icon: ReactNode;
  label: string;
  detail?: string;
  onOpen: () => void;
  /** The middle button or a click with Ctrl or ⌘, where the plain click does something else than open a tab. */
  onOpenInPane?: () => void;
}

const Row = ({ icon, label, detail, onOpen, onOpenInPane }: IRowProps) => {
  const click = (event: MouseEvent) =>
    onOpenInPane && (event.metaKey || event.ctrlKey) ? onOpenInPane() : onOpen();
  const auxClick = (event: MouseEvent) => {
    if (!onOpenInPane || event.button !== MIDDLE_BUTTON) return;
    event.preventDefault();
    onOpenInPane();
  };
  return (
    <li>
      <button
        type="button"
        className="tw-rpane-summary-row"
        title={detail ?? label}
        onClick={click}
        onAuxClick={auxClick}
        onMouseDown={(event) => {
          if (onOpenInPane && event.button === MIDDLE_BUTTON) event.preventDefault();
        }}
      >
        <span className="tw-rpane-summary-icon">{icon}</span>
        <span className="tw-rpane-summary-label">{label}</span>
      </button>
    </li>
  );
};

const Section = ({
  title,
  action,
  children,
}: {
  title: string;
  action?: { label: string; icon: ComponentType; onClick: () => void };
  children: ReactNode;
}) => (
  <section className="tw-rpane-summary-section" aria-label={title}>
    <header className="tw-rpane-summary-head">
      <span>{title}</span>
      {action && (
        <button
          type="button"
          className="tw-icon-button tw-rpane-summary-add"
          aria-label={action.label}
          title={action.label}
          onClick={action.onClick}
        >
          <action.icon />
        </button>
      )}
    </header>
    {children}
  </section>
);

interface ISummaryContentProps {
  mode: 'agent' | 'chat';
  tab: ITab | null;
  chatId: string | null;
  onDone: () => void;
  /** A file of the outputs in the window over the conversation, with the files it pages through. */
  onViewFile: (file: IViewableFile, files: IViewableFile[]) => void;
}

const SummaryContent = ({ mode, tab, chatId, onDone, onViewFile }: ISummaryContentProps) => {
  const { links, files } = usePaneData(mode, tab, chatId);
  const [pane] = usePane(paneKeyOf(mode, tab?.id ?? null, chatId));
  const go = (...request: Parameters<typeof openPaneTab>) => {
    openPaneTab(...request);
    onDone();
  };
  const documents = links.filter((link) => link.group === 'artifact');
  const made = files.filter((file) => file.origin === 'created' || file.origin === 'edited');
  const outputs = [
    ...documents.map((link) => {
      const path = paneDocTargetOf(link.url);
      const label = link.label ?? presentLink(link.url).label;
      return {
        key: link.url,
        icon: <SiteIcon href={link.url} />,
        label,
        detail: link.url,
        open: () => (path ? go('artifacts', path, label) : go('artifacts')),
        openInPane: undefined,
      };
    }),
    ...made.map((file) => {
      const name = file.path.split('/').at(-1) ?? file.path;
      // A click on a file is the window, as on a file card; the tab of the panel is the second way in.
      return {
        key: file.path,
        icon: <FileText />,
        label: name,
        detail: file.path,
        open: () => {
          onViewFile(file, made);
          onDone();
        },
        openInPane: () => go('files', file.path, name),
      };
    }),
  ];
  const sideChats = pane.tabs.filter((paneTab) => paneTab.kind === 'side-chat');
  const sources = links.filter((link) => link.group !== 'artifact');

  return (
    <div className="tw-rpane-summary">
      <Section title={t('pane.paneSummary.results')}>
        {outputs.length === 0 ? (
          <p className="tw-rpane-summary-empty">{t('pane.paneSummary.resultsEmpty')}</p>
        ) : (
          <ul>
            {outputs.slice(0, OUTPUTS_SHOWN).map((item) => (
              <Row
                key={item.key}
                icon={item.icon}
                label={item.label}
                detail={item.detail}
                onOpen={item.open}
                onOpenInPane={item.openInPane}
              />
            ))}
          </ul>
        )}
        {outputs.length > OUTPUTS_SHOWN && (
          <ul>
            <Row
              icon={<Link2 />}
              label={t('pane.paneSummary.more', { n: outputs.length - OUTPUTS_SHOWN })}
              onOpen={() => go(made.length > documents.length ? 'files' : 'artifacts')}
            />
          </ul>
        )}
      </Section>
      <Section
        title={t('pane.paneSummary.sideChats')}
        action={{ label: t('pane.paneSummary.newSideChat'), icon: Plus, onClick: () => go('side-chat') }}
      >
        {sideChats.length === 0 ? (
          <p className="tw-rpane-summary-empty">{t('pane.paneSummary.sideChatsEmpty')}</p>
        ) : (
          <ul>
            {sideChats.map((paneTab) => (
              <Row
                key={paneTab.id}
                icon={<MessageCircle />}
                label={paneTab.title ?? t('pane.paneSummary.sideChat')}
                onOpen={() => go('side-chat', paneTab.target, paneTab.title)}
              />
            ))}
          </ul>
        )}
      </Section>
      <Section title={t('pane.paneSummary.sources')}>
        {sources.length === 0 && (
          <p className="tw-rpane-summary-empty">{t('pane.paneSummary.sourcesEmpty')}</p>
        )}
        <ul>
          {sources.slice(0, SOURCES_SHOWN).map((link) => {
            const label = link.label ?? presentLink(link.url).label;
            const task = taskKeyOf(link.url);
            const open = () =>
              task
                ? go('task', task, task)
                : go(link.group === 'mr' && mode === 'agent' ? 'gitlab' : 'artifacts');
            return (
              <Row
                key={link.url}
                icon={<SiteIcon href={link.url} />}
                label={label}
                detail={link.url}
                onOpen={open}
              />
            );
          })}
          {sources.length > 0 && (
            <Row icon={<Link2 />} label={t('pane.paneSummary.allSources')} onOpen={() => go('artifacts')} />
          )}
        </ul>
      </Section>
    </div>
  );
};

interface IPaneSummaryProps extends Omit<ISummaryContentProps, 'onDone' | 'onViewFile'> {
  open: boolean;
  onClose: (reason?: TDismissReason) => void;
  anchorRef: RefObject<HTMLElement | null>;
}

/** Codex's summary of a conversation from the header button: outputs, side chats and sources; a row opens its tab. */
const PaneSummary = ({ open, onClose, anchorRef, ...conversation }: IPaneSummaryProps) => {
  // The window outlives the popover it was opened from, so it is kept here with the files it pages through.
  const [viewing, setViewing] = useState<{ file: IViewableFile; files: IViewableFile[] } | null>(null);
  return (
    <>
      <TwPopover
        open={open}
        onClose={onClose}
        anchorRef={anchorRef}
        align={LAYER_ALIGN.end}
        aria-label={t('pane.paneSummary.summary')}
        className="tw-rpane-summary-popover"
      >
        {open && (
          <SummaryContent
            {...conversation}
            onDone={() => onClose()}
            onViewFile={(file, files) => setViewing({ file, files })}
          />
        )}
      </TwPopover>
      <FileViewer
        file={viewing?.file ?? null}
        files={viewing?.files}
        onNavigate={(file) => setViewing((current) => (current ? { ...current, file } : current))}
        onClose={() => setViewing(null)}
        onOpenInPane={(file) => openPaneTab('files', file.path, file.path.split('/').at(-1) ?? file.path)}
      />
    </>
  );
};

export default PaneSummary;
