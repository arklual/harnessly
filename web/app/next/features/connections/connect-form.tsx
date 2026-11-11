// «Paste a link»: one field for any link of the person's tracker, wiki or code host (a task, a page, the home page).
// The machine finds what it is; a card then says so (the product, cloud or self-hosted, the version, the address) and
// asks right there for what its sign-in method takes. A typed secret leaves the page's state the moment it is sent.
import { useEffect, useId, useRef, useState } from 'react';
import type { IConnectInput, IDetection, IField, IMethod } from '@/next/features/connections/api';
import ProductGlyph from '@/next/features/connections/glyphs';
import {
  deploymentText,
  fieldLabel,
  firstMethod,
  hostOf,
  kindName,
  methodLabel,
} from '@/next/features/connections/model';
import { FormError, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import SecretField from '@/next/ui/secret-field';
import Segmented from '@/next/ui/segmented';

/** The card's exit, as long as hx-conn-out in connections.css. */
const CARD_EXIT_MS = 200;

const BUSY = { detect: 'detect', connect: 'connect' } as const;
type TBusy = (typeof BUSY)[keyof typeof BUSY];

export type TDetectResult = { detection: IDetection } | { error: string };

export interface IConnectFormProps {
  onDetect: (url: string) => Promise<TDetectResult>;
  /** Resolves null when the connection is kept, else the words of why not. */
  onConnect: (input: IConnectInput) => Promise<string | null>;
  /** An address to find at once (from «Подключить свой GitLab» of the review): the card asks for the token straight. */
  start?: string;
  /** An address only shown in the field: it came from outside the page (a link) and is found by «Найти». */
  prefill?: string;
}

/** The values a method takes, from what was typed: its own fields only, trimmed. */
const credentialsOf = (method: IMethod, values: Readonly<Record<string, string>>): Record<string, string> => {
  const out: Record<string, string> = {};
  for (const field of method.fields) {
    const value = (values[field.id] ?? '').trim();
    if (value !== '') out[field.id] = value;
  }
  return out;
};

const filled = (method: IMethod, values: Readonly<Record<string, string>>): boolean =>
  method.fields.every((field) => field.optional || (values[field.id] ?? '').trim() !== '');

/** «Self-hosted · version 9.12.4», or the deployment alone when the product does not say its version. */
const foundLine = (detection: IDetection): string =>
  detection.version === ''
    ? deploymentText(detection.deployment)
    : t('next.connections.found.versioned', {
        deployment: deploymentText(detection.deployment),
        version: detection.version,
      });

interface IFieldInputProps {
  field: IField;
  value: string;
  disabled: boolean;
  invalid: boolean;
  focus: boolean;
  onChange: (value: string) => void;
  onSubmit: () => void;
}

const FieldInput = ({ field, value, disabled, invalid, focus, onChange, onSubmit }: IFieldInputProps) => {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (focus) input.current?.focus();
  }, [focus]);
  const label = fieldLabel(field.id);
  if (field.secret)
    return (
      <div className="hx-conn-form__field">
        <span className="hx-conn-form__label">{label}</span>
        <SecretField
          ref={input}
          value={value}
          onChange={onChange}
          label={label}
          placeholder={field.placeholder}
          disabled={disabled}
          invalid={invalid}
          onSubmit={onSubmit}
        />
      </div>
    );
  return (
    <div className="hx-conn-form__field">
      <label className="hx-conn-form__label" htmlFor={id}>
        {field.optional ? t('next.connections.optional', { label }) : label}
      </label>
      <input
        id={id}
        ref={input}
        className="hx-set__input"
        type={field.id === 'email' ? 'email' : 'text'}
        value={value}
        disabled={disabled}
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        placeholder={field.placeholder}
        aria-invalid={invalid || undefined}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter') onSubmit();
        }}
      />
    </div>
  );
};

