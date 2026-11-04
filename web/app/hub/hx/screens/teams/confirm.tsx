// A confirm step inside a drawer, as the board's dialogs sit: it waits for the hub and says why it refused.
import { useClosing } from '../../../org/ui/closing';
import { Btn, DIALOG_MS, Icon } from '../../ui';

export const ConfirmDialog = ({
  open,
  label,
  title,
  lead,
  yes,
  busy,
  error,
  onNo,
  onYes,
}: {
  open: boolean;
  label: string;
  title: string;
  lead: string;
  /** The confirming button, «Убрать». */
  yes: string;
  busy: boolean;
  /** The hub's refusal, shown in the dialog until the next try. */
  error: string | null;
  onNo: () => void;
  onYes: () => void;
}) => {
  const { mounted, closing } = useClosing(open, DIALOG_MS);
  if (!mounted) return null;
  const c = closing ? '1' : undefined;
  return (
    <>
      <div className="md-dlg-scrim" data-closing={c} onClick={busy ? undefined : onNo} />
      <div className="md-dlg" data-closing={c} role="dialog" aria-modal="true" aria-label={label}>
        <span className="md-dlg__ic" data-tone="danger">
          <Icon i="trash-2" />
        </span>
        <h4>{title}</h4>
        <p className="md-dlg__lead">{lead}</p>
        {error && (
          <p className="md-note tm-err" role="alert">
            {error}
          </p>
        )}
        <div className="md-dlg__foot">
          <Btn variant="ghost" size="sm" disabled={busy} onClick={onNo}>
            Отмена
          </Btn>
          <Btn variant="primary" size="sm" className="md-btn-danger" busy={busy} onClick={onYes}>
            {yes}
          </Btn>
        </div>
      </div>
    </>
  );
};
