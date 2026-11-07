// The chat's folder (.hx-disk): crumbs from the folder's root, folders first, then files; a text file opens in place
// in the editor the binding hands over, the crumbs lead back. What cannot be shown (a picture, a binary) says why.
// Any entry is saved from its row, the folder on screen as one zip, and several picked entries as one zip.
// «Машина» is the same list over the whole workspace (what «Диск» was): absolute paths, crumbs up to the place the
// folder lies in, and above them the places themselves. Files are sent into the folder on screen, picked or dropped.
import {
  Fragment,
  useRef,
  useState,
  type ChangeEvent,
  type DragEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactElement,
  type ReactNode,
} from 'react';
import Segmented from '@/next/ui/segmented';
import { KEY_SCOPE } from '@/next/features/keys/commands';
import { useKeyHint } from '@/next/features/keys/keys-help';
import { useKeys } from '@/next/features/keys/use-keys';
import ListMenu from '@/next/features/pane/list-menu';
import QuickOpen from '@/next/features/pane/quick-open';
import FindInFiles, {
  NO_FIND,
  type IFindState,
  type IFoundText,
  type ITextHit,
} from '@/next/features/pane/find-in-files';
import { toast } from '@/next/ui/toast';
import Icon from '@/next/ui/icon';
import PaneHead, { Crumbs } from '@/next/features/pane/pane-head';
import { crumbsOf, MACHINE, trimmed } from '@/next/features/pane/crumbs';
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
  /** Opens an entry in a tab of its own (⌘-click or the middle button on its row). */
  onOpenBeside?: (path: string, kind: 'file' | 'directory') => void;
  /** Saves these paths: one file as it is, a folder or several as one zip. */
  onDownload: (paths: readonly string[]) => void;
  /** Two picked files, one against the other. */
  onCompare?: (a: string, b: string) => void;
  /** The open file's own controls (download, «…», «Сохранить»): on the right of the crumbs, one row, after what the
   *  editor puts there (the switch of the view). */
  tools?: ReactNode;
  /** «…» of an entry, around the button that opens it; `acts` rename it or move it to the trash, where it can be. */
  menuOf?: (
    path: string,
    kind: 'file' | 'directory',
    trigger: ReactElement,
    acts: IEntryActs | null,
  ) => ReactNode;
  /** New files, new names and the trash for the folder on screen; null where nothing is changed. */
  entryOps?: IEntryOps | null;
  /** The folder on screen takes files and new folders; null where it does not (outside the workspace's places). */
  take?: IFolderTake | null;
  /** What goes under the list: the files being sent. */
  below?: ReactNode;
  /** «Перейти к файлу» (⌘P): the files matching a query, as paths `onOpen` takes. */
  find?: (query: string, signal: AbortSignal) => Promise<readonly string[]>;
  /** «Найти в файлах» (⇧⌘F): the lines holding a text; `onOpenAt` opens one with the text selected. */
  findText?: (query: string, signal: AbortSignal) => Promise<IFoundText>;
  onOpenAt?: (hit: ITextHit, size: number) => void;
}

export interface IFolderTake {
  /** Files picked in the browser's dialog (a folder's carry their folders). */
  fromInput: (files: ArrayLike<File>) => void;
  /** Files and folders dropped on the list; fails when the browser could not read them. */
  fromDrop: (items: DataTransferItemList) => Promise<void>;
  /** Answers why the folder was not made; null when it was. */
  makeFolder: (name: string) => Promise<string | null>;
}

export interface IEntryOps {
  /** Each answers the path of what it made or renamed, and throws why it was not done. */
  newFile: (name: string) => Promise<string>;
  rename: (path: string, name: string) => Promise<string>;
  trash: (path: string) => Promise<string>;
}

export interface IEntryActs {
  onRename: () => void;
  onTrash: () => void;
}

/** A change typed or confirmed in a row of the list: a new folder or file over it, an entry's new name in its place,
 *  the trash asked under it. */
type TEdit =
  { kind: 'newFolder' | 'newFile'; name: string } | { kind: 'rename' | 'trash'; path: string; name: string };
type TEditing = TEdit & { at: string; failure: string | null; busy: boolean };

interface INameRowProps {
  icon: string;
  label: string;
  done: string;
  value: string;
  busy: boolean;
  failure: string | null;
  /** The name is picked without its extension, as a file manager does on a rename. */
  pickStem?: boolean;
  onChange: (name: string) => void;
  onSubmit: () => void;
  onCancel: () => void;
}

