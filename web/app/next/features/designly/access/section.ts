// What every block of the share dialog gets: the project, the gateway's state, the dialog's one running action.
import type { IProjectAccess } from '@/next/features/designly/access/api';
import type { IAction } from '@/next/features/designly/access/use-action';

export interface ISectionProps {
  project: string;
  access: IProjectAccess;
  action: IAction;
  /** The gateway's fresh state: it replaces the one on screen. */
  onNext: (next: IProjectAccess) => void;
}

/** Runs a change that answers with the project's fresh state and puts that on screen once it has arrived. */
export const change = (
  { action, onNext }: Pick<ISectionProps, 'action' | 'onNext'>,
  key: string,
  call: () => Promise<IProjectAccess>,
) => void action.run(key, async () => onNext(await call()));

/** The person has the project's management: an owner or a manager (not an editor who may share where it is allowed). */
export const isManager = (a: IProjectAccess): boolean => a.role === 'owner' || a.role === 'manage';
