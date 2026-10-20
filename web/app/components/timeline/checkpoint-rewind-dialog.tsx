import { useEffect, useMemo, useState } from 'react';
import dayjs from 'dayjs';
import dynamic from 'next/dynamic';
import { ChevronRight, Loader2 } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { loadCheckpointDiff, rewindTo } from '@/lib/checkpoint-client';
import { summarizeFiles, type TCheckpointView } from '@/lib/hl-checkpoint-match';
import type { ICheckpointDiff, ICheckpointFile } from '@/types/server/hl-checkpoints';
import { plural, t } from '@/lib/i18n';
import { cn } from '@/lib/utils';

const DiffFileList = dynamic(() => import('@/components/workspace/diff-file-list'), { ssr: false });

const FILES = 'timeline.checkpointRewindDialog.files';
const STATUS_LABEL: Record<ICheckpointFile['status'], string> = {
  A: t('timeline.checkpointRewindDialog.statusRestored'),
  D: t('timeline.checkpointRewindDialog.statusRemoved'),
  M: t('timeline.checkpointRewindDialog.statusChanged'),
  T: t('timeline.checkpointRewindDialog.statusChanged'),
};

interface IRewindDialogProps {
  tabId: string;
  sessionName?: string;
  checkpoint: TCheckpointView;
  /** `undo`: the target is the snapshot taken before an earlier rewind. */
  mode: 'rewind' | 'undo';
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

type TPreview =
  { state: 'loading' } | { state: 'failed'; error: string } | { state: 'ready'; diff: ICheckpointDiff };

const Summary = ({ files }: { files: readonly ICheckpointFile[] }) => {
  const sum = summarizeFiles(files);
  const parts = [
    sum.changed > 0 && t('timeline.checkpointRewindDialog.changedCount', { count: sum.changed }),
    sum.restored > 0 && t('timeline.checkpointRewindDialog.restoredCount', { count: sum.restored }),
    sum.removed > 0 && t('timeline.checkpointRewindDialog.removedCount', { count: sum.removed }),
  ].filter(Boolean);
  return (
    <p className="tw-checkpoint-summary">
      <strong>{plural(files.length, FILES)}</strong>
      <span>{parts.join(' · ')}</span>
      <span className="tw-repo-stat">
        <span className="tw-diff-add">+{sum.additions}</span>
        <span className="tw-diff-del">−{sum.deletions}</span>
      </span>
    </p>
  );
};

const CheckpointRewindDialog = ({
  tabId,
  sessionName,
  checkpoint,
  mode,
  open,
  onOpenChange,
}: IRewindDialogProps) => {
  const [preview, setPreview] = useState<TPreview>({ state: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [showDiff, setShowDiff] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    let alive = true;
    void loadCheckpointDiff(tabId, checkpoint.id).then((result) => {
      if (alive)
        setPreview(
          result.ok ? { state: 'ready', diff: result.diff } : { state: 'failed', error: result.error },
        );
    });
    return () => {
      alive = false;
    };
  }, [tabId, checkpoint.id, attempt]);

  const retry = () => {
    setPreview({ state: 'loading' });
    setAttempt((n) => n + 1);
  };

  const files = useMemo(() => (preview.state === 'ready' ? preview.diff.files : []), [preview]);
  const time = dayjs(checkpoint.at).format('HH:mm');

  const confirm = async () => {
    setSubmitting(true);
    const result = await rewindTo(tabId, checkpoint.id);
    setSubmitting(false);
    if (!result.ok) {
      toast.error(result.error);
      return;
    }
    onOpenChange(false);
    const { changed, skipped } = result.done;
    toast.success(
      t(
        mode === 'undo'
          ? 'timeline.checkpointRewindDialog.toastUndone'
          : 'timeline.checkpointRewindDialog.toastRestored',
        { files: plural(changed.length, FILES) },
      ),
    );
    if (skipped.length > 0)
      toast.warning(
        t('timeline.checkpointRewindDialog.toastSkipped', { files: plural(skipped.length, FILES) }),
      );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!submitting) onOpenChange(next);
      }}
    >
      <DialogContent className="tw-checkpoint-dialog">
        <DialogHeader>
          <DialogTitle>
            {mode === 'undo'
              ? t('timeline.checkpointRewindDialog.titleUndo')
              : t('timeline.checkpointRewindDialog.titleRewind')}
          </DialogTitle>
          <DialogDescription>
            {mode === 'undo'
              ? t('timeline.checkpointRewindDialog.descriptionUndo', { time })
              : t('timeline.checkpointRewindDialog.descriptionRewind', { time })}
          </DialogDescription>
        </DialogHeader>

        <div className="tw-checkpoint-body">
          {preview.state === 'loading' && (
            <p className="tw-inspector-muted">
              <Loader2 className="tw-inline-spin" /> {t('timeline.checkpointRewindDialog.comparing')}
            </p>
          )}
          {preview.state === 'failed' && (
            <p className="tw-checkpoint-error" role="alert">
              {preview.error}{' '}
              <button type="button" onClick={retry}>
                {t('timeline.checkpointRewindDialog.retry')}
              </button>
            </p>
          )}
          {preview.state === 'ready' && files.length === 0 && (
            <p className="tw-inspector-muted">{t('timeline.checkpointRewindDialog.nothingToRewind')}</p>
          )}
          {preview.state === 'ready' && files.length > 0 && (
            <>
              <Summary files={files} />
              <ul className="tw-checkpoint-files">
                {files.map((file) => (
                  <li key={file.path}>
                    <span className="tw-checkpoint-status" data-status={file.status}>
                      {STATUS_LABEL[file.status]}
                    </span>
                    <code title={file.path}>{file.path}</code>
                    <span className="tw-repo-stat">
                      <span className="tw-diff-add">+{file.additions}</span>
                      <span className="tw-diff-del">−{file.deletions}</span>
                    </span>
                  </li>
                ))}
              </ul>
              {preview.diff.truncated && (
                <p className="tw-inspector-muted">{t('timeline.checkpointRewindDialog.diffTruncated')}</p>
              )}
              <button
                type="button"
                className="tw-turn-toggle"
                aria-expanded={showDiff}
                onClick={() => setShowDiff((value) => !value)}
              >
                <ChevronRight
                  size={13}
                  className={cn('tw-turn-chevron', showDiff && 'tw-checkpoint-chevron-open')}
                />
                <span>
                  {showDiff
                    ? t('timeline.checkpointRewindDialog.hideChanges')
                    : t('timeline.checkpointRewindDialog.showChanges')}
                </span>
              </button>
              {showDiff && (
                <div className="tw-repo-diff">
                  <DiffFileList
                    diff={preview.diff.diff}
                    viewMode="unified"
                    sessionName={sessionName ?? ''}
                    loadContent={false}
                  />
                </div>
              )}
            </>
          )}
          <p className="tw-checkpoint-note-text">
            {t('timeline.checkpointRewindDialog.scopeNote')}
            {mode === 'rewind' && ` ${t('timeline.checkpointRewindDialog.tellAgent')}`}
          </p>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t('timeline.checkpointRewindDialog.cancel')}
          </Button>
          <Button
            onClick={() => void confirm()}
            disabled={preview.state !== 'ready' || files.length === 0 || submitting}
          >
            {submitting && <Loader2 className="tw-inline-spin" />}
            {mode === 'undo'
              ? t('timeline.checkpointRewindDialog.undo')
              : t('timeline.checkpointRewindDialog.rewind')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default CheckpointRewindDialog;
