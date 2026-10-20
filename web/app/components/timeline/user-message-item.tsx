// A message the person sent: its text (long ones folded), attached images and files, and its delivery state.
import { useLayoutEffect, useRef, useState } from 'react';
import { Clock3 } from 'lucide-react';
import MessageImageGrid from '@/components/timeline/message-image-grid';
import AttachmentViewer, { type IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import type { IGalleryImage } from '@/lib/hl-gallery';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

/** Text folded to a few lines with a "show all" switch when it is longer. */
export function ClampedText({
  text,
  lines = 6,
  className,
}: {
  text: string;
  lines?: 4 | 6;
  className?: string;
}) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (el && !expanded) setOverflows(el.scrollHeight > el.clientHeight + 1);
  }, [text, expanded]);
  return (
    <div className={className}>
      <div
        ref={ref}
        className={cn(
          'text-sm whitespace-pre-wrap break-words',
          !expanded && (lines === 4 ? 'line-clamp-4' : 'line-clamp-6'),
        )}
      >
        {text}
      </div>
      {(overflows || expanded) && (
        <button
          type="button"
          className="text-xs mt-1.5 text-muted-foreground hover:underline"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded ? t('timeline.userMessageItem.collapse') : t('timeline.userMessageItem.showFull')}
        </button>
      )}
    </div>
  );
}

interface IUserMessageItemProps {
  entry: { id: string; text?: string; images?: string[]; queued?: boolean; pending?: boolean };
}

export default function UserMessageItem({ entry }: IUserMessageItemProps) {
  const text = (entry.text ?? '').trim();
  const [viewing, setViewing] = useState<IAttachmentView | null>(null);
  const images: IGalleryImage[] = (entry.images ?? [])
    .map((src, i) => ({ url: src, name: t('timeline.userMessageItem.imageN', { n: i + 1 }) }))
    .filter((img) => img.url)
    .map((img) => ({
      kind: 'image' as const,
      url: img.url,
      name: img.name,
      key: img.url,
      timestamp: 0,
      source: 'user' as const,
      entryId: entry.id,
    }));
  return (
    <div className="flex flex-col items-end">
      {entry.queued && (
        <span className="tw-queued-label">
          <Clock3 size={12} /> {t('timeline.userMessageItem.queued')}
        </span>
      )}
      <div
        className={cn(
          'agent-user-bubble px-4 py-2.5 max-w-[85%] transition-opacity duration-200',
          entry.pending && 'opacity-70',
        )}
      >
        {images.length > 0 && (
          <div className={cn('flex flex-col items-start gap-2', text && 'mb-2')}>
            <MessageImageGrid images={images} onOpen={setViewing} />
          </div>
        )}
        {text && <ClampedText text={text} />}
      </div>
      {viewing && <AttachmentViewer item={viewing} onClose={() => setViewing(null)} />}
    </div>
  );
}
