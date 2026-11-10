// Folders in the library: the crumbs and the tiles above the cards, the dialogs that name, delete and choose a
// folder. A folder is a shelf, not a right: nothing here changes who sees a project. Tiles and crumbs are drop places
// for the cards (`data-drop-folder`, use-card-drag.ts).
import { Menu } from '@base-ui/react/menu';
import { useState } from 'react';
import type { IFolder } from '@/next/features/designly/api';
import { folderChoices } from '@/next/features/designly/library-model';
import { Modal } from '@/next/features/designly/sdk/ui';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';

/** The dialogs leave this long (shell.css, .hx-modal). */
const EXIT_MS = 240;

interface ICrumbsProps {
  folders: readonly IFolder[];
  current: string;
  onOpen: (id: string) => void;
}

/** «Библиотека › Работа › Архив»: every crumb opens its folder and takes a drop. */
export const Crumbs = ({ folders, current, onOpen }: ICrumbsProps) => {
  const here = folders.find((f) => f.id === current);
  const chain = [here?.parent ? folders.find((f) => f.id === here.parent) : undefined, here].filter(
    (f): f is IFolder => f !== undefined,
  );
  return (
    <nav className="hx-dl__crumbs" aria-label={t('next.designly.folderPath')}>
      <button type="button" className="hx-dl__crumb" data-drop-folder="" onClick={() => onOpen('')}>
        <Icon name="layers" size="sm" />
        {t('next.designly.library')}
      </button>
      {chain.map((f, i) => (
        <span key={f.id} className="hx-dl__crumb-wrap">
          <Icon name="chevron-right" size="sm" />
          <button
            type="button"
            className="hx-dl__crumb"
            aria-current={i === chain.length - 1 ? 'page' : undefined}
            data-drop-folder={f.id}
            onClick={() => onOpen(f.id)}
          >
            {f.name}
          </button>
        </span>
      ))}
    </nav>
  );
};

interface ITileProps {
  folder: IFolder;
  onOpen: (id: string) => void;
  onRename: (folder: IFolder) => void;
  onDelete: (folder: IFolder) => void;
}

const FolderTile = ({ folder, onOpen, onRename, onDelete }: ITileProps) => {
  const actions = useMenuActions();
  return (
    <div className="hx-dl-folder" data-drop-folder={folder.id}>
      <button type="button" className="hx-dl-folder__open" onClick={() => onOpen(folder.id)}>
        <Icon name="folder" />
        <span className="hx-dl-folder__name">{folder.name}</span>
      </button>
      <Menu.Root actionsRef={actions}>
        <Menu.Trigger
          className="hx-dl-folder__more"
          aria-label={t('next.designly.folderMenu', { name: folder.name })}
        >
          <Icon name="ellipsis" size="sm" />
        </Menu.Trigger>
        <MenuPanel
          actions={actions}
          className="hx-cmenu"
          label={folder.name}
          side="bottom"
          align="end"
          sideOffset={4}
        >
          <Menu.Item className="hx-cmenu__item" onClick={() => onRename(folder)}>
            <Icon name="pencil" size="sm" />
            <span className="hx-cmenu__txt">{t('next.designly.rename')}</span>
          </Menu.Item>
          <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={() => onDelete(folder)}>
            <Icon name="trash-2" size="sm" />
            <span className="hx-cmenu__txt">{t('next.designly.deleteFolder')}</span>
          </Menu.Item>
        </MenuPanel>
      </Menu.Root>
    </div>
  );
};

export const FolderTiles = ({ folders, ...rest }: { folders: IFolder[] } & Omit<ITileProps, 'folder'>) => (
  <div className="hx-dl__folders">
    {folders.map((f) => (
      <FolderTile key={f.id} folder={f} {...rest} />
    ))}
  </div>
);

export interface IFolderForm {
  /** Set when an existing folder is renamed. */
  folder?: IFolder;
  parent: string;
}

interface INameProps {
  form: IFolderForm | null;
  onClose: () => void;
  onSave: (input: { id?: string; name: string; parent?: string }) => Promise<void>;
}

const NameForm = ({
  form,
  onClose,
  onSave,
}: {
  form: IFolderForm;
  onClose: () => void;
  onSave: INameProps['onSave'];
}) => {
  const [name, setName] = useState(form.folder?.name ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    if (busy || name.trim() === '') return;
    setBusy(true);
    setError(null);
    onSave(form.folder ? { id: form.folder.id, name } : { name, parent: form.parent })
      .then(onClose, (e: Error) => setError(e.message))
      .finally(() => setBusy(false));
  };
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <input
        className="hx-dl__name-input"
        value={name}
        maxLength={60}
        placeholder={t('next.designly.folderName')}
        aria-label={t('next.designly.folderName')}
        ref={(el) => el?.focus()}
        onChange={(event) => setName(event.target.value)}
      />
      {error && <p className="hx-dl__form-error">{error}</p>}
      <div className="hx-modal__foot">
        <Button variant="ghost" disabled={busy} onClick={onClose}>
          {t('next.designly.deleteCancel')}
        </Button>
        <Button variant="primary" busy={busy} disabled={name.trim() === ''} onClick={submit}>
          {t(form.folder ? 'next.designly.save' : 'next.designly.create')}
        </Button>
      </div>
    </form>
  );
};

