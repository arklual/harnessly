// The tools of the base package the person skipped («Пропустить» on the start page's card), with «Вернуть»: the
// machine installs it again. Nothing is drawn while nothing is skipped.
import { Group, SetButton } from '@/next/features/settings/rows';
import { BASE_STATUS, type IBaseItem } from '@/next/features/setup/api';
import { t } from '@/next/i18n';

export interface IBaseSkippedProps {
  items: readonly IBaseItem[];
  /** The item being brought back now. */
  busy: string | null;
  onBack: (id: string) => void;
}

const BaseSkipped = ({ items, busy, onBack }: IBaseSkippedProps) => {
  const skipped = items.filter((it) => it.status === BASE_STATUS.skipped);
  if (skipped.length === 0) return null;
  return (
    <Group title={t('next.settings.base.title')} foot={t('next.settings.base.foot')}>
      {skipped.map((it) => (
        <div className="hx-set__row hx-check" key={it.id}>
          <div className="hx-set__row-text">
            <span className="hx-set__row-label">{it.title}</span>
            <span className="hx-set__row-hint">{t('next.settings.base.without', { what: it.without })}</span>
          </div>
          <div className="hx-check__acts">
            <SetButton
              tone="ghost"
              busy={busy === it.id}
              disabled={busy !== null}
              onClick={() => onBack(it.id)}
            >
              {t('next.settings.base.back')}
            </SetButton>
          </div>
        </div>
      ))}
    </Group>
  );
};

export default BaseSkipped;
