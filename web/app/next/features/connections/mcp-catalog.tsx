// Connecting an MCP server is one form: its address (typed, or put there by a catalog card), what the machine found
// there and how it signs in, and one explicit step — «Sign in with …» opens the service's page in a popup, a server
// that takes a token asks for it in the form, an open one just connects. A catalog card only fills the form: nothing
// is kept and no window opens until the person presses that button.
import { useEffect, useEffectEvent, useId, useRef, useState, type Ref } from 'react';
import { McpGlyph } from '@/next/features/connections/glyphs';
import {
  DEFAULT_HEADER,
  MCP_AUTH,
  MCP_TRANSPORT,
  type IMcpAddInput,
  type IMcpCatalogEntry,
  type IMcpEnvField,
  type IMcpHeader,
  type IMcpProbe,
} from '@/next/features/connections/mcp-api';
import {
  blurbOf,
  envDefaults,
  envPayload,
  envReady,
  headerValue,
  isKeyed,
  probeLine,
} from '@/next/features/connections/mcp-model';
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

interface IKeyedCardProps {
  entry: IMcpCatalogEntry;
  onConnect: TConnectMcp;
  /** Kept: the form lets the entry go. */
  onDone: () => void;
  onCancel: () => void;
}

/**
 * A local server or an API from the catalog: nothing to probe, only its keys. The press of «Connect» keeps it with
 * the keys; the first start of a local server can take a couple of minutes.
 */
