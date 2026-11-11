// «Перенести на другой инстанс» and «Загрузить пакет»: projects with their kits as one file. The dialogs keep
// their last content while they sink out (usePresence), change nothing on screen before the machine answered, and say a
// refusal in the machine's own words.
import { useRef, useState, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import { uploadFile, UPLOAD_FAILURE } from '@/next/commands/uploads';
import { Modal } from '@/next/features/designly/sdk/ui';
import {
  exportPack,
  importPack,
  saveBlob,
  PACK_EXPORT,
  PACK_STATUS,
  type IPackPlan,
  type IPackRestricted,
} from '@/next/features/designly/pack-api';
import { Button } from '@/next/ui/button';
import { usePresence } from '@/next/ui/presence';
import { formatProgress } from '@/next/ui/transfer';
import type { MachineId } from '@/next/protocol/v2';

/** The dialogs leave this long (shell.css, .hx-modal). */
const EXIT_MS = 240;
/** Names shown in a list before «и ещё». */
const SHOWN = 6;

const Names = ({ names }: { names: readonly string[] }) => (
  <ul className="hx-pack__list">
    {names.slice(0, SHOWN).map((n) => (
      <li key={n}>{n}</li>
    ))}
    {names.length > SHOWN && <li>{t('next.pack.more', { count: names.length - SHOWN })}</li>}
  </ul>
);

const Note = ({ children, warn = false }: { children: ReactNode; warn?: boolean }) => (
  <p className="hx-pack__note" data-tone={warn ? 'warn' : undefined} role={warn ? 'alert' : undefined}>
    {children}
  </p>
);

const restrictedName = (r: IPackRestricted): string => r.font ?? r.path;

// ── Export ──────────────────────────────────────────────────────────────────────────────────────────────────────────

export const EXPORT_STEP = { working: 'working', confirm: 'confirm', failed: 'failed' } as const;

export type TExportState =
  | { at: typeof EXPORT_STEP.working; ids: string[]; done: number; total: number }
  | { at: typeof EXPORT_STEP.confirm; ids: string[]; files: IPackRestricted[]; busy: boolean }
  | { at: typeof EXPORT_STEP.failed; message: string };

interface IExportDialogProps {
  state: TExportState | null;
  onConfirm: () => void;
  onClose: () => void;
}

export const PackExportDialog = ({ state, onConfirm, onClose }: IExportDialogProps) => {
  const shown = usePresence(state, EXIT_MS).value;
  const working = shown?.at === EXPORT_STEP.working || (shown?.at === EXPORT_STEP.confirm && shown.busy);
  return (
    <Modal
      open={state !== null}
      onClose={() => {
        if (!working) onClose();
      }}
      title={t('next.pack.exportTitle')}
      boxClass="hx-leave"
    >
      <div className="hx-pack">
        {shown?.at === EXPORT_STEP.working && (
          <Note>
            {shown.total > 0
              ? formatProgress({ sent: shown.done, size: shown.total, speed: 0 })
              : t('next.pack.exporting')}
          </Note>
        )}
        {shown?.at === EXPORT_STEP.confirm && (
          <>
            <Note>{t('next.pack.restrictedLead')}</Note>
            <Names names={shown.files.map(restrictedName)} />
            <Note>{t('next.pack.restrictedKeep')}</Note>
          </>
        )}
        {shown?.at === EXPORT_STEP.failed && <Note warn>{shown.message}</Note>}
      </div>
      <div className="hx-modal__foot">
        {shown?.at === EXPORT_STEP.confirm ? (
          <>
            <Button variant="ghost" disabled={shown.busy} onClick={onClose}>
              {t('next.pack.cancel')}
            </Button>
            <Button variant="primary" busy={shown.busy} onClick={onConfirm}>
              {t('next.pack.restrictedDo')}
            </Button>
          </>
        ) : (
          <Button variant="ghost" disabled={shown?.at === EXPORT_STEP.working} onClick={onClose}>
            {t('next.pack.close')}
          </Button>
        )}
      </div>
    </Modal>
  );
};

/** The export of the projects `ids`: `start` opens the dialog and asks the machine; the file is saved only when the
 *  machine has made it (and, for restricted kit files, only after the person's word). */
export const usePackExport = (machine: MachineId | null) => {
  const [state, setState] = useState<TExportState | null>(null);

  const run = async (ids: string[], acceptRestricted: boolean) => {
    if (machine === null) return;
    setState({ at: EXPORT_STEP.working, ids, done: 0, total: 0 });
    try {
      const r = await exportPack(machine, ids, {
        acceptRestricted,
        onProgress: (done, total) => setState({ at: EXPORT_STEP.working, ids, done, total }),
      });
      if (r.kind === PACK_EXPORT.done) {
        saveBlob(r.blob, r.name);
        setState(null);
      } else if (r.kind === PACK_EXPORT.restricted) {
        setState({ at: EXPORT_STEP.confirm, ids, files: r.files, busy: false });
      } else {
        setState({ at: EXPORT_STEP.failed, message: r.message });
      }
    } catch {
      setState({ at: EXPORT_STEP.failed, message: t('next.pack.networkFailed') });
    }
  };

  const dialog = (
    <PackExportDialog
      state={state}
      onClose={() => setState(null)}
      onConfirm={() => {
        if (state?.at !== EXPORT_STEP.confirm || state.busy) return;
        setState({ ...state, busy: true });
        void run(state.ids, true);
      }}
    />
  );
  return { start: (ids: string[]) => void run(ids, false), dialog };
};

// ── Import ──────────────────────────────────────────────────────────────────────────────────────────────────────────

export const IMPORT_STEP = {
  sending: 'sending',
  checking: 'checking',
  review: 'review',
  same: 'same',
  importing: 'importing',
  done: 'done',
  failed: 'failed',
} as const;

export type TImportState =
  | { at: typeof IMPORT_STEP.sending; done: number; total: number }
  | { at: typeof IMPORT_STEP.checking }
  | { at: typeof IMPORT_STEP.review; upload: string; plan: IPackPlan }
  | { at: typeof IMPORT_STEP.same; plan: IPackPlan }
  | { at: typeof IMPORT_STEP.importing; plan: IPackPlan }
  | { at: typeof IMPORT_STEP.done; plan: IPackPlan }
  | { at: typeof IMPORT_STEP.failed; message: string; conflict: string[] };

const kitName = (k: { id: string; version: string }) => `${k.id} ${k.version}`;

const incoming = (plan: IPackPlan) => plan.projects.filter((p) => p.status !== PACK_STATUS.unchanged);

interface IImportDialogProps {
  state: TImportState | null;
  onImport: () => void;
  onClose: () => void;
  onOpen: (id: string) => void;
}

const Plan = ({ plan, future }: { plan: IPackPlan; future: boolean }) => {
  const projects = incoming(plan);
  const kits = plan.kits.filter((k) => k.status === PACK_STATUS.added);
  const same = plan.projects.length - projects.length;
  return (
    <>
      {projects.length > 0 && (
        <>
          <p className="hx-pack__what">{t(future ? 'next.pack.willAppear' : 'next.pack.appeared')}</p>
          <ul className="hx-pack__list">
            {projects.map((p) => (
              <li key={p.id}>
                {p.title}
                {p.status === PACK_STATUS.renamed && (
                  <span className="hx-pack__aside">{t('next.pack.renamed', { id: p.as })}</span>
                )}
              </li>
            ))}
          </ul>
        </>
      )}
      {kits.length > 0 && (
        <>
          <p className="hx-pack__what">{t('next.pack.kitsAppear')}</p>
          <Names names={kits.map(kitName)} />
        </>
      )}
      {future && same > 0 && <Note>{t('next.pack.alreadyHere')}</Note>}
      {plan.warnings.map((w) => (
        <Note key={w}>{w}</Note>
      ))}
    </>
  );
};

export const PackImportDialog = ({ state, onImport, onClose, onOpen }: IImportDialogProps) => {
  const shown = usePresence(state, EXIT_MS).value;
  const busy =
    shown?.at === IMPORT_STEP.sending ||
    shown?.at === IMPORT_STEP.checking ||
    shown?.at === IMPORT_STEP.importing;
  return (
    <Modal
      open={state !== null}
      onClose={() => {
        if (!busy) onClose();
      }}
      title={t('next.pack.importTitle')}
      boxClass="hx-leave"
    >
      <div className="hx-pack">
        {shown?.at === IMPORT_STEP.sending && (
          <Note>{formatProgress({ sent: shown.done, size: shown.total, speed: 0 })}</Note>
        )}
        {shown?.at === IMPORT_STEP.checking && <Note>{t('next.pack.checking')}</Note>}
        {shown?.at === IMPORT_STEP.review && <Plan plan={shown.plan} future />}
        {shown?.at === IMPORT_STEP.same && <Note>{t('next.pack.allHere')}</Note>}
        {shown?.at === IMPORT_STEP.importing && <Note>{t('next.pack.importing')}</Note>}
        {shown?.at === IMPORT_STEP.done && (
          <>
            <Plan plan={shown.plan} future={false} />
            <div className="hx-pack__links">
              {incoming(shown.plan).map((p) => (
                <Button
                  key={p.id}
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    onClose();
                    onOpen(p.as);
                  }}
                >
                  {t('next.pack.open', { title: p.title })}
                </Button>
              ))}
            </div>
          </>
        )}
        {shown?.at === IMPORT_STEP.failed && (
          <>
            <Note warn>{shown.message}</Note>
            {shown.conflict.length > 0 && <Names names={shown.conflict} />}
          </>
        )}
      </div>
      <div className="hx-modal__foot">
        {shown?.at === IMPORT_STEP.review ? (
          <>
            <Button variant="ghost" onClick={onClose}>
              {t('next.pack.cancel')}
            </Button>
            <Button variant="primary" onClick={onImport}>
              {t('next.pack.importDo')}
            </Button>
          </>
        ) : (
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('next.pack.close')}
          </Button>
        )}
      </div>
    </Modal>
  );
};

