// A file the agent handed to the person: its name biggest, its size under it. The card opens the file in the right pane;
// the icon beside it saves it. Only the call that names the file makes one: files the agent merely wrote never do.
import type { TurnOutput } from '@/next/features/chat/types';
import { bytes } from '@/next/features/pane/machine/charts';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

interface IDeliveryCardProps {
  output: TurnOutput;
  index: number;
  onOpen?: (output: TurnOutput) => void;
  downloadUrl?: (path: string) => string;
}

const iconOf = (mime: string | undefined): string => {
  if (!mime) return 'file-text';
  if (mime.startsWith('image/')) return 'image';
  if (mime === 'application/zip' || mime === 'application/gzip' || mime === 'application/x-tar')
    return 'files';
  if (mime.includes('spreadsheet') || mime === 'text/csv' || mime === 'application/vnd.ms-excel')
    return 'file-spreadsheet';
  if (mime === 'text/html' || mime === 'application/json') return 'file-code';
  return 'file-text';
};

const DeliveryCard = ({ output, index, onOpen, downloadUrl }: IDeliveryCardProps) => {
  const body = (
    <>
      <span className="hx-file__ico" data-tone="accent">
        <Icon name={iconOf(output.mime)} />
      </span>
      <span className="hx-file__text">
        <span className="hx-file__name">{output.title}</span>
        {output.size !== undefined && <span className="hx-file__meta">{bytes(output.size)}</span>}
      </span>
    </>
  );
  const href = downloadUrl?.(output.ref);
  return (
    <div className="hx-file hx-file--delivery" data-j={index}>
      {onOpen ? (
        <button type="button" className="hx-file__open" onClick={() => onOpen(output)}>
          {body}
        </button>
      ) : (
        <div className="hx-file__open">{body}</div>
      )}
      {href && (
        <a className="hx-file__save" href={href} download aria-label={t('next.delivered.save')}>
          <Icon name="download" size="sm" />
        </a>
      )}
    </div>
  );
};

export default DeliveryCard;
