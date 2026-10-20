import { t } from '@/lib/i18n';
import {
  useEffect,
  useId,
  useRef,
  useState,
  useSyncExternalStore,
  type ChangeEvent,
  type DragEvent,
  type FormEvent,
  type KeyboardEvent,
} from 'react';
import dayjs from 'dayjs';
import {
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  Download,
  File as FileIcon,
  Folder,
  FolderPlus,
  FolderUp,
  HardDrive,
  Link2,
  Pause,
  Play,
  RotateCw,
  Star,
  Upload,
  X,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwButton,
  TwMenuButton,
  TwSpinner,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import useDiskFavorites from '@/components/agents/inspector/use-disk-favorites';
import useDiskListing, { useDiskRoots } from '@/components/agents/inspector/use-disk-listing';
import {
  DISK_ENTRY_KIND,
  DiskFailure,
  FAVORITE_STATE,
  createDiskFolder,
  diskAbsolutePath,
  diskDownloadUrl,
  favoriteStateOf,
  fetchDiskPage,
  locateDiskPlace,
  type IDiskEntry,
  type IDiskFavorite,
  type IDiskLocation,
  type TFavoriteState,
} from '@/lib/hl-disk-api';
import { collectDropped, picksFromInput, type IDropItem, type IDiskPick } from '@/lib/hl-disk-drop';
import { formatBytes, formatSpeed, joinDiskPath } from '@/lib/hl-disk-format';
import { browserDiskDeps } from '@/lib/hl-disk-http';
import {
  CONFLICT_CHOICE,
  DiskUploadQueue,
  UPLOAD_STATE,
  isActiveUpload,
  type IDiskUpload,
} from '@/lib/hl-disk-uploader';

/** A folder on screen: the location plus the name its root goes by, which only the roots page knows. */
interface IPlace extends IDiskLocation {
  rootLabel: string;
}

const FOLDER_INPUT_ATTRS = { webkitdirectory: '', directory: '' } as Record<string, string>;
const DATE_FORMAT = 'DD.MM.YYYY HH:mm';
const ROW_SELECTOR = '[data-disk-row]';

const STATE_LABEL: Record<IDiskUpload['state'], string> = {
  queued: t('inspector.diskTab.stateQueued'),
  uploading: '',
  retrying: '',
  paused: t('inspector.diskTab.statePaused'),
  conflict: t('inspector.diskTab.stateConflict'),
  done: t('inspector.diskTab.stateDone'),
  failed: '',
  canceled: t('inspector.diskTab.stateCanceled'),
  skipped: t('inspector.diskTab.stateSkipped'),
};

const segmentsOf = (path: string): string[] => path.split('/').filter(Boolean);

const uploadStatus = (upload: IDiskUpload): string => {
  if (upload.state === UPLOAD_STATE.uploading)
    return upload.speed > 0 ? formatSpeed(upload.speed) : t('inspector.diskTab.uploading');
  if (upload.note) return upload.note;
  return STATE_LABEL[upload.state];
};

interface IUploadRowProps {
  upload: IDiskUpload;
  queue: DiskUploadQueue;
}

const UploadRow = ({ upload, queue }: IUploadRowProps) => {
  const [forAll, setForAll] = useState(false);
  const active = isActiveUpload(upload.state);
  const label = upload.dir ? `${upload.dir}/${upload.name}` : upload.name;
  const running = upload.state === UPLOAD_STATE.uploading || upload.state === UPLOAD_STATE.retrying;
  const choose = (choice: (typeof CONFLICT_CHOICE)[keyof typeof CONFLICT_CHOICE]) =>
    queue.resolveConflict(upload.id, choice, forAll);
  return (
    <li className="tw-disk-upload" data-state={upload.state}>
      <div className="tw-disk-upload-head">
        <span className="tw-disk-upload-name" title={label}>
          {label}
        </span>
        <span className="tw-disk-upload-actions">
          {running && (
            <button
              type="button"
              className="tw-icon-button"
              aria-label={t('inspector.diskTab.pauseNamed', { name: upload.name })}
              title={t('inspector.diskTab.pause')}
              onClick={() => queue.pause(upload.id)}
            >
              <Pause />
            </button>
          )}
          {upload.state === UPLOAD_STATE.paused && (
            <button
              type="button"
              className="tw-icon-button"
              aria-label={t('inspector.diskTab.resumeNamed', { name: upload.name })}
              title={t('inspector.diskTab.resume')}
              onClick={() => queue.resume(upload.id)}
            >
              <Play />
            </button>
          )}
          {active && (
            <button
              type="button"
              className="tw-icon-button"
              aria-label={t('inspector.diskTab.cancelNamed', { name: upload.name })}
              title={t('inspector.diskTab.cancelUpload')}
              onClick={() => queue.cancel(upload.id)}
            >
              <X />
            </button>
          )}
        </span>
      </div>
      {upload.state === UPLOAD_STATE.conflict ? (
        <div
          className="tw-disk-conflict"
          role="group"
          aria-label={t('inspector.diskTab.conflictNamed', { name: upload.name })}
        >
          <div className="tw-disk-conflict-buttons">
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.secondary}
              onClick={() => choose(CONFLICT_CHOICE.replace)}
            >
              {t('inspector.diskTab.replace')}
            </TwButton>
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.secondary}
              onClick={() => choose(CONFLICT_CHOICE.keepBoth)}
            >
              {t('inspector.diskTab.keepBoth')}
            </TwButton>
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              onClick={() => choose(CONFLICT_CHOICE.skip)}
            >
              {t('inspector.diskTab.skip')}
            </TwButton>
          </div>
          <label className="tw-disk-check">
            <input type="checkbox" checked={forAll} onChange={(event) => setForAll(event.target.checked)} />
            <span>{t('inspector.diskTab.forAll')}</span>
          </label>
        </div>
      ) : (
        <>
          {(active || upload.state === UPLOAD_STATE.done) && (
            <progress
              className="tw-disk-progress"
              value={upload.sent}
              max={Math.max(upload.size, 1)}
              aria-label={t('inspector.diskTab.uploadedNamed', { name: upload.name })}
            />
          )}
          <div className="tw-disk-upload-meta">
            <span>
              {active
                ? t('inspector.diskTab.sentOf', {
                    sent: formatBytes(upload.sent),
                    size: formatBytes(upload.size),
                  })
                : formatBytes(upload.size)}
            </span>
            <span
              className="tw-disk-upload-status"
              role={upload.state === UPLOAD_STATE.failed ? 'alert' : undefined}
            >
              {uploadStatus(upload)}
            </span>
          </div>
        </>
      )}
    </li>
  );
};

