import { memo, useMemo, useState } from 'react';
import dayjs from 'dayjs';
import { ImageIcon } from 'lucide-react';
import AttachmentViewer, { type IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import useAvailableFiles from '@/hooks/use-available-files';
import { collectViewableFiles } from '@/lib/hl-artifacts';
import { collectConversationImages, type IGalleryImage } from '@/lib/hl-gallery';
import { cn } from '@/lib/utils';
import type { ITimelineEntry } from '@/types/timeline';
import { t } from '@/lib/i18n';

type TGalleryFilter = 'all' | IGalleryImage['source'];

const FILTERS: readonly { id: TGalleryFilter; label: string }[] = [
  { id: 'all', label: t('gallery.galleryView.filterAll') },
  { id: 'user', label: t('gallery.galleryView.filterMine') },
  { id: 'agent', label: t('gallery.galleryView.filterAgent') },
];

const SOURCE_LABEL: Record<IGalleryImage['source'], string> = {
  user: t('gallery.galleryView.sourceYou'),
  agent: t('gallery.galleryView.sourceAgent'),
};

interface IGalleryViewProps {
  entries: readonly ITimelineEntry[];
  tabId: string | null;
}

const GalleryBody = ({ entries }: { entries: readonly ITimelineEntry[] }) => {
  const [filter, setFilter] = useState<TGalleryFilter>('all');
  const [viewing, setViewing] = useState<IAttachmentView | null>(null);
  const files = useMemo(
    () => collectViewableFiles(entries as ITimelineEntry[]).filter((file) => file.kind === 'image'),
    [entries],
  );
  const available = useAvailableFiles(files);
  const images = useMemo(
    () => collectConversationImages(entries, new Set(available.map((file) => file.path))),
    [entries, available],
  );
  const shown = useMemo(
    () => (filter === 'all' ? images : images.filter((image) => image.source === filter)),
    [images, filter],
  );

  if (images.length === 0) {
    return (
      <div className="tw-gallery tw-gallery-empty">
        <ImageIcon aria-hidden="true" />
        <strong>{t('gallery.galleryView.emptyTitle')}</strong>
        <span>{t('gallery.galleryView.emptyHint')}</span>
      </div>
    );
  }

  return (
    <div className="tw-gallery">
      <div className="tw-gallery-head">
        <h3>{t('gallery.galleryView.title')}</h3>
        <span className="tw-gallery-count">{images.length}</span>
        <div className="tw-gallery-tabs" role="tablist" aria-label={t('gallery.galleryView.sourceTabs')}>
          {FILTERS.map((item) => (
            <button
              key={item.id}
              type="button"
              role="tab"
              aria-selected={filter === item.id}
              className={cn('tw-gallery-tab', filter === item.id && 'tw-gallery-tab-on')}
              onClick={() => setFilter(item.id)}
            >
              {item.label}
            </button>
          ))}
        </div>
      </div>
      {shown.length === 0 && <p className="tw-gallery-none">{t('gallery.galleryView.noneFromSource')}</p>}
      <ul className="tw-gallery-grid">
        {shown.map((image) => (
          <li key={image.key}>
            <button
              type="button"
              className="tw-gallery-tile"
              title={image.name}
              onClick={() => setViewing(image)}
            >
              <img src={image.url} alt="" loading="lazy" decoding="async" draggable={false} />
              <span className="tw-gallery-caption">
                <b>{image.name}</b>
                <small>
                  {SOURCE_LABEL[image.source]} · {dayjs(image.timestamp).format('HH:mm')}
                </small>
              </span>
            </button>
          </li>
        ))}
      </ul>
      <AttachmentViewer item={viewing} items={shown} onSelect={setViewing} onClose={() => setViewing(null)} />
    </div>
  );
};

/** Every picture of a conversation as a grid of cards; a card opens the viewer, which pages through the same set. */
const GalleryView = ({ entries, tabId }: IGalleryViewProps) => (
  <GalleryBody key={tabId ?? 'none'} entries={entries} />
);

export default memo(GalleryView);