const NameRow = ({
  icon,
  label,
  done,
  value,
  busy,
  failure,
  pickStem,
  onChange,
  onSubmit,
  onCancel,
}: INameRowProps) => (
  <form
    className="hx-disk__new"
    onSubmit={(event) => {
      event.preventDefault();
      onSubmit();
    }}
  >
    <Icon name={icon} size="sm" />
    <input
      className="hx-disk__name"
      autoFocus
      value={value}
      placeholder={label}
      aria-label={label}
      aria-invalid={failure !== null || undefined}
      disabled={busy}
      onFocus={(event) => {
        if (!pickStem) return;
        const dot = event.target.value.lastIndexOf('.');
        event.target.setSelectionRange(0, dot > 0 ? dot : event.target.value.length);
      }}
      onChange={(event) => onChange(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === 'Escape') onCancel();
      }}
    />
    <Button size="sm" variant="ghost" onClick={onCancel}>
      {t('next.files.pickCancel')}
    </Button>
    <Button size="sm" variant="primary" disabled={busy || value.trim() === ''} onClick={onSubmit}>
      {done}
    </Button>
    {failure && (
      <span className="hx-disk__fail" role="alert">
        {failure}
      </span>
    )}
  </form>
);

const reasonOf = (failure: unknown) =>
  failure instanceof Error ? failure.message : t('next.files.opFailed');

/** What git's mark of an entry says, in its tip. */
const GIT_LABEL = {
  file: { new: 'next.files.gitNew', changed: 'next.files.gitChanged' },
  directory: { new: 'next.files.gitNewFolder', changed: 'next.files.gitChangedFolder' },
} as const;

const SCOPE = { project: 'project', machine: 'machine' } as const;
type TScope = (typeof SCOPE)[keyof typeof SCOPE];
const FOLDER_INPUT = { webkitdirectory: '', directory: '' } as Record<string, string>;

