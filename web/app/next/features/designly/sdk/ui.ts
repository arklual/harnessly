// The window the editor draws with (HRN-808): a method of its SDK (hl.ui.modal), as an app in the host's page has it,
// not the host's component reached across its modules. The host draws it; the editor gives its words and its content.
import type { ReactNode } from 'react';
import { sinopiaUi } from '@/next/features/app-views/sinopia/host';
import type { IModalProps } from '@/next/features/shell/modal';

export type { IModalProps };

export const Modal = (props: IModalProps): ReactNode => sinopiaUi().ui.modal(props);
