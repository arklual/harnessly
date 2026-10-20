import { X } from 'lucide-react';
import { AttachmentIcon, type IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import type { TAttachmentKind } from '@/lib/attachment-media';
import { t } from '@/lib/i18n';

export interface IAttachmentChip {
  id: string;
  filename: string;
  thumbnail: string;
  kind: TAttachmentKind;
  /** Still on its way to the machine: the chip shows a loader, the message can already go out. */
  uploading?: boolean;
}

interface IAttachmentChipProps {
  attachment: IAttachmentChip;
  removeLabel: string;
  onOpen: (view: IAttachmentView) => void;
  onRemove: (id: string) => void;
}

/** A file attached to the message being written: opens in the viewer, × takes it off. */
const AttachmentChip = ({ attachment, removeLabel, onOpen, onRemove }: IAttachmentChipProps) => {
  const { id, filename, thumbnail, kind, uploading } = attachment;
  const label = (
    <span className="tw-cattach-file">
      <AttachmentIcon kind={kind} />
      <span>{filename}</span>
    </span>
  );
  return (
    <div
      className="tw-cattach-item"
      data-kind={kind}
      data-uploading={uploading || undefined}
      title={filename}
      aria-busy={uploading || undefined}
    >
      {kind === 'file' ? (
        label
      ) : (
        <button
          type="button"
          className="tw-cattach-open"
          onClick={() => onOpen({ kind, url: thumbnail, name: filename })}
          aria-label={t('composer.attachmentChip.open', { filename })}
        >
          {kind === 'image' ? <img src={thumbnail} alt="" /> : label}
        </button>
      )}
      {uploading && <span className="tw-cattach-uploading" aria-hidden="true" />}
      <button
        type="button"
        className="tw-cattach-remove"
        onClick={() => onRemove(id)}
        aria-label={removeLabel}
      >
        <X aria-hidden="true" />
      </button>
    </div>
  );
};

export default AttachmentChip;