interface IStarProps {
  state: TFavoriteState;
  busy: boolean;
  /** Inside the list the arrows reach it; elsewhere it is a stop of the tab order. */
  inList: boolean;
  onToggle: () => void;
}

/** Pinned only as the server answered: while a request is out, and while the favorites are unknown, it does nothing. */
const FavoriteStar = ({ state, busy, inList, onToggle }: IStarProps) => {
  const pinned = state === FAVORITE_STATE.pinned;
  const unknown = state === FAVORITE_STATE.unknown;
  const label = pinned ? t('inspector.diskTab.unpinFolder') : t('inspector.diskTab.pinFolder');
  return (
    <button
      type="button"
      className="tw-icon-button tw-disk-star"
      data-pinned={pinned || undefined}
      data-busy={busy || undefined}
      data-disk-star={inList || undefined}
      tabIndex={inList ? -1 : 0}
      aria-label={label}
      aria-pressed={pinned}
      aria-disabled={busy || unknown || undefined}
      title={unknown ? t('inspector.diskTab.favoritesUnavailable') : label}
      onClick={() => {
        if (!busy && !unknown) onToggle();
      }}
    >
      <Star />
    </button>
  );
};

interface IRowProps {
  entry: IDiskEntry;
  place: IPlace;
  /** One row of the list is in the tab order; the arrows move between the rest. */
  tabbable: boolean;
  onOpen: (name: string) => void;
  favorite: TFavoriteState;
  favoritesBusy: boolean;
  onToggleFavorite: (name: string) => void;
}