const ConnectForm = ({ onDetect, onConnect, start, prefill }: IConnectFormProps) => {
  const [url, setUrl] = useState(start ?? prefill ?? '');
  const [detection, setDetection] = useState<IDetection | null>(null);
  const [methodId, setMethodId] = useState('');
  const [values, setValues] = useState<Record<string, string>>({});
  const [name, setName] = useState('');
  const [busy, setBusy] = useState<TBusy | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [kept, setKept] = useState('');
  const alive = useRef(true);
  const ids = useId();
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  if (error !== null && kept !== error) setKept(error);

  const card = usePresence(detection, CARD_EXIT_MS);
  const shown = card.value;
  const closing = card.state === PRESENCE.closing;
  const method = shown === null ? null : (shown.methods.find((m) => m.id === methodId) ?? firstMethod(shown));
  const hasSecret = method !== null && method.fields.some((field) => field.secret);

  const detect = async (asked?: string) => {
    const link = (asked ?? url).trim();
    if (link === '' || busy !== null) return;
    setBusy(BUSY.detect);
    setError(null);
    const got = await onDetect(link);
    if (!alive.current) return;
    setBusy(null);
    if ('error' in got) {
      setError(got.error);
      return;
    }
    setDetection(got.detection);
    setMethodId(firstMethod(got.detection)?.id ?? '');
    setValues({});
    setName('');
  };

  const connect = async () => {
    if (detection === null || method === null || busy !== null || !filled(method, values)) return;
    const credentials = credentialsOf(method, values);
    // The secrets leave the page's state as they are sent; the rest stays for another try.
    setValues((now) => {
      const rest: Record<string, string> = {};
      for (const field of method.fields) if (!field.secret && now[field.id]) rest[field.id] = now[field.id];
      return rest;
    });
    setBusy(BUSY.connect);
    setError(null);
    const refused = await onConnect({ detection, method: method.id, credentials, name: name.trim() });
    if (!alive.current) return;
    setBusy(null);
    if (refused !== null) {
      setError(refused);
      return;
    }
    setDetection(null);
    setUrl('');
    setValues({});
    setName('');
  };

  const cancel = () => {
    setDetection(null);
    setValues({});
    setName('');
    setError(null);
  };

  // The address it was opened with is found at once; a new one (another «Подключить свой») is found again.
  const [started, setStarted] = useState<string | null>(null);
  if (start && start !== started) {
    setStarted(start);
    setUrl(start);
  }
  useEffect(() => {
    if (!started) return;
    // Found after the paint, as a press of «Найти» would: the form shows the address first.
    const timer = setTimeout(() => void detect(started), 0);
    return () => clearTimeout(timer);
    // Only a new address to start from finds again; the person's own typing is found by «Найти».
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [started]);

  return (
    <div className="hx-conn-form">
      <div className="hx-conn-form__paste">
        <label className="hx-conn-form__label" htmlFor={`${ids}-url`}>
          {t('next.connections.paste')}
        </label>
        <div className="hx-conn-form__line">
          <input
            id={`${ids}-url`}
            className="hx-set__input"
            type="text"
            inputMode="url"
            value={url}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            placeholder={t('next.connections.paste.placeholder')}
            aria-invalid={(error !== null && detection === null) || undefined}
            onChange={(event) => {
              setUrl(event.target.value);
              setError(null);
              if (detection !== null) setDetection(null);
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter') void detect();
            }}
          />
          <SetButton
            tone={detection === null ? 'primary' : 'ghost'}
            busy={busy === BUSY.detect}
            disabled={url.trim() === '' || busy === BUSY.connect}
            onClick={() => void detect()}
          >
            {t('next.connections.find')}
          </SetButton>
        </div>
      </div>
      {shown !== null && method !== null && (
        <div className="hx-conn-found" data-state={card.state} inert={closing || undefined}>
          <div className="hx-conn-found__head">
            <ProductGlyph kind={shown.kind} />
            <span className="hx-conn-found__text">
              <span className="hx-conn-found__name">{kindName(shown.kind)}</span>
              <span className="hx-conn-found__sub">{foundLine(shown)}</span>
              <span className="hx-conn-found__addr">{hostOf(shown.baseUrl)}</span>
            </span>
          </div>
          {shown.methods.length > 1 && (
            <Segmented
              label={t('next.connections.method')}
              value={method.id}
              options={shown.methods.map((m) => ({ value: m.id, label: methodLabel(m.id) }))}
              disabled={busy !== null}
              onChange={(next) => {
                setMethodId(next);
                setValues({});
                setError(null);
              }}
            />
          )}
          {method.fields.length === 0 && (
            <p className="hx-conn-found__note">{t('next.connections.noSignIn')}</p>
          )}
          {method.fields.map((field, index) => (
            <FieldInput
              key={`${method.id}:${field.id}`}
              field={field}
              value={values[field.id] ?? ''}
              disabled={busy !== null}
              invalid={error !== null}
              focus={index === 0}
              onChange={(value) => {
                setValues((now) => ({ ...now, [field.id]: value }));
                setError(null);
              }}
              onSubmit={() => void connect()}
            />
          ))}
          {hasSecret && method.tokenUrl !== '' && (
            <a className="hx-conn-found__token" href={method.tokenUrl} target="_blank" rel="noreferrer">
              {t('next.connections.createToken')}
              <Icon name="external-link" size="sm" />
            </a>
          )}
          <div className="hx-conn-form__field">
            <label className="hx-conn-form__label" htmlFor={`${ids}-name`}>
              {t('next.connections.name')}
            </label>
            <input
              id={`${ids}-name`}
              className="hx-set__input"
              value={name}
              maxLength={80}
              autoComplete="off"
              disabled={busy !== null}
              placeholder={shown.title === '' ? hostOf(shown.baseUrl) : shown.title}
              onChange={(event) => setName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') void connect();
              }}
            />
          </div>
          <div className="hx-conn-found__actions">
            <SetButton
              tone="primary"
              busy={busy === BUSY.connect}
              disabled={!filled(method, values) || busy === BUSY.detect}
              onClick={() => void connect()}
            >
              {t('next.connections.connect')}
            </SetButton>
            <SetButton tone="ghost" disabled={busy === BUSY.connect} onClick={cancel}>
              {t('next.connections.cancel')}
            </SetButton>
          </div>
        </div>
      )}
      <div className="hx-reveal" data-open={error !== null}>
        <div className="hx-reveal__in" inert={error === null || undefined}>
          <FormError>{kept}</FormError>
        </div>
      </div>
    </div>
  );
};

export default ConnectForm;
