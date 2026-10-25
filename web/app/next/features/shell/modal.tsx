import { Dialog } from '@base-ui/react/dialog';
import type { ReactNode, RefObject } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { usePortalContainer } from '@/next/ui/portal';

export interface IModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  lead?: string;
  /** The box's own class: its width and parts (hx-create, hx-resume). */
  boxClass: string;
  initialFocus?: RefObject<HTMLElement | null>;
  /** Buttons of the head before the close (a «…» menu). */
  actions?: ReactNode;
  children: ReactNode;
}

/** A dialog of the design (.hx-modal): a dimmed page, a box that rises in and sinks out, a sheet from below on the phone. */
const Modal = ({ open, onClose, title, lead, boxClass, initialFocus, actions, children }: IModalProps) => {
  const container = usePortalContainer();
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <Dialog.Portal container={container}>
        <div className="hx-modal">
          <Dialog.Backdrop className="hx-modal__scrim" />
          <Dialog.Popup className={`hx-modal__box ${boxClass}`} initialFocus={initialFocus}>
            <div className="hx-modal__head">
              <div>
                <Dialog.Title className="hx-modal__title">{title}</Dialog.Title>
                {lead && <Dialog.Description className="hx-modal__lead">{lead}</Dialog.Description>}
              </div>
              {actions}
              <Dialog.Close className="hx-head-btn" aria-label={t('next.modal.close')}>
                <Icon name="x" />
              </Dialog.Close>
            </div>
            {children}
          </Dialog.Popup>
        </div>
      </Dialog.Portal>
    </Dialog.Root>
  );
};

export default Modal;
