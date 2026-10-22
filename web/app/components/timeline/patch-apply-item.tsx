// Codex: a patch the agent applied: which files it created, changed or deleted, and the diff on demand.
import { lazy, memo, Suspense, useState } from 'react';
import { ChevronRight, FileEdit, FilePen, FilePlus, FileX } from 'lucide-react';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { patchFileAction } from '@/lib/timeline-items';
import { cn } from '@/lib/utils';
import { plural, t } from '@/lib/i18n';
import type { ITimelinePatchApply } from '@/types/timeline';

const DiffFileList = lazy(() => import('@/components/workspace/diff-file-list'));

const FileIcon = ({ status }: { status?: string }) => {
  switch (patchFileAction(status)) {
    case 'create':
      return <FilePlus size={11} aria-hidden />;
    case 'delete':
      return <FileX size={11} aria-hidden />;
    case 'modify':
      return <FilePen size={11} aria-hidden />;
    default:
      return <FileEdit size={11} aria-hidden />;
  }
};

const PatchApplyItem = ({ entry }: { entry: ITimelinePatchApply }) => {
  const holdPosition = useHoldTimelinePosition();
  const [open, setOpen] = useState(false);
  const failed = entry.status === 'error' || entry.success === false;
  const state = entry.status === 'pending' ? 'pending' : failed ? 'failed' : 'done';
  return (
    <div className="tw-patch" data-status={entry.status}>
      <div className="tw-patch-head">
        <FileEdit size={12} className={cn(entry.status === 'pending' && 'tw-pulse')} aria-hidden />
        <strong>{t('timeline.patchApply.title')}</strong>
        <span>{plural(entry.files.length, 'timeline.patchApply.files')}</span>
        <span className={cn(state === 'failed' && 'tw-exec-failed')}>
          {t(`timeline.patchApply.${state}`)}
        </span>
      </div>
      {entry.files.length > 0 && (
        <ul className="tw-patch-files">
          {entry.files.map((file, index) => {
            const action = patchFileAction(file.status ?? file.kind);
            return (
              <li key={`${file.path}-${index}`}>
                <FileIcon status={file.status ?? file.kind} />
                <code>{file.path}</code>
                {action !== 'other' && <span>{t(`timeline.patchApply.${action}`)}</span>}
              </li>
            );
          })}
        </ul>
      )}
      {entry.diff && (
        <>
          <button
            type="button"
            className="tw-fold-toggle"
            aria-expanded={open}
            onClick={() => {
              holdPosition();
              setOpen((v) => !v);
            }}
          >
            <ChevronRight size={10} className={cn('tw-fold-chevron', open && 'tw-fold-chevron-open')} />
            {open ? t('timeline.patchApply.hideDiff') : t('timeline.patchApply.showDiff')}
          </button>
          {open && (
            <Suspense fallback={null}>
              <DiffFileList diff={entry.diff} />
            </Suspense>
          )}
        </>
      )}
    </div>
  );
};

export default memo(PatchApplyItem);
