// A file the agent handed to the person, as the turn's other cards (output-row.tsx): its type's icon, its name, «PDF ·
// 278 КБ» small under it and a download icon at the end. A tap on the card opens it in the right pane, the icon saves
// it, ⌘/Ctrl-click opens it in a new tab of the browser. The browser has already saved it once when it came
// (auto-download.ts). Only the call that names the file makes one: files the agent merely wrote never do.
import type { TurnOutput } from '@/next/features/chat/types';
import { bytes } from '@/next/features/pane/machine/charts';
import Icon from '@/next/ui/icon';
import OutputRow from '@/next/features/chat/output-row';
import { t } from '@/next/i18n';

interface IDeliveryCardProps {
  output: TurnOutput;
  index: number;
  onOpen?: (output: TurnOutput) => void;
  downloadUrl?: (path: string, output?: TurnOutput) => string;
  /** Where the browser shows the file in a tab of its own (⌘/Ctrl-click). */
  viewUrl?: (path: string, output?: TurnOutput) => string;
}

const iconOf = (mime: string | undefined): string => {
  if (!mime) return 'file-text';
  if (mime.startsWith('image/')) return 'image';
  if (mime.startsWith('video/')) return 'film';
  if (mime.startsWith('audio/')) return 'music';
  if (mime === 'application/zip' || mime === 'application/gzip' || mime === 'application/x-tar')
    return 'files';
  if (mime.includes('spreadsheet') || mime === 'text/csv' || mime === 'application/vnd.ms-excel')
    return 'file-spreadsheet';
  if (mime === 'text/html' || mime === 'application/json') return 'file-code';
  return 'file-text';
};

/** What kind of file it is, as the person names it: its extension in capitals (PDF, ZIP, HTML), else the type's. */
export const kindOf = (output: Pick<TurnOutput, 'name' | 'ref' | 'mime'>): string => {
  const name = output.name ?? output.ref;
  const dot = name.lastIndexOf('.');
  const ext = dot > 0 ? name.slice(dot + 1) : '';
  if (ext && ext.length <= 5) return ext.toUpperCase();
  if (!output.mime || output.mime === 'application/octet-stream') return t('next.delivered.file');
  return (output.mime.split('/')[1] ?? output.mime).toUpperCase();
};

const metaOf = (output: TurnOutput): string =>
  output.size === undefined ? kindOf(output) : `${kindOf(output)} · ${bytes(output.size)}`;

const DeliveryCard = ({ output, index, onOpen, downloadUrl, viewUrl }: IDeliveryCardProps) => {
  const save = downloadUrl?.(output.ref, output) || undefined;
  const label = t('next.delivered.save');
  return (
    <OutputRow
      index={index}
      icon={iconOf(output.mime)}
      title={output.title}
      meta={metaOf(output)}
      href={viewUrl?.(output.ref, output) || undefined}
      newTab
      onOpen={onOpen ? () => onOpen(output) : null}
      end={
        save && (
          <a
            className="hx-head-btn hx-file__share hx-file__save"
            href={save}
            download
            aria-label={label}
            title={label}
          >
            <Icon name="download" size="sm" />
          </a>
        )
      }
    />
  );
};

export default DeliveryCard;