const KeyedCard = ({ entry, onConnect, onDone, onCancel }: IKeyedCardProps) => {
  const env = useEnvValues(entry.env, true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const alive = useAlive();
  const local = entry.transport === MCP_TRANSPORT.stdio;
  const ready = envReady(entry.env, env.values);

  const connect = async () => {
    if (busy || !ready) return;
    setBusy(true);
    setError(null);
    const refused = await onConnect(
      {
        url: '',
        name: '',
        catalogId: entry.id,
        transport: null,
        header: null,
        env: envPayload(entry.env, env.values),
      },
      false,
    );
    if (!alive.current) return;
    setBusy(false);
    if (refused !== null) {
      setError(refused);
      return;
    }
    onDone();
  };

  return (
    <div className="hx-conn-found">
      <div className="hx-conn-found__head">
        <McpGlyph id={entry.id} name={entry.name} />
        <span className="hx-conn-found__text">
          <span className="hx-conn-found__name">{entry.name}</span>
          {blurbOf(entry) !== '' && <span className="hx-conn-found__sub">{blurbOf(entry)}</span>}
        </span>
      </div>
      <p className="hx-conn-found__note">
        {local ? t('next.connections.mcp.localNote') : t('next.connections.mcp.apiNote')}
      </p>
      {entry.guide !== '' && <p className="hx-conn-found__note">{entry.guide}</p>}
      {entry.env.length > 0 && (
        <EnvFields
          fields={entry.env}
          values={env.values}
          keep={false}
          disabled={busy}
          invalid={error !== null}
          onChange={(name, value) => {
            env.set(name, value);
            setError(null);
          }}
          onSubmit={() => void connect()}
        />
      )}
      {(entry.tokenUrl !== '' || entry.docsUrl !== '') && (
        <div className="hx-mcp-env__links">
          {entry.tokenUrl !== '' && (
            <a className="hx-conn-found__token" href={entry.tokenUrl} target="_blank" rel="noreferrer">
              {t('next.connections.createToken')}
              <Icon name="external-link" size="sm" />
            </a>
          )}
          {entry.docsUrl !== '' && (
            <a className="hx-conn-found__token" href={entry.docsUrl} target="_blank" rel="noreferrer">
              {t('next.connections.mcp.docs')}
              <Icon name="external-link" size="sm" />
            </a>
          )}
        </div>
      )}
      {busy && <StartingHint />}
      <div className="hx-conn-found__actions">
        <SetButton tone="primary" busy={busy} disabled={!ready} onClick={() => void connect()}>
          {t('next.connections.connect')}
        </SetButton>
        <SetButton tone="ghost" disabled={busy} onClick={onCancel}>
          {t('next.connections.cancel')}
        </SetButton>
      </div>
      <ErrorLine error={error} />
    </div>
  );
};

interface ICatalogCardProps {
  entry: IMcpCatalogEntry;
  leaving: boolean;
  /** This card's service is in the form now. */
  picked: boolean;
  onPick: (entry: IMcpCatalogEntry) => void;
}

/** A known service: its mark, its name, one line of what it gives. A press puts it in the form above. */
export const CatalogCard = ({ entry, leaving, picked, onPick }: ICatalogCardProps) => (
  <button
    type="button"
    className="hx-mcp-card"
    data-leaving={leaving || undefined}
    aria-pressed={picked}
    disabled={leaving}
    onClick={() => onPick(entry)}
  >
    <McpGlyph id={entry.id} name={entry.name} size="sm" />
    <span className="hx-mcp-card__text">
      <span className="hx-mcp-card__name">{entry.name}</span>
      {blurbOf(entry) !== '' && <span className="hx-mcp-card__blurb">{blurbOf(entry)}</span>}
    </span>
    <Icon name="chevron-right" size="sm" className="hx-mcp-card__go" />
  </button>
);

const BUSY = { probe: 'probe', connect: 'connect' } as const;
type TBusy = (typeof BUSY)[keyof typeof BUSY];

/** A catalog entry put into the form; `at` tells two presses of the same card apart. */
export interface IPicked {
  entry: IMcpCatalogEntry;
  at: number;
}

interface IConnectFormProps {
  picked: IPicked | null;
  onProbe: (url: string) => Promise<TProbeResult>;
  onConnect: TConnectMcp;
  /** The form let its catalog entry go (cancelled, connected, or another address typed). */
  onUnpick: () => void;
  ref?: Ref<HTMLDivElement>;
}

const nameOf = (probe: IMcpProbe, entry: IMcpCatalogEntry | null): string => {
  if (entry !== null) return entry.name;
  return probe.name === '' ? hostOf(probe.url) : probe.name;
};

/** The probed server signs in on its own page (and lets this app register there). */
const signsInByPage = (probe: IMcpProbe): boolean => probe.auth === MCP_AUTH.oauth && !probe.oauthUnavailable;
/**
 * The server takes a typed token: the probe says so, or the catalog does for a server that answers initialize without
 * one but refuses its tools (Yandex 360).
 */
const asksToken = (probe: IMcpProbe, entry: IMcpCatalogEntry | null): boolean =>
  probe.auth === MCP_AUTH.header || probe.oauthUnavailable || entry?.auth === MCP_AUTH.header;

/** «MCP server address»: type it or pick a service below, see what it is and how it signs in, then connect. */
export const ConnectForm = ({ picked, onProbe, onConnect, onUnpick, ref }: IConnectFormProps) => {
  const [url, setUrl] = useState('');
  const [entry, setEntry] = useState<IMcpCatalogEntry | null>(null);
  const [probe, setProbe] = useState<IMcpProbe | null>(null);
  const [name, setName] = useState('');
  const [header, setHeader] = useState(DEFAULT_HEADER);
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState<TBusy | null>(null);
  const [error, setError] = useState<string | null>(null);
  // A local server or an API from the catalog: its keys' form takes the place of the probe's card.
  const [keyed, setKeyed] = useState<IPicked | null>(null);
  const alive = useAlive();
  const ids = useId();
  const card = usePresence(probe, CARD_EXIT_MS);
  const shown = card.value;
  const closing = card.state === PRESENCE.closing;
  const token = shown !== null && asksToken(shown, entry);

  const find = async (link: string, from: IMcpCatalogEntry | null) => {
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
    const fromCatalog = from === null ? '' : from.headerName;
    setHeader(got.probe.headerName === '' ? fromCatalog || DEFAULT_HEADER : got.probe.headerName);
    setValue('');
  };

  // A catalog card fills the address and looks at it at once; nothing is kept yet.
  const lastPick = useRef<number | null>(null);
  const takePick = useEffectEvent((next: IPicked) => {
    setEntry(next.entry);
    if (isKeyed(next.entry)) {
      // Nothing to probe: only keys to ask for.
      setUrl('');
      setProbe(null);
      setError(null);
      setKeyed(next);
      return;
    }
    setKeyed(null);
    setUrl(next.entry.url);
    void find(next.entry.url, next.entry);
  });
  useEffect(() => {
    if (picked === null || lastPick.current === picked.at) return;
    lastPick.current = picked.at;
    takePick(picked);
  }, [picked]);

  const ready = probe !== null && (!asksToken(probe, entry) || value.trim() !== '');

  const reset = () => {
    setProbe(null);
    setUrl('');
    setEntry(null);
    setKeyed(null);
    setName('');
    setValue('');
    setError(null);
    onUnpick();
  };

  const connect = async () => {
    if (probe === null || busy !== null || !ready) return;
    // Called in the click: the sign-in popup opens before the await.
    const pending = onConnect(
      {
        url: probe.url,
        name: entry === null ? name.trim() : '',
        catalogId: entry === null ? '' : entry.id,
        transport: probe.transport,
        header: asksToken(probe, entry)
          ? headerOf(header, value, entry === null ? '' : entry.tokenPrefix)
          : null,
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

  const title = shown === null ? '' : nameOf(shown, entry);
  const tokenUrl = entry === null ? '' : entry.tokenUrl;

  return (
    <div ref={ref} className="hx-conn-form">
      <div className="hx-conn-form__paste">
        <label className="hx-conn-form__label" htmlFor={`${ids}-url`}>
          {t('next.connections.mcp.address')}
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
            placeholder={t('next.connections.mcp.address.placeholder')}
            aria-invalid={(error !== null && probe === null) || undefined}
            onChange={(event) => {
              const next = event.target.value;
              setUrl(next);
              setError(null);
              if (probe !== null) setProbe(null);
              if (entry !== null && next.trim() !== entry.url) {
                setEntry(null);
                setKeyed(null);
                onUnpick();
              }
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter') void find(url, entry);
            }}
          />
          {probe === null && (
            <SetButton
              tone="primary"
              busy={busy === BUSY.probe}
              disabled={url.trim() === ''}
              onClick={() => void find(url, entry)}
            >
              {t('next.connections.find')}
            </SetButton>
          )}
        </div>
      </div>
      {shown !== null && (
        <div className="hx-conn-found" data-state={card.state} inert={closing || undefined}>
          <div className="hx-conn-found__head">
            <McpGlyph id={entry === null ? '' : entry.id} name={title} />
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
              tokenUrl={tokenUrl}
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
          {entry === null && (
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
          )}
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
      {keyed !== null && (
        <KeyedCard key={keyed.at} entry={keyed.entry} onConnect={onConnect} onDone={reset} onCancel={reset} />
      )}
      <ErrorLine error={error} />
    </div>
  );
};
