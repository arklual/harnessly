import { TextQuote } from 'lucide-react';
import { ANNOTATE_MESSAGE_EVENT } from '@/lib/hl-selection-target';
import { t } from '@/lib/i18n';

/** For whoever cannot select with a long tap (or only with the keyboard): comment on the selected part of the answer, or on all of it. */
const AnnotateMessageButton = () => (
  <button
    type="button"
    onClick={(event) =>
      event.currentTarget.dispatchEvent(new CustomEvent(ANNOTATE_MESSAGE_EVENT, { bubbles: true }))
    }
    aria-label={t('timeline.annotateMessageButton.label')}
  >
    <TextQuote size={13} />
    <span>{t('timeline.annotateMessageButton.comment')}</span>
  </button>
);

export default AnnotateMessageButton;
