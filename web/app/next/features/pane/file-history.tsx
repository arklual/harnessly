// The earlier states of a file the pane edits, to go back to. A restore keeps what it replaces, so it can be undone the same way.
import { useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { bytes } from '@/next/features/pane/machine/charts';
import { fileVersions, restoreFileVersion, type IFileVersion } from '@/next/features/pane/sheet/file-bytes';
import type { MachineId } from '@/next/protocol/v2';

interface IFileHistoryProps {
  machine: MachineId;
  path: string;
  /** The revision the view shows now: that state is not offered. */
  current: string | null;
  onRestored: () => void;
}

const when = (at: number) =>
  new Date(at).toLocaleString(undefined, {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });

const FileHistory = ({ machine, path, current, onRestored }: IFileHistoryProps) => {
  const [open, setOpen] = useState(false);
  // null: not read yet, a skeleton stands for it.
  const [versions, setVersions] = useState<readonly IFileVersion[] | null>(null);
  const [failed, setFailed] = useState(false);

  const onOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) return;
    setVersions(null);
    setFailed(false);
    fileVersions(machine, path).then(setVersions, () => setFailed(true));
  };

  const restore = (id: string) => {
    restoreFileVersion(machine, path, id).then(
      () => {
        setOpen(false);
        onRestored();
      },
      () => setFailed(true),
    );
  };

  const earlier = versions?.filter((version) => version.id !== current) ?? [];
  return (
    <Popover
      open={open}
      onOpenChange={onOpenChange}
      label={t('next.fileHistory.title')}
      hint={t('next.fileHistory.title')}
      className="hx-history"
      side="bottom"
      align="end"
      trigger={
        <button type="button" className="hx-history__open" aria-label={t('next.fileHistory.title')}>
          <Icon name="history" size="sm" />
        </button>
      }
    >
      {failed && <div className="hx-history__note">{t('next.fileHistory.failed')}</div>}
      {!failed && versions === null && <span className="hx-sk hx-history__sk" />}
      {!failed && versions !== null && earlier.length === 0 && (
        <div className="hx-history__note">{t('next.fileHistory.empty')}</div>
      )}
      {!failed &&
        earlier.map((version) => (
          <button
            key={version.id}
            type="button"
            className="hx-history__row"
            onClick={() => restore(version.id)}
          >
            <span className="hx-history__when">{when(version.at)}</span>
            <span className="hx-history__size">{bytes(version.size)}</span>
            <Icon name="rotate-ccw" size="sm" />
          </button>
        ))}
    </Popover>
  );
};

export default FileHistory;
