import { t } from '@/lib/i18n';
import { useEffect, useState, type KeyboardEvent, type ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ChevronLeft, ChevronRight, Copy, Download, ExternalLink, PanelRight } from 'lucide-react';
import { toast } from 'sonner';
import ZoomableImage from '@/components/agents/inspector/zoomable-image';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { appFetch } from '@/lib/app-path';
import { fileDownloadUrl, filePreviewUrl } from '@/lib/file-preview-url';
import type { IViewableFile } from '@/lib/hl-artifacts';

export { filePreviewUrl };

const MarkdownBody = ({ url }: { url: string }) => {
  const [state, setState] = useState<{ url: string; text?: string; error?: string } | null>(null);
  useEffect(() => {
    let alive = true;
    appFetch(url)
      .then(async (res) =>
        res.ok
          ? { url, text: await res.text() }
          : {
              url,
              error:
                ((await res.json().catch(() => null)) as { error?: string } | null)?.error ??
                t('inspector.fileViewer.openFailed'),
            },
      )
      .then((next) => {
        if (alive) setState(next);
      })
      .catch(() => {
        if (alive) setState({ url, error: t('inspector.fileViewer.openFailed') });
      });
    return () => {
      alive = false;
    };
  }, [url]);
  if (!state || state.url !== url)
    return <div className="tw-viewer-empty">{t('inspector.fileViewer.opening')}</div>;
  if (state.error) return <div className="tw-viewer-empty">{state.error}</div>;
  return (
    <article className="tw-viewer-md">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{state.text ?? ''}</ReactMarkdown>
    </article>
  );
};

interface IFileViewerContentProps {
  file: IViewableFile;
  files?: IViewableFile[];
  onNavigate?: (file: IViewableFile) => void;
  /** The heading element: a dialog title inside the dialog, a plain heading in the reader pane. */
  heading: (name: string) => ReactNode;
  /** Buttons of the place the file is shown in, after the file's own. */
  actions?: ReactNode;
}

/** Name, path, paging and the file itself: images inline, markdown rendered, html and pdf in an isolated frame. */
export const FileViewerContent = ({
  file,
  files = [],
  onNavigate,
  heading,
  actions,
}: IFileViewerContentProps) => {
  const url = filePreviewUrl(file.path);
  const name = file.path.split('/').at(-1) ?? '';
  const index = files.findIndex((f) => f.path === file.path);
  const canPage = !!onNavigate && index !== -1 && files.length > 1;
  const page = (step: number) => {
    if (!canPage) return;
    onNavigate(files[(index + step + files.length) % files.length]);
  };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (
      !canPage ||
      (event.target instanceof HTMLElement &&
        event.target.closest('input, textarea, [contenteditable="true"]'))
    )
      return;
    if (event.key === 'ArrowLeft') {
      event.preventDefault();
      page(-1);
    }
    if (event.key === 'ArrowRight') {
      event.preventDefault();
      page(1);
    }
  };
  return (
    <div className="tw-viewer-content" tabIndex={-1} onKeyDown={onKeyDown}>
      <div className="tw-viewer-head">
        {heading(name)}
        {canPage && (
          <span className="tw-viewer-count">
            {index + 1} / {files.length}
          </span>
        )}
        <small title={file.path}>
          <span dir="ltr">{file.path.split('/').slice(0, -1).join('/')}</span>
        </small>
        <button
          type="button"
          className="tw-icon-btn"
          title={t('inspector.fileViewer.copyPath')}
          onClick={() =>
            void navigator.clipboard
              .writeText(file.path)
              .then(() => toast.success(t('inspector.fileViewer.pathCopied')))
          }
        >
          <Copy />
        </button>
        {file.kind === 'image' || file.kind === 'video' ? (
          <a
            className="tw-icon-btn"
            href={file.kind === 'video' ? fileDownloadUrl(file.path) : url}
            download={name}
            title={t('inspector.fileViewer.download')}
            aria-label={t('inspector.fileViewer.download')}
          >
            <Download />
          </a>
        ) : (
          <a
            className="tw-icon-btn"
            href={url}
            target="_blank"
            rel="noreferrer"
            title={t('inspector.fileViewer.openInNewTab')}
          >
            <ExternalLink />
          </a>
        )}
        {actions}
      </div>
      <div className="tw-viewer-stage">
        {canPage && (
          <>
            <button
              type="button"
              className="tw-viewer-nav tw-viewer-prev"
              aria-label={t('inspector.fileViewer.previousFile')}
              title={t('inspector.fileViewer.previousHint')}
              onClick={() => page(-1)}
            >
              <ChevronLeft />
            </button>
            <button
              type="button"
              className="tw-viewer-nav tw-viewer-next"
              aria-label={t('inspector.fileViewer.nextFile')}
              title={t('inspector.fileViewer.nextHint')}
              onClick={() => page(1)}
            >
              <ChevronRight />
            </button>
          </>
        )}
        <div className="tw-viewer-body">
          {file.kind === 'image' && <ZoomableImage key={url} src={url} alt={name} />}
          {file.kind === 'video' && (
            <video key={url} src={url} controls playsInline preload="metadata" className="tw-viewer-media" />
          )}
          {file.kind === 'markdown' && <MarkdownBody url={url} />}
          {file.kind === 'html' && (
            <iframe src={url} title={name} sandbox="allow-scripts" className="tw-viewer-frame" />
          )}
          {file.kind === 'pdf' && <iframe src={url} title={name} className="tw-viewer-frame" />}
        </div>
      </div>
    </div>
  );
};

interface IFileViewerProps {
  file: IViewableFile | null;
  files?: IViewableFile[];
  onNavigate?: (file: IViewableFile) => void;
  onClose: () => void;
  /** Moves the file from the window into a tab of the right panel. */
  onOpenInPane?: (file: IViewableFile) => void;
}

/** Full-size view of an agent-produced file in a dialog. */
const FileViewer = ({ file, files = [], onNavigate, onClose, onOpenInPane }: IFileViewerProps) => (
  <Dialog
    open={file !== null}
    onOpenChange={(open) => {
      if (!open) onClose();
    }}
  >
    <DialogContent className="tw-island-dialog tw-viewer">
      {file && (
        <FileViewerContent
          file={file}
          files={files}
          onNavigate={onNavigate}
          heading={(name) => <DialogTitle className="tw-island-dialog-title">{name}</DialogTitle>}
          actions={
            onOpenInPane && (
              <button
                type="button"
                className="tw-icon-btn"
                title={t('inspector.fileViewer.openAsTab')}
                aria-label={t('inspector.fileViewer.openAsTab')}
                onClick={() => {
                  onOpenInPane(file);
                  onClose();
                }}
              >
                <PanelRight />
              </button>
            )
          }
        />
      )}
    </DialogContent>
  </Dialog>
);

export default FileViewer;
