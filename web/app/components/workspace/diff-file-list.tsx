// A unified diff of several files: one collapsible block per file with its added and removed lines.
import { useMemo, useState } from 'react';
import { ChevronRight } from 'lucide-react';
import { DiffModeEnum, DiffView } from '@git-diff-view/react';
import '@git-diff-view/react/styles/diff-view.css';
import { parseMultiFileDiff } from '@/lib/parse-git-diff';
import { useTheme } from '@/platform/theme';
import { t } from '@/lib/i18n';

interface IDiffFileListProps {
  diff: string;
  viewMode?: 'unified' | 'split';
  sessionName?: string;
  loadContent?: boolean;
  commenting?: unknown;
}

export default function DiffFileList({ diff, viewMode = 'unified' }: IDiffFileListProps) {
  const files = useMemo(() => parseMultiFileDiff(diff), [diff]);
  const [closed, setClosed] = useState<Set<string>>(() => new Set());
  const { resolvedTheme } = useTheme();
  if (files.length === 0)
    return <p className="text-xs text-muted-foreground">{t('workspace.diffFileList.noChanges')}</p>;
  return (
    <div className="tw-diff-files">
      {files.map((file) => {
        const name = file.isDeleted ? file.oldPath : file.newPath;
        const open = !closed.has(name);
        return (
          <section key={name} className="tw-diff-file">
            <button
              type="button"
              className="tw-diff-file-head"
              aria-expanded={open}
              onClick={() =>
                setClosed((s) => {
                  const n = new Set(s);
                  if (open) n.add(name);
                  else n.delete(name);
                  return n;
                })
              }
            >
              <ChevronRight
                size={14}
                style={{ transform: open ? 'rotate(90deg)' : undefined, transition: 'transform 150ms' }}
              />
              <span className="tw-diff-file-name">{name}</span>
              <span className="tw-diff-file-add">+{file.additions}</span>
              <span className="tw-diff-file-del">−{file.deletions}</span>
            </button>
            {open && !file.isBinary && (
              <DiffView
                data={{
                  oldFile: { fileName: file.oldPath },
                  newFile: { fileName: file.newPath },
                  hunks: [file.raw],
                }}
                diffViewMode={viewMode === 'split' ? DiffModeEnum.Split : DiffModeEnum.Unified}
                diffViewTheme={resolvedTheme}
                diffViewHighlight={false}
                diffViewWrap
              />
            )}
          </section>
        );
      })}
    </div>
  );
}