/** A folder named: new, or renamed. The machine's refusal (a twin name) stays in the dialog. */
export const FolderNameDialog = ({ form, onClose, onSave }: INameProps) => {
  const shown = usePresence(form, EXIT_MS).value;
  return (
    <Modal
      open={form !== null}
      onClose={onClose}
      title={t(shown?.folder ? 'next.designly.renameFolder' : 'next.designly.newFolder')}
      boxClass="hx-leave"
    >
      {shown && <NameForm form={shown} onClose={onClose} onSave={onSave} />}
    </Modal>
  );
};

interface IDeleteProps {
  folder: IFolder | null;
  /** How many projects lie in it and under it. */
  projects: number;
  onClose: () => void;
  onDelete: (id: string, projects: 'keep' | 'trash') => Promise<void>;
}

/** A folder goes. With projects in it, it asks what happens to them: stay in the library, or go to the trash too. */
export const FolderDeleteDialog = ({ folder, projects, onClose, onDelete }: IDeleteProps) => {
  const shown = usePresence(folder, EXIT_MS).value;
  const [busy, setBusy] = useState<'keep' | 'trash' | null>(null);
  const run = (mode: 'keep' | 'trash') => {
    if (!shown || busy) return;
    setBusy(mode);
    onDelete(shown.id, mode)
      .then(onClose, () => undefined)
      .finally(() => setBusy(null));
  };
  return (
    <Modal
      open={folder !== null}
      onClose={() => {
        if (busy === null) onClose();
      }}
      title={t('next.designly.deleteFolderTitle', { name: shown?.name ?? '' })}
      lead={t(projects > 0 ? 'next.designly.deleteFolderFull' : 'next.designly.deleteFolderEmpty')}
      boxClass="hx-leave"
    >
      <div className="hx-modal__foot">
        <Button variant="ghost" disabled={busy !== null} onClick={onClose}>
          {t('next.designly.deleteCancel')}
        </Button>
        {projects > 0 ? (
          <>
            <Button
              variant="ghost"
              busy={busy === 'keep'}
              disabled={busy === 'trash'}
              onClick={() => run('keep')}
            >
              {t('next.designly.keepProjects')}
            </Button>
            <Button
              variant="danger"
              busy={busy === 'trash'}
              disabled={busy === 'keep'}
              onClick={() => run('trash')}
            >
              {t('next.designly.trashProjects')}
            </Button>
          </>
        ) : (
          <Button variant="danger" busy={busy === 'keep'} onClick={() => run('keep')}>
            {t('next.designly.deleteConfirm')}
          </Button>
        )}
      </div>
    </Modal>
  );
};

interface IMoveProps {
  /** The projects to move; null closes the dialog. */
  ids: string[] | null;
  folders: readonly IFolder[];
  /** Where they lie now, when all lie in one place. */
  from: string | null;
  onClose: () => void;
  onMove: (ids: string[], folder: string) => Promise<void>;
}

/** «Переместить в…»: the library itself and every folder. */
export const MoveDialog = ({ ids, folders, from, onClose, onMove }: IMoveProps) => {
  const shown = usePresence(ids, EXIT_MS).value;
  const [busy, setBusy] = useState<string | null>(null);
  const go = (folder: string) => {
    if (!shown || busy !== null) return;
    setBusy(folder);
    onMove(shown, folder)
      .then(onClose, () => undefined)
      .finally(() => setBusy(null));
  };
  const rows = [{ id: '', name: t('next.designly.libraryRoot'), depth: 0 }].concat(
    folderChoices(folders).map((c) => ({ id: c.folder.id, name: c.folder.name, depth: c.depth })),
  );
  return (
    <Modal
      open={ids !== null}
      onClose={() => {
        if (busy === null) onClose();
      }}
      title={t('next.designly.moveTo')}
      boxClass="hx-leave"
    >
      <div className="hx-dl__choices" role="listbox" aria-label={t('next.designly.moveTo')}>
        {rows.map((r) => (
          <button
            key={r.id}
            type="button"
            role="option"
            aria-selected={from === r.id}
            className="hx-dl__choice"
            data-depth={r.depth}
            disabled={busy !== null}
            onClick={() => go(r.id)}
          >
            <Icon name={r.id === '' ? 'layers' : 'folder'} size="sm" />
            <span>{r.name}</span>
            {from === r.id && <Icon name="check" size="sm" />}
          </button>
        ))}
      </div>
    </Modal>
  );
};
