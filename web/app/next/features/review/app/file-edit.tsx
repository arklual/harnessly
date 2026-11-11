// The person's own hand in the review of a chat's work: the new side of a file opened as an editor in place of the
// diff, saved into the worktree (over the text that was read; a file the agent changed since is not overwritten
// silently), and the changed files committed from the panel — or the commit asked of the agent.
import { useEffect, useRef, useState } from 'react';
import LazyFileEditor from '@/next/features/pane/editor/lazy';
import type { IFileEditorHandle } from '@/next/features/pane/editor/file-editor';
import type { FileContent } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';
import { EXIT_MS, submitKeys } from '@/next/features/review/app/review-notes';

export interface IReviewEdit {
  /** Writes the text over the revision that was read (`over`: over whatever the disk has now). */
  save: (path: string, text: string, revision: string, over: boolean) => Promise<FileContent>;
  commit: (message: string, paths: readonly string[]) => Promise<string>;
  /** The revision of a text, as the writes are checked against. */
  hash: (text: string) => Promise<string>;
  /** Reads the change again (after a save or a commit). */
  reload: () => void;
  /** Asks the chat's agent to commit; none where no agent is beside. */
  askCommit?: () => void;
}

interface IFileEditProps {
  path: string;
  /** The file's text as the review read it from the worktree. */
  text: string;
  edit: IReviewEdit;
  onSaved: () => void;
  onClose: () => void;
}

export const FileEdit = ({ path, text, edit, onSaved, onClose }: IFileEditProps) => {
  const [revision, setRevision] = useState<string | null>(null);
  const [state, setState] = useState({ dirty: false, saving: false });
  const handle = useRef<IFileEditorHandle | null>(null);
  const [initial] = useState(text);
  useEffect(() => {
    let live = true;
    void edit.hash(initial).then((h) => live && setRevision(h));
    return () => {
      live = false;
    };
  }, [edit, initial]);

  const save = async (next: string, rev: string, over: boolean) => {
    const file = await edit.save(path, next, rev, over);
    edit.reload();
    onSaved();
    return file;
  };

  return (
    <div className="hx-rvedit">
      <div className="hx-rvedit__bar">
        <Icon name="file-pen" size="sm" />
        <span className="hx-rvedit__title">{t('next.reviewApp.edit.title')}</span>
        <span className="hx-rvedit__hint">{t('next.reviewApp.edit.hint')}</span>
        <span className="hx-rvapp__spacer" />
        <Button size="sm" variant="ghost" onClick={onClose}>
          {state.dirty ? t('next.reviewApp.edit.discard') : t('next.reviewApp.edit.close')}
        </Button>
        <Button
          size="sm"
          variant="primary"
          busy={state.saving}
          disabled={!state.dirty || state.saving}
          onClick={() => void handle.current?.save()}
        >
          {t('next.reviewApp.edit.save')}
        </Button>
      </div>
      <div className="hx-rvedit__body">
        {revision === null ? (
          <p className="hx-rvapp__empty">{t('next.review.loading')}</p>
        ) : (
          <LazyFileEditor
            path={path}
            initial={{ text: initial, base: initial, revision }}
            onSave={save}
            onReload={() => {
              edit.reload();
              onClose();
            }}
            onLeave={() => {}}
            onState={setState}
            onHandle={(h) => {
              handle.current = h;
            }}
          />
        )}
      </div>
    </div>
  );
};

interface ICommitBoxProps {
  edit: IReviewEdit;
  paths: readonly string[];
  /** The box is called (a save's toast); it opens with the field in focus. */
  open: boolean;
  onOpen: (open: boolean) => void;
}

/** «Закоммитить» in the panel's foot: the uncommitted files of the worktree with a message, or asked of the agent. */
export const CommitBox = ({ edit, paths, open, onOpen }: ICommitBoxProps) => {
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const form = usePresence(open ? true : null, EXIT_MS);
  const commit = async () => {
    const text = message.trim();
    if (!text || busy) return;
    setBusy(true);
    try {
      const sha = await edit.commit(text, paths);
      toast(t('next.reviewApp.edit.committed'), { description: sha.slice(0, 8), tone: 'success' });
      setMessage('');
      onOpen(false);
      edit.reload();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), { tone: 'error' });
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="hx-rvcommit">
      {form.value ? (
        <div className="hx-rvcommit__form" data-state={form.state}>
          <textarea
            className="hx-rvnote__field"
            value={message}
            autoFocus
            rows={2}
            placeholder={t('next.reviewApp.edit.message')}
            aria-label={t('next.reviewApp.edit.message')}
            onChange={(event) => setMessage(event.target.value)}
            onKeyDown={(event) =>
              submitKeys(
                event,
                () => void commit(),
                () => onOpen(false),
              )
            }
          />
          <div className="hx-rvnote__actions">
            <span className="hx-rvnote__hint">{t('next.reviewApp.edit.files', { n: paths.length })}</span>
            <Button size="sm" variant="ghost" onClick={() => onOpen(false)}>
              {t('next.reviewApp.notes.cancel')}
            </Button>
            <Button
              size="sm"
              variant="primary"
              busy={busy}
              disabled={!message.trim()}
              onClick={() => void commit()}
            >
              {t('next.reviewApp.edit.commit')}
            </Button>
          </div>
        </div>
      ) : (
        <div className="hx-rvcommit__row">
          <Button size="sm" icon="check" onClick={() => onOpen(true)}>
            {t('next.reviewApp.edit.commit')}
          </Button>
          {edit.askCommit && (
            <Button size="sm" variant="ghost" icon="send" onClick={edit.askCommit}>
              {t('next.reviewApp.edit.askCommit')}
            </Button>
          )}
        </div>
      )}
    </div>
  );
};
