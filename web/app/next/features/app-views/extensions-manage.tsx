// The management half of the Extensions page: install a folder after looking at what it asks for, switch an
// extension off and on, remove it with or without its data, and fill its settings and secrets. The core keeps all of it;
// a secret field only sends what was typed and never shows what is saved. Built of the Settings kit; nothing here is
// drawn without HARNESSLY_UI_SLOTS=on, because the runtime answers 404 then.
import { useState } from 'react';
import {
  deleteSecret,
  inspectExtension,
  installExtension,
  putSecret,
  removeExtension,
  saveSettings,
  switchExtension,
  useExtensions,
  type IExtension,
  type IInspected,
  type ISettingField,
} from '@/next/features/app-views/ext-api';
import { Group, Row, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import SecretField from '@/next/ui/secret-field';
import '@/next/styles/app-views.css';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

const kindLabel = (k: IExtension['kind']) => t(`next.ext.kind.${k}`);

/** A text field of the Settings kit, labelled for the screen reader. */
const TextInput = ({
  value,
  label,
  onChange,
  type = 'text',
  mono = false,
  placeholder,
  onSubmit,
}: {
  value: string;
  label: string;
  onChange: (v: string) => void;
  type?: 'text' | 'number';
  mono?: boolean;
  placeholder?: string;
  onSubmit?: () => void;
}) => (
  <input
    className={mono ? 'hx-set__input hx-set__input--mono' : 'hx-set__input'}
    type={type}
    value={value}
    aria-label={label}
    placeholder={placeholder}
    autoComplete="off"
    spellCheck={false}
    onChange={(e) => onChange(e.target.value)}
    onKeyDown={(e) => e.key === 'Enter' && onSubmit?.()}
  />
);

const SettingsForm = ({ machine, ext, done }: { machine: MachineId; ext: IExtension; done: () => void }) => {
  const props = ext.settings?.properties ?? {};
  const [draft, setDraft] = useState<Record<string, unknown>>(ext.values ?? {});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const keys = Object.keys(props);
  if (keys.length === 0) return null;
  const set = (k: string, v: unknown) => setDraft((d) => ({ ...d, [k]: v }));
  const save = () => {
    setBusy(true);
    setError('');
    saveSettings(machine, ext.id, draft)
      .then(done, (e: unknown) => setError(message(e)))
      .finally(() => setBusy(false));
  };
  const field = (k: string, f: ISettingField) => {
    const label = f.title ?? k;
    const v = draft[k];
    if (f.type === 'boolean')
      return (
        <SwitchRow key={k} label={label} hint={f.description} on={v === true} onChange={(on) => set(k, on)} />
      );
    return (
      <Row key={k} type="link" label={label} hint={f.description}>
        {f.enum ? (
          <select
            className="hx-set__input hx-ext__select"
            aria-label={label}
            value={String(v ?? '')}
            onChange={(e) =>
              set(k, typeof f.enum?.[0] === 'number' ? Number(e.target.value) : e.target.value)
            }
          >
            {f.enum.map((o) => (
              <option key={String(o)} value={String(o)}>
                {String(o)}
              </option>
            ))}
          </select>
        ) : (
          <TextInput
            label={label}
            type={f.type === 'number' || f.type === 'integer' ? 'number' : 'text'}
            value={v === undefined || v === null ? '' : String(v)}
            onChange={(s) =>
              set(k, f.type === 'number' || f.type === 'integer' ? (s === '' ? undefined : Number(s)) : s)
            }
          />
        )}
      </Row>
    );
  };
  return (
    <Group
      title={t('next.ext.settings', { name: ext.name })}
      foot={error ? <span role="alert">{error}</span> : undefined}
    >
      {keys.map((k) => field(k, props[k]!))}
      <div className="hx-ext__bar">
        <Button variant="primary" size="sm" busy={busy} onClick={save}>
          {t('next.ext.save')}
        </Button>
      </div>
    </Group>
  );
};

const SecretRow = ({
  machine,
  ext,
  name,
  set,
  description,
  optional,
  done,
}: {
  machine: MachineId;
  ext: IExtension;
  name: string;
  set: boolean;
  description?: string;
  optional?: boolean;
  done: () => void;
}) => {
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const run = (job: Promise<unknown>) => {
    setBusy(true);
    setError('');
    job
      .then(() => {
        setValue('');
        done();
      })
      .catch((e: unknown) => setError(message(e)))
      .finally(() => setBusy(false));
  };
  const state = set
    ? t('next.ext.secretSet')
    : optional
      ? t('next.ext.secretOptional')
      : t('next.ext.secretMissing');
  return (
    <Row type="link" label={name} hint={[description, state, error].filter(Boolean).join(' · ')} busy={busy}>
      <div className="hx-ext__secret">
        <SecretField
          value={value}
          onChange={setValue}
          label={t('next.ext.secretLabel', { name })}
          placeholder={set ? t('next.ext.secretReplace') : undefined}
          disabled={busy}
          invalid={error !== ''}
          onSubmit={() => value && run(putSecret(machine, ext.id, name, value))}
        />
        <Button
          size="sm"
          disabled={value === ''}
          onClick={() => run(putSecret(machine, ext.id, name, value))}
        >
          {t('next.ext.save')}
        </Button>
        {set && (
          <Button size="sm" variant="ghost" onClick={() => run(deleteSecret(machine, ext.id, name))}>
            {t('next.ext.secretRemove')}
          </Button>
        )}
      </div>
    </Row>
  );
};

const Remove = ({ machine, ext, done }: { machine: MachineId; ext: IExtension; done: () => void }) => {
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const go = (purge: boolean) => {
    setBusy(true);
    removeExtension(machine, ext.id, purge)
      .then(done, (e: unknown) => setError(message(e)))
      .finally(() => setBusy(false));
  };
  if (!asking)
    return (
      <Button size="sm" variant="danger" onClick={() => setAsking(true)}>
        {t('next.ext.remove')}
      </Button>
    );
  return (
    <div className="hx-ext__ask" role="group" aria-label={t('next.ext.removeAsk', { name: ext.name })}>
      <span>{t('next.ext.removeAsk', { name: ext.name })}</span>
      <Button size="sm" busy={busy} onClick={() => go(false)}>
        {t('next.ext.removeKeep')}
      </Button>
      <Button size="sm" variant="danger" busy={busy} onClick={() => go(true)}>
        {t('next.ext.removePurge')}
      </Button>
      <Button size="sm" variant="ghost" onClick={() => setAsking(false)}>
        {t('next.ext.cancel')}
      </Button>
      {error && <span role="alert">{error}</span>}
    </div>
  );
};

const ExtensionCard = ({
  machine,
  ext,
  reload,
}: {
  machine: MachineId;
  ext: IExtension;
  reload: () => void;
}) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const toggle = (on: boolean) => {
    setBusy(true);
    setError('');
    switchExtension(machine, ext.id, !on)
      .then(reload, (e: unknown) => setError(message(e)))
      .finally(() => setBusy(false));
  };
  const issues = (ext.compat ?? []).filter((i) => !i.warning);
  const hint = [
    `${kindLabel(ext.kind)} · ${ext.version}`,
    ext.disabled ? t('next.ext.off') : null,
    ext.dataTarget > 0 ? t('next.ext.data', { v: ext.dataVersion || ext.dataTarget }) : null,
    ext.team ? t('next.ext.team') : null,
    ext.dev ? t('next.ext.dev') : null,
  ]
    .filter(Boolean)
    .join(' · ');
  return (
    <>
      <Group title={ext.name} foot={error ? <span role="alert">{error}</span> : ext.description}>
        <SwitchRow
          label={t('next.ext.enabled')}
          hint={hint}
          on={!ext.disabled}
          busy={busy}
          onChange={toggle}
        />
        {issues.map((i) => (
          <div key={i.message} className="hx-ext__issue" role="alert">
            {i.message}
          </div>
        ))}
        <Row
          type="link"
          label={t('next.ext.rights')}
          hint={ext.permissions.join(', ') || t('next.ext.noRights')}
        >
          <Remove machine={machine} ext={ext} done={reload} />
        </Row>
        {ext.secrets.map((s) => (
          <SecretRow
            key={s.name}
            machine={machine}
            ext={ext}
            name={s.name}
            set={s.set}
            description={s.description}
            optional={s.optional}
            done={reload}
          />
        ))}
      </Group>
      {!ext.disabled && (
        <SettingsForm key={JSON.stringify(ext.values)} machine={machine} ext={ext} done={reload} />
      )}
    </>
  );
};

