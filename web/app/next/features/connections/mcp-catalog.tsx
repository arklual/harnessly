// The ways to connect an MCP server: a catalog card (the product's mark, its name, one line of what it gives,
// «Connect») and «Add your own server» (paste an address, the machine probes it and says how it signs in). A server
// that takes a token asks for it right there, in the card; one that signs in on its own page opens it in a popup.
import { useEffect, useId, useRef, useState } from 'react';
import { McpGlyph } from '@/next/features/connections/glyphs';
import {
  DEFAULT_HEADER,
  MCP_AUTH,
  type IMcpAddInput,
  type IMcpCatalogEntry,
  type IMcpHeader,
  type IMcpProbe,
} from '@/next/features/connections/mcp-api';
import { blurbOf, headerValue, probeLine } from '@/next/features/connections/mcp-model';
import { hostOf } from '@/next/features/connections/model';
import { FormError, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence, useSeen } from '@/next/ui/presence';
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

/** The header as it is sent: a bare token in Authorization gets its «Bearer». */
export const headerOf = (name: string, value: string): IMcpHeader => {
  const header = name.trim() === '' ? DEFAULT_HEADER : name.trim();
  return { name: header, value: headerValue(header, value) };
};

interface ICatalogCardProps {
  entry: IMcpCatalogEntry;
  leaving: boolean;
  onConnect: TConnectMcp;
}

export const CatalogCard = ({ entry, leaving, onConnect }: ICatalogCardProps) => {
  const [open, setOpen] = useState(false);
  const [header, setHeader] = useState(entry.headerName === '' ? DEFAULT_HEADER : entry.headerName);
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const alive = useAlive();
  const takesToken = entry.auth === MCP_AUTH.header;
  // The form stays mounted after it opened once, so it folds away instead of vanishing.
  const seen = useSeen(open);

  const connect = async (withHeader: boolean) => {
    if (busy) return;
    if (withHeader && value.trim() === '') return;
    // Called in the click: a popup for the sign-in can open before the await.
    const pending = onConnect(
      {
        url: entry.url,
        name: '',
        catalogId: entry.id,
        transport: entry.transport,
        header: withHeader ? headerOf(header, value) : null,
      },
      entry.auth === MCP_AUTH.oauth,
    );
    setValue('');
    setBusy(true);
    setError(null);
    const refused = await pending;
    if (!alive.current) return;
    setBusy(false);
    setError(refused);
    if (refused === null) setOpen(false);
  };

  return (
    <div className="hx-mcp-card" data-open={open || undefined} data-leaving={leaving || undefined}>
      <div className="hx-mcp-card__head">
        <McpGlyph id={entry.id} name={entry.name} size="sm" />
        <span className="hx-mcp-card__text">
          <span className="hx-mcp-card__name">{entry.name}</span>
          {blurbOf(entry) !== '' && <span className="hx-mcp-card__blurb">{blurbOf(entry)}</span>}
        </span>
        {!open && (
          <SetButton
            tone="ghost"
            busy={busy}
            disabled={leaving}
            onClick={() => {
              if (takesToken) {
                setError(null);
                setOpen(true);
                return;
              }
              void connect(false);
            }}
          >
            {t('next.connections.connect')}
          </SetButton>
        )}
      </div>
      <div className="hx-reveal" data-open={open && !leaving}>
        <div className="hx-reveal__in" inert={!open || undefined}>
          {seen && (
            <div className="hx-mcp-card__form">
              <HeaderFields
                name={header}
                value={value}
                tokenUrl={entry.tokenUrl}
                disabled={busy}
                invalid={error !== null}
                onName={setHeader}
                onValue={(next) => {
                  setValue(next);
                  setError(null);
                }}
                onSubmit={() => void connect(true)}
              />
              <div className="hx-conn-found__actions">
                <SetButton
                  tone="primary"
                  busy={busy}
                  disabled={value.trim() === ''}
                  onClick={() => void connect(true)}
                >
                  {t('next.connections.connect')}
                </SetButton>
                <SetButton
                  tone="ghost"
                  disabled={busy}
                  onClick={() => {
                    setOpen(false);
                    setValue('');
                    setError(null);
                  }}
                >
                  {t('next.connections.cancel')}
                </SetButton>
              </div>
            </div>
          )}
        </div>
      </div>
      <div className="hx-mcp-card__err">
        <ErrorLine error={error} />
      </div>
    </div>
  );
};

const BUSY = { probe: 'probe', connect: 'connect' } as const;
type TBusy = (typeof BUSY)[keyof typeof BUSY];

interface IOwnServerProps {
  onProbe: (url: string) => Promise<TProbeResult>;
  onConnect: TConnectMcp;
}

/** «Add your own server»: paste its address, see what it is, connect. */
export const OwnServerForm = ({ onProbe, onConnect }: IOwnServerProps) => {
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
  const takesToken = shown !== null && shown.auth === MCP_AUTH.header;

  const find = async () => {
    const link = url.trim();
    if (link === '' || busy !== null) return;
    setBusy(BUSY.probe);
    setError(null);
    const got = await onProbe(link);
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

  const ready = probe !== null && (!takesToken || value.trim() !== '');

  const connect = async () => {
    if (probe === null || busy !== null || !ready) return;
    const pending = onConnect(
      {
        url: probe.url,
        name: name.trim(),
        catalogId: '',
        transport: probe.transport,
        header: takesToken ? headerOf(header, value) : null,
      },
      probe.auth === MCP_AUTH.oauth,
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
    setProbe(null);
    setUrl('');
    setName('');
  };

  const cancel = () => {
    setProbe(null);
    setValue('');
    setName('');
    setError(null);
  };

  return (
    <div className="hx-conn-form">
      <div className="hx-conn-form__paste">
        <label className="hx-conn-form__label" htmlFor={`${ids}-url`}>
          {t('next.connections.mcp.own')}
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
            placeholder={t('next.connections.mcp.own.placeholder')}
            aria-invalid={(error !== null && probe === null) || undefined}
            onChange={(event) => {
              setUrl(event.target.value);
              setError(null);
              if (probe !== null) setProbe(null);
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter') void find();
            }}
          />
          <SetButton
            tone={probe === null ? 'primary' : 'ghost'}
            busy={busy === BUSY.probe}
            disabled={url.trim() === '' || busy === BUSY.connect}
            onClick={() => void find()}
          >
            {t('next.connections.find')}
          </SetButton>
        </div>
      </div>
      {shown !== null && (
        <div className="hx-conn-found" data-state={card.state} inert={closing || undefined}>
          <div className="hx-conn-found__head">
            <McpGlyph id="" name={shown.name === '' ? hostOf(shown.url) : shown.name} />
            <span className="hx-conn-found__text">
              <span className="hx-conn-found__name">
                {shown.name === '' ? hostOf(shown.url) : shown.name}
              </span>
              <span className="hx-conn-found__sub">{probeLine(shown)}</span>
              <span className="hx-conn-found__addr">{hostOf(shown.url)}</span>
            </span>
          </div>
          {shown.oauthUnavailable && (
            <p className="hx-conn-found__note">{t('next.connections.mcp.oauthUnavailable')}</p>
          )}
          {takesToken && (
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
              placeholder={shown.name === '' ? hostOf(shown.url) : shown.name}
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
              disabled={!ready || busy === BUSY.probe}
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
      <ErrorLine error={error} />
    </div>
  );
};
