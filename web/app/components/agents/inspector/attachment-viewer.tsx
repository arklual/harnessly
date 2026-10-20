import { t } from '@/lib/i18n';
import { useEffect, type KeyboardEvent } from 'react';
import { ChevronLeft, ChevronRight, Download, FileText, Music, Play } from 'lucide-react';
import ZoomableImage from '@/components/agents/inspector/zoomable-image';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import type { TAttachmentKind } from '@/lib/attachment-media';
import { downloadNameOf, keyStep, pageTo, type TPageStep } from '@/lib/viewer-paging';

export interface IAttachmentView {
  kind: Exclude<TAttachmentKind, 'file'>;
  url: string;
  name: string;
}

export const AttachmentIcon = ({ kind }: { kind: TAttachmentKind }) => {
  if (kind === 'video') return <Play aria-hidden="true" />;
  if (kind === 'audio') return <Music aria-hidden="true" />;
  return <FileText aria-hidden="true" />;
};

const AttachmentBody = ({
  item,
  onSwipe,
}: {
  item: IAttachmentView;
  onSwipe?: (step: TPageStep) => void;
}) => {
  if (item.kind === 'image')
    return <ZoomableImage key={item.url} src={item.url} alt={item.name} onSwipe={onSwipe} />;
  if (item.kind === 'video')
    return <video key={item.url} className="tw-viewer-media" src={item.url} controls autoPlay playsInline />;
  if (item.kind === 'audio')
    return <audio key={item.url} className="tw-viewer-audio" src={item.url} controls autoPlay />;
  return <iframe key={item.url} src={item.url} title={item.name} className="tw-viewer-frame" />;
};

interface IAttachmentViewerProps {
  item: IAttachmentView | null;
  onClose: () => void;
  /** The set the item belongs to: with it and `onSelect` the viewer pages through the pictures (← →, swipe, arrows). */
  items?: readonly IAttachmentView[];
  onSelect?: (item: IAttachmentView) => void;
}

/** Full-size view of something the person attached: a picture, a video, a recording or a pdf. */
const AttachmentViewer = ({ item, onClose, items = [], onSelect }: IAttachmentViewerProps) => {
  const index = item ? items.findIndex((candidate) => candidate.url === item.url) : -1;
  const canPage = !!onSelect && index !== -1 && items.length > 1;
  const page = (step: -1 | 1) => {
    const next = canPage ? pageTo(items, index, step) : null;
    if (next) onSelect?.(next);
  };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const step = keyStep(event.key);
    if (step === 0 || !canPage) return;
    if (event.target instanceof HTMLElement && event.target.closest('video, audio, input, textarea')) return;
    event.preventDefault();
    page(step);
  };

  // The next pictures load while the current one is looked at, so paging does not wait for them.
  useEffect(() => {
    if (!canPage) return;
    for (const step of [-1, 1] as const) {
      const neighbour = pageTo(items, index, step);
      if (neighbour?.kind === 'image') new Image().src = neighbour.url;
    }
  }, [canPage, items, index]);

  return (
    <Dialog
      open={item !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent
        className="tw-island-dialog tw-viewer"
        aria-describedby={undefined}
        onKeyDown={onKeyDown}
      >
        <div className="tw-viewer-head">
          <DialogTitle className="tw-island-dialog-title">
            {item?.name ?? t('inspector.attachmentViewer.attachment')}
          </DialogTitle>
          {canPage && (
            <span className="tw-viewer-count">
              {index + 1} / {items.length}
            </span>
          )}
          {item && (
            <a
              className="tw-icon-btn"
              href={item.url}
              download={downloadNameOf(item.name, item.url)}
              title={t('inspector.attachmentViewer.download')}
              aria-label={t('inspector.attachmentViewer.download')}
            >
              <Download />
            </a>
          )}
        </div>
        {item && (
          <div className="tw-viewer-stage">
            {canPage && (
              <>
                <button
                  type="button"
                  className="tw-viewer-nav tw-viewer-prev"
                  aria-label={t('inspector.attachmentViewer.previous')}
                  title={t('inspector.attachmentViewer.previousHint')}
                  onClick={() => page(-1)}
                >
                  <ChevronLeft />
                </button>
                <button
                  type="button"
                  className="tw-viewer-nav tw-viewer-next"
                  aria-label={t('inspector.attachmentViewer.next')}
                  title={t('inspector.attachmentViewer.nextHint')}
                  onClick={() => page(1)}
                >
                  <ChevronRight />
                </button>
              </>
            )}
            <div className="tw-viewer-body" data-kind={item.kind}>
              <AttachmentBody
                item={item}
                onSwipe={canPage ? (step) => step !== 0 && page(step) : undefined}
              />
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
};

export default AttachmentViewer;
