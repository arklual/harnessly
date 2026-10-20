import { MousePointerClick, X } from 'lucide-react';
import { toast } from 'sonner';
import { annotationLabel } from '@/lib/hl-annotation-format';
import TextAnnotationChipRow from '@/components/agents/text-annotation-chip-row';
import {
  isTextAnnotation,
  removeAnnotation,
  useAnnotations,
  type IAnnotation,
} from '@/lib/hl-annotations-store';
import { highlightInPreview } from '@/lib/hl-preview-highlight';
import { t } from '@/lib/i18n';

const fileHint = ({ resolve }: IAnnotation): string => {
  if (resolve.status === 'pending') return t('agents.annotationChipRow.findingFile');
  if (resolve.status === 'failed') return t('agents.annotationChipRow.fileNotFound');
  if (resolve.status === 'none') return t('agents.annotationChipRow.componentUnknown');
  const file = resolve.matches.find((match) => match.kind === 'component') ?? resolve.matches[0];
  return file ? `${file.file}:${file.line}` : t('agents.annotationChipRow.fileNotFound');
};

/** Elements picked in the tab's preview, above the editor: a click shows the element there, × drops it. */
const PickedChipRow = ({ tabId }: { tabId: string }) => {
  const picked = useAnnotations(tabId).filter(
    (annotation): annotation is IAnnotation => !isTextAnnotation(annotation),
  );
  if (!picked.length) return null;
  const show = (annotation: IAnnotation) => {
    if (!highlightInPreview(tabId, annotation.element.selector))
      toast.info(t('agents.annotationChipRow.openPreviewHint'));
  };
  return (
    <div className="tw-annot-chips" role="list" aria-label={t('agents.annotationChipRow.listLabel')}>
      {picked.map((annotation) => {
        const label = annotationLabel(annotation);
        return (
          <div
            key={annotation.id}
            className="tw-annot-chip"
            role="listitem"
            data-resolve={annotation.resolve.status}
          >
            <button
              type="button"
              className="tw-annot-chip-show"
              onClick={() => show(annotation)}
              title={[fileHint(annotation), annotation.comment].filter(Boolean).join('\n')}
              aria-label={t('agents.annotationChipRow.showInPreview', { label })}
            >
              <MousePointerClick aria-hidden="true" />
              <span className="tw-annot-chip-label">{label}</span>
            </button>
            <button
              type="button"
              className="tw-annot-chip-remove"
              onClick={() => removeAnnotation(tabId, annotation.id)}
              title={t('agents.annotationChipRow.remove')}
              aria-label={t('agents.annotationChipRow.removeLabel', { label })}
            >
              <X aria-hidden="true" />
            </button>
          </div>
        );
      })}
    </div>
  );
};

/** Everything attached to the next message from the preview and from the timeline. */
const AnnotationChipRow = ({ tabId }: { tabId: string }) => (
  <>
    <PickedChipRow tabId={tabId} />
    <TextAnnotationChipRow tabId={tabId} />
  </>
);

export default AnnotationChipRow;