const EntryRow = ({
  entry,
  place,
  tabbable,
  onOpen,
  favorite,
  favoritesBusy,
  onToggleFavorite,
}: IRowProps) => {
  const date = dayjs(entry.mtime).format(DATE_FORMAT);
  if (entry.kind === DISK_ENTRY_KIND.dir) {
    return (
      <li className="tw-disk-item">
        <button
          type="button"
          className="tw-disk-row"
          data-disk-row
          tabIndex={tabbable ? 0 : -1}
          onClick={() => onOpen(entry.name)}
        >
          <Folder className="tw-disk-row-icon" />
          <span className="tw-disk-row-name" title={entry.name}>
            {entry.name}
          </span>
          <span className="tw-disk-row-size" />
          <span className="tw-disk-row-date">{date}</span>
        </button>
        <FavoriteStar
          state={favorite}
          busy={favoritesBusy}
          inList
          onToggle={() => onToggleFavorite(entry.name)}
        />
      </li>
    );
  }
  if (entry.kind === DISK_ENTRY_KIND.link) {
    return (
      <li>
        <div className="tw-disk-row" data-muted title={t('inspector.diskTab.symlinkHint')}>
          <Link2 className="tw-disk-row-icon" />
          <span className="tw-disk-row-name">{entry.name}</span>
          <span className="tw-disk-row-size">{t('inspector.diskTab.symlink')}</span>
          <span className="tw-disk-row-date">{date}</span>
        </div>
      </li>
    );
  }
  return (
    <li>
      <a
        className="tw-disk-row"
        data-disk-row
        tabIndex={tabbable ? 0 : -1}
        href={diskDownloadUrl(place, entry.name)}
        download={entry.name}
        title={t('inspector.diskTab.downloadNamed', { name: entry.name })}
      >
        <FileIcon className="tw-disk-row-icon" />
        <span className="tw-disk-row-name">{entry.name}</span>
        <span className="tw-disk-row-size">{formatBytes(entry.size)}</span>
        <span className="tw-disk-row-date">{date}</span>
        <Download className="tw-disk-row-download" aria-hidden />
      </a>
    </li>
  );
};

/**
 * The «Disk» tab: the folders of the machine the conversation runs on, upload of files and whole folders
 * (resumable, one file at a time) and download. The bytes go through the files service of that machine, not the runtime.
 */
const UPLOAD_UNAVAILABLE = t('inspector.diskTab.uploadUnavailable');
const COPIED_MS = 2000;

