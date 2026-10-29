import { useRef, useState, type FormEvent } from 'react';
import { t } from '@/next/i18n';
import Modal from '@/next/features/shell/modal';
import { GIVE_UP_MS, SLOW_MS } from '@/next/ui/use-pending-list';

export interface IProjectSettings {
  name: string;
  /** The folder the project's new chats start in. */
  path: string;
}

export interface IProjectSettingsDialogProps {
  /** The project as it is now; null: the dialog is closed. */
  project: IProjectSettings | null;
  onClose: () => void;
  /** Resolves once the server has the change; a rejection is said under the fields. Only what changed is passed. */
  onSave: (change: Partial<IProjectSettings>) => Promise<void>;
}

const STATE = { idle: 'idle', busy: 'busy', slow: 'slow' } as const;
type TState = (typeof STATE)[keyof typeof STATE];

/** «Настройки проекта»: its name and the folder its new chats start in. The chats it has stay where they started. */
const ProjectSettingsDialog = ({ project, onClose, onSave }: IProjectSettingsDialogProps) => {
  const pathRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState('');
  const [path, setPath] = useState('');
  const [state, setState] = useState<TState>(STATE.idle);
  const [error, setError] = useState<string | null>(null);

  // a project opened (or changed on the machine) gives the fields their values again
  const [shown, setShown] = useState<IProjectSettings | null>(null);
  if (project !== shown) {
    setShown(project);
    setName(project?.name ?? '');
    setPath(project?.path ?? '');
    setError(null);
    setState(STATE.idle);
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
    setError(null);
    setState(STATE.busy);
    const slow = setTimeout(() => setState(STATE.slow), SLOW_MS);
    let giveUp: ReturnType<typeof setTimeout> | undefined;
    try {
      await Promise.race([
        onSave(change),
        new Promise((_, reject) => {
          giveUp = setTimeout(() => reject(new Error(t('next.pending.noAnswer'))), GIVE_UP_MS);
        }),
      ]);
      onClose();
    } catch (failure) {
      setError(
        failure instanceof Error && failure.message ? failure.message : t('next.projectSettings.failed'),
      );
      setState(STATE.idle);
    } finally {
      clearTimeout(slow);
      clearTimeout(giveUp);
    }
  };

  const busy = state !== STATE.idle;
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
              className="hx-create__in hx-mono"
              type="text"
              autoComplete="off"
              spellCheck={false}
              placeholder="/home/…"
              value={path}
              disabled={busy}
              onChange={(event) => setPath(event.target.value)}
            />
          </label>
          {error && (
            <p className="hx-create__err" role="alert">
              {error}
            </p>
          )}
          {state === STATE.slow && <p className="hx-create__slow">{t('next.chatRow.slow')}</p>}
        </div>
        <div className="hx-modal__foot">
          <button type="button" className="hx-btn" data-variant="ghost" onClick={onClose}>
            {t('next.rowMenu.cancel')}
          </button>
          <button type="submit" className="hx-btn hx-create__ok" data-variant="primary" data-busy={busy}>
            <span>{t('next.projectSettings.save')}</span>
            <span className="hx-gate__spin" aria-hidden="true" />
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default ProjectSettingsDialog;
