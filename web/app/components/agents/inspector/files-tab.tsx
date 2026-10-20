import { t } from '@/lib/i18n';
import { memo, useState, type MouseEvent } from 'react';
import dayjs from 'dayjs';
import { FileText, Film, Globe2, ImageIcon, FileType2, PanelRight } from 'lucide-react';
import { filePreviewUrl } from '@/components/agents/inspector/file-viewer';
import type { IViewableFile } from '@/lib/hl-artifacts';

const ORIGIN_LABEL: Record<IViewableFile['origin'], string> = {
  created: t('inspector.filesTab.originCreated'),
  edited: t('inspector.filesTab.originEdited'),
  viewed: t('inspector.filesTab.originViewed'),
  mentioned: t('inspector.filesTab.originMentioned'),
};
const KIND_ICON = {
  image: ImageIcon,
  video: Film,
  markdown: FileText,
  html: Globe2,
  pdf: FileType2,
  text: FileText,
};
const KIND_LABEL = {
  image: t('inspector.filesTab.kindImage'),
  video: t('inspector.filesTab.kindVideo'),
  markdown: 'md',
  html: 'html',
  pdf: 'pdf',
  text: t('inspector.filesTab.kindText'),
};

const Thumb = ({ file }: { file: IViewableFile }) => {
  const [failed, setFailed] = useState(false);
  const Icon = KIND_ICON[file.kind];
  if (file.kind === 'image' && !failed)
    return <img src={filePreviewUrl(file.path)} alt="" loading="lazy" onError={() => setFailed(true)} />;
  return (
    <span className={`tw-file-glyph tw-file-glyph-${file.kind}`}>
      <Icon />
      <b>{KIND_LABEL[file.kind]}</b>
    </span>
  );
};

const MIDDLE_BUTTON = 1;

interface IFilesTabProps {
  files: IViewableFile[];
  /** A plain click: the file in a window over the conversation. */
  onOpen: (file: IViewableFile) => void;
  /** The middle button, a click with Ctrl or ⌘, or the button on the card: the file as a tab of the right panel. */
  onOpenInPane?: (file: IViewableFile) => void;
}

/** Pictures and documents from this conversation as cards. */
const FilesTab = ({ files, onOpen, onOpenInPane }: IFilesTabProps) => {
  const click = (event: MouseEvent, file: IViewableFile) => {
    if (onOpenInPane && (event.metaKey || event.ctrlKey)) onOpenInPane(file);
    else onOpen(file);
  };
  const auxClick = (event: MouseEvent, file: IViewableFile) => {
    if (!onOpenInPane || event.button !== MIDDLE_BUTTON) return;
    event.preventDefault();
    onOpenInPane(file);
  };
  // The middle button starts autoscroll on press in some browsers; the press is taken so the click that follows is ours.
  const press = (event: MouseEvent) => {
    if (onOpenInPane && event.button === MIDDLE_BUTTON) event.preventDefault();
  };
  return (
    <ul className="tw-file-grid">
      {files.map((file) => {
        const name = file.path.split('/').at(-1);
        return (
          <li key={file.path} className="tw-file-item">
            <button
              type="button"
              className="tw-file-card"
              title={`${file.path} · ${dayjs(file.timestamp).format('HH:mm')}${onOpenInPane ? `\n${t('inspector.filesTab.middleClickHint')}` : ''}`}
              onClick={(event) => click(event, file)}
              onAuxClick={(event) => auxClick(event, file)}
              onMouseDown={press}
            >
              <span className="tw-file-thumb">
                <Thumb file={file} />
              </span>
              <span className="tw-file-name">{name}</span>
              <span className="tw-file-meta">
                {ORIGIN_LABEL[file.origin]} · {dayjs(file.timestamp).format('HH:mm')}
              </span>
            </button>
            {onOpenInPane && (
              <button
                type="button"
                className="tw-icon-button tw-file-pane"
                aria-label={t('inspector.filesTab.openAsTabNamed', { name: name ?? '' })}
                title={t('inspector.filesTab.openAsTabHint')}
                onClick={() => onOpenInPane(file)}
              >
                <PanelRight />
              </button>
            )}
          </li>
        );
      })}
    </ul>
  );
};

export default memo(FilesTab);
