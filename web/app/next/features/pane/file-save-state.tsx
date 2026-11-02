// How a file edited in the pane stands with the disk: the word beside its tools, and the choice when an edit has nowhere to go.
import { t } from '@/next/i18n';

export const SAVE = {
  clean: 'clean',
  dirty: 'dirty',
  saving: 'saving',
  saved: 'saved',
  conflict: 'conflict',
  failed: 'failed',
} as const;
export type TSave = (typeof SAVE)[keyof typeof SAVE];

const WORDS = {
  [SAVE.dirty]: 'next.sheet.saving',
  [SAVE.saving]: 'next.sheet.saving',
  [SAVE.saved]: 'next.sheet.saved',
  [SAVE.failed]: 'next.sheet.saveFailed',
} as const;

export const SaveWord = ({ save }: { save: TSave }) => {
  const word = save in WORDS ? WORDS[save as keyof typeof WORDS] : null;
  if (!word) return null;
  return (
    <span className="hx-filesave" data-bad={save === SAVE.failed ? 'true' : undefined} role="status">
      {t(word)}
    </span>
  );
};

export const SaveConflict = ({
  onReload,
  onOverwrite,
}: {
  onReload: () => void;
  onOverwrite: () => void;
}) => (
  <div className="hx-filesave__conflict" role="alert">
    <span>{t('next.sheet.conflict')}</span>
    <button type="button" className="hx-filesave__btn" onClick={onReload}>
      {t('next.sheet.reload')}
    </button>
    <button type="button" className="hx-filesave__btn" onClick={onOverwrite}>
      {t('next.sheet.overwrite')}
    </button>
  </div>
);
