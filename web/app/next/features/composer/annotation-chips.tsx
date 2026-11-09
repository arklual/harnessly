// The marks sent from the pane's browser, as chips in the field's row (ui/notes/note-chips.tsx): the card over a chip
// says where the mark is — the page, the element, the component.
import { useMemo } from 'react';
import { annotationName, type IAnnotation } from '@/next/features/pane/annotations';
import { t } from '@/next/i18n';
import { NoteChips, type INoteChip } from '@/next/ui/notes/note-chips';

export interface IAnnotationChipsProps {
  notes: readonly IAnnotation[];
  onEdit: (id: string, comment: string) => void;
  onRemove: (id: string) => void;
}

const chipOf = (note: IAnnotation): INoteChip => {
  const name = annotationName(note);
  return {
    id: note.id,
    name,
    comment: note.comment,
    facts: [
      { label: t('next.composer.note.page'), value: note.url },
      { label: t('next.composer.note.element'), value: name, title: note.selector },
      ...(note.component ? [{ label: t('next.composer.note.component'), value: note.component.name }] : []),
    ],
  };
};

export const AnnotationChips = ({ notes, onEdit, onRemove }: IAnnotationChipsProps) => {
  const chips = useMemo(() => notes.map(chipOf), [notes]);
  return <NoteChips notes={chips} onEdit={onEdit} onRemove={onRemove} />;
};
