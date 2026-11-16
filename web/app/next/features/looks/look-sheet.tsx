// «Оформление…» of a folder or a chat (the folder's button in the sidebar, the chat's menu): the same editor as
// Settings → Оформление, for that one place, in a sheet over the chat so the change is seen behind it.
import { openLookEditor, useLookEditing } from '@/next/features/looks/store';
import LookPanel from '@/next/features/looks/look-panel';
import { t } from '@/next/i18n';
import Sheet from '@/next/ui/sheet';
import Icon from '@/next/ui/icon';

const LookSheet = () => {
  const scope = useLookEditing();
  return (
    <Sheet
      open={scope !== null}
      onClose={() => openLookEditor(null)}
      label={t('next.looks.sheet')}
      className="hx-lk-sheet"
    >
      {scope && (
        <div className="hx-lk-sheet__in">
          <div className="hx-lk-sheet__head">
            <strong>{t(`next.looks.sheet.${scope.k}`)}</strong>
            <button
              type="button"
              className="hx-head-btn"
              aria-label={t('next.looks.close')}
              onClick={() => openLookEditor(null)}
            >
              <Icon name="x" size="sm" />
            </button>
          </div>
          <LookPanel scope={scope} />
        </div>
      )}
    </Sheet>
  );
};

export default LookSheet;
