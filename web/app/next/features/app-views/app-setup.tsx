// What the person sets up for an app, in «О приложении»: its settings (a form drawn from the manifest's JSON Schema)
// and its secrets (put into the machine's safe, off the chat). A secret's value goes in and never comes back: the field
// is empty, and the row only says whether a value is kept. Both save pessimistically: the screen shows what the
// runtime kept, not what was typed.
import { useCallback, useEffect, useState } from 'react';
import {
  loadAppSetup,
  putAppSecret,
  removeAppSecret,
  saveAppSettings,
  type IAppSetup,
  type IInstalledApp,
  type ISettingField,
} from '@/next/features/app-views/api';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import { Button } from '@/next/ui/button';
import SecretField from '@/next/ui/secret-field';
import Segmented from '@/next/ui/segmented';
import Switch from '@/next/ui/switch';
import '@/next/styles/app-main.css';

type TLoad = { k: 'loading' } | { k: 'ready'; setup: IAppSetup } | { k: 'failed' };

/** What a field holds while it is edited: a text for every kind but the switch. */
type TDraft = Record<string, string | boolean>;

const draftOf = (setup: IAppSetup): TDraft => {
  const out: TDraft = {};
  for (const f of setup.fields) {
    const v = setup.values[f.name];
    out[f.name] = f.type === 'boolean' ? v === true : v === undefined || v === null ? '' : String(v);
  }
  return out;
};

/** The values the runtime takes from a draft; a number field left empty is not sent. */
export const valuesOf = (fields: ISettingField[], draft: TDraft): Record<string, unknown> => {
  const out: Record<string, unknown> = {};
  for (const f of fields) {
    const v = draft[f.name];
    if (f.type === 'boolean') out[f.name] = v === true;
    else if (f.type === 'string') {
      if (typeof v === 'string' && (v !== '' || f.required)) out[f.name] = v;
    } else if (typeof v === 'string' && v.trim() !== '') out[f.name] = Number(v);
  }
  return out;
};

/** The word that says where a secret stands. */
const secretStateKey = (s: IAppSetup['secrets'][number]): string => {
  if (s.set) return 'next.inspector.secretSet';
  return s.optional ? 'next.inspector.secretOptional' : 'next.inspector.secretWaiting';
};

