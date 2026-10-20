import { useState, type ImgHTMLAttributes } from 'react';
import AttachmentViewer, { type IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import { t } from '@/lib/i18n';

/** A picture inside an answer: a compact preview that opens in the viewer instead of filling the column. */
const MessageMarkdownImage = ({ src, alt }: ImgHTMLAttributes<HTMLImageElement>) => {
  const [viewing, setViewing] = useState<IAttachmentView | null>(null);
  if (typeof src !== 'string' || !src) return null;
  return (
    <>
      <button
        type="button"
        className="tw-msg-figure"
        onClick={() =>
          setViewing({ kind: 'image', url: src, name: alt || t('timeline.messageMarkdownImage.image') })
        }
        aria-label={t('timeline.messageMarkdownImage.open', {
          name: alt || t('timeline.messageMarkdownImage.imageLower'),
        })}
      >
        <img src={src} alt={alt ?? ''} loading="lazy" decoding="async" draggable={false} />
      </button>
      <AttachmentViewer item={viewing} onClose={() => setViewing(null)} />
    </>
  );
};

export default MessageMarkdownImage;
