// A note the harness typed into the session (a goal check-in, a task's notification, a system reminder): not the
// person's message but a quiet line — a muted icon and what it says, its details under a fold when it has more.
import { memo, useState } from 'react';
import type { NoticeEntry } from '@/next/features/chat/types';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/peer-note.css';

/** The notice code of such a note (internal/feed systemNoteCode). */
export const SYSTEM_NOTE = 'system-note';

/** The details' exit, as long as hx-sysnote-out in peer-note.css. */
const DETAILS_EXIT_MS = 160;

const SystemNote = ({ entry }: { entry: NoticeEntry }) => {
  const [open, setOpen] = useState(false);
  const details = usePresentFlag(open, DETAILS_EXIT_MS);
  const text = (entry.detail ?? '').trim();
  const cut = text.indexOf('\n');
  const head = (cut < 0 ? text : text.slice(0, cut)) || t('next.systemNote.title');
  const more = cut < 0 ? '' : text.slice(cut + 1).trim();
  return (
    <div className="hx-sysnote" data-open={open || undefined}>
      {more ? (
        <button
          type="button"
          className="hx-sysnote__head"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
        >
          <Icon name="bell" size="sm" className="hx-sysnote__ic" />
          <span className="hx-sysnote__text">{head}</span>
          <Icon name="chevron-right" size="sm" className="hx-sysnote__chev" />
        </button>
      ) : (
        <div className="hx-sysnote__head">
          <Icon name="bell" size="sm" className="hx-sysnote__ic" />
          <span className="hx-sysnote__text">{head}</span>
        </div>
      )}
      {more && details.mounted && (
        <div className="hx-sysnote__more" data-state={details.state}>
          {more}
        </div>
      )}
    </div>
  );
};

export default memo(SystemNote);

/** The quiet line under the last turn of a chat whose terminal was ended: by the person, or after so long idle. */
export const TerminalStopNote = ({ text }: { text: string }) => (
  <div className="hx-sysnote" role="status">
    <div className="hx-sysnote__head">
      <Icon name="circle-stop" size="sm" className="hx-sysnote__ic" />
      <span className="hx-sysnote__text">{text}</span>
    </div>
  </div>
);
