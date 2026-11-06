// A file that is not text, in the pane: a zip's contents with each file to save, a picture or a PDF as it is, a video or
// a sound in the player, any other file with its name and the way to save it. Used for what the agent handed over and for any such file the pane opens.
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import Swap from '@/next/ui/swap';
import LoadFailed from '@/next/ui/load-failed';
import { t } from '@/next/i18n';
import { bytes } from '@/next/features/pane/machine/charts';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import type { IFileSelection } from '@/next/features/pane/file-selection';
import { settleFilePlace, useFilePlace } from '@/next/features/pane/file-place';
import { useFileChanges } from '@/next/features/pane/use-file-changes';
import { reportFileSelection } from '@/next/features/pane/report-selection';
import { DELIVERED_VIEW, type DeliveredView } from '@/next/features/pane/delivered-kind';
import ImageStage from '@/next/features/pane/image-stage';
import LazyView from '@/next/ui/lazy-view';
import MediaPlayer from '@/next/features/pane/media/media-player';
import { SHEET_FILE } from '@/next/features/pane/sheet/sheet-api';
import { MEDIA_KIND } from '@/next/features/pane/media/media-model';
import { listZip, previewUrl, zipEntryUrl, type IZipList } from '@/next/features/pane/delivered';
import { downloadHref } from '@/next/features/pane/download';
import PaneHead, { Crumbs, type ICrumb } from '@/next/features/pane/pane-head';

export interface IDeliveredViewProps {
  /** The chat the pane is beside: it hears what is selected in the file. */
  conv: ConvId;
  machine: MachineId;
  /** Absolute path of the file. */
  path: string;
  view: DeliveredView;
  /** The way to the file, for its head row; a crumb opens its folder. */
  crumbs: readonly ICrumb[];
  onCrumb: (path: string) => void;
  /** The file's controls (save, share, «…»), on the right of the crumbs. */
  tools: ReactNode;
}

const loadPdfView = () => import('@/next/features/pane/pdf/pdf-view');
const loadSheetView = () => import('@/next/features/pane/sheet/sheet-view');
const loadDocxView = () => import('@/next/features/pane/docx/docx-view');
const loadPptxView = () => import('@/next/features/pane/pptx/pptx-view');
const loadFontView = () => import('@/next/features/pane/font-view');

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

const DeliveredView = ({ conv, machine, path, view, crumbs, onCrumb, tools }: IDeliveredViewProps) => {
  const name = nameOf(path);
  const onSelection = useCallback(
    (selection: IFileSelection | null) => reportFileSelection(machine, conv, path, selection),
    [machine, conv, path],
  );
  const place = useFilePlace(path);
  const onPlaced = useCallback(() => settleFilePlace(path), [path]);
  // Closed, the file holds no selection any more.
  useEffect(() => () => reportFileSelection(machine, conv, path, null), [machine, conv, path]);
  const toFile = { onSelection, place, onPlaced };
  // Saved through the chat: a file in its folder is saved wherever the folder lies.
  const saveHref = downloadHref(machine, conv, [path]);
  // A spreadsheet looks after its own file; the rest are read again from an address that says the file changed.
  const reads =
    view === DELIVERED_VIEW.pdf ||
    view === DELIVERED_VIEW.image ||
    view === DELIVERED_VIEW.video ||
    view === DELIVERED_VIEW.audio;
  const changes = useFileChanges(machine, path, reads);
  const src = changes === 0 ? previewUrl(machine, path) : `${previewUrl(machine, path)}&v=${changes}`;
  return (
    <div className="hx-tv hx-deliv" data-tool="disk" data-view={view}>
      <PaneHead lead={<Crumbs items={crumbs} onPick={onCrumb} />} tools={tools}>
        {view === DELIVERED_VIEW.zip && <ZipContents machine={machine} path={path} />}
        {view === DELIVERED_VIEW.image && (
          <ImageStage key={path} src={src} name={name} onSelection={onSelection} />
        )}
        {view === DELIVERED_VIEW.pdf && (
          <LazyView
            load={loadPdfView}
            props={{ src, name, saveHref, ...toFile }}
            fallback={<span className="hx-sk hx-pdf__wait-sheet" />}
            title={t('next.pdf.broken')}
          />
        )}
        {view === DELIVERED_VIEW.video && (
          <MediaPlayer
            key={path}
            src={src}
            kind={MEDIA_KIND.video}
            name={name}
            path={path}
            saveHref={saveHref}
            {...toFile}
          />
        )}
        {view === DELIVERED_VIEW.audio && (
          <MediaPlayer
            key={path}
            src={src}
            kind={MEDIA_KIND.audio}
            name={name}
            path={path}
            saveHref={saveHref}
            {...toFile}
          />
        )}
        {view === DELIVERED_VIEW.sheet && (
          <LazyView
            load={loadSheetView}
            props={{
              machine,
              path,
              src: previewUrl(machine, path),
              name,
              kind: /\.(?:csv|tsv)$/i.test(path) ? SHEET_FILE.csv : SHEET_FILE.xlsx,
              saveHref,
              ...toFile,
            }}
            fallback={<span className="hx-sk hx-pdf__wait-sheet" />}
            title={t('next.sheet.failed')}
          />
        )}
        {view === DELIVERED_VIEW.docx && (
          <LazyView
            load={loadDocxView}
            props={{ machine, path, src: previewUrl(machine, path), name, ...toFile }}
            fallback={<span className="hx-sk hx-pdf__wait-sheet" />}
            title={t('next.docx.failed')}
          />
        )}
        {view === DELIVERED_VIEW.pptx && (
          <LazyView
            load={loadPptxView}
            props={{ machine, path, src: previewUrl(machine, path), name, ...toFile }}
            fallback={<span className="hx-sk hx-pdf__wait-sheet" />}
            title={t('next.pptx.failed')}
          />
        )}
        {view === DELIVERED_VIEW.font && (
          <LazyView
            load={loadFontView}
            props={{ href: saveHref, name }}
            fallback={<span className="hx-sk hx-plan__sk" />}
            title={t('next.font.failed')}
          />
        )}
        {view === DELIVERED_VIEW.save && <div className="hx-tv__note">{t('next.delivered.saveOnly')}</div>}
      </PaneHead>
    </div>
  );
};

export default DeliveredView;
