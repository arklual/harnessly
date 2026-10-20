import { useState } from 'react';
import { TextQuote } from 'lucide-react';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import type { TUserSegment } from '@/lib/message-wrappers';
import { INTENT_LABELS } from '@/lib/hl-text-annotation';
import { t } from '@/lib/i18n';

const FROM_LABELS = {
  agent: t('timeline.textAnnotationSegment.fromAgent'),
  user: t('timeline.textAnnotationSegment.fromUser'),
} as const;

/** A fragment of the timeline sent with the message: the label as the composer's chip had it, the quote, the comment; the block the agent got on demand. */
const TextAnnotationSegment = ({ segment }: { segment: Extract<TUserSegment, { kind: 'annotation' }> }) => {
  const holdPosition = useHoldTimelinePosition();
  const [open, setOpen] = useState(false);
  const { quote } = segment;
  if (!quote) return null;
  return (
    <div className="tw-wrapped tw-wrapped-quote">
      <div className="tw-wrapped-head">
        <TextQuote />
        <span
          className="tw-wrapped-annotation-label"
          title={segment.label}
        >{`#${segment.n} ${INTENT_LABELS[quote.intent]}`}</span>
        <button
          type="button"
          className="tw-wrapped-toggle"
          aria-expanded={open}
          onClick={() => {
            holdPosition();
            setOpen((v) => !v);
          }}
        >
          {open
            ? t('timeline.textAnnotationSegment.hideBlock')
            : t('timeline.textAnnotationSegment.asAgentSees')}
        </button>
      </div>
      <blockquote className="tw-quote-text">
        <p>{quote.text}</p>
        <small>{FROM_LABELS[quote.from]}</small>
      </blockquote>
      {segment.comment && <p className="tw-quote-comment">{segment.comment}</p>}
      {open && <pre className="tw-wrapped-raw">{segment.text}</pre>}
    </div>
  );
};

export default TextAnnotationSegment;
