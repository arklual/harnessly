import { thumbSlots, type IGalleryImage } from '@/lib/hl-gallery';
import { useIsUploading } from '@/lib/upload-progress';
import type { IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import { t } from '@/lib/i18n';

interface IMessageImageGridProps {
  images: readonly IGalleryImage[];
  onOpen: (item: IAttachmentView) => void;
}

const UploadingMark = ({ url }: { url: string }) =>
  useIsUploading(url) ? (
    <span
      className="tw-msg-thumb-uploading"
      role="status"
      aria-label={t('timeline.messageImageGrid.uploading')}
    />
  ) : null;

/** The pictures of a message as small tiles: up to four, the rest folded into a «+N» on the last one. */
const MessageImageGrid = ({ images, onOpen }: IMessageImageGridProps) => {
  const { shown, hidden } = thumbSlots(images.length);
  return (
    <div className="tw-msg-thumbs" data-count={images.length}>
      {images.slice(0, shown).map((image, i) => {
        const folded = hidden > 0 && i === shown - 1;
        return (
          <button
            key={image.key}
            type="button"
            className="tw-msg-thumb"
            onClick={() => onOpen(image)}
            aria-label={
              folded
                ? t('timeline.messageImageGrid.andMore', { name: image.name, count: hidden })
                : t('timeline.messageImageGrid.open', { name: image.name })
            }
          >
            <img src={image.url} alt="" loading="lazy" decoding="async" draggable={false} />
            {folded && <span className="tw-msg-thumb-more">+{hidden}</span>}
            <UploadingMark url={image.url} />
          </button>
        );
      })}
    </div>
  );
};

export default MessageImageGrid;
