import { nanoid } from 'nanoid';
import { toast } from 'sonner';
import { useSyncExternalStore } from 'react';
import { applyOrganizerOps } from '@/hooks/use-hl-organizer';
import {
  nextProjectColor,
  type IOrganizerProject,
  type TConversationRef,
  type TOrganizerOp,
  type TProjectColor,
} from '@/lib/hl-organizer';
import { t } from '@/lib/i18n';

/**
 * Every organizer change the panel, the rail, the palette and the header make. Pessimistic:
 * resolves to true once the server has the change; on failure a toast says what did not happen.
 */
const run = async (ops: TOrganizerOp[], keys: readonly string[], failure: string): Promise<boolean> => {
  const result = await applyOrganizerOps(ops, keys);
  if (!result.ok) toast.error(`${failure}. ${result.error}`);
  return result.ok;
};

export const newProjectId = () => `prj-${nanoid(10)}`;

export const organizerActions = {
  pin: (ref: TConversationRef) => run([{ op: 'pin', ref }], [ref], t('sidebar.organizerActions.pinFailed')),
  unpin: (ref: TConversationRef) =>
    run([{ op: 'unpin', ref }], [ref], t('sidebar.organizerActions.unpinFailed')),
  assign: (refs: TConversationRef[], projectId: string | null) =>
    run(
      [{ op: 'assign', refs, projectId }],
      refs,
      projectId ? t('sidebar.organizerActions.assignFailed') : t('sidebar.organizerActions.unassignFailed'),
    ),
  forget: (refs: TConversationRef[]) =>
    run([{ op: 'forget', refs }], refs, t('sidebar.organizerActions.forgetFailed')),
  /** One request for «New project…» from a row menu: the project and its first conversations arrive together. */
  createProject: (name: string, projects: readonly IOrganizerProject[], refs: TConversationRef[] = []) => {
    const id = newProjectId();
    const ops: TOrganizerOp[] = [{ op: 'project.create', id, name, color: nextProjectColor(projects) }];
    if (refs.length) ops.push({ op: 'assign', refs, projectId: id });
    return run(ops, [id, ...refs], t('sidebar.organizerActions.createFailed'));
  },
  renameProject: (id: string, name: string) =>
    run([{ op: 'project.update', id, name }], [id], t('sidebar.organizerActions.renameFailed')),
  recolorProject: (id: string, color: TProjectColor) =>
    run([{ op: 'project.update', id, color }], [id], t('sidebar.organizerActions.recolorFailed')),
  deleteProject: (id: string) =>
    run([{ op: 'project.delete', id }], [id], t('sidebar.organizerActions.deleteFailed')),
  moveProject: (projects: readonly IOrganizerProject[], id: string, delta: -1 | 1) => {
    const ids = projects.map((p) => p.id);
    const from = ids.indexOf(id);
    const to = from + delta;
    if (from < 0 || to < 0 || to >= ids.length) return Promise.resolve(false);
    [ids[from], ids[to]] = [ids[to], ids[from]];
    return run([{ op: 'project.reorder', ids }], [id], t('sidebar.organizerActions.moveProjectFailed'));
  },
  movePin: (pins: readonly TConversationRef[], ref: TConversationRef, delta: -1 | 1) => {
    const from = pins.indexOf(ref);
    const to = from + delta;
    if (from < 0 || to < 0 || to >= pins.length) return Promise.resolve(false);
    return run([{ op: 'pin', ref, index: to }], [ref], t('sidebar.organizerActions.movePinFailed'));
  },
} as const;

export const PROJECT_COLOR_LABELS: Record<TProjectColor, string> = {
  blue: t('sidebar.organizerActions.colorBlue'),
  green: t('sidebar.organizerActions.colorGreen'),
  amber: t('sidebar.organizerActions.colorAmber'),
  rose: t('sidebar.organizerActions.colorRose'),
  purple: t('sidebar.organizerActions.colorPurple'),
  teal: t('sidebar.organizerActions.colorTeal'),
};

const detectMac = () => /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent);
const subscribeNever = () => () => {};

/** `⌘` on Apple devices, `Ctrl+` elsewhere; the server renders `⌘` and the client corrects it after hydration. */
export const useModKey = () =>
  useSyncExternalStore(
    subscribeNever,
    () => (detectMac() ? '⌘' : 'Ctrl+'),
    () => '⌘',
  );
