// The trash: the person's own deleted projects, to put back or to erase for good. The machine keeps the files
// whole in its trash folder until they are erased here.
import { useEffect, useState } from 'react';
import type { ITrashItem } from '@/next/features/designly/api';
import { Modal } from '@/next/features/designly/sdk/ui';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { usePresence } from '@/next/ui/presence';
import { formatAgoFull } from '@/next/i18n/format';
import { t } from '@/next/i18n';

const KIND_ICON: Record<ITrashItem['kind'], string> = {
  site: 'app-window',
  slides: 'presentation',
  video: 'clapperboard',
};
const EXIT_MS = 240;

interface ITrashProps {
  items: readonly ITrashItem[] | null;
  now: number;
  load: () => Promise<void>;
  onBack: () => void;
  onRestore: (id: string) => Promise<void>;
  onPurge: (id: string) => Promise<void>;
}

const TrashView = ({ items, now, load, onBack, onRestore, onPurge }: ITrashProps) => {
  const [failed, setFailed] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [purging, setPurging] = useState<ITrashItem | null>(null);
  const shown = usePresence(purging, EXIT_MS).value;
  useEffect(() => {
    load().then(
      () => setFailed(null),
      (e: Error) => setFailed(e.message),
    );
  }, [load]);
  const act = (id: string, run: () => Promise<void>, after?: () => void) => {
    setBusy(id);
    run()
      .then(after, () => undefined)
      .finally(() => setBusy(null));
  };
  return (
    <div className="hx-dl__trash">
      <nav className="hx-dl__crumbs" aria-label={t('next.designly.folderPath')}>
        <button type="button" className="hx-dl__crumb" onClick={onBack}>
          <Icon name="chevron-left" size="sm" />
          {t('next.designly.library')}
        </button>
        <span className="hx-dl__crumb-wrap">
          <Icon name="chevron-right" size="sm" />
          <span className="hx-dl__crumb" aria-current="page">
            {t('next.designly.trash')}
          </span>
        </span>
      </nav>
      <p className="hx-dl__trash-lead">{t('next.designly.trashLead')}</p>
      {failed !== null && items === null && (
        <LoadFailed
          size={LOAD_FAILED_SIZE.page}
          title={t('next.designly.trashFailed')}
          reason={failed}
          onRetry={() => void load()}
        />
      )}
      {items !== null && items.length === 0 && (
        <div className="hx-dl__empty">{t('next.designly.trashEmpty')}</div>
      )}
      {items !== null && items.length > 0 && (
        <ul className="hx-dl__trash-list">
          {items.map((item) => (
            <li key={item.id} className="hx-dl__trash-row">
              <Icon name={KIND_ICON[item.kind]} size="sm" />
              <span className="hx-dl__trash-title">{item.title}</span>
              <span className="hx-dl__trash-when">{formatAgoFull(item.deletedAt, now)}</span>
              <Button
                variant="ghost"
                disabled={busy === item.id}
                onClick={() => act(item.id, () => onRestore(item.id))}
              >
                <Icon name="rotate-ccw" size="sm" />
                {t('next.designly.restore')}
              </Button>
              <Button variant="ghost" disabled={busy === item.id} onClick={() => setPurging(item)}>
                {t('next.designly.purge')}
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Modal
        open={purging !== null}
        onClose={() => setPurging(null)}
        title={t('next.designly.purgeTitle', { title: shown?.title ?? '' })}
        lead={t('next.designly.purgeLead')}
        boxClass="hx-leave"
      >
        <div className="hx-modal__foot">
          <Button variant="ghost" onClick={() => setPurging(null)}>
            {t('next.designly.deleteCancel')}
          </Button>
          <Button
            variant="danger"
            busy={purging !== null && busy === purging.id}
            onClick={() =>
              purging &&
              act(
                purging.id,
                () => onPurge(purging.id),
                () => setPurging(null),
              )
            }
          >
            {t('next.designly.purgeConfirm')}
          </Button>
        </div>
      </Modal>
    </div>
  );
};

export default TrashView;
