import { useRef, useState, type FormEvent } from 'react';
import { plural, t } from '@/next/i18n';
import type { ProjectChats } from '@/next/protocol/v2';
import Modal from '@/next/features/shell/modal';
import Icon from '@/next/ui/icon';
import { GIVE_UP_MS, SLOW_MS } from '@/next/ui/use-pending-list';

export interface IProjectSettings {
  name: string;
  /** The folder the project's new chats start in. */
  path: string;
}

/** A folder the machine offers: one level below its default folder, and the git folders below those. */
export interface IProjectFolder {
  path: string;
  git: boolean;
  branch: string | null;
}

export interface IProjectSettingsDialogProps {
  /** The project as it is now; null: the dialog is closed. */
  project: IProjectSettings | null;
  /** The folders offered under the field; null while they load (or when the machine did not give them). */
  folders: readonly IProjectFolder[] | null;
  /** The project's chats now: how many, and how many of them have an agent at work. */
  chats: { count: number; working: number };
  onClose: () => void;
  /** Resolves once the server has the change; a rejection is said under the fields. Only what changed is passed. */
  onSave: (change: Partial<IProjectSettings>) => Promise<void>;
  /** Resolves once the project is gone, its chats moved out (keep) or deleted with it; absent: no «Удалить проект». */
  onDelete?: (chats: ProjectChats) => Promise<void>;
}

const STATE = { idle: 'idle', busy: 'busy', slow: 'slow' } as const;
type TState = (typeof STATE)[keyof typeof STATE];

/** The dialog's two faces: the fields, and the deletion asked of the project with what becomes of its chats. */
const STEP = { edit: 'edit', delete: 'delete' } as const;
type TStep = (typeof STEP)[keyof typeof STEP];

/** Runs work with the dialog's busy, slow and give-up; resolves true once it is done, false when it was said why not. */
const useWork =
  (setState: (s: TState) => void, setError: (e: string | null) => void) =>
  async (work: () => Promise<void>, failed: string): Promise<boolean> => {
    setError(null);
    setState(STATE.busy);
    const slow = setTimeout(() => setState(STATE.slow), SLOW_MS);
    let giveUp: ReturnType<typeof setTimeout> | undefined;
    try {
      await Promise.race([
        work(),
        new Promise((_, reject) => {
          giveUp = setTimeout(() => reject(new Error(t('next.pending.noAnswer'))), GIVE_UP_MS);
        }),
      ]);
      return true;
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : failed);
      setState(STATE.idle);
      return false;
    } finally {
      clearTimeout(slow);
      clearTimeout(giveUp);
    }
  };

/** «Настройки проекта»: its name and the folder its new chats start in. The chats it has stay where they started. */
const FOLDERS_SHOWN = 6;