const Install = ({ machine, done }: { machine: MachineId; done: () => void }) => {
  const [dir, setDir] = useState('');
  const [seen, setSeen] = useState<IInspected | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const look = () => {
    setBusy(true);
    setError('');
    setSeen(null);
    inspectExtension(machine, dir.trim())
      .then(setSeen, (e: unknown) => setError(message(e)))
      .finally(() => setBusy(false));
  };
  const install = () => {
    setBusy(true);
    installExtension(machine, dir.trim())
      .then(() => {
        setSeen(null);
        setDir('');
        done();
      })
      .catch((e: unknown) => setError(message(e)))
      .finally(() => setBusy(false));
  };
  const pkg = seen?.package;
  const errors = (seen?.problems ?? []).filter((p) => !p.warning);
  const canInstall = pkg !== undefined && errors.length === 0 && !seen?.blocked;
  return (
    <Group
      title={t('next.ext.install')}
      foot={error ? <span role="alert">{error}</span> : t('next.ext.installFoot')}
    >
      <Row type="link" label={t('next.ext.folder')} hint={t('next.ext.folderHint')}>
        <div className="hx-ext__secret">
          <TextInput
            value={dir}
            label={t('next.ext.folder')}
            mono
            placeholder="/path/to/extension"
            onChange={setDir}
            onSubmit={look}
          />
          <Button size="sm" busy={busy} disabled={dir.trim() === ''} onClick={look}>
            {t('next.ext.inspect')}
          </Button>
        </div>
      </Row>
      {seen && (
        <div className="hx-ext__review" role="region" aria-label={t('next.ext.review')}>
          {pkg && (
            <>
              <div className="hx-ext__review-head">
                <strong>{pkg.name}</strong>
                <span>
                  {kindLabel(pkg.kind)} · {pkg.version}
                  {seen.installed ? ` · ${t('next.ext.replaces', { v: seen.installed })}` : ''}
                </span>
              </div>
              <div className="hx-ext__review-line">
                {t('next.ext.asks')} {pkg.permissions.join(', ') || t('next.ext.noRights')}
              </div>
              {pkg.secrets.length > 0 && (
                <div className="hx-ext__review-line">
                  {t('next.ext.secrets')} {pkg.secrets.map((s) => s.name).join(', ')}
                </div>
              )}
            </>
          )}
          {seen.blocked && (
            <div className="hx-ext__issue" role="alert">
              {t('next.ext.blocked')}
            </div>
          )}
          {seen.problems.map((p) => (
            <div
              key={`${p.path}${p.message}`}
              className="hx-ext__issue"
              data-warning={p.warning || undefined}
            >
              {p.path ? `${p.path}: ` : ''}
              {p.message}
            </div>
          ))}
          <div className="hx-ext__bar">
            <Button variant="primary" size="sm" busy={busy} disabled={!canInstall} onClick={install}>
              {seen.installed ? t('next.ext.update') : t('next.ext.installGo')}
            </Button>
          </div>
        </div>
      )}
    </Group>
  );
};

/** Installing, switching, removing and the settings of extensions; empty while the runtime has the slots off. */
export const ExtensionsManage = ({ machine }: { machine: MachineId }) => {
  const { list, reload } = useExtensions(machine);
  if (list === null) return null;
  return (
    <>
      <Install machine={machine} done={reload} />
      {list.map((e) => (
        <ExtensionCard key={e.id} machine={machine} ext={e} reload={reload} />
      ))}
    </>
  );
};
