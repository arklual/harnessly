// The chat's folder (.hx-disk): crumbs from the folder's root, folders first, then files; a text file opens in place
// in the editor the binding hands over, the crumbs lead back. What cannot be shown (a picture, a binary) says why.
// Any entry is saved from its row, the folder on screen as one zip, and several picked entries as one zip.
import { useState, type ReactElement, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import type { DirListing, FileContent } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import LoadFailed from '@/next/ui/load-failed';
import Swap from '@/next/ui/swap';
import { usePresence } from '@/next/ui/presence';
import { Button } from '@/next/ui/button';

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
  /** Saves these paths: one file as it is, a folder or several as one zip. */
  onDownload: (paths: readonly string[]) => void;
  /** Two picked files, one against the other. */
  onCompare?: (a: string, b: string) => void;
  /** «…» of an entry, around the button that opens it. */
  menuOf?: (path: string, kind: 'file' | 'directory', trigger: ReactElement) => ReactNode;
}

const EXIT_MS = 180;

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
  onDownload,
  onCompare,
  menuOf,
}: IFilesViewProps) => {
  const crumbs = [{ name: root.split('/').filter(Boolean).pop() ?? root, path: '' }, ...crumbsOf(path)];
  // The picked entries of the folder on screen; null outside the picking mode. Another folder starts it over.
  const [picked, setPicked] = useState<{ at: string; paths: ReadonlySet<string> } | null>(null);
  const picking = picked !== null && picked.at === path ? picked.paths : null;
  const bar = usePresence(picking, EXIT_MS);
  const folder = error === null && file === null && opening === 'folder';
  const toggle = (entry: string) =>
    setPicked((now) => {
      const next = new Set(now?.at === path ? now.paths : []);
      if (!next.delete(entry)) next.add(entry);
      return { at: path, paths: next };
    });
  const pickedList = [...(picking ?? [])];
  const kindOf = (p: string) => listing?.entries.find((e) => e.path === p)?.kind;
  const pair = pickedList.length === 2 && pickedList.every((p) => kindOf(p) === 'file') ? pickedList : null;
  return (
    <div className="hx-tv" data-tool="disk" data-file={file !== null || undefined}>
      <div className="hx-disk">
        <div className="hx-disk__head">
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
          {folder && listing !== null && listing.entries.length > 0 && (
            <div className="hx-disk__tools">
              <button
                type="button"
                className="hx-head-btn hx-tipped"
                aria-label={t('next.files.pick')}
                aria-pressed={picking !== null}
                onClick={() => setPicked(picking ? null : { at: path, paths: new Set() })}
              >
                <Icon name="list-checks" size="sm" />
                <span className="hx-tip" data-pos="below" data-align="end">
                  {t('next.files.pick')}
                </span>
              </button>
              <button
                type="button"
                className="hx-head-btn hx-tipped"
                aria-label={t('next.files.downloadFolder')}
                onClick={() => onDownload([path])}
              >
                <Icon name="download" size="sm" />
                <span className="hx-tip" data-pos="below" data-align="end">
                  {t('next.files.downloadFolder')}
                </span>
              </button>
            </div>
          )}
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
            className="hx-disk__list"
            data-picking={picking !== null || undefined}
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
              {listing?.entries.map((entry) => {
                const on = picking?.has(entry.path) ?? false;
                return (
                  <div key={entry.path} className="hx-disk__item">
                    <button
                      type="button"
                      className="hx-tv__row"
                      role={picking ? 'checkbox' : undefined}
                      aria-checked={picking ? on : undefined}
                      onClick={() => (picking ? toggle(entry.path) : onOpen(entry.path, entry.kind))}
                    >
                      {picking && (
                        <span className="hx-disk__check" data-on={on || undefined} aria-hidden="true">
                          <Icon name="check" size="sm" />
                        </span>
                      )}
                      <Icon name={entry.kind === 'directory' ? 'folder' : 'file-text'} size="sm" />
                      <span className="hx-tv__main">
                        <span className="hx-tv__name">{entry.name}</span>
                      </span>
                    </button>
                    {!picking && (
                      <span className="hx-disk__acts">
                        <button
                          type="button"
                          className="hx-head-btn"
                          aria-label={t(
                            entry.kind === 'directory' ? 'next.files.downloadZip' : 'next.files.download',
                            { name: entry.name },
                          )}
                          title={t(
                            entry.kind === 'directory'
                              ? 'next.files.downloadZipHint'
                              : 'next.files.downloadHint',
                          )}
                          onClick={() => onDownload([entry.path])}
                        >
                          <Icon name="download" size="sm" />
                        </button>
                        {menuOf?.(
                          entry.path,
                          entry.kind,
                          <button
                            type="button"
                            className="hx-head-btn"
                            aria-label={t('next.fileMenu.of', { name: entry.name })}
                          >
                            <Icon name="ellipsis" size="sm" />
                          </button>,
                        )}
                      </span>
                    )}
                  </div>
                );
              })}
              {listing?.entries.length === 0 && <div className="hx-tv__empty">{t('next.files.empty')}</div>}
              {listing?.truncated && <div className="hx-tv__note">{t('next.files.truncated')}</div>}
            </Swap>
          </div>
        )}
        {bar.value && (
          <div
            className="hx-disk__bar"
            data-state={bar.state}
            role="toolbar"
            aria-label={t('next.files.pick')}
          >
            <span className="hx-disk__count">
              {bar.value.size === 0
                ? t('next.files.pickHint')
                : t('next.files.picked', { n: bar.value.size })}
            </span>
            <span className="hx-spacer" />
            <Button size="sm" variant="ghost" onClick={() => setPicked(null)}>
              {t('next.files.pickCancel')}
            </Button>
            {onCompare && pair && (
              <Button
                size="sm"
                icon="git-compare"
                onClick={() => {
                  onCompare(pair[0], pair[1]);
                  setPicked(null);
                }}
              >
                {t('next.files.compare')}
              </Button>
            )}
            <Button
              size="sm"
              variant="primary"
              icon="download"
              disabled={pickedList.length === 0}
              onClick={() => {
                onDownload(pickedList);
                setPicked(null);
              }}
            >
              {t('next.files.downloadPicked')}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
};

export default FilesView;
