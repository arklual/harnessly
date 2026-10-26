// «Как вас зовут?»: asked once when a guest first comments (and again from the menu to change it). The dialog is the
// shell's own Modal, the same box as «Новый проект».
import { useRef, useState, type FormEvent } from 'react';
import Modal from '@/next/features/shell/modal';
import { t } from '@/next/i18n';

export const NAME_MAX = 60;

export interface IGuestNameDialogProps {
  open: boolean;
  /** The name kept now; the field starts with it. */
  name: string;
  onClose: () => void;
  onSave: (name: string) => void;
}

const GuestNameDialog = ({ open, name, onClose, onSave }: IGuestNameDialogProps) => {
  const ref = useRef<HTMLInputElement>(null);
  const [value, setValue] = useState(name);
  const trimmed = value.trim();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (trimmed) onSave(trimmed);
  };
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('next.guest.nameTitle')}
      lead={t('next.guest.nameLead')}
      boxClass="hx-create"
      initialFocus={ref}
    >
      <form onSubmit={submit}>
        <div className="hx-modal__body">
          <label className="hx-create__f">
            <input
              ref={ref}
              className="hx-create__in"
              type="text"
              autoComplete="name"
              maxLength={NAME_MAX}
              placeholder={t('next.guest.name')}
              aria-label={t('next.guest.name')}
              value={value}
              onChange={(event) => setValue(event.target.value)}
            />
          </label>
        </div>
        <div className="hx-modal__foot">
          <button type="button" className="hx-btn" data-variant="ghost" onClick={onClose}>
            {t('next.rowMenu.cancel')}
          </button>
          <button type="submit" className="hx-btn" data-variant="primary" disabled={!trimmed}>
            {t('next.guest.nameGo')}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default GuestNameDialog;