const EXIT_MS = 180;
/** A finger held this long on a row picks it (the phone's way into picking several). */
const HOLD_MS = 450;
/** A finger that moved this far is scrolling, not holding. */
const HOLD_SLOP = 8;

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
  onOpenBeside,
  onDownload,
  onCompare,
  tools,
  menuOf,
  take = null,
  entryOps = null,
  below,
  find,
  findText,
  onOpenAt,
}: IFilesViewProps) => {
  const [quick, setQuick] = useState(false);
  const [finding, setFinding] = useState(false);
  const [findState, setFindState] = useState<IFindState>(NO_FIND);
  const canFind = findText !== undefined && onOpenAt !== undefined;
  const openQuick = () => {
    setFinding(false);
    setQuick(true);
  };
  const openFind = () => {
    setQuick(false);
    setFinding(true);
  };
  useKeys(
    KEY_SCOPE.files,
    { 'files.quickOpen': find ? openQuick : undefined, 'files.find': canFind ? openFind : undefined },
    find !== undefined || canFind,
  );
  const quickKey = useKeyHint('files.quickOpen');
  const findKey = useKeyHint('files.find');
  const findButton = canFind && (
    <button
      type="button"
      className="hx-head-btn hx-tipped"
      aria-label={t('next.files.find')}
      aria-expanded={finding}
      onClick={openFind}
    >
      <Icon name="text-search" size="sm" />
      <span className="hx-tip" data-pos="below" data-align="end">
        {t('next.files.find')}
        {findKey && <kbd>{findKey}</kbd>}
      </span>
    </button>
  );
  const quickButton = find && (
    <button
      type="button"
      className="hx-head-btn hx-tipped"
      aria-label={t('next.files.quickOpen')}
      aria-expanded={quick}
      onClick={openQuick}
    >
      <Icon name="search" size="sm" />
      <span className="hx-tip" data-pos="below" data-align="end">
        {t('next.files.quickOpen')}
        {quickKey && <kbd>{quickKey}</kbd>}
      </span>
    </button>
  );
  const home = trimmed(root);
  const scope: TScope = path.startsWith('/') ? SCOPE.machine : SCOPE.project;
  const crumbs = crumbsOf(root, path, listing?.root, opening === 'file');
  // The same folder in the other scope: the chat's folder seen from the machine, or back to it.
  const rescope = (to: TScope) => {
    if (to === scope) return;
    if (to === SCOPE.machine) onOpen(path === '' ? home : `${home}/${path}`, 'directory');
    else onOpen(path.startsWith(`${home}/`) ? path.slice(home.length + 1) : '', 'directory');
  };
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
  // A held finger starts picking with that row; the click the finger then makes is the hold's, not an open.
  const hold = useRef<{ timer: number; x: number; y: number } | null>(null);
  const held = useRef(false);
  const release = () => {
    if (hold.current) window.clearTimeout(hold.current.timer);
    hold.current = null;
  };
  const press = (entry: string) => (event: PointerEvent) => {
    if (event.pointerType !== 'touch' || picking) return;
    release();
    held.current = false;
    hold.current = {
      x: event.clientX,
      y: event.clientY,
      timer: window.setTimeout(() => {
        hold.current = null;
        held.current = true;
        navigator.vibrate?.(10);
        setPicked({ at: path, paths: new Set([entry]) });
      }, HOLD_MS),
    };
  };
  const drift = (event: PointerEvent) => {
    const at = hold.current;
    if (at && Math.hypot(event.clientX - at.x, event.clientY - at.y) > HOLD_SLOP) release();
  };
  // The change being typed or confirmed in the folder on screen; another folder drops it.
  const [edit, setEdit] = useState<TEditing | null>(null);
  const editing = edit !== null && edit.at === path ? edit : null;
  const startEdit = (next: TEdit) => setEdit({ ...next, at: path, failure: null, busy: false });
  const typeName = (name: string) => setEdit((now) => now && { ...now, name, failure: null });
  /** Answers why the change was not made; null when it was. */
  const runEdit = async (change: TEdit, name: string): Promise<string | null> => {
    if (change.kind === 'newFolder') {
      const failure = take ? await take.makeFolder(name) : t('next.files.newFolderNowhere');
      if (failure === null) onRetry();
      return failure;
    }
    if (!entryOps) return t('next.files.opFailed');
    try {
      switch (change.kind) {
        case 'newFile':
          onOpen(await entryOps.newFile(name), 'file');
          break;
        case 'rename':
          await entryOps.rename(change.path, name);
          onRetry();
          break;
        case 'trash':
          await entryOps.trash(change.path);
          toast(t('next.files.trashed', { name: change.name }), { tone: 'success' });
          onRetry();
          break;
      }
      return null;
    } catch (failure) {
      return reasonOf(failure);
    }
  };
  const commit = async () => {
    if (!editing || editing.busy) return;
    const name = editing.name.trim();
    if (editing.kind !== 'trash' && name === '') return;
    setEdit({ ...editing, busy: true, failure: null });
    const failure = await runEdit(editing, name);
    setEdit((now) => (failure === null ? null : now && { ...now, busy: false, failure }));
  };
  const actsOf = (entry: { path: string; name: string }): IEntryActs | null =>
    entryOps && {
      onRename: () => startEdit({ kind: 'rename', path: entry.path, name: entry.name }),
      onTrash: () => startEdit({ kind: 'trash', path: entry.path, name: entry.name }),
    };
  const filesInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const sendPicked = (event: ChangeEvent<HTMLInputElement>) => {
    take?.fromInput(event.target.files ?? []);
    event.target.value = '';
  };
  const [dropping, setDropping] = useState(false);
  const [menuAt, setMenuAt] = useState<{ x: number; y: number } | null>(null);
  const listItems = [
    ...(take
      ? [
          {
            icon: 'folder-plus' as const,
            label: t('next.files.newFolder'),
            onPick: () => startEdit({ kind: 'newFolder', name: '' }),
          },
        ]
      : []),
    ...(entryOps
      ? [
          {
            icon: 'file-plus' as const,
            label: t('next.files.newFile'),
            onPick: () => startEdit({ kind: 'newFile', name: '' }),
          },
        ]
      : []),
    ...(take
      ? [
          {
            icon: 'upload' as const,
            label: t('next.files.upload'),
            onPick: () => filesInput.current?.click(),
          },
        ]
      : []),
  ];
  const openListMenu = (event: MouseEvent) => {
    if (listItems.length === 0 || (event.target as HTMLElement).closest('.hx-disk__item, .hx-disk__name'))
      return;
    event.preventDefault();
    setMenuAt({ x: event.clientX, y: event.clientY });
  };
  const carriesFiles = (event: DragEvent) => event.dataTransfer.types.includes('Files');
  const dropTo = take
    ? {
        onDragOver: (event: DragEvent) => {
          if (!carriesFiles(event)) return;
          event.preventDefault();
          event.dataTransfer.dropEffect = 'copy';
          setDropping(true);
        },
        onDragLeave: (event: DragEvent) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropping(false);
        },
        onDrop: (event: DragEvent) => {
          if (!carriesFiles(event)) return;
          event.preventDefault();
          setDropping(false);
          take
            .fromDrop(event.dataTransfer.items)
            .catch(() => toast(t('next.files.dropFailed'), { tone: 'error' }));
        },
      }
    : {};
  const pickedList = [...(picking ?? [])];
  const kindOf = (p: string) => listing?.entries.find((e) => e.path === p)?.kind;
  const pair = pickedList.length === 2 && pickedList.every((p) => kindOf(p) === 'file') ? pickedList : null;
  return (
    <div className="hx-tv" data-tool="disk" data-file={file !== null || undefined}>
      <div className="hx-disk">
        <PaneHead
          lead={<Crumbs items={crumbs} onPick={(at) => onOpen(at, 'directory')} />}
          tools={
            <>
              {error === null && file !== null && (
                <>
                  {quickButton}
                  {findButton}
                  {tools}
                </>
              )}
              {folder && (
                <>
                  <Segmented
                    label={t('next.files.scope')}
                    value={scope}
                    options={[
                      { value: SCOPE.project, label: t('next.files.project') },
                      { value: SCOPE.machine, label: t('next.files.machine') },
                    ]}
                    onChange={rescope}
                  />
                  {quickButton}
                  {findButton}
                  {entryOps && (
                    <button
                      type="button"
                      className="hx-head-btn hx-tipped"
                      aria-label={t('next.files.newFile')}
                      onClick={() => startEdit({ kind: 'newFile', name: '' })}
                    >
                      <Icon name="file-plus" size="sm" />
                      <span className="hx-tip" data-pos="below" data-align="end">
                        {t('next.files.newFile')}
                      </span>
                    </button>
                  )}
                  {take && (
                    <>
                      <button
                        type="button"
                        className="hx-head-btn hx-tipped"
                        aria-label={t('next.files.newFolder')}
                        onClick={() => startEdit({ kind: 'newFolder', name: '' })}
                      >
                        <Icon name="folder-plus" size="sm" />
                        <span className="hx-tip" data-pos="below" data-align="end">
                          {t('next.files.newFolder')}
                        </span>
                      </button>
                      <button
                        type="button"
                        className="hx-head-btn hx-tipped"
                        aria-label={t('next.files.upload')}
                        onClick={() => filesInput.current?.click()}
                      >
                        <Icon name="upload" size="sm" />
                        <span className="hx-tip" data-pos="below" data-align="end">
                          {t('next.files.uploadHint')}
                        </span>
                      </button>
                      <button
                        type="button"
                        className="hx-head-btn hx-tipped"
                        aria-label={t('next.files.uploadFolder')}
                        onClick={() => folderInput.current?.click()}
                      >
                        <Icon name="folder-input" size="sm" />
                        <span className="hx-tip" data-pos="below" data-align="end">
                          {t('next.files.uploadFolder')}
                        </span>
                      </button>
                      <input ref={filesInput} type="file" multiple hidden onChange={sendPicked} />
                      <input
                        ref={folderInput}
                        type="file"
                        multiple
                        hidden
                        {...FOLDER_INPUT}
                        onChange={sendPicked}
                      />
                    </>
                  )}
                </>
              )}
              {folder && path !== MACHINE && listing !== null && listing.entries.length > 0 && (
                <>
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
                </>
              )}
            </>
          }
        >
          {finding && findText && onOpenAt && (
            <FindInFiles
              find={findText}
              state={findState}
              onState={setFindState}
              onClose={() => setFinding(false)}
              onOpen={(hit, size) => {
                setFinding(false);
                onOpenAt(hit, size);
              }}
            />
          )}
          {quick && find && (
            <QuickOpen
              find={find}
              onClose={() => setQuick(false)}
              onOpen={(found) => {
                setQuick(false);
                onOpen(found, 'file');
              }}
            />
          )}
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
              data-drop={dropping || undefined}
              {...dropTo}
              onContextMenu={openListMenu}
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
                {editing && (editing.kind === 'newFolder' || editing.kind === 'newFile') && (
                  <NameRow
                    icon={editing.kind === 'newFolder' ? 'folder' : 'file-text'}
                    label={t(
                      editing.kind === 'newFolder' ? 'next.files.newFolderName' : 'next.files.newFileName',
                    )}
                    done={t('next.files.newFolderMake')}
                    value={editing.name}
                    busy={editing.busy}
                    failure={editing.failure}
                    onChange={typeName}
                    onSubmit={() => void commit()}
                    onCancel={() => setEdit(null)}
                  />
                )}
                {listing?.entries.map((entry) => {
                  const on = picking?.has(entry.path) ?? false;
                  const mine = editing && 'path' in editing && editing.path === entry.path ? editing : null;
                  if (mine?.kind === 'rename')
                    return (
                      <NameRow
                        key={entry.path}
                        icon={entry.kind === 'directory' ? 'folder' : 'file-text'}
                        label={t('next.files.renameName')}
                        done={t('next.files.renameDo')}
                        value={mine.name}
                        busy={mine.busy}
                        failure={mine.failure}
                        pickStem={entry.kind === 'file'}
                        onChange={typeName}
                        onSubmit={() => void commit()}
                        onCancel={() => setEdit(null)}
                      />
                    );
                  return (
                    <Fragment key={entry.path}>
                      <div className="hx-disk__item">
                        <button
                          type="button"
                          className="hx-tv__row"
                          role={picking ? 'checkbox' : undefined}
                          aria-checked={picking ? on : undefined}
                          onPointerDown={press(entry.path)}
                          onPointerMove={drift}
                          onPointerUp={release}
                          onPointerCancel={release}
                          onContextMenu={(event) => {
                            if (held.current || hold.current) event.preventDefault();
                          }}
                          onClick={(event) => {
                            if (held.current) {
                              held.current = false;
                              return;
                            }
                            if (picking) toggle(entry.path);
                            else if (onOpenBeside && (event.metaKey || event.ctrlKey))
                              onOpenBeside(entry.path, entry.kind);
                            else onOpen(entry.path, entry.kind);
                          }}
                          onAuxClick={(event) => {
                            if (event.button !== 1 || picking || !onOpenBeside) return;
                            event.preventDefault();
                            onOpenBeside(entry.path, entry.kind);
                          }}
                        >
                          {picking && (
                            <span className="hx-disk__check" data-on={on || undefined} aria-hidden="true">
                              <Icon name="check" size="sm" />
                            </span>
                          )}
                          <Icon name={entry.kind === 'directory' ? 'folder' : 'file-text'} size="sm" />
                          <span className="hx-tv__main">
                            <span className="hx-tv__name" data-git={entry.git}>
                              {entry.name}
                            </span>
                          </span>
                          {entry.git && (
                            <span
                              className="hx-disk__git"
                              data-git={entry.git}
                              role="img"
                              aria-label={t(GIT_LABEL[entry.kind][entry.git])}
                              title={t(GIT_LABEL[entry.kind][entry.git])}
                            />
                          )}
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
                              actsOf(entry),
                            )}
                          </span>
                        )}
                      </div>
                      {mine?.kind === 'trash' && (
                        <form
                          className="hx-disk__new"
                          role="alertdialog"
                          aria-label={t('next.files.trashAsk', { name: entry.name })}
                          onSubmit={(event) => {
                            event.preventDefault();
                            void commit();
                          }}
                        >
                          <Icon name="trash-2" size="sm" />
                          <span className="hx-disk__ask">
                            {t('next.files.trashAsk', { name: entry.name })}
                          </span>
                          <Button size="sm" variant="ghost" autoFocus onClick={() => setEdit(null)}>
                            {t('next.files.pickCancel')}
                          </Button>
                          <Button
                            size="sm"
                            variant="danger"
                            disabled={mine.busy}
                            onClick={() => void commit()}
                          >
                            {t('next.files.trashDo')}
                          </Button>
                          {mine.failure && (
                            <span className="hx-disk__fail" role="alert">
                              {mine.failure}
                            </span>
                          )}
                        </form>
                      )}
                    </Fragment>
                  );
                })}
                {listing?.entries.length === 0 && (
                  <div className="hx-tv__empty">{t(take ? 'next.files.emptyDrop' : 'next.files.empty')}</div>
                )}
                {listing?.truncated && <div className="hx-tv__note">{t('next.files.truncated')}</div>}
              </Swap>
            </div>
          )}
          {below}
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
        </PaneHead>
      </div>
      <ListMenu at={menuAt} items={listItems} onClose={() => setMenuAt(null)} />
    </div>
  );
};

export default FilesView;
