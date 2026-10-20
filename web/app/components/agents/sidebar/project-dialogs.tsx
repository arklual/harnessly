import { useId, useRef, useState, type FormEvent } from 'react';
import { BUTTON_VARIANT, CONFIRM_TONE, TwButton, TwConfirm, TwDialog } from '@/components/agents/ui';
import { organizerActions } from '@/components/agents/sidebar/organizer-actions';
import {
  ORGANIZER_LIMITS,
  normalizeProjectName,
  type IOrganizerProject,
  type TConversationRef,
} from '@/lib/hl-organizer';
import { t } from '@/lib/i18n';

export type TProjectDialog =
  | { kind: 'create'; refs: TConversationRef[] }
  | { kind: 'rename'; project: IOrganizerProject }
  | { kind: 'delete'; project: IOrganizerProject };

interface IProjectDialogsProps {
  dialog: TProjectDialog | null;
  projects: readonly IOrganizerProject[];
  onClose: () => void;
}

const NameForm = ({
  dialog,
  projects,
  onClose,
}: {
  dialog: Exclude<TProjectDialog, { kind: 'delete' }>;
  projects: readonly IOrganizerProject[];
  onClose: () => void;
}) => {
  const inputRef = useRef<HTMLInputElement>(null);
  const inputId = useId();
  const formId = useId();
  const [name, setName] = useState(dialog.kind === 'rename' ? dialog.project.name : '');
  const [pending, setPending] = useState(false);
  const valid = normalizeProjectName(name) !== null;
  const creating = dialog.kind === 'create';

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const clean = normalizeProjectName(name);
    if (!clean || pending) return;
    setPending(true);
    const ok = creating
      ? await organizerActions.createProject(clean, projects, dialog.refs)
      : await organizerActions.renameProject(dialog.project.id, clean);
    setPending(false);
    if (ok) onClose();
  };

  return (
    <TwDialog
      open
      onClose={onClose}
      dismissible={!pending}
      title={creating ? t('sidebar.projectDialogs.newProject') : t('sidebar.projectDialogs.renameProject')}
      description={creating && dialog.refs.length ? t('sidebar.projectDialogs.movesIntoProject') : undefined}
      initialFocusRef={inputRef}
      footer={
        <>
          <TwButton variant={BUTTON_VARIANT.ghost} onClick={onClose} aria-disabled={pending || undefined}>
            {t('sidebar.projectDialogs.cancel')}
          </TwButton>
          <TwButton
            variant={BUTTON_VARIANT.primary}
            type="submit"
            form={formId}
            pending={pending}
            disabled={!valid}
          >
            {creating ? t('sidebar.projectDialogs.create') : t('sidebar.projectDialogs.save')}
          </TwButton>
        </>
      }
    >
      <form id={formId} className="tw-sform" onSubmit={(event) => void submit(event)}>
        <label htmlFor={inputId} className="tw-sform-label">
          {t('sidebar.projectDialogs.name')}
        </label>
        <input
          ref={inputRef}
          id={inputId}
          className="tw-sform-input"
          value={name}
          maxLength={ORGANIZER_LIMITS.nameLength}
          placeholder={t('sidebar.projectDialogs.namePlaceholder')}
          autoComplete="off"
          disabled={pending}
          onChange={(event) => setName(event.target.value)}
        />
      </form>
    </TwDialog>
  );
};

const DeleteConfirm = ({ project, onClose }: { project: IOrganizerProject; onClose: () => void }) => {
  const [pending, setPending] = useState(false);
  return (
    <TwConfirm
      open
      tone={CONFIRM_TONE.danger}
      title={t('sidebar.projectDialogs.deleteTitle', { name: project.name })}
      description={t('sidebar.projectDialogs.deleteDescription')}
      confirmLabel={t('sidebar.projectDialogs.delete')}
      pending={pending}
      onCancel={onClose}
      onConfirm={async () => {
        setPending(true);
        const ok = await organizerActions.deleteProject(project.id);
        setPending(false);
        if (ok) onClose();
      }}
    />
  );
};

/** Name and delete dialogs of projects; one open at a time. */
const ProjectDialogs = ({ dialog, projects, onClose }: IProjectDialogsProps) => {
  if (!dialog) return null;
  if (dialog.kind === 'delete') return <DeleteConfirm project={dialog.project} onClose={onClose} />;
  return <NameForm dialog={dialog} projects={projects} onClose={onClose} />;
};

export default ProjectDialogs;
