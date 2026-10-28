// A file that is not text, in the pane: a zip's contents with each file to save, a picture or a PDF as it is, any other
// file with its name and the way to save it. Used for what the agent handed over and for any such file the pane opens.
import { useEffect, useState } from 'react';
import Icon from '@/next/ui/icon';
import Swap from '@/next/ui/swap';
import LoadFailed from '@/next/ui/load-failed';
import { t } from '@/next/i18n';
import { bytes } from '@/next/features/pane/machine/charts';
import type { MachineId } from '@/next/protocol/v2';
import { DELIVERED_VIEW, type DeliveredView } from '@/next/features/pane/delivered-kind';
import { downloadUrl, listZip, previewUrl, zipEntryUrl, type IZipList } from '@/next/features/pane/delivered';

export interface IDeliveredViewProps {
  machine: MachineId;
  /** Absolute path of the file. */
  path: string;
  view: DeliveredView;
}

const nameOf = (path: string) => path.slice(path.lastIndexOf('/') + 1);

const Save = ({ href }: { href: string }) => (
  <a className="hx-deliv__save" href={href} download aria-label={t('next.delivered.save')}>
    <Icon name="download" size="sm" />
  </a>
);

const ZipContents = ({ machine, path }: { machine: MachineId; path: string }) => {
  const [list, setList] = useState<IZipList | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const abort = new AbortController();
    listZip(machine, path, abort.signal).then(
      (next) => {
        if (abort.signal.aborted) return;
        setList(next);
        setError(null);
      },
      (failure: Error) => {
        if (!abort.signal.aborted) setError(failure.message);
      },
    );
    return () => abort.abort();
  }, [machine, path, attempt]);
  if (error !== null)
    return (
      <LoadFailed
        title={t('next.delivered.zipFailed')}
        reason={error}
        onRetry={() => {
          setError(null);
          setAttempt((n) => n + 1);
        }}
      />
    );
  const files = list?.entries.filter((e) => !e.directory) ?? [];
  return (
    <div
      role={list === null ? 'status' : undefined}
      aria-label={list === null ? t('next.delivered.loading') : undefined}
    >
      <Swap
        ready={list !== null}
        skeleton={
          <>
            <span className="hx-sk hx-plan__sk" />
            <span className="hx-sk hx-plan__sk" />
          </>
        }
      >
        {files.map((entry) => (
          <div key={entry.name} className="hx-tv__row" data-static="true">
            <Icon name="file-text" size="sm" />
            <span className="hx-tv__main">
              <span className="hx-tv__name">{entry.name}</span>
            </span>
            <span className="hx-tv__muted">{bytes(entry.size)}</span>
            <Save href={zipEntryUrl(machine, path, entry.name)} />
          </div>
        ))}
        {list !== null && files.length === 0 && (
          <div className="hx-tv__empty">{t('next.delivered.zipEmpty')}</div>
        )}
        {list?.truncated && <div className="hx-tv__note">{t('next.files.truncated')}</div>}
      </Swap>
    </div>
  );
};

const DeliveredView = ({ machine, path, view }: IDeliveredViewProps) => {
  const name = nameOf(path);
  return (
    <div className="hx-tv hx-deliv" data-tool="disk" data-view={view}>
      <div className="hx-tv__sum">
        <span className="hx-tv__big hx-deliv__name">{name}</span>
        <Save href={downloadUrl(machine, path)} />
      </div>
      {view === DELIVERED_VIEW.zip && <ZipContents machine={machine} path={path} />}
      {view === DELIVERED_VIEW.image && (
        <img className="hx-deliv__image" src={previewUrl(machine, path)} alt={name} />
      )}
      {view === DELIVERED_VIEW.pdf && (
        <iframe className="hx-deliv__pdf" src={previewUrl(machine, path)} title={name} />
      )}
      {view === DELIVERED_VIEW.save && <div className="hx-tv__note">{t('next.delivered.saveOnly')}</div>}
    </div>
  );
};

export default DeliveredView;
