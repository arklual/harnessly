// The files going into a folder of «Файлы», under its list: each with its progress, pause and cancel, and the
// question when a file of that name is there already. Done ones stay until cleared, so the person sees what arrived.
import { useState } from 'react';
import Icon from '@/next/ui/icon';
import { Button } from '@/next/ui/button';
import { t } from '@/next/i18n';
import { formatBytes, formatSpeed } from '@/lib/hl-disk-format';
import {
  CONFLICT_CHOICE,
  UPLOAD_STATE,
  isActiveUpload,
  type DiskUploadQueue,
  type IDiskUpload,
  type TConflictChoice,
} from '@/lib/hl-disk-uploader';

const STATE_KEY = {
  queued: 'next.files.upQueued',
  paused: 'next.files.upPaused',
  conflict: 'next.files.upConflict',
  done: 'next.files.upDone',
  canceled: 'next.files.upCanceled',
  skipped: 'next.files.upSkipped',
} as const;

const statusOf = (upload: IDiskUpload): string => {
  if (upload.state === UPLOAD_STATE.uploading)
    return upload.speed > 0
      ? `${formatBytes(upload.sent)} / ${formatBytes(upload.size)} · ${formatSpeed(upload.speed)}`
      : `${formatBytes(upload.sent)} / ${formatBytes(upload.size)}`;
  if (upload.note) return upload.note;
  if (upload.state === UPLOAD_STATE.retrying || upload.state === UPLOAD_STATE.failed) return '';
  return t(STATE_KEY[upload.state]);
};

const Row = ({ upload, queue }: { upload: IDiskUpload; queue: DiskUploadQueue }) => {
  const [forAll, setForAll] = useState(false);
  const label = upload.dir ? `${upload.dir}/${upload.name}` : upload.name;
  const running = upload.state === UPLOAD_STATE.uploading || upload.state === UPLOAD_STATE.retrying;
  const active = isActiveUpload(upload.state);
  const choose = (choice: TConflictChoice) => queue.resolveConflict(upload.id, choice, forAll);
  return (
    <li className="hx-upl__row" data-state={upload.state}>
      <span className="hx-upl__head">
        <span className="hx-upl__name" title={label}>
          {label}
        </span>
        {running && (
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.files.upPause', { name: upload.name })}
            onClick={() => queue.pause(upload.id)}
          >
            <Icon name="pause" size="sm" />
          </button>
        )}
        {upload.state === UPLOAD_STATE.paused && (
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.files.upResume', { name: upload.name })}
            onClick={() => queue.resume(upload.id)}
          >
            <Icon name="play" size="sm" />
          </button>
        )}
        {active && (
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.files.upCancel', { name: upload.name })}
            onClick={() => queue.cancel(upload.id)}
          >
            <Icon name="x" size="sm" />
          </button>
        )}
      </span>
      {upload.state === UPLOAD_STATE.conflict ? (
        <span className="hx-upl__ask" role="group" aria-label={t('next.files.upConflict')}>
          <span className="hx-upl__acts">
            <Button size="sm" onClick={() => choose(CONFLICT_CHOICE.replace)}>
              {t('next.files.upReplace')}
            </Button>
            <Button size="sm" onClick={() => choose(CONFLICT_CHOICE.keepBoth)}>
              {t('next.files.upKeepBoth')}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => choose(CONFLICT_CHOICE.skip)}>
              {t('next.files.upSkip')}
            </Button>
          </span>
          <label className="hx-upl__all">
            <input type="checkbox" checked={forAll} onChange={(e) => setForAll(e.target.checked)} />
            {t('next.files.upForAll')}
          </label>
        </span>
      ) : (
        <>
          {(active || upload.state === UPLOAD_STATE.done) && (
            <span className="hx-upl__bar" aria-hidden="true">
              <span style={{ width: `${(100 * upload.sent) / Math.max(upload.size, 1)}%` }} />
            </span>
          )}
          <span className="hx-upl__status" role={upload.state === UPLOAD_STATE.failed ? 'alert' : undefined}>
            {statusOf(upload)}
          </span>
        </>
      )}
    </li>
  );
};

const UploadsStrip = ({ uploads, queue }: { uploads: readonly IDiskUpload[]; queue: DiskUploadQueue }) => {
  if (uploads.length === 0) return null;
  const busy = uploads.some((u) => isActiveUpload(u.state));
  return (
    <div className="hx-upl" role="region" aria-label={t('next.files.uploads')}>
      <div className="hx-upl__top">
        <Icon name="upload" size="sm" />
        <span className="hx-upl__title">{t('next.files.uploads')}</span>
        {!busy && (
          <Button size="sm" variant="ghost" onClick={() => queue.clearFinished()}>
            {t('next.files.upClear')}
          </Button>
        )}
      </div>
      <ul className="hx-upl__list">
        {uploads.map((upload) => (
          <Row key={upload.id} upload={upload} queue={queue} />
        ))}
      </ul>
    </div>
  );
};

export default UploadsStrip;