const DiskTab = ({ cwd }: { cwd: string | null }) => {
  const [place, setPlace] = useState<IPlace | null>(null);
  const roots = useDiskRoots();
  const hintId = useId();
  const started = useRef(false);
  const [copied, setCopied] = useState(false);
  const {
    favorites,
    busy: favoritesBusy,
    error: favoriteError,
    toggle: toggleFavorite,
    dismissError,
  } = useDiskFavorites();
  const listing = useDiskListing(place);
  const {
    entries,
    roots: listedRoots,
    failure,
    nextCursor,
    truncated,
    loadingMore,
    loadMore,
    reload,
  } = listing;

  const [queue] = useState(() => new DiskUploadQueue(browserDiskDeps()));
  const uploads = useSyncExternalStore(queue.subscribe, queue.getSnapshot, queue.getSnapshot);
  const finished = useRef(0);
  const filesInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const [dragging, setDragging] = useState(false);

  const [creating, setCreating] = useState(false);
  const [folderName, setFolderName] = useState('');
  const [folderPending, setFolderPending] = useState(false);
  const [folderError, setFolderError] = useState<string | null>(null);

  useEffect(
    () =>
      queue.subscribe(() => {
        const done = queue.getSnapshot().filter((upload) => upload.state === UPLOAD_STATE.done).length;
        if (done > finished.current) reload();
        finished.current = done;
      }),
    [queue, reload],
  );
  useEffect(() => () => queue.stopAll(), [queue]);
  // Once, when the roots and the folder of the conversation are both known: into that folder if it can be listed, else the roots stay.
  useEffect(() => {
    if (started.current || !cwd || !roots) return;
    started.current = true;
    const found = locateDiskPlace(cwd, roots);
    if (!found) return;
    const start: IPlace = { root: found.root.id, path: found.path, rootLabel: found.root.label };
    fetchDiskPage(start, null, new AbortController().signal).then(
      () => setPlace((current) => current ?? start),
      () => {},
    );
  }, [cwd, roots]);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), COPIED_MS);
    return () => window.clearTimeout(timer);
  }, [copied]);
  const uploading = uploads.some((upload) => isActiveUpload(upload.state));
  useEffect(() => {
    if (!uploading) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [uploading]);

  const enter = (name: string) => {
    if (!place) return;
    setPlace({ ...place, path: joinDiskPath(place.path, name) });
  };
  const openRoot = (id: string, label: string) => setPlace({ root: id, path: '', rootLabel: label });
  const goTo = (depth: number) => {
    if (!place) return;
    setPlace({ ...place, path: segmentsOf(place.path).slice(0, depth).join('/') });
  };
  const goUp = () => {
    if (!place) return;
    if (place.path) goTo(segmentsOf(place.path).length - 1);
    else setPlace(null);
  };

  const add = (picks: readonly IDiskPick[]) => {
    if (!place || picks.length === 0) return;
    queue.enqueue({ root: place.root, path: place.path }, picks);
  };
  const onFiles = (event: ChangeEvent<HTMLInputElement>) => {
    add(picksFromInput(event.target.files ?? []));
    event.target.value = '';
  };

  const hasFiles = (event: DragEvent) => event.dataTransfer.types.includes('Files');
  const onDragOver = (event: DragEvent) => {
    if (!hasFiles(event)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = place ? 'copy' : 'none';
    setDragging(true);
  };
  const onDragLeave = (event: DragEvent) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragging(false);
  };
  const onDrop = (event: DragEvent) => {
    if (!hasFiles(event)) return;
    event.preventDefault();
    setDragging(false);
    if (!place) {
      toast.error(t('inspector.diskTab.dropNoFolder'));
      return;
    }
    collectDropped(event.dataTransfer.items as unknown as ArrayLike<IDropItem>).then(add, () =>
      toast.error(t('inspector.diskTab.dropReadFailed')),
    );
  };

  const closeForm = () => {
    setCreating(false);
    setFolderName('');
    setFolderError(null);
  };
  const submitFolder = async (event: FormEvent) => {
    event.preventDefault();
    const name = folderName.trim();
    if (!place || !name || folderPending) return;
    setFolderPending(true);
    setFolderError(null);
    try {
      await createDiskFolder(place, name);
      closeForm();
      reload();
    } catch (error) {
      setFolderError(
        error instanceof DiskFailure ? error.message : t('inspector.diskTab.createFolderFailed'),
      );
    } finally {
      setFolderPending(false);
    }
  };

  const onListKey = (event: KeyboardEvent<HTMLUListElement>) => {
    const rows = Array.from(listRef.current?.querySelectorAll<HTMLElement>(ROW_SELECTOR) ?? []);
    const active = document.activeElement as HTMLElement;
    const item = active.closest('li');
    const onStar = active.hasAttribute('data-disk-star');
    if (event.key === 'ArrowLeft' && onStar) {
      event.preventDefault();
      item?.querySelector<HTMLElement>(ROW_SELECTOR)?.focus();
      return;
    }
    if (event.key === 'ArrowRight') {
      const star = item?.querySelector<HTMLElement>('[data-disk-star]');
      if (star) {
        event.preventDefault();
        star.focus();
      }
      return;
    }
    const at = rows.indexOf(item?.querySelector<HTMLElement>(ROW_SELECTOR) ?? active);
    let next: HTMLElement | undefined;
    if (event.key === 'ArrowDown') next = rows[Math.min(rows.length - 1, at + 1)];
    else if (event.key === 'ArrowUp') next = rows[Math.max(0, at - 1)];
    else if (event.key === 'Home') next = rows[0];
    else if (event.key === 'End') next = rows[rows.length - 1];
    else if (event.key === 'ArrowLeft' || event.key === 'Backspace') {
      event.preventDefault();
      goUp();
      return;
    } else return;
    event.preventDefault();
    next?.focus();
  };

  const crumbs = place ? segmentsOf(place.path) : [];
  const finishedCount = uploads.filter((upload) => !isActiveUpload(upload.state)).length;
  const canUpload = place !== null;
  const showEntries = place !== null && entries !== null;
  const showRoots = place === null && listedRoots !== null;
  const rootPath = place ? roots?.find((root) => root.id === place.root)?.path : undefined;
  const absolutePath = place && rootPath !== undefined ? diskAbsolutePath(rootPath, place.path) : null;
  const copyPath = () => {
    if (absolutePath === null) return;
    navigator.clipboard.writeText(absolutePath).then(
      () => setCopied(true),
      () => toast.error(t('inspector.diskTab.copyPathFailed')),
    );
  };
  const uploadMenu: TTwMenuEntry[] = [
    {
      kind: TW_MENU_KIND.action,
      id: 'files',
      label: t('inspector.diskTab.uploadFiles'),
      icon: <Upload />,
      onSelect: () => filesInput.current?.click(),
    },
    {
      kind: TW_MENU_KIND.action,
      id: 'folder',
      label: t('inspector.diskTab.uploadFolder'),
      icon: <FolderUp />,
      onSelect: () => folderInput.current?.click(),
    },
  ];
  const here = place && place.path ? favoriteStateOf(favorites, place) : null;
  const openFavorite = (favorite: IDiskFavorite) => {
    const label = roots?.find((root) => root.id === favorite.root)?.label;
    if (favorite.missing || label === undefined) return;
    setPlace({ root: favorite.root, path: favorite.path, rootLabel: label });
  };
  const toggleHere = (name: string) => {
    if (place) void toggleFavorite({ root: place.root, path: joinDiskPath(place.path, name) });
  };

  return (
    <div
      className="tw-disk"
      data-dragging={dragging || undefined}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
      <header className="tw-disk-bar">
        <nav className="tw-disk-crumbs" aria-label={t('inspector.diskTab.path')}>
          <ol>
            <li>
              <button
                type="button"
                className="tw-disk-crumb"
                aria-current={place ? undefined : 'page'}
                onClick={() => setPlace(null)}
              >
                <HardDrive />
                <span>{t('inspector.diskTab.disk')}</span>
              </button>
            </li>
            {place && (
              <li>
                <ChevronRight aria-hidden />
                <button
                  type="button"
                  className="tw-disk-crumb"
                  aria-current={crumbs.length === 0 ? 'page' : undefined}
                  onClick={() => goTo(0)}
                >
                  {place.rootLabel}
                </button>
              </li>
            )}
            {crumbs.map((segment, index) => (
              <li key={`${index}:${segment}`}>
                <ChevronRight aria-hidden />
                <button
                  type="button"
                  className="tw-disk-crumb"
                  aria-current={index === crumbs.length - 1 ? 'page' : undefined}
                  onClick={() => goTo(index + 1)}
                >
                  {segment}
                </button>
              </li>
            ))}
          </ol>
          {place && here && (
            <FavoriteStar
              state={here}
              busy={favoritesBusy}
              inList={false}
              onToggle={() => void toggleFavorite(place)}
            />
          )}
        </nav>
        <div className="tw-disk-actions">
          <TwButton
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.secondary}
            icon={<FolderPlus />}
            aria-disabled={!canUpload || undefined}
            aria-describedby={canUpload ? undefined : hintId}
            title={canUpload ? undefined : UPLOAD_UNAVAILABLE}
            onClick={() => {
              if (canUpload) setCreating(true);
            }}
          >
            {t('inspector.diskTab.newFolder')}
          </TwButton>
          {canUpload ? (
            <TwMenuButton
              items={uploadMenu}
              menuLabel={t('inspector.diskTab.upload')}
              align={LAYER_ALIGN.end}
              className="tw-ui-button tw-disk-upload"
              data-variant="primary"
              data-size="sm"
            >
              <Upload />
              <span className="tw-ui-button-label">{t('inspector.diskTab.upload')}</span>
              <ChevronDown className="tw-disk-upload-caret" />
            </TwMenuButton>
          ) : (
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.primary}
              className="tw-disk-upload"
              icon={<Upload />}
              aria-disabled
              aria-describedby={hintId}
              title={UPLOAD_UNAVAILABLE}
            >
              {t('inspector.diskTab.upload')}
            </TwButton>
          )}
        </div>
        <input ref={filesInput} type="file" multiple hidden onChange={onFiles} />
        <input ref={folderInput} type="file" multiple hidden onChange={onFiles} {...FOLDER_INPUT_ATTRS} />
      </header>
      {!canUpload && (
        <p id={hintId} className="tw-disk-hint">
          {UPLOAD_UNAVAILABLE}
        </p>
      )}
      {absolutePath !== null && (
        <button
          type="button"
          className="tw-disk-path"
          title={t('inspector.diskTab.pathCopyHint', { path: absolutePath })}
          onClick={copyPath}
        >
          <span className="tw-disk-path-text" dir="rtl">
            <bdi>{absolutePath}</bdi>
          </span>
          {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
          <span className="tw-disk-path-state" role="status">
            {copied ? t('inspector.diskTab.copied') : ''}
          </span>
        </button>
      )}

      {favorites !== null && favorites.length > 0 && roots !== null && (
        <nav className="tw-disk-favorites" aria-label={t('inspector.diskTab.favorites')}>
          {favorites.map((favorite) => (
            <span
              key={`${favorite.root}:${favorite.path}`}
              className="tw-disk-chip"
              data-missing={favorite.missing || undefined}
            >
              <button
                type="button"
                className="tw-disk-chip-open"
                aria-disabled={favorite.missing || undefined}
                title={favorite.missing ? t('inspector.diskTab.folderGone') : favorite.path}
                onClick={() => openFavorite(favorite)}
              >
                <Folder />
                <span>{favorite.name}</span>
              </button>
              {favorite.missing && (
                <button
                  type="button"
                  className="tw-disk-chip-remove"
                  aria-label={t('inspector.diskTab.unpinNamed', { name: favorite.name })}
                  title={t('inspector.diskTab.unpin')}
                  disabled={favoritesBusy}
                  onClick={() => void toggleFavorite(favorite)}
                >
                  <X />
                </button>
              )}
            </span>
          ))}
        </nav>
      )}

      {creating && (
        <form className="tw-disk-new" onSubmit={submitFolder}>
          <input
            className="tw-disk-input"
            value={folderName}
            onChange={(event) => setFolderName(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Escape') closeForm();
            }}
            placeholder={t('inspector.diskTab.folderName')}
            aria-label={t('inspector.diskTab.newFolderName')}
            autoFocus
            maxLength={255}
          />
          <TwButton
            type="submit"
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.primary}
            pending={folderPending}
            disabled={!folderName.trim()}
          >
            {t('inspector.diskTab.create')}
          </TwButton>
          <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.ghost} onClick={closeForm}>
            {t('inspector.diskTab.cancel')}
          </TwButton>
          {folderError && (
            <p className="tw-disk-error" role="alert">
              {folderError}
            </p>
          )}
        </form>
      )}

      <div className="tw-disk-body">
        {favoriteError && (
          <p className="tw-disk-error" role="alert">
            {favoriteError.failure.message}
            {favoriteError.failure.retryable && (
              <button
                type="button"
                className="tw-disk-link"
                onClick={() => void toggleFavorite(favoriteError.place)}
              >
                {t('inspector.diskTab.retry')}
              </button>
            )}
            <button type="button" className="tw-disk-link" onClick={dismissError}>
              {t('inspector.diskTab.close')}
            </button>
          </p>
        )}
        {failure && (showEntries || showRoots) && (
          <p className="tw-disk-error" role="alert">
            {failure.message}
            {failure.retryable && (
              <button type="button" className="tw-disk-link" onClick={nextCursor ? loadMore : reload}>
                {t('inspector.diskTab.retry')}
              </button>
            )}
          </p>
        )}
        {failure && !showEntries && !showRoots && (
          <div className="tw-disk-note" role="alert">
            <p>{failure.message}</p>
            {failure.retryable && (
              <TwButton
                size={BUTTON_SIZE.sm}
                variant={BUTTON_VARIANT.secondary}
                icon={<RotateCw />}
                onClick={reload}
              >
                {t('inspector.diskTab.retry')}
              </TwButton>
            )}
          </div>
        )}
        {!failure && !showEntries && !showRoots && (
          <div className="tw-disk-note" role="status">
            <TwSpinner />
            <span>{t('inspector.diskTab.loading')}</span>
          </div>
        )}
        {showRoots && (
          <ul className="tw-disk-list" ref={listRef} onKeyDown={onListKey}>
            {listedRoots.map((root, index) => (
              <li key={root.id}>
                <button
                  type="button"
                  className="tw-disk-row"
                  data-disk-row
                  tabIndex={index === 0 ? 0 : -1}
                  title={root.path}
                  onClick={() => openRoot(root.id, root.label)}
                >
                  <HardDrive className="tw-disk-row-icon" />
                  <span className="tw-disk-row-name">{root.label}</span>
                  <span className="tw-disk-row-size" />
                  <span className="tw-disk-row-date">{root.path}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
        {showEntries && entries.length === 0 && (
          <p className="tw-disk-note">{t('inspector.diskTab.emptyFolder')}</p>
        )}
        {showEntries && entries.length > 0 && (
          <ul className="tw-disk-list" ref={listRef} onKeyDown={onListKey}>
            {entries.map((entry, index) => (
              <EntryRow
                key={entry.name}
                entry={entry}
                place={place}
                tabbable={index === 0}
                onOpen={enter}
                favorite={favoriteStateOf(favorites, {
                  root: place.root,
                  path: joinDiskPath(place.path, entry.name),
                })}
                favoritesBusy={favoritesBusy}
                onToggleFavorite={toggleHere}
              />
            ))}
          </ul>
        )}
        {showEntries && nextCursor && (
          <div className="tw-disk-more">
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.secondary}
              pending={loadingMore}
              onClick={loadMore}
            >
              {t('inspector.diskTab.showMore')}
            </TwButton>
          </div>
        )}
        {showEntries && truncated && !nextCursor && (
          <p className="tw-disk-note">{t('inspector.diskTab.truncated')}</p>
        )}
      </div>

      {uploads.length > 0 && (
        <section className="tw-disk-queue" aria-label={t('inspector.diskTab.uploads')}>
          <header className="tw-disk-queue-head">
            <span>{t('inspector.diskTab.uploadsCount', { count: uploads.length })}</span>
            {finishedCount > 0 && (
              <button type="button" className="tw-disk-link" onClick={() => queue.clearFinished()}>
                {t('inspector.diskTab.clearFinished')}
              </button>
            )}
          </header>
          <ul className="tw-disk-uploads">
            {uploads.map((upload) => (
              <UploadRow key={upload.id} upload={upload} queue={queue} />
            ))}
          </ul>
        </section>
      )}
      {dragging && (
        <div className="tw-disk-drop" aria-hidden>
          {canUpload ? t('inspector.diskTab.dropHere') : t('inspector.diskTab.dropNeedsFolder')}
        </div>
      )}
    </div>
  );
};

export default DiskTab;