/** «Загрузить пакет»: `pick` asks for the file, the pack is sent to the machine, checked without writing, shown, and only
 *  then installed. `dialog` is drawn once, anywhere in the screen; it carries the hidden file field. */
export const usePackImport = (
  machine: MachineId | null,
  onImported: () => void,
  onOpen: (id: string) => void,
) => {
  const input = useRef<HTMLInputElement>(null);
  const [state, setState] = useState<TImportState | null>(null);

  const failed = (message: string, conflict: string[] = []) =>
    setState({ at: IMPORT_STEP.failed, message, conflict });

  const check = async (file: File) => {
    if (machine === null) return;
    setState({ at: IMPORT_STEP.sending, done: 0, total: file.size });
    try {
      const sent = await uploadFile(machine, file, (done, total) =>
        setState({ at: IMPORT_STEP.sending, done, total }),
      );
      if (!sent.ok) {
        failed(
          sent.code === UPLOAD_FAILURE.refused && sent.message
            ? sent.message
            : t(sent.code === UPLOAD_FAILURE.network ? 'next.pack.networkFailed' : 'next.pack.uploadFailed'),
        );
        return;
      }
      setState({ at: IMPORT_STEP.checking });
      const r = await importPack(machine, sent.ref.id, { dryRun: true });
      if (!r.ok) failed(r.message, r.conflict.map(kitName));
      else if (incoming(r.plan).length === 0 && r.plan.kits.every((k) => k.status !== PACK_STATUS.added))
        setState({ at: IMPORT_STEP.same, plan: r.plan });
      else setState({ at: IMPORT_STEP.review, upload: sent.ref.id, plan: r.plan });
    } catch {
      failed(t('next.pack.networkFailed'));
    }
  };

  const install = async () => {
    if (machine === null || state?.at !== IMPORT_STEP.review) return;
    const { upload, plan } = state;
    setState({ at: IMPORT_STEP.importing, plan });
    try {
      const r = await importPack(machine, upload);
      if (!r.ok) {
        failed(r.message, r.conflict.map(kitName));
        return;
      }
      setState({ at: IMPORT_STEP.done, plan: r.plan });
      onImported();
    } catch {
      failed(t('next.pack.networkFailed'));
    }
  };

  const dialog = (
    <>
      <input
        ref={input}
        type="file"
        accept=".gz,.tgz,application/gzip"
        className="hx-pack__file"
        aria-label={t('next.pack.importPick')}
        tabIndex={-1}
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = '';
          if (f) void check(f);
        }}
      />
      <PackImportDialog
        state={state}
        onImport={() => void install()}
        onClose={() => setState(null)}
        onOpen={onOpen}
      />
    </>
  );
  return { pick: () => input.current?.click(), dialog };
};
