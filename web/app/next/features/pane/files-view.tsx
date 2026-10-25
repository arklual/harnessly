// The chat's folder (.hx-disk): crumbs from the folder's root, folders first, then files; a text file opens in place
// in the editor the binding hands over, the crumbs lead back. What cannot be shown (a picture, a binary) says why.
import type { ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import type { DirListing, FileContent } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import LoadFailed from '@/next/ui/load-failed';
import Swap from '@/next/ui/swap';

export interface IFilesViewProps {
  root: string;
  /** The folder or file on screen, relative to the root ('' is the root). */
  path: string;
  /** null while it loads. */
  listing: DirListing | null;
  file: FileContent | null;
  /** The editor of the file; null while there is no file. */
  editor: ReactNode;
  error: string | null;
  /** What the tab is on, for what it says when that did not open. */
  opening: 'file' | 'folder';
  /** Reads the folder or the file again. */
  onRetry: () => void;
  onOpen: (path: string, kind: 'file' | 'directory') => void;
}

const crumbsOf = (path: string) => {
  // A file outside the folder (one the agent made in a worktree beside it) is shown by its name: its folders are not
  // the chat's to browse.
  if (path.startsWith('/')) return [{ name: path.split('/').pop() ?? path, path }];
  const parts = path.split('/').filter(Boolean);
  return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join('/') }));
};

const FilesView = ({
  root,
  path,
  listing,
  file,
  editor,
  error,
  opening,
  onRetry,
  onOpen,
}: IFilesViewProps) => {
  const crumbs = [{ name: root.split('/').filter(Boolean).pop() ?? root, path: '' }, ...crumbsOf(path)];
  return (
    <div className="hx-tv" data-tool="disk" data-file={file !== null || undefined}>
      <div className="hx-disk">
        <div className="hx-disk__crumbs">
          {crumbs.map((c, i) => (
            <span key={c.path} className="hx-disk__step">
              <button
                type="button"
                className="hx-disk__crumb"
                aria-current={i === crumbs.length - 1}
                onClick={() => onOpen(c.path, 'directory')}
              >
                {c.name}
              </button>
              {i < crumbs.length - 1 && <Icon name="chevron-right" size="sm" />}
            </span>
          ))}
        </div>
        {error !== null && (
          <LoadFailed
            title={t(opening === 'file' ? 'next.load.file' : 'next.load.files')}
            reason={error}
            onRetry={onRetry}
          />
        )}
        {error === null && file !== null && editor}
        {error === null && file === null && (
          <div
            role={listing === null ? 'status' : undefined}
            aria-label={listing === null ? t('next.files.loading') : undefined}
          >
            <Swap
              ready={listing !== null}
              skeleton={
                <>
                  <span className="hx-sk hx-plan__sk" />
                  <span className="hx-sk hx-plan__sk" />
                </>
              }
            >
              {listing?.entries.map((entry) => (
                <button
                  key={entry.path}
                  type="button"
                  className="hx-tv__row"
                  onClick={() => onOpen(entry.path, entry.kind)}
                >
                  <Icon name={entry.kind === 'directory' ? 'folder' : 'file-text'} size="sm" />
                  <span className="hx-tv__main">
                    <span className="hx-tv__name">{entry.name}</span>
                  </span>
                </button>
              ))}
              {listing?.entries.length === 0 && <div className="hx-tv__empty">{t('next.files.empty')}</div>}
              {listing?.truncated && <div className="hx-tv__note">{t('next.files.truncated')}</div>}
            </Swap>
          </div>
        )}
      </div>
    </div>
  );
};

export default FilesView;
