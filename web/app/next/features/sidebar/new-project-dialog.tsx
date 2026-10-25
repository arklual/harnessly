import { useRef, useState, type FormEvent } from 'react';
import { t } from '@/next/i18n';
import Modal from '@/next/features/shell/modal';
import { GIVE_UP_MS, SLOW_MS } from '@/next/ui/use-pending-list';

export interface INewProjectDialogProps {
  open: boolean;
  onClose: () => void;
  /** Resolves once the server has the project; a rejection is said under the field. */
  onCreate: (name: string) => Promise<void>;
}

const STATE = { idle: 'idle', busy: 'busy', slow: 'slow' } as const;
type TState = (typeof STATE)[keyof typeof STATE];

/** «Новый проект»: a name alone. The button spins until the server answers; it does not close before. */
const NewProjectDialog = ({ open, onClose, onCreate }: INewProjectDialogProps) => {
  const nameRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState('');
  const [state, setState] = useState<TState>(STATE.idle);
  const [error, setError] = useState<string | null>(null);

  const close = () => {
    setName('');
    setError(null);
    setState(STATE.idle);
    onClose();
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setError(t('next.newProject.nameMissing'));
      return;
    }
    setError(null);
    setState(STATE.busy);
    const slow = setTimeout(() => setState(STATE.slow), SLOW_MS);
    let giveUp: ReturnType<typeof setTimeout> | undefined;
    try {
      await Promise.race([
        onCreate(trimmed),
        new Promise((_, reject) => {
          giveUp = setTimeout(() => reject(new Error(t('next.pending.noAnswer'))), GIVE_UP_MS);
        }),
      ]);
      close();
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : t('next.newProject.failed'));
      setState(STATE.idle);
    } finally {
      clearTimeout(slow);
      clearTimeout(giveUp);
    }
  };

  const busy = state !== STATE.idle;
  return (
    <Modal
      open={open}
      onClose={close}
      title={t('next.newProject.title')}
      lead={t('next.newProject.lead')}
      boxClass="hx-create"
      initialFocus={nameRef}
    >
      <form onSubmit={(event) => void submit(event)}>
        <div className="hx-modal__body">
          <label className="hx-create__f">
            <span className="hx-create__l">{t('next.newProject.name')}</span>
            <input
              ref={nameRef}
              className="hx-create__in"
              type="text"
              autoComplete="off"
              placeholder={t('next.newProject.namePlaceholder')}
              value={name}
              disabled={busy}
              onChange={(event) => setName(event.target.value)}
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
          <button type="button" className="hx-btn" data-variant="ghost" onClick={close}>
            {t('next.rowMenu.cancel')}
          </button>
          <button type="submit" className="hx-btn hx-create__ok" data-variant="primary" data-busy={busy}>
            <span>{t('next.newProject.create')}</span>
            <span className="hx-gate__spin" aria-hidden="true" />
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default NewProjectDialog;
