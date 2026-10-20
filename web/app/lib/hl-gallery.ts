import { t } from '@/lib/i18n';
import { scopedAppPath, stripAppPath } from '@/lib/app-path';
import { uploadedFilesInText, uploadUrlOfPath } from '@/lib/attachment-media';
import { filePreviewUrl } from '@/lib/file-preview-url';
import { collectViewableFiles } from '@/lib/hl-artifacts';
import type { IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import type { ITimelineEntry, ITimelineUserMessage } from '@/types/timeline';

export interface IGalleryImage extends IAttachmentView {
  kind: 'image';
  /** Same picture, same key: a file read, written and shown again, or an upload mentioned twice. */
  key: string;
  timestamp: number;
  source: 'user' | 'agent';
  entryId: string;
}

const MARKDOWN_IMAGE = /!\[([^\]\n]{0,200})\]\((https?:\/\/[^)\s]+)\)/g;
const MAX_THUMBS = 4;

const userImage = (entry: ITimelineUserMessage, src: string, name: string): IGalleryImage => ({
  kind: 'image',
  key: stripAppPath(src),
  url: scopedAppPath(src),
  name,
  timestamp: entry.timestamp,
  source: 'user',
  entryId: entry.id,
});

/** Pictures of one message of the person: the ones the transcript carries and uploaded ones the text points to. */
export const userMessageImages = (entry: ITimelineUserMessage): IGalleryImage[] => {
  const found = new Map<string, IGalleryImage>();
  const add = (image: IGalleryImage) => {
    if (!found.has(image.key)) found.set(image.key, image);
  };
  (entry.images ?? []).forEach((src, i) =>
    add(userImage(entry, src, t('lib.hlGallery.imageNumber', { number: i + 1 }))),
  );
  for (const file of uploadedFilesInText(entry.text)) {
    if (file.kind === 'image') add(userImage(entry, file.url, file.name));
  }
  return [...found.values()];
};

const agentImage = (entry: ITimelineEntry, key: string, url: string, name: string): IGalleryImage => ({
  kind: 'image',
  key,
  url,
  name,
  timestamp: entry.timestamp,
  source: 'agent',
  entryId: entry.id,
});

const embeddedImages = (entry: ITimelineEntry & { type: 'assistant-message' }): IGalleryImage[] =>
  [...(entry.markdown ?? '').matchAll(MARKDOWN_IMAGE)].map((match) =>
    agentImage(
      entry,
      match[2],
      match[2],
      match[1].trim() || match[2].split('/').at(-1)?.split('?')[0] || t('lib.hlGallery.image'),
    ),
  );

/**
 * Every picture of a conversation, newest first (the order of pictures inside one message is kept): what the person
 * sent, what the agent wrote or looked at, and what its answers embed. A picture shows once however it got in.
 */
export const collectConversationImages = (
  entries: readonly ITimelineEntry[],
  availableFiles: ReadonlySet<string>,
): IGalleryImage[] => {
  const units: { timestamp: number; images: IGalleryImage[] }[] = [];
  for (const entry of entries) {
    if (entry.type === 'user-message')
      units.push({ timestamp: entry.timestamp, images: userMessageImages(entry) });
    else if (entry.type === 'assistant-message')
      units.push({ timestamp: entry.timestamp, images: embeddedImages(entry) });
  }
  for (const file of collectViewableFiles(entries as ITimelineEntry[])) {
    if (file.kind !== 'image' || !availableFiles.has(file.path)) continue;
    const name = file.path.split('/').at(-1) ?? file.path;
    const uploaded = uploadUrlOfPath(file.path);
    const entry = { id: file.path, timestamp: file.timestamp } as ITimelineEntry;
    units.push({
      timestamp: file.timestamp,
      images: [
        uploaded
          ? agentImage(entry, uploaded, scopedAppPath(uploaded), name)
          : agentImage(entry, file.path, filePreviewUrl(file.path), name),
      ],
    });
  }
  units.sort((a, b) => b.timestamp - a.timestamp);

  const seen = new Set<string>();
  const images: IGalleryImage[] = [];
  for (const unit of units) {
    for (const image of unit.images) {
      if (seen.has(image.key)) continue;
      seen.add(image.key);
      images.push(image);
    }
  }
  return images;
};

/** How a message of `total` pictures lays out its thumbnails: up to four, the rest folded into a «+N» counter. */
export const thumbSlots = (total: number): { shown: number; hidden: number } => {
  const shown = Math.min(total, MAX_THUMBS);
  return { shown, hidden: total - shown };
};