const Field = ({
  field,
  value,
  problem,
  onChange,
}: {
  field: ISettingField;
  value: string | boolean;
  problem: string | undefined;
  onChange: (v: string | boolean) => void;
}) => {
  const choices = field.enum?.map((o) => ({ value: String(o), label: String(o) }));
  let input;
  if (field.type === 'boolean') {
    input = <Switch label={field.title} checked={value === true} onChange={onChange} />;
  } else if (choices && choices.length > 0 && choices.length <= 4) {
    input = (
      <Segmented
        label={field.title}
        value={typeof value === 'string' ? value : ''}
        options={choices}
        onChange={onChange}
      />
    );
  } else {
    input = (
      <input
        className="hx-insp__input"
        type={field.type === 'string' ? 'text' : 'number'}
        inputMode={field.type === 'integer' ? 'numeric' : undefined}
        step={field.type === 'integer' ? 1 : 'any'}
        min={field.minimum}
        max={field.maximum}
        list={choices ? `hx-insp-${field.name}` : undefined}
        value={typeof value === 'string' ? value : ''}
        aria-label={field.title}
        aria-invalid={problem !== undefined || undefined}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }
  return (
    <div className="hx-insp__field" data-invalid={problem !== undefined || undefined}>
      <span className="hx-insp__label">
        {field.title}
        {field.required && <span aria-hidden> *</span>}
      </span>
      {input}
      {choices && choices.length > 4 && (
        <datalist id={`hx-insp-${field.name}`}>
          {choices.map((c) => (
            <option key={c.value} value={c.value} />
          ))}
        </datalist>
      )}
      {field.description && <span className="hx-insp__hint">{field.description}</span>}
      {problem !== undefined && <span className="hx-insp__problem">{problem}</span>}
    </div>
  );
};

const Settings = ({
  machine,
  app,
  setup,
  onSetup,
}: {
  machine: MachineId;
  app: IInstalledApp;
  setup: IAppSetup;
  onSetup: (s: IAppSetup) => void;
}) => {
  const [draft, setDraft] = useState<TDraft>(() => draftOf(setup));
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [problems, setProblems] = useState<Record<string, string>>({});
  const save = () => {
    setBusy(true);
    setFailed(false);
    saveAppSettings(machine, app.id, valuesOf(setup.fields, draft))
      .then((next) => {
        setProblems(next.problems ?? {});
        if (!next.problems) {
          setDraft(draftOf(next));
          onSetup(next);
        }
      })
      .catch(() => setFailed(true))
      .finally(() => setBusy(false));
  };
  return (
    <div className="hx-insp__sec">
      <h3 className="hx-insp__h">{t('next.inspector.settings')}</h3>
      <div className="hx-insp__form">
        {setup.fields.map((f) => (
          <Field
            key={f.name}
            field={f}
            value={draft[f.name] ?? ''}
            problem={problems[f.name]}
            onChange={(v) => setDraft((d) => ({ ...d, [f.name]: v }))}
          />
        ))}
      </div>
      {problems[''] !== undefined && <p className="hx-insp__problem">{problems['']}</p>}
      {failed && <p className="hx-insp__problem">{t('next.inspector.saveFailed')}</p>}
      <Button size="sm" variant="primary" busy={busy} onClick={save}>
        {t('next.inspector.save')}
      </Button>
    </div>
  );
};

const Secret = ({
  machine,
  app,
  secret,
  onSetup,
}: {
  machine: MachineId;
  app: IInstalledApp;
  secret: IAppSetup['secrets'][number];
  onSetup: (s: IAppSetup) => void;
}) => {
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const run = (work: Promise<IAppSetup>) => {
    setBusy(true);
    setFailed(false);
    work
      .then((next) => {
        setValue('');
        onSetup(next);
      })
      .catch(() => setFailed(true))
      .finally(() => setBusy(false));
  };
  return (
    <div className="hx-insp__field">
      <span className="hx-insp__label">
        {secret.name}
        <span className="hx-insp__state" data-set={secret.set || undefined}>
          {t(secretStateKey(secret))}
        </span>
      </span>
      <span className="hx-insp__hint">{secret.description}</span>
      <SecretField
        value={value}
        onChange={setValue}
        label={secret.name}
        placeholder={secret.set ? t('next.inspector.secretNew') : t('next.inspector.secretValue')}
        disabled={busy}
        invalid={failed}
        onSubmit={() => value !== '' && run(putAppSecret(machine, app.id, secret.name, value))}
      />
      <div className="hx-insp__row">
        <Button
          size="sm"
          variant="primary"
          busy={busy}
          disabled={value === ''}
          onClick={() => run(putAppSecret(machine, app.id, secret.name, value))}
        >
          {secret.set ? t('next.inspector.secretReplace') : t('next.inspector.secretPut')}
        </Button>
        {secret.set && (
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onClick={() => run(removeAppSecret(machine, app.id, secret.name))}
          >
            {t('next.inspector.secretRemove')}
          </Button>
        )}
      </div>
      {failed && <span className="hx-insp__problem">{t('next.inspector.saveFailed')}</span>}
    </div>
  );
};

/** The settings form and the secrets of an app; nothing for an app that has neither. */
const AppSetup = ({ machine, app }: { machine: MachineId; app: IInstalledApp }) => {
  const [state, setState] = useState<TLoad>({ k: 'loading' });
  const [again, setAgain] = useState(0);
  useEffect(() => {
    let live = true;
    loadAppSetup(machine, app.id).then(
      (setup) => live && setState({ k: 'ready', setup }),
      () => live && setState({ k: 'failed' }),
    );
    return () => {
      live = false;
    };
  }, [machine, app.id, again]);
  const onSetup = useCallback((setup: IAppSetup) => setState({ k: 'ready', setup }), []);
  if (!app.setup) return null;
  if (state.k === 'loading') return <p className="hx-insp__none">{t('next.inspector.loading')}</p>;
  if (state.k === 'failed')
    return (
      <div className="hx-insp__sec">
        <p className="hx-insp__problem">{t('next.inspector.setupFailed')}</p>
        <Button size="sm" variant="ghost" onClick={() => setAgain((n) => n + 1)}>
          {t('next.inspector.refresh')}
        </Button>
      </div>
    );
  const { setup } = state;
  return (
    <>
      {setup.waiting.length > 0 && (
        <p className="hx-insp__warn" role="status">
          {t('next.inspector.waiting', { names: setup.waiting.join(', ') })}
        </p>
      )}
      {setup.fields.length > 0 && <Settings machine={machine} app={app} setup={setup} onSetup={onSetup} />}
      {setup.secrets.length > 0 && (
        <div className="hx-insp__sec">
          <h3 className="hx-insp__h">{t('next.inspector.secrets')}</h3>
          <p className="hx-insp__hint">{t('next.inspector.secretsNote')}</p>
          <div className="hx-insp__form">
            {setup.secrets.map((s) => (
              <Secret key={s.name} machine={machine} app={app} secret={s} onSetup={onSetup} />
            ))}
          </div>
        </div>
      )}
    </>
  );
};

export default AppSetup;
