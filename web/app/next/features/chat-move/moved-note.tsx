// The place of the field in a chat that was continued on another machine: it takes no more messages, and says where
// the work went on.
import type { IMoved } from '@/next/features/chat-move/api';
import { t } from '@/next/i18n';
import '@/next/styles/chat-move.css';

export interface IMovedNoteProps {
  moved: IMoved;
  onOpen: () => void;
}

const MovedNote = ({ moved, onOpen }: IMovedNoteProps) => (
  <div className="hx-moved" role="status">
    <span className="hx-moved__text">{t('next.moveVm.moved', { machine: moved.toName })}</span>
    <button type="button" className="hx-btn" data-variant="ghost" onClick={onOpen}>
      {t('next.moveVm.open')}
    </button>
  </div>
);

export default MovedNote;
