import { MessageSquareText, X } from 'lucide-react';
import { diffCommentLabel } from '@/lib/hl-diff-comment';
import { removeDiffComment, useDiffComments } from '@/lib/hl-diff-comments-store';
import { t } from '@/lib/i18n';

/** Comments on diff lines, above the editor: they go out with the next message, × drops one. */
const DiffCommentChipRow = ({ tabId }: { tabId: string }) => {
  const comments = useDiffComments(tabId);
  if (!comments.length) return null;
  return (
    <div className="tw-annot-chips" role="list" aria-label={t('agents.diffCommentChipRow.listLabel')}>
      {comments.map((comment) => {
        const label = diffCommentLabel(comment);
        return (
          <div key={comment.id} className="tw-annot-chip" role="listitem">
            <span className="tw-annot-chip-show" title={`${comment.file}\n${comment.comment}`}>
              <MessageSquareText aria-hidden="true" />
              <span className="tw-annot-chip-label">{label}</span>
            </span>
            <button
              type="button"
              className="tw-annot-chip-remove"
              onClick={() => removeDiffComment(tabId, comment.id)}
              title={t('agents.diffCommentChipRow.remove')}
              aria-label={t('agents.diffCommentChipRow.removeLabel', { label })}
            >
              <X aria-hidden="true" />
            </button>
          </div>
        );
      })}
    </div>
  );
};

export default DiffCommentChipRow;
