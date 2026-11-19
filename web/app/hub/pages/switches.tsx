// /switches: the rollout switches of the whole installation (docs/hub/rollout-switches.md) — feature × person × machine
// with the flag, the note and who changed it. Only an administrator of the installation; every change and every delete
// is confirmed with the exact request the hub will get.
import { useEffect, useState } from 'react';

import { Button, Dialog, TextArea, TextField } from '@kit';

import { useApi } from '../lib/env';
import { errorText } from '../lib/errors';
import { currentLang, t } from '../lib/i18n';
import { Link } from '../lib/router';
import { useMe } from '../lib/session';
import {
  deletePath,
  emptyForm,
  fromConfig,
  putJson,
  type RolloutSwitch,
  same,
  sorted,
  type SwitchForm,
  type SwitchKey,
  toForm,
  toPut,
} from '../lib/switches';
import { PlainPage } from '../ui/frame';
import { Plus } from '../ui/icons';
import { CodeLine, ListSkeleton, Msg, Note, Pill, Segmented } from '../ui/parts';

const when = (iso: string): string => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? iso
    : d.toLocaleString(currentLang(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
};

const anyOr = (v: string): string => (v === '*' ? t('switches.any') : v);

export const SwitchesScreen = () => {
  const me = useMe();
  return (
    <PlainPage title={t('switches.title')}>
      <p className="hub-note">
        <Link to="/settings" className="hub-link">
          {t('account.back')}
        </Link>
      </p>
      {me.user.isAdmin ? <Switches /> : <Msg tone="warn">{t('switches.adminOnly')}</Msg>}
    </PlainPage>
  );
};

/** What the dialog is on: the form of a row, then its confirm; or the confirm of a delete. */
type Step =
  | { kind: 'form'; edit: SwitchKey | null; form: SwitchForm }
  | { kind: 'put'; edit: SwitchKey | null; form: SwitchForm }
  | { kind: 'delete'; row: RolloutSwitch };

export const Switches = () => {
  const api = useApi();
  const [rows, setRows] = useState<RolloutSwitch[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [step, setStep] = useState<Step | null>(null);
  const [dialogError, setDialogError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<string | null>(null);

  const [tick, setTick] = useState(0);

  useEffect(() => {
    let alive = true;
    api.admin.switches.list().then(
      (r) => {
        if (!alive) return;
        setRows(sorted(r.switches ?? []));
        setError(null);
      },
      (e: unknown) => {
        if (alive) setError(errorText(e));
      },
    );
    return () => {
      alive = false;
    };
  }, [api, tick]);

  const close = () => {
    if (busy) return;
    setStep(null);
    setDialogError(null);
  };

  const send = async () => {
    if (!step || step.kind === 'form') return;
    setBusy(true);
    setDialogError(null);
    try {
      if (step.kind === 'put') await api.admin.switches.put(toPut(step.form));
      else await api.admin.switches.remove(step.row);
      setDone(step.kind === 'put' ? t('switches.saved') : t('switches.deleted'));
      setStep(null);
      setTick((n) => n + 1);
    } catch (e) {
      setDialogError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  if (rows === null) return error ? <Msg tone="danger">{error}</Msg> : <ListSkeleton rows={4} />;

  const open = (s: Step) => {
    setDone(null);
    setDialogError(null);
    setStep(s);
  };

  return (
    <>
      <Note>{t('switches.lead')}</Note>
      <Msg tone="warn">{t('switches.seedWarn')}</Msg>
      {error && <Msg tone="danger">{error}</Msg>}
      {done && <Msg tone="success">{done}</Msg>}
      <section className="hub-section">
        <div className="hub-row" data-between>
          <h2 className="hub-section__t">{t('switches.title')}</h2>
          <Button size="s" onClick={() => open({ kind: 'form', edit: null, form: emptyForm() })}>
            <Plus size={14} /> {t('switches.add')}
          </Button>
        </div>
        {rows.length === 0 ? (
          <Note>{t('switches.empty')}</Note>
        ) : (
          <div className="hub-list">
            {rows.map((r) => (
              <div key={`${r.feature}\u0000${r.person}\u0000${r.machine}`} className="hub-item">
                <div className="hub-item__head">
                  <span className="hub-t">
                    <span className="hub-t__n">{r.feature}</span>
                    <span className="hub-item__s">
                      {t('switches.person')}: {anyOr(r.person)} · {t('switches.machine')}: {anyOr(r.machine)}
                    </span>
                  </span>
                  <Pill tone={r.enabled ? undefined : 'muted'}>
                    {r.enabled ? t('switches.on') : t('switches.off')}
                  </Pill>
                  {fromConfig(r) && <Pill tone="warn">{t('switches.seeded')}</Pill>}
                  <Button
                    size="xs"
                    appearance="ghost"
                    onClick={() => open({ kind: 'form', edit: r, form: toForm(r) })}
                  >
                    {t('switches.edit')}
                  </Button>
                  <Button size="xs" appearance="ghost" onClick={() => open({ kind: 'delete', row: r })}>
                    {t('switches.delete')}
                  </Button>
                </div>
                <span className="hub-item__s">
                  {r.note || t('switches.noNote')} ·{' '}
                  {t('switches.updated', { by: r.updatedBy, at: when(r.updatedAt) })}
                </span>
              </div>
            ))}
          </div>
        )}
      </section>
      {step && (
        <StepDialog
          step={step}
          busy={busy}
          error={dialogError}
          rows={rows}
          onChange={setStep}
          onClose={close}
          onSend={() => void send()}
        />
      )}
    </>
  );
};

const StepDialog = ({
  step,
  busy,
  error,
  rows,
  onChange,
  onClose,
  onSend,
}: {
  step: Step;
  busy: boolean;
  error: string | null;
  rows: readonly RolloutSwitch[];
  onChange: (s: Step) => void;
  onClose: () => void;
  onSend: () => void;
}) => {
  if (step.kind === 'delete') {
    return (
      <Dialog
        open
        onClose={onClose}
        size="m"
        title={t('switches.confirmDeleteTitle')}
        description={t('switches.confirmDeleteText')}
        footer={
          <>
            <Button onClick={onClose}>{t('common.cancel')}</Button>
            <Button appearance="danger" loading={busy} onClick={onSend}>
              {t('switches.delete')}
            </Button>
          </>
        }
      >
        <Note>{t('switches.confirmDelete')}</Note>
        <CodeLine text={deletePath(step.row)} />
        {fromConfig(step.row) && <Msg tone="warn">{t('switches.seedWarn')}</Msg>}
        {error && <Msg tone="danger">{error}</Msg>}
      </Dialog>
    );
  }
  const body = toPut(step.form);
  const target = rows.find((r) => same(r, body));
  if (step.kind === 'put') {
    return (
      <Dialog
        open
        onClose={onClose}
        size="m"
        title={t('switches.confirmTitle')}
        footer={
          <>
            <Button disabled={busy} onClick={() => onChange({ ...step, kind: 'form' })}>
              {t('switches.back')}
            </Button>
            <Button appearance="primary" loading={busy} onClick={onSend}>
              {t('switches.confirmSend')}
            </Button>
          </>
        }
      >
        <Note>{t('switches.confirmPut')}</Note>
        <CodeLine text={putJson(body)} />
        {target && fromConfig(target) && <Msg tone="warn">{t('switches.seedWarn')}</Msg>}
        {error && <Msg tone="danger">{error}</Msg>}
      </Dialog>
    );
  }
  const form = step.form;
  const patch = (p: Partial<SwitchForm>) => onChange({ ...step, form: { ...form, ...p } });
  const nameless = body.feature === '';
  return (
    <Dialog
      open
      onClose={onClose}
      size="m"
      title={step.edit ? t('switches.editTitle') : t('switches.addTitle')}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button appearance="primary" disabled={nameless} onClick={() => onChange({ ...step, kind: 'put' })}>
            {t('switches.review')}
          </Button>
        </>
      }
    >
      <TextField
        size="m"
        label={t('switches.feature')}
        value={form.feature}
        placeholder="own-work"
        disabled={step.edit !== null}
        spellCheck={false}
        onChange={(e) => patch({ feature: e.target.value })}
      />
      <TextField
        size="m"
        label={t('switches.person')}
        hint={t('switches.hint.person')}
        value={form.person}
        placeholder="*"
        disabled={step.edit !== null}
        spellCheck={false}
        onChange={(e) => patch({ person: e.target.value })}
      />
      <TextField
        size="m"
        label={t('switches.machine')}
        hint={t('switches.hint.machine')}
        value={form.machine}
        placeholder="*"
        disabled={step.edit !== null}
        spellCheck={false}
        onChange={(e) => patch({ machine: e.target.value })}
      />
      <Segmented
        label={t('switches.enabled')}
        value={form.enabled ? 'on' : 'off'}
        options={[
          { value: 'on', label: t('switches.on') },
          { value: 'off', label: t('switches.off') },
        ]}
        onChange={(v) => patch({ enabled: v === 'on' })}
      />
      <TextArea
        label={t('switches.note')}
        rows={2}
        value={form.note}
        onChange={(e) => patch({ note: e.target.value })}
      />
      {nameless && <Msg tone="warn">{t('switches.err.feature')}</Msg>}
    </Dialog>
  );
};
