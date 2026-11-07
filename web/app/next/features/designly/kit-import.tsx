// «Добавить дизайн-систему»: a kit file from another machine (or this one) is checked first, then added. Nothing is
// overwritten: a version already held with other content is shown as a difference and offered as a new version.
import { useRef, useState } from 'react';
import { t } from '@/next/i18n';
import { uploadFile, UPLOAD_FAILURE } from '@/next/commands/uploads';
import { importKit, type IKitDiff, type TKitImport } from '@/next/features/designly/api';
import { formatProgress } from '@/next/ui/transfer';
import type { MachineId } from '@/next/protocol/v2';

interface IProps {
  machine: MachineId;
  /** The kit was installed: the list of systems is read again. */
  onAdded: () => void;
}

type TState =
  | { at: 'idle' }
  | { at: 'busy' }
  | { at: 'sending'; done: number; total: number }
  | { at: 'review'; upload: string; found: Extract<TKitImport, { ok: true }> }
  | { at: 'conflict'; upload: string; id: string; version: string; diff: IKitDiff }
  | { at: 'done'; id: string; version: string; unchanged: boolean }
  | { at: 'error'; message: string };

const SHOWN = 5;

const DiffList = ({ title, files }: { title: string; files: readonly string[] }) =>
  files.length === 0 ? null : (
    <li>
      <span className="hx-kit-add__what">{title}</span>
      {files.slice(0, SHOWN).join(', ')}
      {files.length > SHOWN ? ` +${files.length - SHOWN}` : ''}
    </li>
  );

const nextPatch = (v: string): string => {
  const m = /^(\d+)\.(\d+)\.(\d+)/.exec(v);
  return m ? `${m[1]}.${m[2]}.${Number(m[3]) + 1}` : v;
};

export const KitImport = ({ machine, onAdded }: IProps) => {
  const input = useRef<HTMLInputElement>(null);
  const [state, setState] = useState<TState>({ at: 'idle' });

  const check = async (file: File) => {
    setState({ at: 'sending', done: 0, total: file.size });
    try {
      const sent = await uploadFile(machine, file, (done, total) => setState({ at: 'sending', done, total }));
      if (!sent.ok) {
        setState({
          at: 'error',
          message:
            sent.code === UPLOAD_FAILURE.aborted
              ? t('next.designly.kitFailed')
              : (sent.message ?? t('next.designly.kitFailed')),
        });
        return;
      }
      const upload = sent.ref.id;
      setState({ at: 'busy' });
      const r = await importKit(machine, upload, { dryRun: true });
      if (r.ok) setState({ at: 'review', upload, found: r });
      else if (r.conflict)
        setState({ at: 'conflict', upload, id: r.id, version: r.version, diff: r.conflict });
      else setState({ at: 'error', message: r.error });
    } catch {
      setState({ at: 'error', message: t('next.designly.kitFailed') });
    }
  };

  const add = async (upload: string, asVersion?: string) => {
    setState({ at: 'busy' });
    try {
      const r = await importKit(machine, upload, { asVersion });
      if (!r.ok) {
        setState({ at: 'error', message: r.error });
        return;
      }
      setState({ at: 'done', id: r.id, version: r.version, unchanged: r.unchanged });
      if (r.installed) onAdded();
    } catch {
      setState({ at: 'error', message: t('next.designly.kitFailed') });
    }
  };

  const close = () => setState({ at: 'idle' });

  return (
    <div className="hx-kit-add">
      <input
        ref={input}
        type="file"
        accept=".gz,.tgz,.tar,application/gzip"
        className="hx-kit-add__file"
        aria-label={t('next.designly.kitAdd')}
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = '';
          if (f) void check(f);
        }}
      />
      {(state.at === 'idle' || state.at === 'done' || state.at === 'error') && (
        <button type="button" className="hx-btn" onClick={() => input.current?.click()}>
          {t('next.designly.kitAdd')}
        </button>
      )}
      {state.at === 'sending' && (
        <p className="hx-kit-add__note">
          {formatProgress({ sent: state.done, size: state.total, speed: 0 })}
        </p>
      )}
      {state.at === 'busy' && <p className="hx-kit-add__note">{t('next.designly.kitChecking')}</p>}
      {state.at === 'review' && (
        <div className="hx-kit-add__box" role="group" aria-label={t('next.designly.kitAdd')}>
          <p className="hx-kit-add__title">{`${state.found.id} ${state.found.version}`}</p>
          <p className="hx-kit-add__note">
            {state.found.unchanged
              ? t('next.designly.kitSame')
              : t('next.designly.kitFiles', { count: state.found.files })}
          </p>
          {state.found.restricted > 0 && (
            <p className="hx-kit-add__note" data-tone="warn">
              {t('next.designly.kitRestricted', { count: state.found.restricted })}
            </p>
          )}
          {state.found.warnings.map((w) => (
            <p key={w} className="hx-kit-add__note">
              {w}
            </p>
          ))}
          <div className="hx-kit-add__acts">
            <button type="button" className="hx-btn" onClick={close}>
              {t('next.designly.kitCancel')}
            </button>
            {!state.found.unchanged && (
              <button
                type="button"
                className="hx-btn"
                data-variant="primary"
                onClick={() => void add(state.upload)}
              >
                {t('next.designly.kitDo')}
              </button>
            )}
          </div>
        </div>
      )}
      {state.at === 'conflict' && (
        <div className="hx-kit-add__box" role="group" aria-label={t('next.designly.kitAdd')}>
          <p className="hx-kit-add__title">
            {t('next.designly.kitConflict', { id: state.id, version: state.version })}
          </p>
          <ul className="hx-kit-add__diff">
            <DiffList title={t('next.designly.kitChanged')} files={state.diff.changed} />
            <DiffList title={t('next.designly.kitAddedBy')} files={state.diff.added} />
            <DiffList title={t('next.designly.kitMissing')} files={state.diff.removed} />
          </ul>
          <div className="hx-kit-add__acts">
            <button type="button" className="hx-btn" onClick={close}>
              {t('next.designly.kitCancel')}
            </button>
            <button
              type="button"
              className="hx-btn"
              data-variant="primary"
              onClick={() => void add(state.upload, nextPatch(state.version))}
            >
              {t('next.designly.kitAsVersion', { version: nextPatch(state.version) })}
            </button>
          </div>
        </div>
      )}
      {state.at === 'done' && (
        <p className="hx-kit-add__note">
          {state.unchanged
            ? t('next.designly.kitAlready', { id: state.id, version: state.version })
            : t('next.designly.kitAdded', { id: state.id, version: state.version })}
        </p>
      )}
      {state.at === 'error' && (
        <p className="hx-kit-add__note" data-tone="warn" role="alert">
          {state.message}
        </p>
      )}
    </div>
  );
};

export default KitImport;
