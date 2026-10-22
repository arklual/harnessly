import { useId, useState, type FormEvent } from 'react';
import { BUTTON_SIZE, BUTTON_VARIANT, DIALOG_SIZE, TwButton, TwDialog } from '@/components/agents/ui';
import { Note } from '@/components/agents/design/library/library-parts';
import { currentMachineId } from '@/lib/app-path';
import { newTaskMachine } from '@/lib/new-task-machine';
import { DesignRequestError, createDesignProject } from '@/lib/hl-design-library-client';
import { LIBRARY_LIMITS } from '@/lib/hl-design-library/types';
import { t } from '@/lib/i18n';

const BRIEF_MAX = 4_000;

interface INewProjectDialogProps {
  open: boolean;
  onClose: () => void;
  /** The project exists and its chat runs. */
  onCreated: (created: { id: string; tabId: string; machineId: string | undefined }) => void;
  /** The project exists but its chat did not start: the person can still open it. */
  onCreatedWithoutChat: (id: string) => void;
}

const createdId = (error: unknown): string | null => {
  const body = error instanceof DesignRequestError ? error.body : null;
  return typeof body === 'object' && body !== null && 'id' in body && typeof body.id === 'string'
    ? body.id
    : null;
};

/** "New project": a name and what to draw; the runtime copies the template and opens a chat that starts on the brief. */
export const NewProjectDialog = ({
  open,
  onClose,
  onCreated,
  onCreatedWithoutChat,
}: INewProjectDialogProps) => {
  const ids = { title: useId(), brief: useId() };
  const [title, setTitle] = useState('');
  const [brief, setBrief] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<{ message: string; id: string | null } | null>(null);
  const ready = title.trim().length > 0 && brief.trim().length > 0;

  const submit = async (event?: FormEvent) => {
    event?.preventDefault();
    if (!ready || pending) return;
    setPending(true);
    setError(null);
    try {
      // A new project starts on the machine picked for new tasks.
      const machine = newTaskMachine() ?? currentMachineId();
      const created = { ...(await createDesignProject(machine, { title, brief })), machineId: machine };
      setTitle('');
      setBrief('');
      onCreated(created);
    } catch (failure) {
      setError({
        message: failure instanceof Error ? failure.message : t('design.newProjectDialog.createFailed'),
        id: createdId(failure),
      });
    } finally {
      setPending(false);
    }
  };

  return (
    <TwDialog
      open={open}
      onClose={onClose}
      title={t('design.newProjectDialog.title')}
      description={t('design.newProjectDialog.description')}
      size={DIALOG_SIZE.md}
      dismissible={!pending}
      footer={
        <>
          {error?.id && (
            <TwButton
              size={BUTTON_SIZE.md}
              variant={BUTTON_VARIANT.ghost}
              onClick={() => onCreatedWithoutChat(error.id as string)}
            >
              {t('design.newProjectDialog.openProject')}
            </TwButton>
          )}
          <TwButton size={BUTTON_SIZE.md} variant={BUTTON_VARIANT.ghost} disabled={pending} onClick={onClose}>
            {t('design.newProjectDialog.cancel')}
          </TwButton>
          <TwButton
            size={BUTTON_SIZE.md}
            variant={BUTTON_VARIANT.primary}
            pending={pending}
            disabled={!ready}
            onClick={() => void submit()}
          >
            {t('design.newProjectDialog.create')}
          </TwButton>
        </>
      }
    >
      <form className="tw-dhome-form" onSubmit={(event) => void submit(event)}>
        <label className="tw-dhome-field" htmlFor={ids.title}>
          <span>{t('design.newProjectDialog.name')}</span>
          <input
            id={ids.title}
            className="tw-design-input"
            value={title}
            maxLength={LIBRARY_LIMITS.title}
            placeholder={t('design.newProjectDialog.namePlaceholder')}
            autoComplete="off"
            disabled={pending}
            onChange={(event) => setTitle(event.target.value)}
          />
        </label>
        <label className="tw-dhome-field" htmlFor={ids.brief}>
          <span>{t('design.newProjectDialog.brief')}</span>
          <textarea
            id={ids.brief}
            className="tw-design-input tw-design-textarea"
            rows={5}
            value={brief}
            maxLength={BRIEF_MAX}
            placeholder={t('design.newProjectDialog.briefPlaceholder')}
            disabled={pending}
            onChange={(event) => setBrief(event.target.value)}
          />
        </label>
        {error && <Note error>{error.message}</Note>}
        <button type="submit" hidden aria-hidden="true" tabIndex={-1} />
      </form>
    </TwDialog>
  );
};
