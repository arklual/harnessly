// The pieces of connecting an MCP server, shared by the service page and the form for an address of one's own. That form:
// its address, what the machine found there and how it signs in, and one explicit step — «Sign in with …» opens the
// service's page in a popup, a server that takes a token asks for it in the form, an open one just connects. Nothing is
// kept and no window opens until the person presses that button. A catalog service is connected on its own page
// (service-page.tsx).
import { useEffect, useId, useRef, useState } from 'react';
import { McpGlyph } from '@/next/features/connections/glyphs';
import {
  DEFAULT_HEADER,
  MCP_AUTH,
  type IMcpAddInput,
  type IMcpEnvField,
  type IMcpHeader,
  type IMcpProbe,
} from '@/next/features/connections/mcp-api';
import { envDefaults, headerValue, probeLine } from '@/next/features/connections/mcp-model';
import { hostOf } from '@/next/features/connections/model';
import { FormError, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import SecretField from '@/next/ui/secret-field';

/** The found card's exit, as long as hx-conn-out in connections.css. */
const CARD_EXIT_MS = 200;

/** Keeps the server and starts its sign-in when it needs one; null when kept, else the words of why not. */
export type TConnectMcp = (input: IMcpAddInput, expectOAuth: boolean) => Promise<string | null>;
export type TProbeResult = { probe: IMcpProbe } | { error: string };

/** The last error stays on screen while its line folds away. */
const useKeptError = (error: string | null): string => {
  const [kept, setKept] = useState('');
  if (error !== null && kept !== error) setKept(error);
  return kept;
};

const useAlive = () => {
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  return alive;
};

const ErrorLine = ({ error }: { error: string | null }) => {
  const kept = useKeptError(error);
  return (
    <div className="hx-reveal" data-open={error !== null}>
      <div className="hx-reveal__in" inert={error === null || undefined}>
        <FormError>{kept}</FormError>
      </div>
    </div>
  );
};

interface IHeaderFieldsProps {
  name: string;
  value: string;
  tokenUrl: string;
  disabled: boolean;
  invalid: boolean;
  onName: (name: string) => void;
  onValue: (value: string) => void;
  onSubmit: () => void;
}

/** The header a token goes in (Authorization unless the server says otherwise) and the token itself. */
export const HeaderFields = ({
  name,
  value,
  tokenUrl,
  disabled,
  invalid,
  onName,
  onValue,
  onSubmit,
}: IHeaderFieldsProps) => {
  const ids = useId();
  const secret = useRef<HTMLInputElement>(null);
  useEffect(() => {
    secret.current?.focus();
  }, []);
  const label = t('next.connections.mcp.header.value');
  return (
    <>
      <div className="hx-mcp-head">
        <div className="hx-conn-form__field">
          <label className="hx-conn-form__label" htmlFor={`${ids}-header`}>
            {t('next.connections.mcp.header.name')}
          </label>
          <input
            id={`${ids}-header`}
            className="hx-set__input"
            value={name}
            disabled={disabled}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            onChange={(event) => onName(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') onSubmit();
            }}
          />
        </div>
        <div className="hx-conn-form__field">
          <span className="hx-conn-form__label">{label}</span>
          <SecretField
            ref={secret}
            value={value}
            onChange={onValue}
            label={label}
            disabled={disabled}
            invalid={invalid}
            onSubmit={onSubmit}
          />
        </div>
      </div>
      {tokenUrl !== '' && (
        <a className="hx-conn-found__token" href={tokenUrl} target="_blank" rel="noreferrer">
          {t('next.connections.createToken')}
          <Icon name="external-link" size="sm" />
        </a>
      )}
    </>
  );
};

/** The header as it is sent: a bare token in Authorization gets its «Bearer» (or the catalog entry's own prefix). */
export const headerOf = (name: string, value: string, prefix: string): IMcpHeader => {
  const header = name.trim() === '' ? DEFAULT_HEADER : name.trim();
  return { name: header, value: headerValue(header, value, prefix) };
};

interface IEnvFieldsProps {
  fields: readonly IMcpEnvField[];
  values: Readonly<Record<string, string>>;
  /** The keys are kept already: an empty field keeps the stored one. */
  keep: boolean;
  disabled: boolean;
  invalid: boolean;
  onChange: (name: string, value: string) => void;
  onSubmit: () => void;
}

/** One input per key of a local server or an API: masked for a secret, plain otherwise, with its hint under it. */
export const EnvFields = ({
  fields,
  values,
  keep,
  disabled,
  invalid,
  onChange,
  onSubmit,
}: IEnvFieldsProps) => {
  const ids = useId();
  return (
    <div className="hx-mcp-env">
      {fields.map((field, index) => {
        const label = field.optional ? t('next.connections.optional', { label: field.label }) : field.label;
        const id = `${ids}-${index}`;
        const value = values[field.name] ?? '';
        const placeholder = keep && field.secret ? t('next.connections.mcp.keepKey') : field.default;
        return (
          <div key={field.name} className="hx-conn-form__field">
            {field.secret ? (
              <>
                <span className="hx-conn-form__label">{label}</span>
                <SecretField
                  value={value}
                  onChange={(next) => onChange(field.name, next)}
                  label={label}
                  placeholder={placeholder}
                  disabled={disabled}
                  invalid={invalid}
                  onSubmit={onSubmit}
                />
              </>
            ) : (
              <>
                <label className="hx-conn-form__label" htmlFor={id}>
                  {label}
                </label>
                <input
                  id={id}
                  className="hx-set__input"
                  value={value}
                  disabled={disabled}
                  autoComplete="off"
                  autoCapitalize="off"
                  spellCheck={false}
                  placeholder={placeholder}
                  aria-invalid={invalid || undefined}
                  onChange={(event) => onChange(field.name, event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') onSubmit();
                  }}
                />
              </>
            )}
            {field.hint !== '' && <span className="hx-mcp-env__hint">{field.hint}</span>}
          </div>
        );
      })}
    </div>
  );
};

/** The keys' form's state: what was typed per variable name. */
export const useEnvValues = (fields: readonly IMcpEnvField[], withDefaults: boolean) => {
  const [values, setValues] = useState<Record<string, string>>(() =>
    withDefaults ? envDefaults(fields) : {},
  );
  const set = (name: string, value: string) => setValues((now) => ({ ...now, [name]: value }));
  const clear = () => setValues({});
  return { values, set, clear };
};

/** What a local server or an API still needs from the person: said once, under the keys. */
export const StartingHint = () => (
  <p className="hx-conn-found__note" role="status">
    {t('next.connections.mcp.starting')}
  </p>
);

const BUSY = { probe: 'probe', connect: 'connect' } as const;
type TBusy = (typeof BUSY)[keyof typeof BUSY];

/** The probed server signs in on its own page (and lets this app register there). */
const signsInByPage = (probe: IMcpProbe): boolean => probe.auth === MCP_AUTH.oauth && !probe.oauthUnavailable;
/** The server takes a typed token: the probe says so. */
const asksToken = (probe: IMcpProbe): boolean => probe.auth === MCP_AUTH.header || probe.oauthUnavailable;

const nameOf = (probe: IMcpProbe): string => (probe.name === '' ? hostOf(probe.url) : probe.name);

interface IConnectFormProps {
  onProbe: (url: string) => Promise<TProbeResult>;
  onConnect: TConnectMcp;
}

/** «A server of your own»: the address in one line with its button inside, then what was found there and the step. */
export const ConnectForm = ({ onProbe, onConnect }: IConnectFormProps) => {
  const [url, setUrl] = useState('');
  const [probe, setProbe] = useState<IMcpProbe | null>(null);
  const [name, setName] = useState('');
  const [header, setHeader] = useState(DEFAULT_HEADER);
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState<TBusy | null>(null);
  const [error, setError] = useState<string | null>(null);
  const alive = useAlive();
  const ids = useId();
  const card = usePresence(probe, CARD_EXIT_MS);
  const shown = card.value;
  const closing = card.state === PRESENCE.closing;
  const token = shown !== null && asksToken(shown);

  const find = async (link: string) => {
    const address = link.trim();
    if (address === '' || busy === BUSY.connect) return;
    setBusy(BUSY.probe);
    setError(null);
    setProbe(null);
    const got = await onProbe(address);
    if (!alive.current) return;
    setBusy(null);
    if ('error' in got) {
      setError(got.error);
      return;
    }
    setProbe(got.probe);
    setName('');
    setHeader(got.probe.headerName === '' ? DEFAULT_HEADER : got.probe.headerName);
    setValue('');
  };

  const ready = probe !== null && (!asksToken(probe) || value.trim() !== '');

  const reset = () => {
    setProbe(null);
    setUrl('');
    setName('');
    setValue('');
    setError(null);
  };

  const connect = async () => {
    if (probe === null || busy !== null || !ready) return;
    // Called in the click: the sign-in popup opens before the await.
    const pending = onConnect(
      {
        url: probe.url,
        name: name.trim(),
        catalogId: '',
        transport: probe.transport,
        header: asksToken(probe) ? headerOf(header, value, '') : null,
      },
      signsInByPage(probe),
    );
    setValue('');
    setBusy(BUSY.connect);
    setError(null);
    const refused = await pending;
    if (!alive.current) return;
    setBusy(null);
    if (refused !== null) {
      setError(refused);
      return;
    }
    reset();
  };

  const title = shown === null ? '' : nameOf(shown);

  return (
    <div className="hx-conn-form">
      <label className="hx-conn-form__label" htmlFor={`${ids}-url`}>
        {t('next.connections.mcp.address')}
      </label>
      <div className="hx-cn-paste" data-invalid={(error !== null && probe === null) || undefined}>
        <input
          id={`${ids}-url`}
          className="hx-cn-paste__input"
          type="text"
          inputMode="url"
          value={url}
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          placeholder={t('next.connections.mcp.address.placeholder')}
          aria-invalid={(error !== null && probe === null) || undefined}
          onChange={(event) => {
            setUrl(event.target.value);
            setError(null);
            if (probe !== null) setProbe(null);
          }}
          onKeyDown={(event) => {
            if (event.key === 'Enter') void find(url);
          }}
        />
        {probe === null && (
          <button
            type="button"
            className="hx-cn-paste__go"
            disabled={url.trim() === '' || busy !== null}
            data-busy={busy === BUSY.probe || undefined}
            onClick={() => void find(url)}
          >
            {t('next.connections.find')}
          </button>
        )}
      </div>
      {shown !== null && (
        <div className="hx-conn-found" data-state={card.state} inert={closing || undefined}>
          <div className="hx-conn-found__head">
            <McpGlyph id="" name={title} />
            <span className="hx-conn-found__text">
              <span className="hx-conn-found__name">{title}</span>
              <span className="hx-conn-found__sub">
                {probeLine(token ? { ...shown, auth: MCP_AUTH.header } : shown)}
              </span>
            </span>
          </div>
          {shown.oauthUnavailable && (
            <p className="hx-conn-found__note">{t('next.connections.mcp.oauthUnavailable')}</p>
          )}
          {token && (
            <HeaderFields
              name={header}
              value={value}
              tokenUrl=""
              disabled={busy !== null}
              invalid={error !== null}
              onName={setHeader}
              onValue={(next) => {
                setValue(next);
                setError(null);
              }}
              onSubmit={() => void connect()}
            />
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
              placeholder={title}
              onChange={(event) => setName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') void connect();
              }}
            />
          </div>
          <div className="hx-conn-found__actions">
            <SetButton
              tone="primary"
              icon={signsInByPage(shown) ? 'log-in' : undefined}
              busy={busy === BUSY.connect}
              disabled={!ready}
              onClick={() => void connect()}
            >
              {signsInByPage(shown)
                ? t('next.connections.mcp.signInWith', { name: title })
                : t('next.connections.connect')}
            </SetButton>
            <SetButton tone="ghost" disabled={busy === BUSY.connect} onClick={reset}>
              {t('next.connections.cancel')}
            </SetButton>
          </div>
        </div>
      )}
      <ErrorLine error={error} />
    </div>
  );
};