const ProjectSettingsDialog = ({
  project,
  folders,
  chats,
  onClose,
  onSave,
  onDelete,
}: IProjectSettingsDialogProps) => {
  const pathRef = useRef<HTMLInputElement>(null);
  const keepRef = useRef<HTMLButtonElement>(null);
  const [name, setName] = useState('');
  const [path, setPath] = useState('');
  const [state, setState] = useState<TState>(STATE.idle);
  const [error, setError] = useState<string | null>(null);
  const [step, setStep] = useState<TStep>(STEP.edit);
  // the chats move out unless the person picks to delete them: the choice that loses nothing comes first
  const [fate, setFate] = useState<ProjectChats>('keep');
  const work = useWork(setState, setError);

  // a project opened (or changed on the machine) gives the fields their values again
  const [shown, setShown] = useState<IProjectSettings | null>(null);
  if (project !== shown) {
    setShown(project);
    setName(project?.name ?? '');
    setPath(project?.path ?? '');
    setError(null);
    setState(STATE.idle);
    if (project === null) {
      setStep(STEP.edit);
      setFate('keep');
    }
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!project) return;
    const nextName = name.trim();
    const nextPath = path.trim().replace(/(.)\/+$/, '$1');
    if (!nextName) {
      setError(t('next.newProject.nameMissing'));
      return;
    }
    if (!nextPath.startsWith('/')) {
      setError(t('next.projectSettings.pathAbsolute'));
      return;
    }
    const change: Partial<IProjectSettings> = {};
    if (nextName !== project.name) change.name = nextName;
    if (nextPath !== project.path) change.path = nextPath;
    if (change.name === undefined && change.path === undefined) {
      onClose();
      return;
    }
    if (await work(() => onSave(change), t('next.projectSettings.failed'))) onClose();
  };

  const remove = async (event: FormEvent) => {
    event.preventDefault();
    if (!project || !onDelete) return;
    // a project with no chats has nothing to keep: it goes as «keep», which moves nothing
    const chosen = chats.count === 0 ? 'keep' : fate;
    if (await work(() => onDelete(chosen), t('next.projectDelete.failed'))) onClose();
  };

  const toStep = (next: TStep) => {
    setStep(next);
    setError(null);
  };

  const busy = state !== STATE.idle;
  // the folders that have what is typed in their path; the current one is not offered again
  const typed = path.trim().toLowerCase();
  const offered = (folders ?? [])
    .filter((folder) => folder.path !== path.trim())
    .filter(
      (folder) =>
        !typed || typed === project?.path.toLowerCase() || folder.path.toLowerCase().includes(typed),
    )
    .slice(0, FOLDERS_SHOWN);
  const spin = <span className="hx-gate__spin" aria-hidden="true" />;
  const errorLine = error && (
    <p className="hx-create__err" role="alert">
      {error}
    </p>
  );
  const slowLine = state === STATE.slow && <p className="hx-create__slow">{t('next.chatRow.slow')}</p>;

  if (step === STEP.delete) {
    const choices: { value: ProjectChats; label: string; note: string }[] = [
      { value: 'keep', label: t('next.projectDelete.keep'), note: t('next.projectDelete.keepNote') },
      {
        value: 'delete',
        label: t('next.projectDelete.drop'),
        note:
          chats.working > 0
            ? plural(chats.working, 'next.projectDelete.dropWorking')
            : t('next.projectDelete.dropNote'),
      },
    ];
    const dropping = chats.count > 0 && fate === 'delete';
    return (
      <Modal
        open={project !== null}
        onClose={onClose}
        title={t('next.projectDelete.title', { project: project?.name ?? '' })}
        lead={chats.count === 0 ? t('next.projectDelete.leadEmpty') : t('next.projectDelete.lead')}
        boxClass="hx-create"
        initialFocus={keepRef}
      >
        <form onSubmit={(event) => void remove(event)}>
          <div className="hx-modal__body">
            {chats.count > 0 && (
              <div
                className="hx-pdel"
                role="radiogroup"
                aria-label={plural(chats.count, 'next.projectDelete.chats')}
              >
                <p className="hx-pdel__ask">{plural(chats.count, 'next.projectDelete.chats')}</p>
                {choices.map((choice) => (
                  <button
                    key={choice.value}
                    ref={choice.value === 'keep' ? keepRef : undefined}
                    type="button"
                    role="radio"
                    className="hx-pdel__opt"
                    data-tone={choice.value === 'delete' ? 'danger' : undefined}
                    aria-checked={fate === choice.value}
                    disabled={busy}
                    onClick={() => setFate(choice.value)}
                  >
                    <span className="hx-pdel__dot" aria-hidden="true" />
                    <span className="hx-pdel__text">
                      <span className="hx-pdel__label">
                        {choice.value === 'delete' ? `${choice.label} (${chats.count})` : choice.label}
                      </span>
                      <span className="hx-pdel__note">{choice.note}</span>
                    </span>
                  </button>
                ))}
              </div>
            )}
            {errorLine}
            {slowLine}
          </div>
          <div className="hx-modal__foot">
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              disabled={busy}
              onClick={() => toStep(STEP.edit)}
            >
              {t('next.projectDelete.back')}
            </button>
            <button type="submit" className="hx-btn hx-create__ok" data-variant="danger" data-busy={busy}>
              <span>{dropping ? t('next.projectDelete.drop') : t('next.projectDelete.yes')}</span>
              {spin}
            </button>
          </div>
        </form>
      </Modal>
    );
  }
  return (
    <Modal
      open={project !== null}
      onClose={onClose}
      title={t('next.projectSettings.title')}
      lead={t('next.projectSettings.lead')}
      boxClass="hx-create"
      initialFocus={pathRef}
    >
      <form onSubmit={(event) => void submit(event)}>
        <div className="hx-modal__body">
          <label className="hx-create__f">
            <span className="hx-create__l">{t('next.newProject.name')}</span>
            <input
              className="hx-create__in"
              type="text"
              autoComplete="off"
              value={name}
              disabled={busy}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <label className="hx-create__f">
            <span className="hx-create__l">{t('next.projectSettings.path')}</span>
            <input
              ref={pathRef}
              className="hx-create__in hx-create__in--path"
              type="text"
              autoComplete="off"
              spellCheck={false}
              placeholder="/home/…"
              value={path}
              disabled={busy}
              onChange={(event) => setPath(event.target.value)}
            />
          </label>
          {offered.length > 0 && (
            <div className="hx-folders" role="list" aria-label={t('next.projectSettings.folders')}>
              {offered.map((folder) => (
                <button
                  key={folder.path}
                  type="button"
                  role="listitem"
                  className="hx-folders__item"
                  disabled={busy}
                  title={folder.path}
                  onClick={() => {
                    setPath(folder.path);
                    setError(null);
                  }}
                >
                  <Icon name="folder" size="sm" />
                  <span className="hx-folders__path">{folder.path}</span>
                  {folder.branch && <span className="hx-folders__branch">{folder.branch}</span>}
                </button>
              ))}
            </div>
          )}
          {errorLine}
          {slowLine}
        </div>
        <div className="hx-modal__foot">
          {onDelete && (
            <button
              type="button"
              className="hx-btn hx-pdel__start"
              data-variant="ghost"
              disabled={busy}
              onClick={() => toStep(STEP.delete)}
            >
              <Icon name="trash-2" size="sm" />
              <span>{t('next.projectSettings.delete')}</span>
            </button>
          )}
          <button type="button" className="hx-btn" data-variant="ghost" onClick={onClose}>
            {t('next.rowMenu.cancel')}
          </button>
          <button type="submit" className="hx-btn hx-create__ok" data-variant="primary" data-busy={busy}>
            <span>{t('next.projectSettings.save')}</span>
            {spin}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default ProjectSettingsDialog;
